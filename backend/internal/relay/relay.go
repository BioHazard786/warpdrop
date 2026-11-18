// Package relay pairs two WebSocket clients by single-use ticket and copies frames between them.
// Frames are end-to-end encrypted by the clients; the server never inspects them.
package relay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
)

const (
	TicketTTL   = 30 * time.Second
	IdleTimeout = 60 * time.Second
	maxMessage  = 1 << 20
)

type entry struct {
	peer chan *websocket.Conn // second arrival hands its conn to the first
	used int                  // 0..2 sockets claimed
}

type Relay struct {
	Upgrader  websocket.Upgrader
	MaxMbps   int
	MaxConns  int
	IdleAfter time.Duration // zero means IdleTimeout
	mu        sync.Mutex
	tickets   map[string]*entry
	active    atomic.Int64
}

func New(maxMbps, maxConns int, up websocket.Upgrader) *Relay {
	return &Relay{Upgrader: up, MaxMbps: maxMbps, MaxConns: maxConns, tickets: map[string]*entry{}}
}

// Issue creates a ticket that two sockets may redeem within TicketTTL.
func (r *Relay) Issue() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	t := hex.EncodeToString(b[:])
	r.mu.Lock()
	r.tickets[t] = &entry{peer: make(chan *websocket.Conn, 1)}
	r.mu.Unlock()
	time.AfterFunc(TicketTTL, func() { r.drop(t) })
	return t
}

func (r *Relay) drop(t string) {
	r.mu.Lock()
	delete(r.tickets, t)
	r.mu.Unlock()
}

// claim returns the entry and whether this caller is the first of the pair.
func (r *Relay) claim(t string) (e *entry, first, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e = r.tickets[t]
	if e == nil || e.used >= 2 {
		return nil, false, false
	}
	e.used++
	if e.used == 2 {
		delete(r.tickets, t) // single use
	}
	return e, e.used == 1, true
}

func (r *Relay) idle() time.Duration {
	if r.IdleAfter > 0 {
		return r.IdleAfter
	}
	return IdleTimeout
}

// Handler serves /relay/<ticket>.
func (r *Relay) Handler(w http.ResponseWriter, req *http.Request) {
	t := req.PathValue("ticket")
	if r.active.Load() >= int64(r.MaxConns) {
		slog.Warn("relay busy", "max", r.MaxConns)
		http.Error(w, "relay busy", http.StatusServiceUnavailable)
		return
	}
	e, first, ok := r.claim(t)
	if !ok {
		slog.Debug("relay: unknown ticket", "remote", req.RemoteAddr)
		http.Error(w, "unknown ticket", http.StatusNotFound)
		return
	}
	conn, err := r.Upgrader.Upgrade(w, req, nil)
	if err != nil {
		slog.Debug("relay upgrade failed", "err", err)
		return
	}
	conn.SetReadLimit(maxMessage)
	if !first {
		e.peer <- conn
		return // the first handler owns the pipe
	}
	r.active.Add(1)
	defer r.active.Add(-1)
	select {
	case other := <-e.peer:
		start := time.Now()
		slog.Info("relay paired", "active", r.active.Load())
		r.pipe(conn, other)
		slog.Info("relay ended", "duration", time.Since(start).Round(time.Millisecond))
	case <-time.After(TicketTTL):
		slog.Debug("relay: peer never arrived")
		r.drop(t)
		_ = conn.Close()
	case <-req.Context().Done():
		_ = conn.Close()
	}
}

func (r *Relay) pipe(a, b *websocket.Conn) {
	var once sync.Once
	closeAll := func() { _ = a.Close(); _ = b.Close() }
	var wg sync.WaitGroup
	wg.Add(2)
	for _, p := range [][2]*websocket.Conn{{a, b}, {b, a}} {
		go func() {
			defer wg.Done()
			r.copy(p[1], p[0])
			once.Do(closeAll)
		}()
	}
	wg.Wait()
}

// copy forwards messages src→dst. Blocking writes pass TCP backpressure to the sender.
func (r *Relay) copy(dst, src *websocket.Conn) {
	var lim *rate.Limiter
	if r.MaxMbps > 0 {
		bps := r.MaxMbps * 1_000_000 / 8
		lim = rate.NewLimiter(rate.Limit(bps), max(bps, 64<<10))
	}
	for {
		_ = src.SetReadDeadline(time.Now().Add(r.idle()))
		mt, rd, err := src.NextReader()
		if err != nil {
			return
		}
		w, err := dst.NextWriter(mt)
		if err != nil {
			return
		}
		var in io.Reader = rd
		if lim != nil {
			in = &limited{r: rd, l: lim}
		}
		if _, err = io.Copy(w, in); err != nil {
			return
		}
		if w.Close() != nil {
			return
		}
	}
}

type limited struct {
	r io.Reader
	l *rate.Limiter
}

func (l *limited) Read(p []byte) (int, error) {
	if len(p) > 32<<10 {
		p = p[:32<<10]
	}
	n, err := l.r.Read(p)
	if n > 0 {
		if werr := l.l.WaitN(context.Background(), n); werr != nil {
			return n, werr
		}
	}
	return n, err
}

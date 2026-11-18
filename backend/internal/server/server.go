// Package server wires HTTP routes: /ws, /relay/<ticket>, /api/config, /healthz and the embedded SPA.
package server

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/BioHazard786/Warpdrop/backend/internal/config"
	"github.com/BioHazard786/Warpdrop/backend/internal/ice"
	"github.com/BioHazard786/Warpdrop/backend/internal/ratelimit"
	"github.com/BioHazard786/Warpdrop/backend/internal/relay"
	"github.com/BioHazard786/Warpdrop/backend/internal/signaling"
)

const maxConnsPerIP = 20

const csp = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self' ws: wss:; " +
	"frame-src 'self'; worker-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'self'"

type Server struct {
	cfg   config.Config
	hub   *signaling.Hub
	relay *relay.Relay
	conns *ratelimit.Conns
	up    websocket.Upgrader
	spa   fs.FS
}

// New builds the handler. spa may be nil (API only).
func New(cfg config.Config, spa fs.FS) (*Server, http.Handler) {
	s := &Server{cfg: cfg, conns: ratelimit.NewConns(maxConnsPerIP), spa: spa}
	s.up = websocket.Upgrader{ReadBufferSize: 32 << 10, WriteBufferSize: 32 << 10, CheckOrigin: s.checkOrigin}
	var tickets signaling.Tickets
	if cfg.RelayEnabled {
		s.relay = relay.New(cfg.RelayMaxMbps, cfg.RelayMaxConns, websocket.Upgrader{
			ReadBufferSize: 64 << 10, WriteBufferSize: 64 << 10, CheckOrigin: s.checkOrigin,
		})
		tickets = s.relay
	}
	s.hub = signaling.NewHub(tickets)
	if k := cfg.RateScale; k > 0 && k != 1 {
		s.hub.Create = ratelimit.NewIP(10*k, max(1, int(5*k)))
		s.hub.Join = ratelimit.NewIP(30*k, max(1, int(10*k)))
		s.hub.Relay = ratelimit.NewIP(30*k, max(1, int(10*k)))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", s.serveWS)
	if s.relay != nil {
		mux.HandleFunc("GET /relay/{ticket}", s.relay.Handler)
	}
	mux.HandleFunc("GET /api/config", s.serveConfig)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("GET /", s.serveSPA)
	return s, mux
}

// Hub exposes the signaling hub (tests, janitor).
func (s *Server) Hub() *signaling.Hub { return s.hub }

// Janitor sweeps idle rate-limit buckets until stop is closed.
func (s *Server) Janitor(stop <-chan struct{}) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			for _, l := range []*ratelimit.IP{s.hub.Create, s.hub.Join, s.hub.Relay} {
				l.Sweep(10 * time.Minute)
			}
		case <-stop:
			return
		}
	}
}

// checkOrigin allows no Origin (CLI), the same host, and ALLOWED_ORIGINS.
func (s *Server) checkOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, a := range s.cfg.AllowedOrigins {
		if strings.EqualFold(a, o) {
			return true
		}
	}
	return false
}

// ClientIP returns the peer IP, honoring X-Forwarded-For only when the direct peer is a trusted proxy.
func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if len(trusted) == 0 {
		return host
	}
	isTrusted := func(ip string) bool {
		a, err := netip.ParseAddr(ip)
		if err != nil {
			return false
		}
		for _, p := range trusted {
			if p.Contains(a.Unmap()) {
				return true
			}
		}
		return false
	}
	if !isTrusted(host) {
		return host
	}
	// walk X-Forwarded-For right to left; the first hop that is not a trusted proxy is the client
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		h := strings.TrimSpace(hops[i])
		if h == "" {
			continue
		}
		if !isTrusted(h) {
			return h
		}
	}
	return host
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	ip := ClientIP(r, s.cfg.TrustedProxies)
	if !s.conns.Acquire(ip) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	defer s.conns.Release(ip)
	conn, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote the response
	}
	signaling.NewClient(s.hub, conn, ip).Serve()
}

func (s *Server) serveConfig(w http.ResponseWriter, _ *http.Request) {
	c := ice.Build(s.cfg.STUNURLs, s.cfg.TURNURLs, s.cfg.TURNSecret, s.relay != nil, signaling.MaxReceivers, time.Now())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(c); err != nil {
		slog.Warn("config encode", "err", err)
	}
}

func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", csp)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	if s.spa == nil {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if strings.HasPrefix(name, "api/") || strings.HasPrefix(name, "relay/") || name == "ws" {
		http.NotFound(w, r) // never serve the SPA for backend paths
		return
	}
	if st, err := fs.Stat(s.spa, name); err != nil || st.IsDir() {
		if strings.HasPrefix(name, "assets/") || path.Ext(name) != "" {
			http.NotFound(w, r) // a missing asset must 404, not return HTML
			return
		}
		name = "index.html" // client-side route
	}
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	if name == "sw.js" {
		h.Set("Service-Worker-Allowed", "/")
	}
	http.ServeFileFS(w, r, s.spa, name)
}

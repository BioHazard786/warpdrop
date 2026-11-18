// Package ratelimit holds per-IP token buckets and a concurrent-connection counter.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type entry struct {
	l    *rate.Limiter
	seen time.Time
}

// IP is a set of token buckets keyed by client IP.
type IP struct {
	mu    sync.Mutex
	m     map[string]*entry
	limit rate.Limit
	burst int
}

// NewIP allows `burst` events at once, refilling `perMinute` per minute.
func NewIP(perMinute float64, burst int) *IP {
	return &IP{m: map[string]*entry{}, limit: rate.Limit(perMinute / 60), burst: burst}
}

func (r *IP) Allow(ip string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.m[ip]
	if e == nil {
		e = &entry{l: rate.NewLimiter(r.limit, r.burst)}
		r.m[ip] = e
	}
	e.seen = time.Now()
	return e.l.Allow()
}

// Sweep drops buckets idle for longer than ttl so the map cannot grow without bound.
func (r *IP) Sweep(ttl time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, e := range r.m {
		if time.Since(e.seen) > ttl {
			delete(r.m, k)
		}
	}
}

// Conns counts concurrent connections per IP.
type Conns struct {
	mu  sync.Mutex
	n   map[string]int
	max int
}

func NewConns(max int) *Conns { return &Conns{n: map[string]int{}, max: max} }

// Acquire returns false when ip is at its cap; callers must Release on success.
func (c *Conns) Acquire(ip string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n[ip] >= c.max {
		return false
	}
	c.n[ip]++
	return true
}

func (c *Conns) Release(ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n[ip]--; c.n[ip] <= 0 {
		delete(c.n, ip)
	}
}

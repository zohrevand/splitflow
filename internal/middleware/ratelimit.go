package middleware

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientLimiter
	rps     rate.Limit
	burst   int
}

// NewRateLimiter creates an in-memory per-IP token bucket rate limiter.
// Pass a cancellable context so the background cleanup goroutine exits cleanly on shutdown.
func NewRateLimiter(ctx context.Context, rps rate.Limit, burst int) *RateLimiter {
	rl := &RateLimiter{
		clients: make(map[string]*clientLimiter),
		rps:     rps,
		burst:   burst,
	}

	go rl.cleanupLoop(ctx)
	return rl
}

func (rl *RateLimiter) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rl.mu.Lock()
			for ip, c := range rl.clients {
				if time.Since(c.lastSeen) > 5*time.Minute {
					delete(rl.clients, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}

func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	c, exists := rl.clients[ip]
	if !exists {
		c = &clientLimiter{
			limiter: rate.NewLimiter(rl.rps, rl.burst),
		}
		rl.clients[ip] = c
	}
	c.lastSeen = time.Now()
	return c.limiter
}

func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}

		if !rl.getLimiter(ip).Allow() {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded, please try again later"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

package server

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

const (
	limiterIdleTTL   = 10 * time.Minute
	limiterSweepEach = time.Minute
)

// ipRateLimiter is a per-client-IP token bucket. Idle entries are swept so
// memory stays bounded by the number of recently active clients.
type ipRateLimiter struct {
	mu        sync.Mutex
	limit     rate.Limit
	burst     int
	clients   map[string]*limiterEntry
	lastSweep time.Time
	now       func() time.Time
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newIPRateLimiter(perMinute int) *ipRateLimiter {
	return &ipRateLimiter{
		limit:   rate.Limit(float64(perMinute) / 60),
		burst:   perMinute,
		clients: make(map[string]*limiterEntry),
		now:     time.Now,
	}
}

func (l *ipRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if now.Sub(l.lastSweep) > limiterSweepEach {
		for k, e := range l.clients {
			if now.Sub(e.lastSeen) > limiterIdleTTL {
				delete(l.clients, k)
			}
		}
		l.lastSweep = now
	}

	e, ok := l.clients[ip]
	if !ok {
		e = &limiterEntry{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.clients[ip] = e
	}
	e.lastSeen = now
	return e.limiter.AllowN(now, 1)
}

func (l *ipRateLimiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.allow(c.ClientIP()) {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"status": "error", "code": "rate_limited", "error": "terlalu banyak request, coba lagi nanti",
			})
			return
		}
		c.Next()
	}
}

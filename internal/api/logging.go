package api

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	maxAPIBodyBytes int64 = 2 << 20

	authRateLimitPerSec     = 5.0
	authRateLimitBurst      = 20.0
	apiRateLimitPerSec      = 80.0
	apiRateLimitBurst       = 160.0
	protocolRateLimitPerSec = 20.0
	protocolRateLimitBurst  = 40.0

	limiterBucketTTL       = 10 * time.Minute
	limiterCleanupInterval = time.Minute
)

type rateLimitBucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

type tokenBucketLimiter struct {
	mu          sync.Mutex
	ratePerSec  float64
	burst       float64
	buckets     map[string]rateLimitBucket
	lastCleanup time.Time
}

func newTokenBucketLimiter(ratePerSec, burst float64) *tokenBucketLimiter {
	return &tokenBucketLimiter{
		ratePerSec: ratePerSec,
		burst:      burst,
		buckets:    make(map[string]rateLimitBucket),
	}
}

func (l *tokenBucketLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.buckets[key]
	if b.lastRefill.IsZero() {
		b.tokens = l.burst
		b.lastRefill = now
	}
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * l.ratePerSec
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.lastRefill = now
	}
	b.lastSeen = now
	allowed := b.tokens >= 1.0
	if allowed {
		b.tokens -= 1.0
	}
	l.buckets[key] = b

	if l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= limiterCleanupInterval {
		for k, entry := range l.buckets {
			if now.Sub(entry.lastSeen) > limiterBucketTTL {
				delete(l.buckets, k)
			}
		}
		l.lastCleanup = now
	}
	return allowed
}

type requestRateLimiter struct {
	auth     *tokenBucketLimiter
	api      *tokenBucketLimiter
	protocol *tokenBucketLimiter
}

func newRequestRateLimiter() *requestRateLimiter {
	return &requestRateLimiter{
		auth:     newTokenBucketLimiter(authRateLimitPerSec, authRateLimitBurst),
		api:      newTokenBucketLimiter(apiRateLimitPerSec, apiRateLimitBurst),
		protocol: newTokenBucketLimiter(protocolRateLimitPerSec, protocolRateLimitBurst),
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func generateRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}

func requestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := generateRequestID()
		w.Header().Set("X-Request-ID", reqID)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("request",
			"request_id", reqID,
			"method", r.Method,
			"path", r.URL.RequestURI(),
			"status", rec.status,
			"duration", time.Since(start),
			"ip", clientIPFromRequest(r),
		)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/git/") || strings.HasPrefix(path, "/got/") {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func requestRateLimitMiddleware(limiter *requestRateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limiter == nil || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		path := r.URL.Path
		scope := ""
		switch {
		case strings.HasPrefix(path, "/api/v1/auth/"):
			scope = "auth"
		case strings.HasPrefix(path, "/api/v1/"):
			scope = "api"
		case strings.HasPrefix(path, "/git/"), strings.HasPrefix(path, "/got/"):
			scope = "protocol"
		default:
			next.ServeHTTP(w, r)
			return
		}
		key := clientIPFromRequest(r)
		now := time.Now()
		allowed := true
		switch scope {
		case "auth":
			allowed = limiter.auth.allow(key, now)
		case "api":
			allowed = limiter.api.allow(key, now)
		case "protocol":
			allowed = limiter.protocol.allow(key, now)
		}
		if !allowed {
			w.Header().Set("Retry-After", "1")
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIPFromRequest(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if idx := strings.Index(forwarded, ","); idx >= 0 {
			forwarded = strings.TrimSpace(forwarded[:idx])
		}
		if forwarded != "" {
			return forwarded
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func requestBodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
		default:
			next.ServeHTTP(w, r)
			return
		}
		if r.ContentLength > maxAPIBodyBytes {
			http.Error(w, `{"error":"request body too large"}`, http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxAPIBodyBytes)
		next.ServeHTTP(w, r)
	})
}

package handlers

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ─── Allowed CORS Origins ─────────────────────────────────────────────────────

func GetAllowedOrigins() map[string]bool {
	envOrigins := os.Getenv("ALLOWED_ORIGINS")
	var list []string
	if envOrigins != "" {
		list = strings.Split(envOrigins, ",")
	} else {
		list = []string{
			"https://apuracao.fbtax.cloud",
			"https://fbtax.cloud",
			"http://localhost:3000",
			"http://localhost:5173",
		}
	}
	m := make(map[string]bool, len(list))
	for _, o := range list {
		m[strings.TrimSpace(o)] = true
	}
	return m
}

// ─── CORS-fixing ResponseWriter ───────────────────────────────────────────────
// Intercepts WriteHeader/Write to override any wildcard CORS set by handlers
// and inject security headers before the response is flushed.

type secureResponseWriter struct {
	http.ResponseWriter
	origin      string
	headersDone bool
}

func (s *secureResponseWriter) applyHeaders() {
	if s.headersDone {
		return
	}
	s.headersDone = true
	h := s.ResponseWriter.Header()

	// Override any wildcard CORS set by individual handlers
	h.Del("Access-Control-Allow-Origin")
	h.Del("Access-Control-Allow-Credentials")
	h.Del("Vary")
	if s.origin != "" {
		h.Set("Access-Control-Allow-Origin", s.origin)
		h.Set("Access-Control-Allow-Credentials", "true")
		h.Set("Vary", "Origin")
	}

	// Security headers
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-XSS-Protection", "1; mode=block")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
	if h.Get("Content-Security-Policy") == "" {
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"img-src 'self' data: blob:; "+
				"font-src 'self' data: https://fonts.gstatic.com; "+
				"connect-src 'self' https://fonts.googleapis.com")
	}
}

func (s *secureResponseWriter) WriteHeader(code int) {
	s.applyHeaders()
	s.ResponseWriter.WriteHeader(code)
}

func (s *secureResponseWriter) Write(b []byte) (int, error) {
	s.applyHeaders()
	return s.ResponseWriter.Write(b)
}

// ─── Security Middleware ──────────────────────────────────────────────────────
// Wrap the entire mux: handles CORS, preflight OPTIONS, and security headers.

func SecurityMiddleware(next http.Handler, allowedOrigins map[string]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		var allowedOrigin string
		if origin != "" && allowedOrigins[origin] {
			allowedOrigin = origin
		}

		srw := &secureResponseWriter{
			ResponseWriter: w,
			origin:         allowedOrigin,
		}

		// Handle CORS preflight
		if r.Method == http.MethodOptions {
			srw.applyHeaders()
			h := w.Header()
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Company-ID")
			h.Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(srw, r)
	})
}

// ─── Rate Limiter ─────────────────────────────────────────────────────────────

type rateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	max      int
	window   time.Duration
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		requests: make(map[string][]time.Time),
		max:      max,
		window:   window,
	}
}

// Exported rate limiters used by auth handlers
var (
	LoginRL          = newRateLimiter(5, 15*time.Minute)
	RegisterRL       = newRateLimiter(10, time.Hour)
	ForgotPasswordRL = newRateLimiter(3, time.Hour)
)

// Allow checks AND records one attempt. Returns false if limit is exceeded.
func (rl *rateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	existing := rl.requests[key]
	valid := existing[:0]
	for _, t := range existing {
		if now.Sub(t) < rl.window {
			valid = append(valid, t)
		}
	}
	rl.requests[key] = valid

	if len(valid) >= rl.max {
		return false
	}
	rl.requests[key] = append(rl.requests[key], now)
	return true
}

// IsLimited checks if the key is already over the limit without recording a new attempt.
func (rl *rateLimiter) IsLimited(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	existing := rl.requests[key]
	valid := existing[:0]
	for _, t := range existing {
		if now.Sub(t) < rl.window {
			valid = append(valid, t)
		}
	}
	rl.requests[key] = valid
	return len(valid) >= rl.max
}

// RecordFailure records one failed attempt without returning a decision.
func (rl *rateLimiter) RecordFailure(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.requests[key] = append(rl.requests[key], time.Now())
}

// Reset clears the counter for a key (e.g. on successful login).
func (rl *rateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.requests, key)
}

// GetClientIP extracts the real client IP from reverse proxy headers.
// Uses the LAST entry in X-Forwarded-For (set by trusted proxy) to prevent spoofing.
func GetClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		// Use rightmost IP (added by our trusted reverse proxy, not spoofable by client)
		return strings.TrimSpace(parts[len(parts)-1])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		return addr[:idx]
	}
	return addr
}

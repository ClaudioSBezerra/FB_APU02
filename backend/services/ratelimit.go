package services

import (
	"sync"
	"time"
)

// RateLimiter é um limitador de taxa genérico por janela deslizante, chaveado por uma string
// arbitrária (IP, userID, companyID, ...).
//
// Vivia em handlers/middleware.go (tipo não-exportado `rateLimiter`) até a sub-spec
// spec-nfse-agendamento-persistencia.md, que precisou de UMA MESMA instância de rate limiter
// compartilhada entre um handler HTTP (`handlers.BuscarNFSeHandler`) e um scheduler em
// background (`services.StartNFSeScheduler`) — ver `NFSeBuscaRL` em nfse_processor.go. Como
// `services` não pode depender de `handlers` (ver Architectural Constraints do projeto), o tipo
// foi movido pra cá; `handlers/middleware.go` mantém `LoginRL`/`RegisterRL`/etc. funcionando via
// alias de tipo (`type rateLimiter = services.RateLimiter`), sem duplicar a lógica.
type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	max      int
	window   time.Duration
}

func NewRateLimiter(max int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
		max:      max,
		window:   window,
	}
}

// Allow checks AND records one attempt. Returns false if limit is exceeded.
func (rl *RateLimiter) Allow(key string) bool {
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
func (rl *RateLimiter) IsLimited(key string) bool {
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
func (rl *RateLimiter) RecordFailure(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.requests[key] = append(rl.requests[key], time.Now())
}

// Reset clears the counter for a key (e.g. on successful login).
func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.requests, key)
}

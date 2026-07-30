package channer

import (
	"sync"
	"time"

	"golang.org/x/time/rate"

	"martie/internal/gateway"
)

type limitResult uint8

const (
	limitAllowed limitResult = iota
	limitGlobal
	limitThread
)

type threadLimit struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// Limiter applies the process-wide and per-thread reply budgets as one
// decision, so a denied request never consumes capacity from the other budget.
type Limiter struct {
	mu sync.Mutex

	global      *rate.Limiter
	threadRate  rate.Limit
	threadBurst int
	threads     map[gateway.ThreadRef]*threadLimit
}

func NewLimiter(globalLimit, globalBurst, perThreadLimit, perThreadBurst int) *Limiter {
	return &Limiter{
		global:      hourlyLimiter(globalLimit, globalBurst),
		threadRate:  hourlyRate(perThreadLimit),
		threadBurst: perThreadBurst,
		threads:     make(map[gateway.ThreadRef]*threadLimit),
	}
}

func (l *Limiter) allow(thread gateway.ThreadRef, now time.Time) limitResult {
	l.mu.Lock()
	defer l.mu.Unlock()

	for ref, limit := range l.threads {
		if now.Sub(limit.lastSeen) >= time.Hour {
			delete(l.threads, ref)
		}
	}

	if l.global.TokensAt(now) < 1 {
		if limit := l.threads[thread]; limit != nil {
			limit.lastSeen = now
		}
		return limitGlobal
	}

	limit := l.threads[thread]
	if limit == nil {
		limit = &threadLimit{
			limiter: rate.NewLimiter(l.threadRate, l.threadBurst),
		}
		l.threads[thread] = limit
	}
	limit.lastSeen = now
	if limit.limiter.TokensAt(now) < 1 {
		return limitThread
	}

	l.global.AllowN(now, 1)
	limit.limiter.AllowN(now, 1)
	return limitAllowed
}

func hourlyLimiter(requests, burst int) *rate.Limiter {
	return rate.NewLimiter(hourlyRate(requests), burst)
}

func hourlyRate(requests int) rate.Limit {
	return rate.Limit(float64(requests) / time.Hour.Seconds())
}

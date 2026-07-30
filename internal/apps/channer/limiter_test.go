package channer

import (
	"testing"
	"time"

	"martie/internal/gateway"
)

func TestLimiterKeepsThreadAndGlobalBudgetsIndependent(t *testing.T) {
	now := time.Now()
	first := gateway.ThreadRef{Board: "i", ThreadID: 1}
	second := gateway.ThreadRef{Board: "i", ThreadID: 2}
	limit := NewLimiter(3, 3, 2, 2)

	if got := limit.allow(first, now); got != limitAllowed {
		t.Fatalf("first request = %v, want allowed", got)
	}
	if got := limit.allow(first, now); got != limitAllowed {
		t.Fatalf("second request = %v, want allowed", got)
	}
	if got := limit.allow(first, now); got != limitThread {
		t.Fatalf("third request = %v, want thread limit", got)
	}
	if got := limit.allow(second, now); got != limitAllowed {
		t.Fatalf("other thread after denial = %v, want allowed", got)
	}
	if got := limit.allow(gateway.ThreadRef{Board: "i", ThreadID: 3}, now); got != limitGlobal {
		t.Fatalf("request after global budget = %v, want global limit", got)
	}
}

func TestLimiterDiscardsInactiveThreads(t *testing.T) {
	now := time.Now()
	first := gateway.ThreadRef{Board: "i", ThreadID: 1}
	second := gateway.ThreadRef{Board: "i", ThreadID: 2}
	limit := NewLimiter(10, 10, 2, 2)

	limit.allow(first, now)
	limit.allow(second, now.Add(30*time.Minute))
	limit.allow(second, now.Add(time.Hour))

	if _, ok := limit.threads[first]; ok {
		t.Fatal("inactive thread bucket was retained")
	}
	if _, ok := limit.threads[second]; !ok {
		t.Fatal("active thread bucket was discarded")
	}
}

package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPollingWorkerWaitsAfterPollCompletes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interval := 30 * time.Millisecond
	var calls []time.Time
	worker := pollingWorker("test", interval, func(context.Context) error {
		calls = append(calls, time.Now())
		if len(calls) == 2 {
			cancel()
		}
		return nil
	}, newMetrics(), discardLogger())

	if err := worker.run(ctx); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("poll calls = %d, want 2", len(calls))
	}
	if elapsed := calls[1].Sub(calls[0]); elapsed < interval {
		t.Fatalf("time between polls = %s, want at least %s", elapsed, interval)
	}
}

func TestHTTPHandlerServesHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	httpHandler(newMetrics(), &atomic.Bool{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 OK", response.Code)
	}
}

func TestHTTPHandlerReportsReadiness(t *testing.T) {
	var ready atomic.Bool
	handler := httpHandler(newMetrics(), &ready)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 Service Unavailable", response.Code)
	}

	ready.Store(true)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 OK", response.Code)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

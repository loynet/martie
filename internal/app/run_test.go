package app

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

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

func TestMetricsInitializeChannerOutcomes(t *testing.T) {
	metrics := newMetrics()
	response := httptest.NewRecorder()
	metrics.handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	for _, want := range []string{
		`martie_channer_outcomes_total{outcome="completion_error"} 0`,
		`martie_channer_outcomes_total{outcome="posted"} 0`,
		`martie_channer_outcomes_total{outcome="posting_unknown"} 0`,
	} {
		if !strings.Contains(response.Body.String(), want) {
			t.Fatalf("metrics missing %q:\n%s", want, response.Body.String())
		}
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

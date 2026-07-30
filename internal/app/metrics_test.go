package app

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"martie/internal/deepseek"
)

func TestMetricsExposeConsolidatedContract(t *testing.T) {
	metrics := newMetrics()
	metrics.ObserveOperation("streamnotifier", time.Second, nil)
	metrics.ObserveOperation("streamnotifier", time.Second, errors.New("failed"))
	metrics.ObserveChannerOutcome("local_thread_rate_limited")
	metrics.ObserveModelCompletion("channer", "deepseek", "deepseek-chat", time.Second, deepseek.Completion{
		FinishReason: deepseek.FinishStop,
		Usage: deepseek.Usage{
			PromptCacheHitTokens:  2,
			PromptCacheMissTokens: 3,
			CompletionTokens:      5,
		},
	}, nil)

	response := httptest.NewRecorder()
	metrics.handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	body := response.Body.String()

	for _, want := range []string{
		"martie_operation_duration_seconds_count{operation=\"streamnotifier\",result=\"success\"} 1",
		"martie_operation_duration_seconds_count{operation=\"streamnotifier\",result=\"error\"} 1",
		"martie_operation_last_success{operation=\"streamnotifier\"} 0",
		"martie_operation_last_completed_timestamp_seconds{operation=\"streamnotifier\"}",
		"martie_channer_requests_total{outcome=\"local_thread_rate_limited\"} 1",
		"martie_model_completion_duration_seconds_count{model=\"deepseek-chat\",outcome=\"stop\",provider=\"deepseek\",surface=\"channer\"} 1",
		"martie_model_tokens_total{model=\"deepseek-chat\",provider=\"deepseek\",surface=\"channer\",type=\"output\"} 5",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q:\n%s", want, body)
		}
	}
	for _, obsolete := range []string{"martie_up ", "martie_worker_runs_total", "martie_model_requests_total"} {
		if strings.Contains(body, obsolete) {
			t.Fatalf("metrics still contain obsolete series %q", obsolete)
		}
	}
}

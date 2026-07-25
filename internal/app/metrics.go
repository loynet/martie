package app

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"martie/internal/deepseek"
)

type metrics struct {
	registry *prometheus.Registry

	workflowRuns             *prometheus.CounterVec
	workflowDuration         *prometheus.HistogramVec
	workflowLastSuccess      *prometheus.GaugeVec
	workflowLastRun          *prometheus.GaugeVec
	notifications            *prometheus.CounterVec
	assistantUpdates         *prometheus.CounterVec
	assistantResponses       *prometheus.CounterVec
	assistantContextRequests *prometheus.CounterVec
	activeConversations      prometheus.Gauge
	aiRequests               *prometheus.CounterVec
	aiDuration               prometheus.Histogram
	aiTokens                 *prometheus.CounterVec
}

const (
	metricResultSuccess = "success"
	metricResultError   = "error"
)

func newMetrics() *metrics {
	m := &metrics{
		registry: prometheus.NewRegistry(),
		workflowRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_workflow_runs_total",
			Help: "Completed workflow runs by result.",
		}, []string{"workflow", "result"}),
		workflowDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "martie_workflow_duration_seconds",
			Help:    "Workflow run duration in seconds.",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 12),
		}, []string{"workflow"}),
		workflowLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "martie_workflow_last_success",
			Help: "Whether the last workflow run succeeded.",
		}, []string{"workflow"}),
		workflowLastRun: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "martie_workflow_last_successful_timestamp_seconds",
			Help: "Unix timestamp of the last successful workflow run.",
		}, []string{"workflow"}),
		notifications: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_notifications_sent_total",
			Help: "Notifications sent by source.",
		}, []string{"source"}),
		assistantUpdates: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_assistant_updates_total",
			Help: "Assistant updates by admission result.",
		}, []string{"result"}),
		assistantResponses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_assistant_responses_total",
			Help: "Assistant responses by delivery result.",
		}, []string{"result"}),
		assistantContextRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_assistant_context_requests_total",
			Help: "Assistant requests that include recent history, replied-to message, or gateway ptchan context.",
		}, []string{"type"}),
		activeConversations: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "martie_assistant_active_conversations",
			Help: "In-memory conversations with unexpired history.",
		}),
		aiRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_ai_requests_total",
			Help: "AI completion requests by result and finish reason.",
		}, []string{"result", "finish_reason"}),
		aiDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "martie_ai_request_duration_seconds",
			Help:    "AI completion request duration in seconds.",
			Buckets: prometheus.ExponentialBuckets(0.5, 2, 10),
		}),
		aiTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_ai_tokens_total",
			Help: "AI tokens consumed by mutually exclusive input cache status or output.",
		}, []string{"type"}),
	}

	m.registry.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "martie_up",
			Help: "Whether the martie process is running.",
		}, func() float64 { return 1 }),
		m.workflowRuns,
		m.workflowDuration,
		m.workflowLastSuccess,
		m.workflowLastRun,
		m.notifications,
		m.assistantUpdates,
		m.assistantResponses,
		m.assistantContextRequests,
		m.activeConversations,
		m.aiRequests,
		m.aiDuration,
		m.aiTokens,
	)

	return m
}

func (m *metrics) handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *metrics) observeWorkflow(name string, duration time.Duration, err error) {
	result := metricResultSuccess
	success := 1.0
	if err != nil {
		result = metricResultError
		success = 0
	}

	m.workflowRuns.WithLabelValues(name, result).Inc()
	m.workflowDuration.WithLabelValues(name).Observe(duration.Seconds())
	m.workflowLastSuccess.WithLabelValues(name).Set(success)
	if err == nil {
		m.workflowLastRun.WithLabelValues(name).SetToCurrentTime()
	}
}

func (m *metrics) addNotifications(source string, count int) {
	if count > 0 {
		m.notifications.WithLabelValues(source).Add(float64(count))
	}
}

func (m *metrics) observeAssistantUpdate(result admissionResult) {
	m.assistantUpdates.WithLabelValues(string(result)).Inc()
}

func (m *metrics) observeAssistantResponse(result string) {
	m.assistantResponses.WithLabelValues(result).Inc()
}

func (m *metrics) observeAssistantContext(contextType string) {
	m.assistantContextRequests.WithLabelValues(contextType).Inc()
}

func (m *metrics) setActiveConversations(count int) {
	m.activeConversations.Set(float64(count))
}

func (m *metrics) observeAICompletion(duration time.Duration, completion deepseek.Completion, err error) {
	m.aiDuration.Observe(duration.Seconds())
	if err != nil {
		m.aiRequests.WithLabelValues(metricResultError, "").Inc()
		return
	}
	m.aiRequests.WithLabelValues(metricResultSuccess, string(completion.FinishReason)).Inc()
	m.aiTokens.WithLabelValues("input_cache_hit").Add(float64(completion.Usage.PromptCacheHitTokens))
	m.aiTokens.WithLabelValues("input_cache_miss").Add(float64(completion.Usage.PromptCacheMissTokens))
	m.aiTokens.WithLabelValues("output").Add(float64(completion.Usage.CompletionTokens))
}

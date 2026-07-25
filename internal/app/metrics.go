package app

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"martie/internal/deepseek"
	"martie/internal/gateway"
)

type metrics struct {
	registry *prometheus.Registry

	componentRuns             *prometheus.CounterVec
	componentRunDuration      *prometheus.HistogramVec
	componentLastRunSuccess   *prometheus.GaugeVec
	componentLastRunTimestamp *prometheus.GaugeVec

	gatewayWebhooks *prometheus.CounterVec
	gatewayEvents   *prometheus.CounterVec

	notifications *prometheus.CounterVec

	assistantAdmissions          *prometheus.CounterVec
	assistantReplies             *prometheus.CounterVec
	assistantContext             *prometheus.CounterVec
	assistantActiveConversations *prometheus.GaugeVec

	modelRequests *prometheus.CounterVec
	modelDuration *prometheus.HistogramVec
	modelTokens   *prometheus.CounterVec
}

const (
	metricResultSuccess = "success"
	metricResultError   = "error"
)

const (
	notificationSent   = "sent"
	notificationFailed = "failed"
)

func newMetrics() *metrics {
	m := &metrics{
		registry: prometheus.NewRegistry(),
		componentRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_component_runs_total",
			Help: "Component run attempts by result.",
		}, []string{"component", "result"}),
		componentRunDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "martie_component_run_duration_seconds",
			Help:    "Component run duration in seconds.",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 12),
		}, []string{"component"}),
		componentLastRunSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "martie_component_last_run_success",
			Help: "Whether the last observed component run succeeded.",
		}, []string{"component"}),
		componentLastRunTimestamp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "martie_component_last_run_timestamp_seconds",
			Help: "Unix timestamp of the last observed component run.",
		}, []string{"component", "result"}),
		gatewayWebhooks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_gateway_webhooks_total",
			Help: "Signed ptchan-gateway webhook requests by result.",
		}, []string{"result"}),
		gatewayEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_gateway_events_total",
			Help: "Gateway events dispatched to consumers by consumer, kind, and result.",
		}, []string{"consumer", "kind", "result"}),
		notifications: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_notifications_total",
			Help: "Notification delivery attempts by source and result.",
		}, []string{"source", "result"}),
		assistantAdmissions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_assistant_admissions_total",
			Help: "Assistant input admission decisions by surface and result.",
		}, []string{"surface", "result"}),
		assistantReplies: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_assistant_replies_total",
			Help: "Assistant reply delivery attempts by surface and result.",
		}, []string{"surface", "result"}),
		assistantContext: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_assistant_context_total",
			Help: "Assistant requests that included a context source by surface and type.",
		}, []string{"surface", "type"}),
		assistantActiveConversations: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "martie_assistant_active_conversations",
			Help: "In-memory conversations with unexpired history by surface.",
		}, []string{"surface"}),
		modelRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_model_requests_total",
			Help: "Model completion requests by surface, provider, model, result, and finish reason.",
		}, []string{"surface", "provider", "model", "result", "finish_reason"}),
		modelDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "martie_model_request_duration_seconds",
			Help:    "Model completion request duration in seconds.",
			Buckets: prometheus.ExponentialBuckets(0.5, 2, 10),
		}, []string{"surface", "provider", "model"}),
		modelTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_model_tokens_total",
			Help: "Model tokens by surface, provider, model, and token type.",
		}, []string{"surface", "provider", "model", "type"}),
	}

	m.registry.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "martie_up",
			Help: "Whether the martie process is running.",
		}, func() float64 { return 1 }),
		m.componentRuns,
		m.componentRunDuration,
		m.componentLastRunSuccess,
		m.componentLastRunTimestamp,
		m.gatewayWebhooks,
		m.gatewayEvents,
		m.notifications,
		m.assistantAdmissions,
		m.assistantReplies,
		m.assistantContext,
		m.assistantActiveConversations,
		m.modelRequests,
		m.modelDuration,
		m.modelTokens,
	)

	return m
}

func (m *metrics) handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *metrics) observeComponentRun(component string, duration time.Duration, err error) {
	result := metricResultSuccess
	success := 1.0
	if err != nil {
		result = metricResultError
		success = 0
	}

	m.componentRuns.WithLabelValues(component, result).Inc()
	m.componentRunDuration.WithLabelValues(component).Observe(duration.Seconds())
	m.componentLastRunSuccess.WithLabelValues(component).Set(success)
	m.componentLastRunTimestamp.WithLabelValues(component, result).SetToCurrentTime()
}

func (m *metrics) observeGatewayWebhook(result string) {
	m.gatewayWebhooks.WithLabelValues(result).Inc()
}

func (m *metrics) observeGatewayEvent(consumer string, kind gateway.Kind, result string) {
	m.gatewayEvents.WithLabelValues(consumer, string(kind), result).Inc()
}

func (m *metrics) observeNotification(source, result string) {
	m.notifications.WithLabelValues(source, result).Inc()
}

func (m *metrics) observeAssistantAdmission(surface ComponentName, result admissionResult) {
	m.assistantAdmissions.WithLabelValues(string(surface), string(result)).Inc()
}

func (m *metrics) observeAssistantReply(surface ComponentName, result string) {
	m.assistantReplies.WithLabelValues(string(surface), result).Inc()
}

func (m *metrics) observeAssistantContext(surface ComponentName, contextType string) {
	m.assistantContext.WithLabelValues(string(surface), contextType).Inc()
}

func (m *metrics) setActiveConversations(surface ComponentName, count int) {
	m.assistantActiveConversations.WithLabelValues(string(surface)).Set(float64(count))
}

func (m *metrics) observeModelCompletion(surface ComponentName, provider, model string, duration time.Duration, completion deepseek.Completion, err error) {
	m.modelDuration.WithLabelValues(string(surface), provider, model).Observe(duration.Seconds())
	if err != nil {
		m.modelRequests.WithLabelValues(string(surface), provider, model, metricResultError, "").Inc()
		return
	}
	m.modelRequests.WithLabelValues(string(surface), provider, model, metricResultSuccess, string(completion.FinishReason)).Inc()
	m.modelTokens.WithLabelValues(string(surface), provider, model, "input_cache_hit").Add(float64(completion.Usage.PromptCacheHitTokens))
	m.modelTokens.WithLabelValues(string(surface), provider, model, "input_cache_miss").Add(float64(completion.Usage.PromptCacheMissTokens))
	m.modelTokens.WithLabelValues(string(surface), provider, model, "output").Add(float64(completion.Usage.CompletionTokens))
}

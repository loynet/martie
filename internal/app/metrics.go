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

	operationDuration      *prometheus.HistogramVec
	operationLastSuccess   *prometheus.GaugeVec
	operationLastCompleted *prometheus.GaugeVec

	gatewayWebhooks *prometheus.CounterVec
	gatewayEvents   *prometheus.CounterVec

	notifications *prometheus.CounterVec

	assistantAdmissions          *prometheus.CounterVec
	assistantReplies             *prometheus.CounterVec
	assistantContext             *prometheus.CounterVec
	assistantActiveConversations *prometheus.GaugeVec

	channerOutcomes *prometheus.CounterVec

	modelDuration *prometheus.HistogramVec
	modelTokens   *prometheus.CounterVec
}

const (
	metricResultSuccess = "success"
	metricResultError   = "error"
)

func newMetrics() *metrics {
	m := &metrics{
		registry: prometheus.NewRegistry(),
		operationDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "martie_operation_duration_seconds",
			Help:    "Duration of recurring operations by result.",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 12),
		}, []string{"operation", "result"}),
		operationLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "martie_operation_last_success",
			Help: "Whether the last recurring operation succeeded.",
		}, []string{"operation"}),
		operationLastCompleted: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "martie_operation_last_completed_timestamp_seconds",
			Help: "Unix timestamp when the recurring operation last completed.",
		}, []string{"operation"}),
		gatewayWebhooks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_gateway_webhook_requests_total",
			Help: "Incoming ptchan-gateway webhook requests by result.",
		}, []string{"result"}),
		gatewayEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_gateway_event_dispatches_total",
			Help: "Gateway event dispatches by consumer, kind, and result.",
		}, []string{"consumer", "kind", "result"}),
		notifications: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_notification_delivery_attempts_total",
			Help: "Telegram notification delivery attempts by source and result.",
		}, []string{"source", "result"}),
		assistantAdmissions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_assistant_admissions_total",
			Help: "Assistant input admission decisions by surface and result.",
		}, []string{"surface", "result"}),
		assistantReplies: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_assistant_reply_deliveries_total",
			Help: "Assistant reply delivery attempts by surface and result.",
		}, []string{"surface", "result"}),
		assistantContext: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_assistant_context_uses_total",
			Help: "Assistant requests that used a context source by surface and type.",
		}, []string{"surface", "type"}),
		assistantActiveConversations: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "martie_assistant_active_conversations",
			Help: "In-memory conversations with unexpired history by surface.",
		}, []string{"surface"}),
		channerOutcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_channer_requests_total",
			Help: "Terminal outcomes for admitted channer requests.",
		}, []string{"outcome"}),
		modelDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "martie_model_completion_duration_seconds",
			Help:    "Model completion duration by outcome.",
			Buckets: prometheus.ExponentialBuckets(0.5, 2, 10),
		}, []string{"surface", "provider", "model", "outcome"}),
		modelTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_model_tokens_total",
			Help: "Model tokens by surface, provider, model, and token type.",
		}, []string{"surface", "provider", "model", "type"}),
	}

	m.registry.MustRegister(
		m.operationDuration,
		m.operationLastSuccess,
		m.operationLastCompleted,
		m.gatewayWebhooks,
		m.gatewayEvents,
		m.notifications,
		m.assistantAdmissions,
		m.assistantReplies,
		m.assistantContext,
		m.assistantActiveConversations,
		m.channerOutcomes,
		m.modelDuration,
		m.modelTokens,
	)

	return m
}

func (m *metrics) handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *metrics) ObserveOperation(operation string, duration time.Duration, err error) {
	result := metricResultSuccess
	success := 1.0
	if err != nil {
		result = metricResultError
		success = 0
	}

	m.operationDuration.WithLabelValues(operation, result).Observe(duration.Seconds())
	m.operationLastSuccess.WithLabelValues(operation).Set(success)
	m.operationLastCompleted.WithLabelValues(operation).SetToCurrentTime()
}

func (m *metrics) observeGatewayWebhook(result string) {
	m.gatewayWebhooks.WithLabelValues(result).Inc()
}

func (m *metrics) observeGatewayEvent(consumer string, kind gateway.EventKind, result string) {
	m.gatewayEvents.WithLabelValues(consumer, string(kind), result).Inc()
}

func (m *metrics) ObserveNotification(source, result string) {
	m.notifications.WithLabelValues(source, result).Inc()
}

func (m *metrics) ObserveAssistantAdmission(surface, result string) {
	m.assistantAdmissions.WithLabelValues(surface, result).Inc()
}

func (m *metrics) ObserveAssistantReply(surface, result string) {
	m.assistantReplies.WithLabelValues(surface, result).Inc()
}

func (m *metrics) ObserveAssistantContext(surface, contextType string) {
	m.assistantContext.WithLabelValues(surface, contextType).Inc()
}

func (m *metrics) SetActiveConversations(surface string, count int) {
	m.assistantActiveConversations.WithLabelValues(surface).Set(float64(count))
}

func (m *metrics) ObserveChannerOutcome(outcome string) {
	m.channerOutcomes.WithLabelValues(outcome).Inc()
}

func (m *metrics) ObserveModelCompletion(surface, provider, model string, duration time.Duration, completion deepseek.Completion, err error) {
	outcome := string(completion.FinishReason)
	if err != nil {
		outcome = metricResultError
	}
	if outcome == "" {
		outcome = "unknown"
	}
	m.modelDuration.WithLabelValues(surface, provider, model, outcome).Observe(duration.Seconds())
	if err != nil {
		return
	}
	m.modelTokens.WithLabelValues(surface, provider, model, "input_cache_hit").Add(float64(completion.Usage.PromptCacheHitTokens))
	m.modelTokens.WithLabelValues(surface, provider, model, "input_cache_miss").Add(float64(completion.Usage.PromptCacheMissTokens))
	m.modelTokens.WithLabelValues(surface, provider, model, "output").Add(float64(completion.Usage.CompletionTokens))
}

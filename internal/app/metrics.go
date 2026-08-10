package app

import (
	"net/http"
	"time"

	"github.com/loynet/ptchan-ai/deepseek"
	"github.com/loynet/ptchan-gateway/clients/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"martie/internal/channer"
)

type metrics struct {
	registry *prometheus.Registry

	gatewayWebhooks *prometheus.CounterVec
	gatewayEvents   *prometheus.CounterVec

	channerAdmissions *prometheus.CounterVec
	channerReplies    *prometheus.CounterVec
	channerContext    *prometheus.CounterVec
	channerOutcomes   *prometheus.CounterVec

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
		gatewayWebhooks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_gateway_webhook_requests_total",
			Help: "Incoming ptchan-gateway webhook requests by result.",
		}, []string{"result"}),
		gatewayEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_gateway_event_deliveries_total",
			Help: "Gateway event deliveries by kind and result.",
		}, []string{"kind", "result"}),
		channerAdmissions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_channer_admissions_total",
			Help: "Channer input admission decisions by result.",
		}, []string{"result"}),
		channerReplies: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_channer_reply_deliveries_total",
			Help: "Channer reply delivery attempts by result.",
		}, []string{"result"}),
		channerContext: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_channer_context_uses_total",
			Help: "Channer requests that used a context source by type.",
		}, []string{"type"}),
		channerOutcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_channer_outcomes_total",
			Help: "Terminal outcomes for admitted channer requests.",
		}, []string{"outcome"}),
		modelDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "martie_model_completion_duration_seconds",
			Help:    "Model completion duration by outcome.",
			Buckets: prometheus.ExponentialBuckets(0.5, 2, 10),
		}, []string{"provider", "model", "outcome"}),
		modelTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "martie_model_tokens_total",
			Help: "Model tokens by surface, provider, model, and token type.",
		}, []string{"provider", "model", "type"}),
	}

	m.registry.MustRegister(
		m.gatewayWebhooks,
		m.gatewayEvents,
		m.channerAdmissions,
		m.channerReplies,
		m.channerContext,
		m.channerOutcomes,
		m.modelDuration,
		m.modelTokens,
	)
	for _, outcome := range channer.TerminalOutcomes() {
		m.channerOutcomes.WithLabelValues(outcome)
	}

	return m
}

func (m *metrics) handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *metrics) observeGatewayWebhook(result string) {
	m.gatewayWebhooks.WithLabelValues(result).Inc()
}

func (m *metrics) observeGatewayEvent(kind gateway.EventKind, result string) {
	m.gatewayEvents.WithLabelValues(string(kind), result).Inc()
}

func (m *metrics) ObserveChannerAdmission(result string) {
	m.channerAdmissions.WithLabelValues(result).Inc()
}

func (m *metrics) ObserveChannerReply(result string) {
	m.channerReplies.WithLabelValues(result).Inc()
}

func (m *metrics) ObserveChannerContext(contextType string) {
	m.channerContext.WithLabelValues(contextType).Inc()
}

func (m *metrics) ObserveChannerOutcome(outcome string) {
	m.channerOutcomes.WithLabelValues(outcome).Inc()
}

func (m *metrics) ObserveModelCompletion(provider, model string, duration time.Duration, completion deepseek.Completion, err error) {
	outcome := string(completion.FinishReason)
	if err != nil {
		outcome = metricResultError
	}
	if outcome == "" {
		outcome = "unknown"
	}
	m.modelDuration.WithLabelValues(provider, model, outcome).Observe(duration.Seconds())
	if err != nil {
		return
	}
	m.modelTokens.WithLabelValues(provider, model, "input_cache_hit").Add(float64(completion.Usage.PromptCacheHitTokens))
	m.modelTokens.WithLabelValues(provider, model, "input_cache_miss").Add(float64(completion.Usage.PromptCacheMissTokens))
	m.modelTokens.WithLabelValues(provider, model, "output").Add(float64(completion.Usage.CompletionTokens))
}

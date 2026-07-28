package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"martie/internal/gateway"
)

const maxGatewayEventBytes = 1 << 20

type gatewayEventConsumer interface {
	ConsumeGatewayEvent(context.Context, gateway.WebhookEvent) error
}

type gatewayEventTarget struct {
	name     WorkerName
	consumer gatewayEventConsumer
}

type gatewayEventServer struct {
	cfg       GatewayWebhookConfig
	ptchan    PtchanConfig
	consumers []gatewayEventTarget
	logger    *slog.Logger
	metrics   *metrics
	nowFunc   func() time.Time
}

func (s gatewayEventServer) run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.Path, s.handleEvent)

	listener, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen for gateway events: %w", err)
	}

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errors := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errors <- err
		}
	}()

	s.logger.Info("gateway event server listening", "address", listener.Addr().String(), "path", s.cfg.Path, "consumers", len(s.consumers))
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown gateway event server: %w", err)
		}
		return nil
	case err := <-errors:
		return err
	}
}

func (s gatewayEventServer) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.metrics.observeGatewayWebhook("method_not_allowed")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(&io.LimitedReader{R: r.Body, N: maxGatewayEventBytes + 1})
	if err != nil {
		s.metrics.observeGatewayWebhook("bad_request")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if len(body) > maxGatewayEventBytes {
		s.metrics.observeGatewayWebhook("payload_too_large")
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}

	timestamp := r.Header.Get("x-ptchan-timestamp")
	eventID := r.Header.Get("x-ptchan-event-id")
	signature := r.Header.Get("x-ptchan-signature")
	event, err := gateway.VerifyWebhookBody(s.ptchan.Secret, eventID, timestamp, signature, body, s.nowFunc())
	if err != nil {
		s.logger.Warn("gateway webhook rejected", "error", err)
		s.metrics.observeGatewayWebhook("unauthorized")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	for _, target := range s.consumers {
		if err := target.consumer.ConsumeGatewayEvent(r.Context(), *event); err != nil {
			s.metrics.observeGatewayEvent(string(target.name), event.Kind, metricResultError)
			s.logger.Warn("gateway event failed", "event_id", event.EventID, "kind", event.Kind, "board", event.Post.Board, "thread_id", event.Post.ThreadID, "error", err)
			s.metrics.observeGatewayWebhook("consumer_error")
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		s.metrics.observeGatewayEvent(string(target.name), event.Kind, metricResultSuccess)
	}
	s.metrics.observeGatewayWebhook(metricResultSuccess)
	w.WriteHeader(http.StatusNoContent)
}

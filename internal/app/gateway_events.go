package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/loynet/ptchan-gateway/clients/go"
)

const (
	gatewayWebhookPath   = "/internal/ptchan/events"
	maxGatewayEventBytes = gateway.DefaultWebhookMaxBodyBytes
)

type gatewayEventServer struct {
	addr    string
	ptchan  PtchanConfig
	consume func(context.Context, gateway.WebhookEvent) error
	logger  *slog.Logger
	metrics *metrics
}

func (s gatewayEventServer) run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc(gatewayWebhookPath, func(w http.ResponseWriter, r *http.Request) {
		s.handleEvent(ctx, w, r)
	})

	listener, err := net.Listen("tcp", s.addr)
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

	s.logger.Info("gateway event server listening", "address", listener.Addr().String(), "path", gatewayWebhookPath)
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

func (s gatewayEventServer) handleEvent(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.metrics.observeGatewayWebhook("method_not_allowed")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxGatewayEventBytes))
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			s.metrics.observeGatewayWebhook("payload_too_large")
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		s.metrics.observeGatewayWebhook("bad_request")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	timestamp := r.Header.Get("x-ptchan-timestamp")
	eventID := r.Header.Get("x-ptchan-event-id")
	signature := r.Header.Get("x-ptchan-signature")
	event, err := gateway.VerifyWebhookBody(
		s.ptchan.Secret,
		eventID,
		timestamp,
		signature,
		body,
		gateway.WithWebhookMaxBodyBytes(maxGatewayEventBytes),
	)
	if err != nil {
		s.logger.Warn("gateway webhook rejected", "error", err)
		if errors.Is(err, gateway.ErrWebhookAuthentication) {
			s.metrics.observeGatewayWebhook("unauthorized")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		s.metrics.observeGatewayWebhook("invalid_event")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if err := s.consume(ctx, *event); err != nil {
		s.metrics.observeGatewayEvent(event.Kind, metricResultError)
		s.logger.Warn("gateway event failed", "event_id", event.EventID, "kind", event.Kind, "board", event.Post.Board, "thread_id", event.Post.ThreadID, "error", err)
		s.metrics.observeGatewayWebhook("consumer_error")
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	s.metrics.observeGatewayEvent(event.Kind, metricResultSuccess)
	s.metrics.observeGatewayWebhook(metricResultSuccess)
	w.WriteHeader(http.StatusNoContent)
}

package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/loynet/ptchan-ai/context/thread"
	"github.com/loynet/ptchan-ai/deepseek"
	"github.com/loynet/ptchan-gateway/clients/go"

	"martie/internal/channer"
	channerstate "martie/internal/channer/state"
	"martie/internal/storage"
)

type worker struct {
	name string
	run  func(context.Context) error
}

const (
	workerChanner       = "channer"
	workerGatewayEvents = "gateway_events"
)

func Run(ctx context.Context, cfg Config, db *storage.DB, logger *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	metrics := newMetrics()
	var ready atomic.Bool
	httpServer, httpErrors, err := startHTTPServer(cfg.Runtime.HTTPAddr, metrics, &ready, logger.With("worker", "http"))
	if err != nil {
		return err
	}
	defer ready.Store(false)
	defer shutdownMetricsServer(httpServer, logger)
	store, err := channerstate.New(db)
	if err != nil {
		return err
	}
	client, err := gateway.New(cfg.Ptchan.GatewayURL, gateway.Credentials{Name: cfg.Ptchan.IntegrationName, Secret: cfg.Ptchan.Secret})
	if err != nil {
		return fmt.Errorf("create ptchan gateway client: %w", err)
	}
	responder := channer.Responder{
		Config:    cfg.Channer,
		Store:     store,
		Completer: deepseek.New(cfg.DeepSeek.APIKey, cfg.DeepSeek.Model, cfg.DeepSeek.MaxTokens),
		ModelName: cfg.DeepSeek.Model,
		ThreadContext: thread.New(
			cfg.Channer.ThreadContext,
			client,
			logger.With("worker", workerChanner, "context", "ptchan"),
		),
		Poster: client,
		Limit: channer.NewLimiter(
			cfg.Channer.GlobalPerHour,
			cfg.Channer.GlobalBurst,
			cfg.Channer.ThreadPerHour,
			cfg.Channer.ThreadBurst,
		),
		Logger:  logger.With("worker", workerChanner),
		Metrics: metrics,
	}
	gatewayServer := gatewayEventServer{
		addr:    cfg.GatewayAddr,
		ptchan:  cfg.Ptchan,
		consume: responder.ConsumeGatewayEvent,
		logger:  logger.With("worker", workerGatewayEvents),
		metrics: metrics,
	}
	workers := []worker{{name: workerChanner, run: responder.Run}, {name: workerGatewayEvents, run: gatewayServer.run}}
	ready.Store(true)
	var group sync.WaitGroup
	group.Add(len(workers))
	for _, w := range workers {
		go func(w worker) { defer group.Done(); supervise(ctx, w, logger) }(w)
	}
	var runErr error
	select {
	case <-ctx.Done():
	case err := <-httpErrors:
		runErr = fmt.Errorf("http server: %w", err)
	}
	cancel()
	group.Wait()
	return runErr
}
func supervise(ctx context.Context, w worker, l *slog.Logger) {
	for {
		err := w.run(ctx)
		if ctx.Err() != nil {
			return
		}
		l.Error("worker stopped", "worker", w.name, "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
func startHTTPServer(addr string, m *metrics, ready *atomic.Bool, l *slog.Logger) (*http.Server, <-chan error, error) {
	if addr == "" {
		return nil, nil, nil
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	s := &http.Server{Handler: httpHandler(m, ready), ReadHeaderTimeout: 5 * time.Second}
	errs := make(chan error, 1)
	go func() {
		if err := s.Serve(ln); err != nil && err != http.ErrServerClosed {
			errs <- err
		}
	}()
	return s, errs, nil
}
func httpHandler(m *metrics, r *atomic.Bool) http.Handler {
	x := http.NewServeMux()
	x.HandleFunc("/healthz", healthHandler)
	x.HandleFunc("/readyz", readinessHandler(r))
	x.Handle("/metrics", m.handler())
	return x
}
func healthHandler(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok\n") }
func readinessHandler(r *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if r == nil || !r.Load() {
			http.Error(w, "not ready", 503)
			return
		}
		healthHandler(w, nil)
	}
}
func CheckHealth(addr string) error {
	if addr == "" {
		addr = "127.0.0.1:9090"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("health check status %s", resp.Status)
	}
	return nil
}
func shutdownMetricsServer(s *http.Server, l *slog.Logger) {
	if s == nil {
		return
	}
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	if err := s.Shutdown(ctx); err != nil {
		l.Warn("http shutdown failed", "error", err)
	}
}

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
	"time"

	"golang.org/x/time/rate"

	channerapp "martie/internal/apps/channer"
	channerstate "martie/internal/apps/channer/state"
	chatterapp "martie/internal/apps/chatter"
	chatterstate "martie/internal/apps/chatter/state"
	streamnotifierapp "martie/internal/apps/streamnotifier"
	"martie/internal/apps/streamnotifier/probe"
	streamnotifierstate "martie/internal/apps/streamnotifier/state"
	threadnotifierapp "martie/internal/apps/threadnotifier"
	threadnotifierstate "martie/internal/apps/threadnotifier/state"
	"martie/internal/assistant"
	"martie/internal/deepseek"
	"martie/internal/gateway"
	"martie/internal/localization"
	"martie/internal/storage"
	"martie/internal/telegram"
)

type worker struct {
	name WorkerName
	run  func(context.Context) error
}

func Run(
	ctx context.Context,
	cfg Config,
	db *storage.DB,
	streamClient *probe.Client,
	telegramClient *telegram.Client,
	logger *slog.Logger,
) error {
	metrics := newMetrics()
	server, serverErrors, err := startHTTPServer(cfg.Runtime.HTTPAddr, metrics, logger.With("worker", "http"))
	if err != nil {
		return err
	}
	defer shutdownMetricsServer(server, logger)
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	text := localization.New(cfg.Locale)

	var workers []worker
	var gatewayEventConsumers []gatewayEventTarget
	switch cfg.App {
	case AppStreamNotifier:
		store, err := streamnotifierstate.New(db)
		if err != nil {
			return err
		}
		streamNotifier := streamnotifierapp.Poller{
			Channels:         cfg.StreamNotifier.Channels,
			Format:           streamnotifierapp.NewFormatter(text),
			EndMissThreshold: cfg.StreamNotifier.EndMissThreshold,
			ChatID:           cfg.Telegram.NotificationChatID,
			Store:            store,
			Client:           streamClient,
			Telegram:         telegramClient,
			Metrics:          metrics,
			Logger:           logger.With("worker", workerStreamNotifier),
		}
		workers = append(workers, pollingWorker(workerStreamNotifier, cfg.StreamNotifier.PollInterval, streamNotifier.Poll, metrics, logger))
	case AppThreadNotifier:
		store, err := threadnotifierstate.New(db)
		if err != nil {
			return err
		}
		threadNotifier := &threadnotifierapp.Notifier{
			Config:   cfg.ThreadNotifier,
			Ptchan:   threadnotifierapp.PtchanConfig{BaseURL: cfg.Ptchan.BaseURL},
			Format:   threadnotifierapp.NewFormatter(text),
			ChatID:   cfg.Telegram.NotificationChatID,
			Store:    store,
			Telegram: telegramClient,
			Metrics:  metrics,
			Logger:   logger.With("worker", workerThreadNotifier),
		}
		threadNotifier.SetNowFunc(time.Now)
		gatewayEventConsumers = append(gatewayEventConsumers, gatewayEventTarget{name: workerThreadNotifier, consumer: threadNotifier})
		workers = append(workers, worker{name: workerThreadNotifier, run: threadNotifier.Run})
	case AppChatter:
		store, err := chatterstate.New(db)
		if err != nil {
			return err
		}
		completer := deepseek.New(cfg.DeepSeek.APIKey, cfg.DeepSeek.Model, cfg.DeepSeek.MaxTokens, cfg.DeepSeek.Timeout)
		chatter := chatterapp.New(cfg.Chatter, text, store, telegramClient, completer, metrics, logger.With("worker", workerChatter))
		chatter.SetModelName(cfg.DeepSeek.Model)
		chatter.SetTraces(assistant.NewTraceDumper(cfg.Chatter.Trace))
		var threadReader assistant.PtchanThreadReader
		if cfg.Chatter.PtchanContext.Enabled {
			client := gateway.NewClient(
				cfg.Chatter.PtchanContext.GatewayURL,
				gateway.Credentials{Name: cfg.Ptchan.IntegrationName, Secret: cfg.Ptchan.Secret},
				cfg.Chatter.PtchanContext.Timeout,
			)
			checkCtx, cancel := context.WithTimeout(ctx, cfg.Chatter.PtchanContext.Timeout)
			if err := client.CheckReachable(checkCtx); err != nil {
				logger.Warn("ptchan gateway thread reader unreachable", "worker", workerChatter, "gateway_url", cfg.Chatter.PtchanContext.GatewayURL, "error", err)
			}
			cancel()
			threadReader = client
		}
		chatter.SetPtchanContext(assistant.NewPtchanContext(cfg.Chatter.PtchanContext, threadReader, logger.With("worker", workerChatter, "context", "ptchan")))
		workers = append(workers, worker{name: workerChatter, run: chatter.Run})
	case AppChanner:
		store, err := channerstate.New(db)
		if err != nil {
			return err
		}
		completer := deepseek.New(cfg.DeepSeek.APIKey, cfg.DeepSeek.Model, cfg.DeepSeek.MaxTokens, cfg.DeepSeek.Timeout)
		client := gateway.NewClient(
			cfg.Ptchan.GatewayURL,
			gateway.Credentials{Name: cfg.Ptchan.IntegrationName, Secret: cfg.Ptchan.Secret},
			cfg.Channer.PtchanContext.Timeout,
		)
		var threadReader assistant.PtchanThreadReader
		if cfg.Channer.PtchanContext.Enabled {
			contextClient := gateway.NewClient(
				cfg.Channer.PtchanContext.GatewayURL,
				gateway.Credentials{Name: cfg.Ptchan.IntegrationName, Secret: cfg.Ptchan.Secret},
				cfg.Channer.PtchanContext.Timeout,
			)
			checkCtx, cancel := context.WithTimeout(ctx, cfg.Channer.PtchanContext.Timeout)
			if err := contextClient.CheckReachable(checkCtx); err != nil {
				logger.Warn("ptchan gateway thread reader unreachable", "worker", workerChanner, "gateway_url", cfg.Channer.PtchanContext.GatewayURL, "error", err)
			}
			cancel()
			threadReader = contextClient
		}
		ptchanResponder := channerapp.Responder{
			Config:    cfg.Channer,
			Store:     store,
			Completer: completer,
			ModelName: cfg.DeepSeek.Model,
			Ptchan:    assistant.NewPtchanContext(cfg.Channer.PtchanContext, threadReader, logger.With("worker", workerChanner, "context", "ptchan")),
			Poster:    client,
			Traces:    assistant.NewTraceDumper(cfg.Channer.Trace),
			Limit:     newRateLimiter(cfg.Channer.RequestLimit, cfg.Channer.RateLimitWindow, cfg.Channer.RequestBurst),
			Logger:    logger.With("worker", workerChanner),
			Metrics:   metrics,
		}
		ptchanResponder.SetNowFunc(time.Now)
		gatewayEventConsumers = append(gatewayEventConsumers, gatewayEventTarget{name: workerChanner, consumer: ptchanResponder})
		workers = append(workers, worker{name: workerChanner, run: ptchanResponder.Run})
	default:
		return fmt.Errorf("unknown app %q", cfg.App)
	}
	if len(gatewayEventConsumers) > 0 {
		server := gatewayEventServer{
			cfg:       cfg.Gateway.Webhook,
			ptchan:    cfg.Ptchan,
			consumers: gatewayEventConsumers,
			logger:    logger.With("worker", workerGatewayEvents),
			metrics:   metrics,
			nowFunc:   time.Now,
		}
		workers = append(workers, worker{name: workerGatewayEvents, run: server.run})
	}
	logger.Info("service starting", "app", cfg.App)

	var group sync.WaitGroup
	group.Add(len(workers))
	for _, worker := range workers {
		go func() {
			defer group.Done()
			supervise(runCtx, worker, logger)
		}()
	}

	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		stop()
		group.Wait()
		return fmt.Errorf("http server: %w", err)
	}
	stop()
	group.Wait()
	logger.Info("shutdown requested")
	return nil
}

func pollingWorker(name WorkerName, interval time.Duration, poll func(context.Context) error, metrics *metrics, logger *slog.Logger) worker {
	return worker{
		name: name,
		run: func(ctx context.Context) error {
			for {
				startedAt := time.Now()
				err := poll(ctx)
				metrics.observeWorkerRun(string(name), time.Since(startedAt), err)
				if err != nil && ctx.Err() == nil {
					logger.Warn("poll failed", "worker", name, "error", err)
				}

				timer := time.NewTimer(interval)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil
				case <-timer.C:
				}
			}
		},
	}
}

func newRateLimiter(requests int, window time.Duration, burst int) *rate.Limiter {
	refill := rate.Limit(float64(requests) / window.Seconds())
	return rate.NewLimiter(refill, burst)
}

func supervise(ctx context.Context, worker worker, logger *slog.Logger) {
	for {
		err := worker.run(ctx)
		if ctx.Err() != nil {
			return
		}
		logger.Error("worker stopped", "worker", worker.name, "error", err)

		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func startHTTPServer(addr string, metrics *metrics, logger *slog.Logger) (*http.Server, <-chan error, error) {
	if addr == "" {
		return nil, nil, nil
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("listen for http: %w", err)
	}

	server := &http.Server{
		Handler:           httpHandler(metrics),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errors := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errors <- err
		}
	}()

	logger.Info("http listening", "address", listener.Addr().String())
	return server, errors, nil
}

func httpHandler(metrics *metrics) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/readyz", healthHandler)
	mux.Handle("/metrics", metrics.handler())
	return mux
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

func CheckHealth(addr string) error {
	if addr == "" {
		addr = "127.0.0.1:9090"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		return fmt.Errorf("send health check: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check status %s", resp.Status)
	}
	return nil
}

func shutdownMetricsServer(server *http.Server, logger *slog.Logger) {
	if server == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Warn("metrics server shutdown failed", "error", err)
	}
}

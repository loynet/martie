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

	"martie/internal/deepseek"
	"martie/internal/gateway"
	"martie/internal/localization"
	"martie/internal/miau"
	"martie/internal/state"
	"martie/internal/telegram"
)

type component struct {
	name ComponentName
	run  func(context.Context) error
}

const ptchanGatewayContextLimit = 50

func Run(
	ctx context.Context,
	cfg Config,
	store *state.Store,
	streamClient *miau.Client,
	telegramClient *telegram.Client,
	logger *slog.Logger,
) error {
	metrics := newMetrics()
	server, serverErrors, err := startHTTPServer(cfg.Runtime.HTTPAddr, metrics, logger.With("component", "http"))
	if err != nil {
		return err
	}
	defer shutdownMetricsServer(server, logger)
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	text := localization.New(cfg.Locale)
	formatter := telegram.NewFormatter(text)

	var components []component
	if cfg.runs(componentStreams) {
		streams := streamPoller{
			channels:         cfg.Streams.Channels,
			format:           formatter,
			endMissThreshold: cfg.Streams.EndMissThreshold,
			chatID:           cfg.Telegram.NotificationChatID,
			store:            store,
			client:           streamClient,
			telegram:         telegramClient,
			metrics:          metrics,
			logger:           logger.With("component", componentStreams),
		}
		components = append(components, pollingComponent(componentStreams, cfg.Streams.PollInterval, streams.poll, metrics, logger))
	}
	if cfg.runs(componentGateway) {
		gateway := gatewayConsumer{
			cfg:      cfg.Gateway,
			ptchan:   cfg.Ptchan,
			format:   formatter,
			chatID:   cfg.Telegram.NotificationChatID,
			store:    store,
			telegram: telegramClient,
			metrics:  metrics,
			logger:   logger.With("component", componentGateway),
			nowFunc:  time.Now,
		}
		components = append(components, component{name: componentGateway, run: gateway.run})
	}
	if cfg.runs(componentAssistant) {
		completer := deepseek.New(cfg.DeepSeek.APIKey, cfg.DeepSeek.Model, cfg.DeepSeek.MaxTokens, cfg.DeepSeek.Timeout)
		assistant := newAssistant(cfg.Assistant, text, store, telegramClient, completer, metrics, logger.With("component", componentAssistant))
		assistant.traces = newAssistantTraceDumper(cfg.Assistant.Trace)
		var contextClient ptchanThreadFetcher
		if cfg.Assistant.PtchanContext.Enabled {
			client := gateway.NewContextClient(
				cfg.Assistant.PtchanContext.GatewayURL,
				cfg.Ptchan.IntegrationName,
				cfg.Ptchan.Secret,
				cfg.Assistant.PtchanContext.Timeout,
				ptchanGatewayContextLimit,
			)
			checkCtx, cancel := context.WithTimeout(ctx, cfg.Assistant.PtchanContext.Timeout)
			if err := client.CheckReachable(checkCtx); err != nil {
				logger.Warn("ptchan gateway context unreachable", "component", componentAssistant, "gateway_url", cfg.Assistant.PtchanContext.GatewayURL, "error", err)
			}
			cancel()
			contextClient = client
		}
		assistant.ptchan = newPtchanContextSource(cfg.Assistant.PtchanContext, contextClient, logger.With("component", componentAssistant, "context", "ptchan"))
		components = append(components, component{name: componentAssistant, run: assistant.run})
	}
	logger.Info("service starting", "components", cfg.Runtime.Components)

	var workers sync.WaitGroup
	workers.Add(len(components))
	for _, component := range components {
		go func() {
			defer workers.Done()
			supervise(runCtx, component, logger)
		}()
	}

	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		stop()
		workers.Wait()
		return fmt.Errorf("http server: %w", err)
	}
	stop()
	workers.Wait()
	logger.Info("shutdown requested")
	return nil
}

func pollingComponent(name ComponentName, interval time.Duration, poll func(context.Context) error, metrics *metrics, logger *slog.Logger) component {
	return component{
		name: name,
		run: func(ctx context.Context) error {
			for {
				startedAt := time.Now()
				err := poll(ctx)
				metrics.observeWorkflow(string(name), time.Since(startedAt), err)
				if err != nil && ctx.Err() == nil {
					logger.Warn("poll failed", "component", name, "error", err)
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

func supervise(ctx context.Context, component component, logger *slog.Logger) {
	for {
		err := component.run(ctx)
		if ctx.Err() != nil {
			return
		}
		logger.Error("component stopped", "component", component.name, "error", err)

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

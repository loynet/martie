package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"martie/internal/deepseek"
	"martie/internal/gateway"
	"martie/internal/localization"
	"martie/internal/miau"
	"martie/internal/state"
	"martie/internal/telegram"
)

type streamPoller struct {
	channels         []miau.Channel
	format           telegram.Formatter
	endMissThreshold int
	chatID           int64
	store            streamStore
	client           streamClient
	telegram         messageSender
	metrics          *metrics
	logger           *slog.Logger
}

type streamClient interface {
	IsLive(context.Context, miau.Channel) (bool, error)
}

type streamStore interface {
	GetStreamState(context.Context, string) (state.StreamState, bool, error)
	UpsertStreamState(context.Context, state.StreamState) error
}

type messageSender interface {
	Send(context.Context, telegram.SendRequest) error
}

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
	server, serverErrors, err := startMetricsServer(cfg.Runtime.MetricsAddr, metrics, logger.With("component", "metrics"))
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
			cfg:       cfg.Gateway,
			format:    formatter,
			chatID:    cfg.Telegram.NotificationChatID,
			store:     store,
			telegram:  telegramClient,
			metrics:   metrics,
			logger:    logger.With("component", componentGateway),
			nowFunc:   time.Now,
			consumeMu: &sync.Mutex{},
		}
		components = append(components, component{name: componentGateway, run: gateway.run})
	}
	if cfg.runs(componentAssistant) {
		completer := deepseek.New(cfg.DeepSeek.APIKey, cfg.DeepSeek.Model, cfg.DeepSeek.MaxTokens, cfg.DeepSeek.Timeout)
		assistant := newAssistant(cfg.Assistant, text, store, telegramClient, completer, metrics, logger.With("component", componentAssistant))
		assistant.traces = newAssistantTraceDumper(cfg.Assistant.Trace)
		var contextClient ptchanThreadFetcher
		if cfg.Assistant.PtchanContext.Enabled {
			contextClient = gateway.NewContextClient(
				cfg.Assistant.PtchanContext.GatewayURL,
				cfg.Gateway.ConsumerName,
				cfg.Gateway.Secret,
				cfg.Assistant.PtchanContext.Timeout,
				ptchanGatewayContextLimit,
			)
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
		return fmt.Errorf("metrics server: %w", err)
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

func startMetricsServer(addr string, metrics *metrics, logger *slog.Logger) (*http.Server, <-chan error, error) {
	if addr == "" {
		return nil, nil, nil
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("listen for metrics: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.handler())

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

	logger.Info("metrics listening", "address", listener.Addr().String())
	return server, errors, nil
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

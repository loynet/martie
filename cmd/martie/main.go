package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"martie/internal/app"
	"martie/internal/apps/streamnotifier/probe"
	"martie/internal/storage"
	"martie/internal/telegram"
)

func main() {
	command, appName, err := parseCommand(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	if command == "check-health" {
		if err := app.CheckHealth(os.Getenv("HEALTHCHECK_ADDR")); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg, err = cfg.ForApp(appName)
	if err != nil {
		log.Fatalf("select app: %v", err)
	}
	logger := newLogger(cfg.Runtime.Logging)

	if command == "run" || command == "check-config" {
		if err := cfg.ValidateRun(); err != nil {
			logger.Error("load config", "error", err)
			os.Exit(1)
		}
	}
	if command == "check-config" {
		return
	}

	store, err := storage.Open(cfg.Storage.SQLitePath)
	if err != nil {
		logger.Error("open sqlite", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch command {
	case "run":
		if err := app.Run(ctx, cfg, store, probe.New(), telegram.New(cfg.Telegram.BotToken, logger), logger); err != nil {
			logger.Error("run service", "error", err)
			os.Exit(1)
		}
	default:
		log.Fatalf("unsupported command: %s", command)
	}
}

func newLogger(cfg app.LoggingConfig) *slog.Logger {
	options := &slog.HandlerOptions{Level: cfg.Level}
	if cfg.Format == app.LogJSON {
		return slog.New(slog.NewJSONHandler(os.Stdout, options))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, options))
}

func parseCommand(args []string) (string, app.AppName, error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("usage: martie [chatter|channer|threadnotifier|streamnotifier|check-config|check-health]")
	}

	switch args[0] {
	case "chatter", "channer", "threadnotifier", "streamnotifier":
		appName, err := app.ParseAppName(args[0])
		if err != nil {
			return "", "", err
		}
		if len(args) > 1 {
			return "", "", fmt.Errorf("usage: martie [chatter|channer|threadnotifier|streamnotifier|check-config|check-health]")
		}
		return "run", appName, nil
	case "check-config":
		if len(args) != 2 {
			return "", "", fmt.Errorf("usage: martie check-config [chatter|channer|threadnotifier|streamnotifier]")
		}
		appName, err := app.ParseAppName(args[1])
		if err != nil {
			return "", "", err
		}
		return "check-config", appName, nil
	case "check-health":
		if len(args) > 1 {
			return "", "", fmt.Errorf("usage: martie check-health")
		}
		return "check-health", "", nil
	default:
		return "", "", fmt.Errorf("usage: martie [chatter|channer|threadnotifier|streamnotifier|check-config|check-health]")
	}
}

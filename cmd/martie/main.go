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
	"martie/internal/storage"
)

func main() {
	cmd, err := parseCommand(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	if cmd == "check-health" {
		if err := app.CheckHealth(os.Getenv("HEALTHCHECK_ADDR")); err != nil {
			log.Fatal(err)
		}
		return
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if cmd == "check-config" {
		return
	}
	db, err := storage.Open(cfg.SQLitePath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, cfg, db, newLogger(cfg.Runtime.LogLevel)); err != nil {
		log.Fatal(err)
	}
}

func newLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

func parseCommand(a []string) (string, error) {
	if len(a) != 1 {
		return "", fmt.Errorf("usage: martie [run|check-config|check-health]")
	}
	switch a[0] {
	case "run", "check-config", "check-health":
		return a[0], nil
	}
	return "", fmt.Errorf("usage: martie [run|check-config|check-health]")
}

package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"martie/internal/gateway"
	"martie/internal/ptchan"
	"martie/internal/state"
	"martie/internal/telegram"
)

const maxGatewayEventBytes = 1 << 20

type gatewayConsumer struct {
	cfg         GatewayConfig
	format      telegram.Formatter
	chatID      int64
	store       *state.Store
	telegram    messageSender
	metrics     *metrics
	logger      *slog.Logger
	nowFunc     func() time.Time
	bootstrapAt time.Time
	consumeMu   sync.Mutex
}

func (g *gatewayConsumer) run(ctx context.Context) error {
	bootstrapAt, err := g.store.EnsureGatewayBootstrapAt(ctx, g.nowFunc())
	if err != nil {
		return fmt.Errorf("load gateway bootstrap watermark: %w", err)
	}
	g.bootstrapAt = bootstrapAt

	mux := http.NewServeMux()
	mux.HandleFunc(g.cfg.Path, g.handleEvent)

	listener, err := net.Listen("tcp", g.cfg.Addr)
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

	workerCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()

	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		g.deliverNotifications(workerCtx)
	}()

	g.logger.Info("gateway consumer listening", "address", listener.Addr().String(), "path", g.cfg.Path)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := server.Shutdown(shutdownCtx)
		stopWorkers()
		workers.Wait()
		if err != nil {
			return fmt.Errorf("shutdown gateway consumer: %w", err)
		}
		return nil
	case err := <-errors:
		stopWorkers()
		workers.Wait()
		return err
	}
}

func (g *gatewayConsumer) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(&io.LimitedReader{R: r.Body, N: maxGatewayEventBytes + 1})
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if len(body) > maxGatewayEventBytes {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}

	timestamp := r.Header.Get("x-ptchan-timestamp")
	eventID := r.Header.Get("x-ptchan-event-id")
	signature := r.Header.Get("x-ptchan-signature")
	if err := gateway.VerifyWebhook(g.cfg.Secret, timestamp, signature, body, g.nowFunc()); err != nil {
		g.logger.Warn("gateway webhook rejected", "error", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	event, err := gateway.DecodeEvent(body)
	if err != nil {
		g.logger.Warn("gateway event rejected", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if eventID != "" && eventID != event.EventID {
		g.logger.Warn("gateway event id mismatch", "event_id", event.EventID)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if err := g.consumeEvent(r.Context(), event); err != nil {
		g.logger.Warn("gateway event failed", "event_id", event.EventID, "kind", event.Kind, "board", event.Post.Board, "thread_id", event.Post.ThreadID, "error", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (g *gatewayConsumer) consumeEvent(ctx context.Context, event gateway.Event) error {
	g.consumeMu.Lock()
	defer g.consumeMu.Unlock()

	now := g.nowFunc().UTC()
	record, found, err := g.store.GetThread(ctx, gatewayThreadKey(event.Post.Board, event.Post.ThreadID))
	if err != nil {
		return fmt.Errorf("load gateway thread: %w", err)
	}

	record = g.threadRecordForEvent(record, found, event, now)
	notification := g.notificationForEvent(record, event, now)
	if notification != nil {
		record.NotifiedNewAt = &now
	}

	queued, err := g.store.StoreGatewayEvent(ctx, event.EventID, record, notification, now)
	if err != nil {
		return err
	}
	if queued {
		g.logger.Debug("gateway notification queued", "thread", record.ThreadID)
	}
	return nil
}

func (g *gatewayConsumer) threadRecordForEvent(record state.ThreadRecord, found bool, event gateway.Event, now time.Time) state.ThreadRecord {
	postTime := event.Post.Date
	if postTime.IsZero() {
		postTime = now
	}
	if !found {
		record = state.ThreadRecord{
			ThreadID:     gatewayThreadKey(event.Post.Board, event.Post.ThreadID),
			Board:        event.Post.Board,
			PostID:       event.Post.ThreadID,
			CreatedAt:    postTime,
			LastBumpedAt: postTime,
			LastSeenAt:   now,
		}
	}

	record.Board = event.Post.Board
	record.PostID = event.Post.ThreadID
	if record.CreatedAt.IsZero() || event.Kind == gateway.KindThreadCreated {
		record.CreatedAt = postTime
	}
	if event.Kind == gateway.KindThreadCreated || postTime.After(record.LastBumpedAt) {
		record.LastBumpedAt = postTime
	}
	record.LastSeenAt = now
	switch event.Kind {
	case gateway.KindThreadCreated:
		record.Ignored = !g.cfg.Filter.Allows(gatewayFilterThread(event), now)
		record.HasOP = true
		if event.Post.AttachmentCount > record.ReplyFiles {
			record.ReplyFiles = event.Post.AttachmentCount
		}
	case gateway.KindPostCreated:
		record.ReplyPosts++
		record.ReplyFiles += event.Post.AttachmentCount
	}

	return record
}

func (g *gatewayConsumer) notificationForEvent(record state.ThreadRecord, event gateway.Event, now time.Time) *state.GatewayNotification {
	if !record.HasOP || record.Ignored || record.NotifiedNewAt != nil || record.ReplyPosts < g.cfg.MinReplyPosts || !g.shouldNotify(event) {
		return nil
	}

	message := g.format.ThreadNotification(g.cfg.BaseURL, telegram.ThreadNotice{
		Board:      record.Board,
		PostID:     record.PostID,
		Date:       record.CreatedAt,
		ReplyPosts: record.ReplyPosts,
		ReplyFiles: record.ReplyFiles,
	}, g.cfg.MinReplyPosts, now)
	return &state.GatewayNotification{
		ThreadID:  record.ThreadID,
		ChatID:    g.chatID,
		Text:      message.Text(),
		ParseMode: message.ParseMode(),
	}
}

func (g *gatewayConsumer) shouldNotify(event gateway.Event) bool {
	if g.bootstrapAt.IsZero() || event.ObservedAt.IsZero() {
		return true
	}
	return !event.ObservedAt.Before(g.bootstrapAt)
}

func (g *gatewayConsumer) deliverNotifications(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	cleanup := time.NewTicker(time.Hour)
	defer cleanup.Stop()
	g.prune(ctx)
	for {
		g.deliverPendingNotifications(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-cleanup.C:
			g.prune(ctx)
		}
	}
}

func (g *gatewayConsumer) deliverPendingNotifications(ctx context.Context) {
	notifications, err := g.store.PendingGatewayNotifications(ctx, 10, g.nowFunc())
	if err != nil {
		g.logger.Warn("load gateway notifications failed", "error", err)
		return
	}
	for _, notification := range notifications {
		message := telegram.TextMessage(notification.Text)
		if notification.ParseMode == "Markdown" {
			message = telegram.MarkdownMessage(notification.Text)
		}
		now := g.nowFunc()
		if err := g.telegram.Send(ctx, telegram.SendRequest{ChatID: notification.ChatID, Message: message}); err == nil {
			if err := g.store.MarkGatewayNotificationSent(ctx, notification.ID, now); err != nil {
				g.logger.Warn("mark gateway notification sent failed", "id", notification.ID, "error", err)
				continue
			}
			g.metrics.addNotifications(string(componentGateway), 1)
			continue
		}

		attempts := notification.Attempts + 1
		next := now.Add(gatewayNotificationBackoff(attempts))
		if err := g.store.MarkGatewayNotificationFailed(ctx, notification.ID, attempts, next); err != nil {
			g.logger.Warn("mark gateway notification failed", "id", notification.ID, "error", err)
		}
		g.logger.Warn("gateway notification delivery failed", "id", notification.ID, "attempts", attempts, "error", err)
	}
}

func gatewayNotificationBackoff(attempts int) time.Duration {
	if attempts <= 0 {
		return time.Second
	}
	return time.Duration(1<<min(attempts-1, 6)) * time.Second
}

func (g *gatewayConsumer) prune(ctx context.Context) {
	if g.cfg.PruneAfter == 0 {
		return
	}
	cutoff := g.nowFunc().UTC().Add(-g.cfg.PruneAfter)
	count, err := g.store.PruneGatewayBefore(ctx, cutoff)
	if err != nil {
		g.logger.Warn("gateway prune failed", "error", err)
		return
	}
	if count > 0 {
		g.logger.Info("gateway data pruned", "count", count, "cutoff", cutoff.Format(time.RFC3339))
	}
}

func gatewayThreadKey(board string, threadID int64) string {
	return board + ":" + strconv.FormatInt(threadID, 10)
}

func gatewayFilterThread(event gateway.Event) ptchan.Thread {
	return ptchan.Thread{
		Date:    event.Post.Date,
		Board:   event.Post.Board,
		Subject: event.Post.Subject,
		Message: strings.TrimSpace(event.Post.Message),
		PostID:  event.Post.ThreadID,
	}
}

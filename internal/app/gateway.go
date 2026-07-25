package app

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"martie/internal/gateway"
	"martie/internal/ptchan"
	"martie/internal/state"
	"martie/internal/telegram"
)

type gatewayNotifier struct {
	cfg          GatewayConfig
	ptchan       PtchanConfig
	format       telegram.Formatter
	chatID       int64
	store        *state.Store
	telegram     messageSender
	metrics      *metrics
	logger       *slog.Logger
	nowFunc      func() time.Time
	bootstrapAt  time.Time
	bootstrapped bool
	consumeMu    sync.Mutex
}

func (g *gatewayNotifier) loadBootstrap(ctx context.Context) error {
	bootstrapAt, err := g.store.EnsureGatewayBootstrapAt(ctx, g.nowFunc())
	if err != nil {
		return fmt.Errorf("load gateway bootstrap watermark: %w", err)
	}
	g.consumeMu.Lock()
	defer g.consumeMu.Unlock()
	g.bootstrapAt = bootstrapAt
	g.bootstrapped = true
	return nil
}

func (g *gatewayNotifier) run(ctx context.Context) error {
	if err := g.loadBootstrap(ctx); err != nil {
		return err
	}
	g.deliverNotifications(ctx)
	return nil
}

func (g *gatewayNotifier) consumeGatewayEvent(ctx context.Context, event gateway.Event) error {
	g.consumeMu.Lock()
	defer g.consumeMu.Unlock()
	if !g.bootstrapped {
		return fmt.Errorf("gateway notifier not ready")
	}

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

func (g *gatewayNotifier) threadRecordForEvent(record state.ThreadRecord, found bool, event gateway.Event, now time.Time) state.ThreadRecord {
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
		record.Ignored = !g.cfg.Notifications.Filter.Allows(gatewayFilterThread(event), now)
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

func (g *gatewayNotifier) notificationForEvent(record state.ThreadRecord, event gateway.Event, now time.Time) *state.GatewayNotification {
	if !record.HasOP || record.Ignored || record.NotifiedNewAt != nil || record.ReplyPosts < g.cfg.Notifications.MinReplyPosts || !g.shouldNotify(event) {
		return nil
	}

	message := g.format.ThreadNotification(g.ptchan.BaseURL, telegram.ThreadNotice{
		Board:      record.Board,
		PostID:     record.PostID,
		Date:       record.CreatedAt,
		ReplyPosts: record.ReplyPosts,
		ReplyFiles: record.ReplyFiles,
	}, g.cfg.Notifications.MinReplyPosts, now)
	return &state.GatewayNotification{
		ThreadID:  record.ThreadID,
		ChatID:    g.chatID,
		Text:      message.Text(),
		ParseMode: message.ParseMode(),
	}
}

func (g *gatewayNotifier) shouldNotify(event gateway.Event) bool {
	if g.bootstrapAt.IsZero() || event.ObservedAt.IsZero() {
		return true
	}
	return !event.ObservedAt.Before(g.bootstrapAt)
}

func (g *gatewayNotifier) deliverNotifications(ctx context.Context) {
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

func (g *gatewayNotifier) deliverPendingNotifications(ctx context.Context) {
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
			g.metrics.observeNotification(string(componentGateway), notificationSent)
			continue
		}

		attempts := notification.Attempts + 1
		next := now.Add(gatewayNotificationBackoff(attempts))
		g.metrics.observeNotification(string(componentGateway), notificationFailed)
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

func (g *gatewayNotifier) prune(ctx context.Context) {
	if g.cfg.Notifications.PruneAfter == 0 {
		return
	}
	cutoff := g.nowFunc().UTC().Add(-g.cfg.Notifications.PruneAfter)
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

package threadnotifier

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	threadnotifierstate "martie/internal/apps/threadnotifier/state"
	"martie/internal/gateway"
	"martie/internal/telegram"
)

const (
	Source             = "threadnotifier"
	NotificationSent   = "sent"
	NotificationFailed = "failed"
)

type Config struct {
	MinReplyPosts int
	Filter        Filter
	PruneAfter    time.Duration
}

type Sender interface {
	Send(context.Context, telegram.SendRequest) error
}

type Metrics interface {
	ObserveNotification(source, result string)
}

type Notifier struct {
	Config       Config
	BaseURL      string
	Format       Formatter
	ChatID       int64
	Store        *threadnotifierstate.Store
	Telegram     Sender
	Metrics      Metrics
	Logger       *slog.Logger
	nowFunc      func() time.Time
	bootstrapAt  time.Time
	bootstrapped bool
	consumeMu    sync.Mutex
}

func (g *Notifier) SetNowFunc(nowFunc func() time.Time) {
	g.nowFunc = nowFunc
}

func (g *Notifier) Initialize(ctx context.Context) error {
	bootstrapAt, err := g.Store.EnsureBootstrapAt(ctx, g.now())
	if err != nil {
		return fmt.Errorf("load threadnotifier bootstrap watermark: %w", err)
	}
	g.consumeMu.Lock()
	defer g.consumeMu.Unlock()
	g.bootstrapAt = bootstrapAt
	g.bootstrapped = true
	return nil
}

func (g *Notifier) Run(ctx context.Context) error {
	g.deliverNotifications(ctx)
	return nil
}

func (g *Notifier) ConsumeGatewayEvent(ctx context.Context, event gateway.WebhookEvent) error {
	if event.Kind != gateway.ThreadCreated && event.Kind != gateway.PostCreated {
		g.Logger.Debug("threadnotifier event ignored", "event_id", event.EventID, "kind", event.Kind)
		return nil
	}
	g.consumeMu.Lock()
	defer g.consumeMu.Unlock()
	if !g.bootstrapped {
		return fmt.Errorf("threadnotifier not ready")
	}

	now := g.now().UTC()
	ref := event.Post.ThreadRef()
	record, found, err := g.Store.GetThread(ctx, gatewayThreadKey(ref))
	if err != nil {
		return fmt.Errorf("load threadnotifier thread: %w", err)
	}

	record = g.threadRecordForEvent(record, found, event, now)
	notification := g.notificationForEvent(record, event, now)
	if notification != nil {
		record.NotifiedNewAt = &now
	}

	queued, err := g.Store.StoreEvent(ctx, event.EventID, record, notification, now)
	if err != nil {
		return err
	}
	if queued {
		g.Logger.Debug("threadnotifier notification queued", "thread", record.ThreadID)
	}
	return nil
}

func (g *Notifier) threadRecordForEvent(record threadnotifierstate.Thread, found bool, event gateway.WebhookEvent, now time.Time) threadnotifierstate.Thread {
	postTime := event.Post.Date
	if postTime.IsZero() {
		postTime = now
	}
	if !found {
		record = threadnotifierstate.Thread{
			ThreadID:     gatewayThreadKey(event.Post.ThreadRef()),
			Board:        event.Post.Board,
			PostID:       event.Post.ThreadID,
			CreatedAt:    postTime,
			LastBumpedAt: postTime,
			LastSeenAt:   now,
		}
	}

	record.Board = event.Post.Board
	record.PostID = event.Post.ThreadID
	if record.CreatedAt.IsZero() || event.Kind == gateway.ThreadCreated {
		record.CreatedAt = postTime
	}
	if event.Kind == gateway.ThreadCreated || postTime.After(record.LastBumpedAt) {
		record.LastBumpedAt = postTime
	}
	record.LastSeenAt = now
	switch event.Kind {
	case gateway.ThreadCreated:
		record.Ignored = !g.Config.Filter.allows(gatewayFilterThread(event), now)
		record.HasOP = true
		if event.Post.AttachmentCount > record.ReplyFiles {
			record.ReplyFiles = event.Post.AttachmentCount
		}
	case gateway.PostCreated:
		record.ReplyPosts++
		record.ReplyFiles += event.Post.AttachmentCount
	}

	return record
}

func (g *Notifier) notificationForEvent(record threadnotifierstate.Thread, event gateway.WebhookEvent, now time.Time) *threadnotifierstate.Notification {
	if !record.HasOP || record.Ignored || record.NotifiedNewAt != nil || record.ReplyPosts < g.Config.MinReplyPosts || !g.shouldNotify(event) {
		return nil
	}

	message := g.Format.ThreadNotification(g.BaseURL, ThreadNotice{
		Board:      record.Board,
		PostID:     record.PostID,
		Date:       record.CreatedAt,
		ReplyPosts: record.ReplyPosts,
		ReplyFiles: record.ReplyFiles,
	}, g.Config.MinReplyPosts, now)
	return &threadnotifierstate.Notification{
		ThreadID:  record.ThreadID,
		ChatID:    g.ChatID,
		Text:      message.Text(),
		ParseMode: message.ParseMode(),
	}
}

func (g *Notifier) shouldNotify(event gateway.WebhookEvent) bool {
	if g.bootstrapAt.IsZero() || event.ObservedAt.IsZero() {
		return true
	}
	return !event.ObservedAt.Before(g.bootstrapAt)
}

func (g *Notifier) deliverNotifications(ctx context.Context) {
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

func (g *Notifier) deliverPendingNotifications(ctx context.Context) {
	notifications, err := g.Store.PendingNotifications(ctx, 10, g.now())
	if err != nil {
		g.Logger.Warn("load threadnotifier notifications failed", "error", err)
		return
	}
	for _, notification := range notifications {
		message := telegram.TextMessage(notification.Text)
		if notification.ParseMode == "Markdown" {
			message = telegram.MarkdownMessage(notification.Text)
		}
		now := g.now()
		if err := g.Telegram.Send(ctx, telegram.SendRequest{ChatID: notification.ChatID, Message: message}); err == nil {
			g.Metrics.ObserveNotification(Source, NotificationSent)
			if err := g.Store.MarkNotificationSent(ctx, notification.ID, now); err != nil {
				g.Logger.Warn("mark threadnotifier notification sent failed", "id", notification.ID, "error", err)
				continue
			}
			continue
		}

		attempts := notification.Attempts + 1
		next := now.Add(gatewayNotificationBackoff(attempts))
		g.Metrics.ObserveNotification(Source, NotificationFailed)
		if err := g.Store.MarkNotificationFailed(ctx, notification.ID, attempts, next); err != nil {
			g.Logger.Warn("mark threadnotifier notification failed", "id", notification.ID, "error", err)
		}
		g.Logger.Warn("threadnotifier notification delivery failed", "id", notification.ID, "attempts", attempts, "error", err)
	}
}

func gatewayNotificationBackoff(attempts int) time.Duration {
	if attempts <= 0 {
		return time.Second
	}
	return time.Duration(1<<min(attempts-1, 6)) * time.Second
}

func (g *Notifier) prune(ctx context.Context) {
	if g.Config.PruneAfter == 0 {
		return
	}
	cutoff := g.now().UTC().Add(-g.Config.PruneAfter)
	count, err := g.Store.PruneBefore(ctx, cutoff)
	if err != nil {
		g.Logger.Warn("threadnotifier prune failed", "error", err)
		return
	}
	if count > 0 {
		g.Logger.Info("threadnotifier data pruned", "count", count, "cutoff", cutoff.Format(time.RFC3339))
	}
}

func (g *Notifier) now() time.Time {
	if g.nowFunc != nil {
		return g.nowFunc()
	}
	return time.Now()
}

func gatewayThreadKey(ref gateway.ThreadRef) string {
	return ref.Board + ":" + strconv.FormatInt(ref.ThreadID, 10)
}

func gatewayFilterThread(event gateway.WebhookEvent) filterThread {
	return filterThread{
		Date:    event.Post.Date,
		Board:   event.Post.Board,
		Subject: event.Post.Subject,
		Message: strings.TrimSpace(event.Post.Message),
		PostID:  event.Post.ThreadID,
	}
}

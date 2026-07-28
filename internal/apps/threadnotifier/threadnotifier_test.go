package threadnotifier

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	threadnotifierstate "martie/internal/apps/threadnotifier/state"
	"martie/internal/gateway"
	"martie/internal/localization"
	"martie/internal/storage"
	"martie/internal/telegram"
)

func TestThreadNotifierTracksRepliesAndNotifiesAtThreshold(t *testing.T) {
	store := testStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testThreadNotifier(store, sender, now)

	thread := gateway.WebhookEvent{
		EventID: "ptchan:thread.created:i:100",
		Kind:    gateway.ThreadCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 100,
			PostID:   100,
			Date:     now.Add(-10 * time.Minute),
			Message:  "op",
		},
	}
	firstReply := gateway.WebhookEvent{
		EventID: "ptchan:post.created:i:101",
		Kind:    gateway.PostCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 100,
			PostID:   101,
			Date:     now.Add(-time.Minute),
			Message:  "reply one",
		},
	}
	secondReply := gateway.WebhookEvent{
		EventID: "ptchan:post.created:i:102",
		Kind:    gateway.PostCreated,
		Post: gateway.Post{
			Board:           "i",
			ThreadID:        100,
			PostID:          102,
			Date:            now,
			Message:         "reply two",
			AttachmentCount: 1,
		},
	}

	if err := consumer.ConsumeGatewayEvent(context.Background(), thread); err != nil {
		t.Fatal(err)
	}
	if err := consumer.ConsumeGatewayEvent(context.Background(), firstReply); err != nil {
		t.Fatal(err)
	}
	if len(sender.requests) != 0 {
		t.Fatalf("notifications = %d, want 0 before threshold", len(sender.requests))
	}
	if err := consumer.ConsumeGatewayEvent(context.Background(), secondReply); err != nil {
		t.Fatal(err)
	}
	if len(sender.requests) != 0 {
		t.Fatalf("notification was sent during webhook handling")
	}
	consumer.deliverPendingNotifications(context.Background())
	if len(sender.requests) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.requests))
	}

	record, ok, err := store.GetThread(context.Background(), "i:100")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || record.ReplyPosts != 2 || record.ReplyFiles != 1 || record.NotifiedNewAt == nil {
		t.Fatalf("record = %+v, exists = %v", record, ok)
	}
	if err := consumer.ConsumeGatewayEvent(context.Background(), secondReply); err != nil {
		t.Fatal(err)
	}
	consumer.deliverPendingNotifications(context.Background())
	if len(sender.requests) != 1 {
		t.Fatalf("duplicate event sent another notification")
	}
}

func TestThreadNotifierSerializesConcurrentEvents(t *testing.T) {
	store := testStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testThreadNotifier(store, sender, now)
	consumer.Config.MinReplyPosts = 1000

	if err := consumer.ConsumeGatewayEvent(context.Background(), gateway.WebhookEvent{
		EventID: "ptchan:thread.created:i:400",
		Kind:    gateway.ThreadCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 400,
			PostID:   400,
			Date:     now,
			Message:  "op",
		},
	}); err != nil {
		t.Fatal(err)
	}

	var workers sync.WaitGroup
	for i := int64(0); i < 50; i++ {
		workers.Add(1)
		go func(i int64) {
			defer workers.Done()
			if err := consumer.ConsumeGatewayEvent(context.Background(), gateway.WebhookEvent{
				EventID: "ptchan:post.created:i:" + strconv.FormatInt(401+i, 10),
				Kind:    gateway.PostCreated,
				Post: gateway.Post{
					Board:    "i",
					ThreadID: 400,
					PostID:   401 + i,
					Date:     now.Add(time.Duration(i) * time.Second),
					Message:  "reply",
				},
			}); err != nil {
				t.Errorf("ConsumeGatewayEvent() error = %v", err)
			}
		}(i)
	}
	workers.Wait()

	record, ok, err := store.GetThread(context.Background(), "i:400")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || record.ReplyPosts != 50 {
		t.Fatalf("record = %+v, exists = %v, want 50 replies", record, ok)
	}
}

func TestThreadNotifierWaitsForOPBeforeNotifying(t *testing.T) {
	store := testStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testThreadNotifier(store, sender, now)

	for _, id := range []int64{501, 502} {
		if err := consumer.ConsumeGatewayEvent(context.Background(), gateway.WebhookEvent{
			EventID: "ptchan:post.created:i:" + strconv.FormatInt(id, 10),
			Kind:    gateway.PostCreated,
			Post: gateway.Post{
				Board:    "i",
				ThreadID: 500,
				PostID:   id,
				Date:     now,
				Message:  "reply",
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	consumer.deliverPendingNotifications(context.Background())
	if len(sender.requests) != 0 {
		t.Fatalf("notified before OP arrived")
	}

	if err := consumer.ConsumeGatewayEvent(context.Background(), gateway.WebhookEvent{
		EventID: "ptchan:thread.created:i:500",
		Kind:    gateway.ThreadCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 500,
			PostID:   500,
			Date:     now.Add(-time.Minute),
			Message:  "op",
		},
	}); err != nil {
		t.Fatal(err)
	}
	consumer.deliverPendingNotifications(context.Background())
	if len(sender.requests) != 1 {
		t.Fatalf("notifications = %d, want 1 after OP", len(sender.requests))
	}
}

func TestThreadNotifierAppliesFiltersWhenOPArrivesLate(t *testing.T) {
	store := testStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testThreadNotifier(store, sender, now)
	consumer.Config.Filter.KeywordDenylist = []string{"blocked"}

	for _, id := range []int64{601, 602} {
		if err := consumer.ConsumeGatewayEvent(context.Background(), gateway.WebhookEvent{
			EventID: "ptchan:post.created:i:" + strconv.FormatInt(id, 10),
			Kind:    gateway.PostCreated,
			Post: gateway.Post{
				Board:    "i",
				ThreadID: 600,
				PostID:   id,
				Date:     now,
				Message:  "reply",
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := consumer.ConsumeGatewayEvent(context.Background(), gateway.WebhookEvent{
		EventID: "ptchan:thread.created:i:600",
		Kind:    gateway.ThreadCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 600,
			PostID:   600,
			Date:     now.Add(-time.Minute),
			Message:  "blocked OP",
		},
	}); err != nil {
		t.Fatal(err)
	}
	consumer.deliverPendingNotifications(context.Background())
	if len(sender.requests) != 0 {
		t.Fatalf("denied OP sent notification")
	}
	record, ok, err := store.GetThread(context.Background(), "i:600")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !record.HasOP || !record.Ignored || record.ReplyPosts != 2 {
		t.Fatalf("record = %+v, exists = %v", record, ok)
	}
}

func TestThreadNotifierSuppressesFirstRunBacklogNotifications(t *testing.T) {
	store := testStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testThreadNotifier(store, sender, now)
	consumer.bootstrapAt = now
	consumer.Config.MinReplyPosts = 1

	if err := consumer.ConsumeGatewayEvent(context.Background(), gateway.WebhookEvent{
		EventID:    "ptchan:post.created:i:301",
		Kind:       gateway.PostCreated,
		ObservedAt: now.Add(-time.Minute),
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 300,
			PostID:   301,
			Date:     now.Add(-time.Minute),
			Message:  "old reply",
		},
	}); err != nil {
		t.Fatal(err)
	}
	consumer.deliverPendingNotifications(context.Background())
	if len(sender.requests) != 0 {
		t.Fatalf("backlog notification was sent")
	}
	record, ok, err := store.GetThread(context.Background(), "i:300")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || record.ReplyPosts != 1 {
		t.Fatalf("record = %+v, exists = %v", record, ok)
	}
}

func TestThreadNotifierBacklogDoesNotConsumeFutureNotification(t *testing.T) {
	store := testStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testThreadNotifier(store, sender, now)
	consumer.bootstrapAt = now
	consumer.Config.MinReplyPosts = 1

	if err := consumer.ConsumeGatewayEvent(context.Background(), gateway.WebhookEvent{
		EventID:    "ptchan:thread.created:i:700",
		Kind:       gateway.ThreadCreated,
		ObservedAt: now.Add(-time.Minute),
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 700,
			PostID:   700,
			Date:     now.Add(-time.Minute),
			Message:  "old op",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := consumer.ConsumeGatewayEvent(context.Background(), gateway.WebhookEvent{
		EventID:    "ptchan:post.created:i:701",
		Kind:       gateway.PostCreated,
		ObservedAt: now.Add(-time.Minute),
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 700,
			PostID:   701,
			Date:     now.Add(-time.Minute),
			Message:  "old reply",
		},
	}); err != nil {
		t.Fatal(err)
	}
	consumer.deliverPendingNotifications(context.Background())
	if len(sender.requests) != 0 {
		t.Fatalf("backlog notification was sent")
	}

	if err := consumer.ConsumeGatewayEvent(context.Background(), gateway.WebhookEvent{
		EventID:    "ptchan:post.created:i:702",
		Kind:       gateway.PostCreated,
		ObservedAt: now.Add(time.Second),
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 700,
			PostID:   702,
			Date:     now.Add(time.Second),
			Message:  "new reply",
		},
	}); err != nil {
		t.Fatal(err)
	}
	consumer.deliverPendingNotifications(context.Background())
	if len(sender.requests) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.requests))
	}
}

func TestThreadNotifierStoresIgnoredThreads(t *testing.T) {
	store := testStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testThreadNotifier(store, sender, now)
	consumer.Config.Filter.KeywordDenylist = []string{"blocked"}

	if err := consumer.ConsumeGatewayEvent(context.Background(), gateway.WebhookEvent{
		EventID: "ptchan:thread.created:i:200",
		Kind:    gateway.ThreadCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 200,
			PostID:   200,
			Date:     now,
			Message:  "blocked topic",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := consumer.ConsumeGatewayEvent(context.Background(), gateway.WebhookEvent{
		EventID: "ptchan:post.created:i:201",
		Kind:    gateway.PostCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 200,
			PostID:   201,
			Date:     now,
			Message:  "ordinary reply",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if len(sender.requests) != 0 {
		t.Fatalf("ignored thread sent notification")
	}
	record, ok, err := store.GetThread(context.Background(), "i:200")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !record.Ignored || record.ReplyPosts != 1 {
		t.Fatalf("record = %+v, exists = %v", record, ok)
	}
}

func testStore(t *testing.T) *threadnotifierstate.Store {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := threadnotifierstate.New(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testThreadNotifier(store *threadnotifierstate.Store, sender *fakeMessageSender, now time.Time) *Notifier {
	consumer := Notifier{
		Config:   Config{MinReplyPosts: 2},
		Ptchan:   PtchanConfig{BaseURL: "https://ptchan.org"},
		Format:   NewFormatter(localization.New(localization.English)),
		ChatID:   123,
		Store:    store,
		Telegram: sender,
		Metrics:  &fakeMetrics{},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	consumer.SetNowFunc(func() time.Time { return now })
	consumer.MarkBootstrapped(now)
	return &consumer
}

type fakeMessageSender struct {
	requests []telegram.SendRequest
}

func (f *fakeMessageSender) Send(_ context.Context, request telegram.SendRequest) error {
	f.requests = append(f.requests, request)
	return nil
}

type fakeMetrics struct{}

func (*fakeMetrics) ObserveNotification(string, string) {}

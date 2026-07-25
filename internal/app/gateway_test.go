package app

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"martie/internal/gateway"
	"martie/internal/localization"
	"martie/internal/state"
	"martie/internal/telegram"
)

func TestGatewayConsumerTracksRepliesAndNotifiesAtThreshold(t *testing.T) {
	store := testGatewayStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testGatewayConsumer(store, sender, now)

	thread := gateway.Event{
		EventID: "ptchan:thread.created:i:100",
		Kind:    gateway.KindThreadCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 100,
			PostID:   100,
			Date:     now.Add(-10 * time.Minute),
			Message:  "op",
		},
	}
	firstReply := gateway.Event{
		EventID: "ptchan:post.created:i:101",
		Kind:    gateway.KindPostCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 100,
			PostID:   101,
			Date:     now.Add(-time.Minute),
			Message:  "reply one",
		},
	}
	secondReply := gateway.Event{
		EventID: "ptchan:post.created:i:102",
		Kind:    gateway.KindPostCreated,
		Post: gateway.Post{
			Board:           "i",
			ThreadID:        100,
			PostID:          102,
			Date:            now,
			Message:         "reply two",
			AttachmentCount: 1,
		},
	}

	if err := consumer.consumeEvent(context.Background(), thread); err != nil {
		t.Fatal(err)
	}
	if err := consumer.consumeEvent(context.Background(), firstReply); err != nil {
		t.Fatal(err)
	}
	if len(sender.requests) != 0 {
		t.Fatalf("notifications = %d, want 0 before threshold", len(sender.requests))
	}
	if err := consumer.consumeEvent(context.Background(), secondReply); err != nil {
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
	if err := consumer.consumeEvent(context.Background(), secondReply); err != nil {
		t.Fatal(err)
	}
	consumer.deliverPendingNotifications(context.Background())
	if len(sender.requests) != 1 {
		t.Fatalf("duplicate event sent another notification")
	}
}

func TestGatewayConsumerSerializesConcurrentEvents(t *testing.T) {
	store := testGatewayStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testGatewayConsumer(store, sender, now)
	consumer.cfg.Notifications.MinReplyPosts = 1000

	if err := consumer.consumeEvent(context.Background(), gateway.Event{
		EventID: "ptchan:thread.created:i:400",
		Kind:    gateway.KindThreadCreated,
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
			if err := consumer.consumeEvent(context.Background(), gateway.Event{
				EventID: "ptchan:post.created:i:" + strconv.FormatInt(401+i, 10),
				Kind:    gateway.KindPostCreated,
				Post: gateway.Post{
					Board:    "i",
					ThreadID: 400,
					PostID:   401 + i,
					Date:     now.Add(time.Duration(i) * time.Second),
					Message:  "reply",
				},
			}); err != nil {
				t.Errorf("consumeEvent() error = %v", err)
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

func TestGatewayConsumerWaitsForOPBeforeNotifying(t *testing.T) {
	store := testGatewayStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testGatewayConsumer(store, sender, now)

	for _, id := range []int64{501, 502} {
		if err := consumer.consumeEvent(context.Background(), gateway.Event{
			EventID: "ptchan:post.created:i:" + strconv.FormatInt(id, 10),
			Kind:    gateway.KindPostCreated,
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

	if err := consumer.consumeEvent(context.Background(), gateway.Event{
		EventID: "ptchan:thread.created:i:500",
		Kind:    gateway.KindThreadCreated,
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

func TestGatewayConsumerAppliesFiltersWhenOPArrivesLate(t *testing.T) {
	store := testGatewayStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testGatewayConsumer(store, sender, now)
	consumer.cfg.Notifications.Filter.KeywordDenylist = []string{"blocked"}

	for _, id := range []int64{601, 602} {
		if err := consumer.consumeEvent(context.Background(), gateway.Event{
			EventID: "ptchan:post.created:i:" + strconv.FormatInt(id, 10),
			Kind:    gateway.KindPostCreated,
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
	if err := consumer.consumeEvent(context.Background(), gateway.Event{
		EventID: "ptchan:thread.created:i:600",
		Kind:    gateway.KindThreadCreated,
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

func TestGatewayConsumerSuppressesFirstRunBacklogNotifications(t *testing.T) {
	store := testGatewayStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testGatewayConsumer(store, sender, now)
	consumer.bootstrapAt = now
	consumer.cfg.Notifications.MinReplyPosts = 1

	if err := consumer.consumeEvent(context.Background(), gateway.Event{
		EventID:    "ptchan:post.created:i:301",
		Kind:       gateway.KindPostCreated,
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

func TestGatewayConsumerBacklogDoesNotConsumeFutureNotification(t *testing.T) {
	store := testGatewayStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testGatewayConsumer(store, sender, now)
	consumer.bootstrapAt = now
	consumer.cfg.Notifications.MinReplyPosts = 1

	if err := consumer.consumeEvent(context.Background(), gateway.Event{
		EventID:    "ptchan:thread.created:i:700",
		Kind:       gateway.KindThreadCreated,
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
	if err := consumer.consumeEvent(context.Background(), gateway.Event{
		EventID:    "ptchan:post.created:i:701",
		Kind:       gateway.KindPostCreated,
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

	if err := consumer.consumeEvent(context.Background(), gateway.Event{
		EventID:    "ptchan:post.created:i:702",
		Kind:       gateway.KindPostCreated,
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

func TestGatewayConsumerStoresIgnoredThreads(t *testing.T) {
	store := testGatewayStore(t)
	sender := &fakeMessageSender{}
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	consumer := testGatewayConsumer(store, sender, now)
	consumer.cfg.Notifications.Filter.KeywordDenylist = []string{"blocked"}

	if err := consumer.consumeEvent(context.Background(), gateway.Event{
		EventID: "ptchan:thread.created:i:200",
		Kind:    gateway.KindThreadCreated,
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
	if err := consumer.consumeEvent(context.Background(), gateway.Event{
		EventID: "ptchan:post.created:i:201",
		Kind:    gateway.KindPostCreated,
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

func testGatewayStore(t *testing.T) *state.Store {
	t.Helper()
	store, err := state.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func testGatewayConsumer(store *state.Store, sender *fakeMessageSender, now time.Time) gatewayConsumer {
	return gatewayConsumer{
		cfg: GatewayConfig{
			Webhook: GatewayWebhookConfig{
				Addr: ":0",
				Path: "/internal/ptchan/events",
			},
			Notifications: GatewayNotificationConfig{MinReplyPosts: 2},
		},
		ptchan:   PtchanConfig{BaseURL: "https://ptchan.org", Secret: "secret"},
		format:   telegram.NewFormatter(localization.New(localization.English)),
		chatID:   123,
		store:    store,
		telegram: sender,
		metrics:  newMetrics(),
		logger:   discardLogger(),
		nowFunc:  func() time.Time { return now },
	}
}

package channer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"

	channerstate "martie/internal/apps/channer/state"
	"martie/internal/assistant"
	"martie/internal/deepseek"
	"martie/internal/gateway"
	"martie/internal/storage"
)

func TestChannerAdmitsConfiguredMention(t *testing.T) {
	channer := Responder{
		Config: Config{
			Mentions:      []string{"@martie"},
			MaxInputRunes: 100,
		},
		Logger: discardLogger(),
	}

	request, result := channer.admit(gateway.WebhookEvent{
		EventID: "event-1",
		Kind:    gateway.PostCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 100,
			PostID:   101,
			Message:  "@Martie what is this?",
		},
	})
	if result != admissionAccepted {
		t.Fatalf("admission = %q", result)
	}
	if request == nil || request.Text != "what is this?" || request.Mention != "@martie" || request.PostID != 101 {
		t.Fatalf("request = %+v", request)
	}
}

func TestChannerAdmissionRejectsIntegrationPosts(t *testing.T) {
	channer := Responder{
		Config: Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Logger: discardLogger(),
	}

	_, result := channer.admit(gateway.WebhookEvent{
		Kind: gateway.PostCreated,
		Post: gateway.Post{
			Message: "@martie hello",
			Origin:  &gateway.PostOrigin{Kind: "integration", Name: "weather-bot"},
		},
	})
	if result != admissionBot {
		t.Fatalf("admission = %q, want bot", result)
	}
}

func TestChannerAdmissionRejectsMartieTripcodePosts(t *testing.T) {
	channer := Responder{
		Config: Config{
			Mentions:      []string{"@martie"},
			MaxInputRunes: 100,
			PtchanContext: assistant.PtchanContextConfig{SelfTripcodes: []string{"!martie"}},
		},
		Logger: discardLogger(),
	}

	_, result := channer.admit(gateway.WebhookEvent{
		Kind: gateway.PostCreated,
		Post: gateway.Post{
			Message:  "@martie hello",
			Tripcode: "!martie",
		},
	})
	if result != admissionBot {
		t.Fatalf("admission = %q, want bot", result)
	}
}

func TestChannerAdmissionAcceptsUnknownHumanPosts(t *testing.T) {
	channer := Responder{
		Config: Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Logger: discardLogger(),
	}

	_, result := channer.admit(gateway.WebhookEvent{
		Kind: gateway.PostCreated,
		Post: gateway.Post{
			Message: "@martie hello",
		},
	})
	if result != admissionAccepted {
		t.Fatalf("admission = %q, want accepted", result)
	}
}

func TestChannerAdmissionRequiresMentionBoundary(t *testing.T) {
	channer := Responder{
		Config: Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Logger: discardLogger(),
	}

	_, result := channer.admit(gateway.WebhookEvent{
		Kind: gateway.PostCreated,
		Post: gateway.Post{Message: "hello @martie_bot"},
	})
	if result != admissionUnaddressed {
		t.Fatalf("admission = %q, want unaddressed", result)
	}
}

func TestChannerAdmissionStripsMatchedMention(t *testing.T) {
	channer := Responder{
		Config: Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Logger: discardLogger(),
	}

	request, result := channer.admit(gateway.WebhookEvent{
		Kind: gateway.PostCreated,
		Post: gateway.Post{Message: "ignore @martie_bot but @martie answer this"},
	})
	if result != admissionAccepted {
		t.Fatalf("admission = %q", result)
	}
	if request.Text != "ignore @martie_bot but  answer this" {
		t.Fatalf("text = %q", request.Text)
	}
}

func TestChannerAdmissionFindsMentionAfterUnicode(t *testing.T) {
	channer := Responder{
		Config: Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Logger: discardLogger(),
	}

	request, result := channer.admit(gateway.WebhookEvent{
		Kind: gateway.PostCreated,
		Post: gateway.Post{Message: "olá @Martie responde"},
	})
	if result != admissionAccepted {
		t.Fatalf("admission = %q", result)
	}
	if request.Text != "olá  responde" {
		t.Fatalf("text = %q", request.Text)
	}
}

func TestChannerAdmissionRejectsLongPost(t *testing.T) {
	channer := Responder{
		Config: Config{Mentions: []string{"@martie"}, MaxInputRunes: 10},
		Logger: discardLogger(),
	}

	_, result := channer.admit(gateway.WebhookEvent{
		Kind: gateway.PostCreated,
		Post: gateway.Post{Message: "@martie " + strings.Repeat("x", 20)},
	})
	if result != admissionTooLong {
		t.Fatalf("admission = %q, want too_long", result)
	}
}

func TestFormatChannerReplySanitizesControls(t *testing.T) {
	got := formatChannerReply(" hello\x00\nthere\x1f ")
	want := "hello\nthere"
	if got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
}

func TestFormatChannerReplyKeepsModelPostReference(t *testing.T) {
	got := formatChannerReply(" >>101\n\nhello")
	want := ">>101\n\nhello"
	if got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
}

func TestFormatChannerReplyFitsGatewayByteLimit(t *testing.T) {
	got := formatChannerReply(strings.Repeat("界", 4000))
	if len(got) > maxChannerReplyBytes {
		t.Fatalf("reply is %d bytes", len(got))
	}
	if !strings.Contains(got, "[truncated]") {
		t.Fatalf("reply was not marked truncated")
	}
}

func TestChannerConsumesEventOnce(t *testing.T) {
	store := testChannerStore(t)
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	channer := Responder{
		Config:    Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Store:     store,
		Completer: &fakeCompleter{completion: deepseek.Completion{Text: "hello back", FinishReason: deepseek.FinishStop}},
		Poster:    &fakePtchanPoster{reply: gateway.ReplyResponse{Board: "i", ThreadID: 100, PostID: 105}},
		Logger:    discardLogger(),
		nowFunc:   func() time.Time { return now },
	}
	event := gateway.WebhookEvent{
		EventID: "Ptchan:post.created:i:101",
		Kind:    gateway.PostCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 100,
			PostID:   101,
			Message:  "@martie hello",
		},
	}

	if err := channer.ConsumeGatewayEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := channer.ConsumeGatewayEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	record, ok, err := store.GetEvent(context.Background(), event.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("event was not recorded")
	}
	if record.Status != channerstate.EventPosted || record.Board != "i" || record.ThreadID != 100 || record.PostID != 101 || record.ReplyPostID != 105 {
		t.Fatalf("record = %+v", record)
	}
	if !record.CreatedAt.Equal(now) {
		t.Fatalf("created at = %v, want %v", record.CreatedAt, now)
	}
}

func TestChannerIgnoresPostsReferencingOwnReply(t *testing.T) {
	store := testChannerStore(t)
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	completer := &fakeCompleter{completion: deepseek.Completion{Text: "hello back", FinishReason: deepseek.FinishStop}}
	poster := &fakePtchanPoster{reply: gateway.ReplyResponse{Board: "i", ThreadID: 100, PostID: 105}}
	channer := Responder{
		Config:    Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Store:     store,
		Completer: completer,
		Poster:    poster,
		Logger:    discardLogger(),
		nowFunc:   func() time.Time { return now },
	}

	first := gateway.WebhookEvent{
		EventID: "Ptchan:post.created:i:101",
		Kind:    gateway.PostCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 100,
			PostID:   101,
			Message:  "@martie hello",
		},
	}
	if err := channer.ConsumeGatewayEvent(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	replyToMartie := gateway.WebhookEvent{
		EventID: "Ptchan:post.created:i:106",
		Kind:    gateway.PostCreated,
		Post: gateway.Post{
			Board:      "i",
			ThreadID:   100,
			PostID:     106,
			Message:    ">>105\n@martie one more?",
			References: []gateway.PostRef{{Board: "i", ThreadID: 100, PostID: 105}},
		},
	}
	if err := channer.ConsumeGatewayEvent(context.Background(), replyToMartie); err != nil {
		t.Fatal(err)
	}

	if len(completer.requests) != 1 {
		t.Fatalf("completion requests = %d, want 1", len(completer.requests))
	}
	if len(poster.requests) != 1 {
		t.Fatalf("posted requests = %d, want 1", len(poster.requests))
	}
	if record, ok, err := store.GetEvent(context.Background(), replyToMartie.EventID); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatalf("reply-to-bot event was persisted: %+v", record)
	}
}

func TestChannerDoesNotPersistIgnoredEvent(t *testing.T) {
	store := testChannerStore(t)
	channer := Responder{
		Config:  Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Store:   store,
		Logger:  discardLogger(),
		nowFunc: func() time.Time { return time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC) },
	}
	event := gateway.WebhookEvent{
		EventID: "Ptchan:thread.created:i:100",
		Kind:    gateway.ThreadCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 100,
			PostID:   100,
			Message:  "op",
		},
	}

	if err := channer.ConsumeGatewayEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	record, ok, err := store.GetEvent(context.Background(), event.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("record = %+v", record)
	}
}

func TestChannerPostsReplyToFocusPost(t *testing.T) {
	store := testChannerStore(t)
	completer := &fakeCompleter{completion: deepseek.Completion{Text: "here is the answer", FinishReason: deepseek.FinishStop}}
	poster := &fakePtchanPoster{reply: gateway.ReplyResponse{Board: "i", ThreadID: 100, PostID: 106}}
	channer := Responder{
		Config:    Config{Mentions: []string{"@martie"}, MaxInputRunes: 100, SystemPrompt: "public prompt"},
		Store:     store,
		Completer: completer,
		Poster:    poster,
		Logger:    discardLogger(),
		nowFunc:   func() time.Time { return time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC) },
	}
	event := gateway.WebhookEvent{
		EventID: "Ptchan:post.created:i:102",
		Kind:    gateway.PostCreated,
		Post: gateway.Post{
			Board:    "i",
			ThreadID: 100,
			PostID:   102,
			Message:  "@martie what now?",
		},
	}

	if err := channer.ConsumeGatewayEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(completer.requests) != 1 || completer.requests[0].systemPrompt != "public prompt" {
		t.Fatalf("completion requests = %+v", completer.requests)
	}
	if len(completer.requests[0].messages) != 1 || !strings.Contains(completer.requests[0].messages[0].Content, "what now?") {
		t.Fatalf("messages = %+v", completer.requests[0].messages)
	}
	if len(poster.requests) != 1 {
		t.Fatalf("posted requests = %d", len(poster.requests))
	}
	if got := poster.requests[0]; got.ref.Board != "i" || got.ref.ThreadID != 100 || got.message != "here is the answer" {
		t.Fatalf("post request = %+v", got)
	}
}

func TestChannerMarksStructuredPostingFailureFinal(t *testing.T) {
	store := testChannerStore(t)
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	channer := Responder{
		Config:    Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Store:     store,
		Completer: &fakeCompleter{completion: deepseek.Completion{Text: "answer", FinishReason: deepseek.FinishStop}},
		Poster:    &fakePtchanPoster{err: &gateway.Error{Code: "rate_limited", Message: "wait", Retryable: true}},
		Logger:    discardLogger(),
		nowFunc:   func() time.Time { return now },
	}
	event := gateway.WebhookEvent{
		EventID: "Ptchan:post.created:i:103",
		Kind:    gateway.PostCreated,
		Post:    gateway.Post{Board: "i", ThreadID: 100, PostID: 103, Message: "@martie hello"},
	}

	if err := channer.ConsumeGatewayEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	record, ok, err := store.GetEvent(context.Background(), event.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("event was not recorded")
	}
	if record.Status != channerstate.EventFailedFinal || record.ErrorCode != "rate_limited" {
		t.Fatalf("record = %+v", record)
	}
}

func TestChannerMarksRateLimitedEventFinal(t *testing.T) {
	store := testChannerStore(t)
	channer := Responder{
		Config:    Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Store:     store,
		Completer: &fakeCompleter{completion: deepseek.Completion{Text: "answer", FinishReason: deepseek.FinishStop}},
		Poster:    &fakePtchanPoster{reply: gateway.ReplyResponse{Board: "i", ThreadID: 100, PostID: 106}},
		Limit:     rate.NewLimiter(rate.Limit(0.001), 0),
		Logger:    discardLogger(),
		nowFunc:   func() time.Time { return time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC) },
	}
	event := gateway.WebhookEvent{
		EventID: "Ptchan:post.created:i:105",
		Kind:    gateway.PostCreated,
		Post:    gateway.Post{Board: "i", ThreadID: 100, PostID: 105, Message: "@martie hello"},
	}

	if err := channer.ConsumeGatewayEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	record, ok, err := store.GetEvent(context.Background(), event.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("event was not recorded")
	}
	if record.Status != channerstate.EventFailedFinal || record.ErrorCode != "rate_limited" {
		t.Fatalf("record = %+v", record)
	}
}

func TestChannerMarksReplyStateUnknownWithoutRetry(t *testing.T) {
	store := testChannerStore(t)
	channer := Responder{
		Config:    Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Store:     store,
		Completer: &fakeCompleter{completion: deepseek.Completion{Text: "answer", FinishReason: deepseek.FinishStop}},
		Poster:    &fakePtchanPoster{err: &gateway.Error{Code: "reply_state_unknown", Message: "check thread", Retryable: false}},
		Logger:    discardLogger(),
		nowFunc:   func() time.Time { return time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC) },
	}
	event := gateway.WebhookEvent{
		EventID: "Ptchan:post.created:i:104",
		Kind:    gateway.PostCreated,
		Post:    gateway.Post{Board: "i", ThreadID: 100, PostID: 104, Message: "@martie hello"},
	}

	if err := channer.ConsumeGatewayEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	record, ok, err := store.GetEvent(context.Background(), event.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("event was not recorded")
	}
	if record.Status != channerstate.EventUnknown || record.ErrorCode != "reply_state_unknown" {
		t.Fatalf("record = %+v", record)
	}
}

func TestChannerMarksUnstructuredPostingErrorUnknown(t *testing.T) {
	store := testChannerStore(t)
	channer := Responder{
		Config:    Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Store:     store,
		Completer: &fakeCompleter{completion: deepseek.Completion{Text: "answer", FinishReason: deepseek.FinishStop}},
		Poster:    &fakePtchanPoster{err: errors.New("connection reset")},
		Logger:    discardLogger(),
		nowFunc:   func() time.Time { return time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC) },
	}
	event := gateway.WebhookEvent{
		EventID: "Ptchan:post.created:i:107",
		Kind:    gateway.PostCreated,
		Post:    gateway.Post{Board: "i", ThreadID: 100, PostID: 107, Message: "@martie hello"},
	}

	if err := channer.ConsumeGatewayEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	record, ok, err := store.GetEvent(context.Background(), event.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("event was not recorded")
	}
	if record.Status != channerstate.EventUnknown || record.ErrorCode != "posting_state_unknown" {
		t.Fatalf("record = %+v", record)
	}
}

func testChannerStore(t *testing.T) *channerstate.Store {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := channerstate.New(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

type fakePtchanPoster struct {
	reply    gateway.ReplyResponse
	err      error
	requests []postedReply
}

type postedReply struct {
	ref     gateway.ThreadRef
	message string
	sage    bool
}

func (f *fakePtchanPoster) PostReply(_ context.Context, ref gateway.ThreadRef, message string, sage bool) (*gateway.ReplyResponse, error) {
	f.requests = append(f.requests, postedReply{ref: ref, message: message, sage: sage})
	return &f.reply, f.err
}

type fakeCompleter struct {
	completion deepseek.Completion
	err        error
	requests   []completionRequest
}

type completionRequest struct {
	systemPrompt string
	messages     []deepseek.Message
}

func (f *fakeCompleter) Complete(_ context.Context, systemPrompt string, messages []deepseek.Message) (deepseek.Completion, error) {
	f.requests = append(f.requests, completionRequest{
		systemPrompt: systemPrompt,
		messages:     append([]deepseek.Message(nil), messages...),
	})
	return f.completion, f.err
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

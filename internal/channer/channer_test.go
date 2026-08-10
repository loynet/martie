package channer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/loynet/ptchan-ai/deepseek"
	"github.com/loynet/ptchan-gateway/clients/go"
	channerstate "martie/internal/channer/state"
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

func TestChannerAdmissionDoesNotGuessOriginFromTripcode(t *testing.T) {
	channer := Responder{
		Config: Config{
			Mentions:      []string{"@martie"},
			MaxInputRunes: 100,
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
	if result != admissionAccepted {
		t.Fatalf("admission = %q, want accepted", result)
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

func TestChannerAdmissionStripsUnsafeControls(t *testing.T) {
	channer := Responder{
		Config: Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Logger: discardLogger(),
	}

	request, result := channer.admit(gateway.WebhookEvent{
		Kind: gateway.PostCreated,
		Post: gateway.Post{Message: "@martie hello\x00\nworld\x1b"},
	})
	if result != admissionAccepted {
		t.Fatalf("admission = %q", result)
	}
	if request.Text != "hello\nworld" {
		t.Fatalf("text = %q", request.Text)
	}
}

func TestFormatChannerReplySanitizesControls(t *testing.T) {
	got := formatChannerReply(101, " hello\x00\nthere\x1f ")
	want := ">>101\nhello\nthere"
	if got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
}

func TestFormatChannerRequestKeepsRulesBeforeDynamicContext(t *testing.T) {
	context := "BEGIN PTCHAN CONTEXT\nTHREAD TRANSCRIPT\n[100]"
	got := formatChannerRequest(request{
		Thread: gateway.ThreadRef{Board: "i", ThreadID: 100},
		PostID: 101,
		Text:   "what is this?",
	}, context)

	if !strings.HasPrefix(got, "CHANNER RESPONSE RULES\n") {
		t.Fatalf("request does not start with static rules:\n%s", got)
	}
	rules, remainder, found := strings.Cut(got, "\n\n"+context)
	if !found || strings.Contains(rules, "101") || !strings.Contains(remainder, "Focus post: 101") {
		t.Fatalf("request did not separate static rules from dynamic request:\n%s", got)
	}
	if strings.Contains(got, "Board: /i/") || strings.Contains(got, "Thread ID: 100") {
		t.Fatalf("request repeats thread coordinates:\n%s", got)
	}
}

func TestFormatChannerReplyAddsFocusBeforeOtherModelReference(t *testing.T) {
	got := formatChannerReply(101, " >>99\n\nhello")
	want := ">>101\n>>99\nhello"
	if got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
}

func TestFormatChannerReplyPreservesOtherReferencesAndParagraphSpacing(t *testing.T) {
	got := formatChannerReply(101, ">>99\n>>98\n\nfirst paragraph\n\nsecond paragraph")
	want := ">>101\n>>99\n>>98\nfirst paragraph\n\nsecond paragraph"
	if got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
}

func TestFormatChannerReplyRemovesModelFocusReferencesAndNormalizesNewlines(t *testing.T) {
	got := formatChannerReply(101, " \r\n>>101\r\n>>101\r\n\r\nhello\r\nthere ")
	want := ">>101\nhello\nthere"
	if got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
}

func TestFormatChannerReplyRejectsOnlyModelFocusReferences(t *testing.T) {
	if got := formatChannerReply(101, ">>101\r\n>>101"); got != "" {
		t.Fatalf("reply = %q, want empty", got)
	}
}

func TestFormatChannerReplyFitsGatewayByteLimit(t *testing.T) {
	got := formatChannerReply(101, strings.Repeat("界", 4000))
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
	metrics := &recordingMetrics{}
	channer := Responder{
		Config:    Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Store:     store,
		Completer: &fakeCompleter{completion: deepseek.Completion{Text: "hello back", FinishReason: deepseek.FinishStop}},
		Poster:    &fakePtchanPoster{reply: gateway.ReplyResponse{Board: "i", ThreadID: 100, PostID: 105}},
		Logger:    discardLogger(),
		Metrics:   metrics,
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
	if record.Status != channerstate.EventPosted || record.Board != "i" || record.ThreadID != 100 || record.PostID != 101 {
		t.Fatalf("record = %+v", record)
	}
	if !record.CreatedAt.Equal(now) {
		t.Fatalf("created at = %v, want %v", record.CreatedAt, now)
	}
	if metrics.admissions[admissionAccepted] != 1 || metrics.admissions[admissionDuplicate] != 1 {
		t.Fatalf("admissions = %v, want one accepted and one duplicate", metrics.admissions)
	}
	if metrics.outcomes[outcomePosted] != 1 {
		t.Fatalf("outcomes = %v, want one posted", metrics.outcomes)
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
	if len(completer.requests[0].messages) != 1 ||
		!strings.Contains(completer.requests[0].messages[0].Content, "what now?") ||
		!strings.Contains(completer.requests[0].messages[0].Content, "The posting layer adds the leading reference to the focus post") ||
		!strings.Contains(completer.requests[0].messages[0].Content, "Focus post: 102") {
		t.Fatalf("messages = %+v", completer.requests[0].messages)
	}
	if len(poster.requests) != 1 {
		t.Fatalf("posted requests = %d", len(poster.requests))
	}
	if got := poster.requests[0]; got.ref.Board != "i" || got.ref.ThreadID != 100 || got.message != ">>102\nhere is the answer" {
		t.Fatalf("post request = %+v", got)
	}
}

func TestChannerPostsCompletionText(t *testing.T) {
	store := testChannerStore(t)
	metrics := &recordingMetrics{}
	poster := &fakePtchanPoster{}
	channer := Responder{
		Config:    Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Store:     store,
		Completer: &fakeCompleter{completion: deepseek.Completion{Text: ">>102\nI can help with that.", FinishReason: deepseek.FinishStop}},
		Poster:    poster,
		Logger:    discardLogger(),
		nowFunc:   func() time.Time { return time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC) },
		Metrics:   metrics,
	}
	event := gateway.WebhookEvent{
		EventID: "Ptchan:post.created:i:110",
		Kind:    gateway.PostCreated,
		Post:    gateway.Post{Board: "i", ThreadID: 100, PostID: 102, Message: "@martie do not answer"},
	}

	if err := channer.ConsumeGatewayEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(poster.requests) != 1 || poster.requests[0].message != ">>102\nI can help with that." {
		t.Fatalf("posted requests = %+v", poster.requests)
	}
	record, ok, err := store.GetEvent(context.Background(), event.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || record.Status != channerstate.EventPosted {
		t.Fatalf("record = %+v, found %t", record, ok)
	}
	if metrics.outcomes[outcomePosted] != 1 {
		t.Fatalf("outcomes = %v, want one posted", metrics.outcomes)
	}
}

func TestChannerMarksStructuredPostingFailureFinal(t *testing.T) {
	store := testChannerStore(t)
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	metrics := &recordingMetrics{}
	var logs bytes.Buffer
	channer := Responder{
		Config:    Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Store:     store,
		Completer: &fakeCompleter{completion: deepseek.Completion{Text: "answer", FinishReason: deepseek.FinishStop}},
		Poster:    &fakePtchanPoster{err: &gateway.Error{Code: "rate_limited", Message: "wait", Retryable: true}},
		Logger:    slog.New(slog.NewTextHandler(&logs, nil)),
		Metrics:   metrics,
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
	if metrics.outcomes[outcomeGatewayRateLimited] != 1 {
		t.Fatalf("outcomes = %v, want one gateway_rate_limited", metrics.outcomes)
	}
	if got := logs.String(); !strings.Contains(got, `msg="channer request rate limited"`) || !strings.Contains(got, "scope=gateway") {
		t.Fatalf("gateway rate-limit log missing: %s", got)
	}
}

func TestChannerMarksThreadRateLimitedEventFinal(t *testing.T) {
	store := testChannerStore(t)
	metrics := &recordingMetrics{}
	var logs bytes.Buffer
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	limit := NewLimiter(25, 3, 6, 2)
	thread := gateway.ThreadRef{Board: "i", ThreadID: 100}
	if limit.allow(thread, now) != limitAllowed || limit.allow(thread, now) != limitAllowed {
		t.Fatal("could not exhaust thread burst")
	}
	channer := Responder{
		Config:    Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Store:     store,
		Completer: &fakeCompleter{completion: deepseek.Completion{Text: "answer", FinishReason: deepseek.FinishStop}},
		Poster:    &fakePtchanPoster{reply: gateway.ReplyResponse{Board: "i", ThreadID: 100, PostID: 106}},
		Limit:     limit,
		Logger:    slog.New(slog.NewTextHandler(&logs, nil)),
		Metrics:   metrics,
		nowFunc:   func() time.Time { return now },
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
	if record.Status != channerstate.EventFailedFinal || record.ErrorCode != outcomeThreadRateLimited {
		t.Fatalf("record = %+v", record)
	}
	if metrics.outcomes[outcomeThreadRateLimited] != 1 {
		t.Fatalf("outcomes = %v, want one local_thread_rate_limited", metrics.outcomes)
	}
	for _, want := range []string{
		`msg="channer request rate limited"`,
		"scope=thread",
		"board=i",
		"thread_id=100",
		"post_id=105",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log missing %q: %s", want, logs.String())
		}
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

func TestChannerFinalizesFailureAfterContextCancellation(t *testing.T) {
	store := testChannerStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	var logs bytes.Buffer
	channer := Responder{
		Config:    Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Store:     store,
		Completer: cancelingCompleter{cancel: cancel},
		Poster:    &fakePtchanPoster{},
		Logger:    slog.New(slog.NewTextHandler(&logs, nil)),
	}
	event := gateway.WebhookEvent{
		EventID: "Ptchan:post.created:i:108",
		Kind:    gateway.PostCreated,
		Post:    gateway.Post{Board: "i", ThreadID: 100, PostID: 108, Message: "@martie hello"},
	}

	if err := channer.ConsumeGatewayEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	record, ok, err := store.GetEvent(context.Background(), event.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || record.Status != channerstate.EventFailedFinal || record.ErrorCode != "completion_error" {
		t.Fatalf("record = %+v, found %t", record, ok)
	}
	if got := logs.String(); !strings.Contains(got, "level=WARN") || !strings.Contains(got, "msg=\"channer request failed\"") || !strings.Contains(got, "event_id=Ptchan:post.created:i:108") || !strings.Contains(got, "board=i") || !strings.Contains(got, "thread_id=100") || !strings.Contains(got, "post_id=108") || !strings.Contains(got, "status=failed_final") || !strings.Contains(got, "code=completion_error") || !strings.Contains(got, "error=\"context canceled\"") {
		t.Fatalf("failure log = %q", got)
	}
}

func TestChannerRecordsUnknownWhenPostedStateCannotBeSaved(t *testing.T) {
	store := testChannerStore(t)
	channer := Responder{
		Config:    Config{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		Store:     failingPostedStore{Store: store},
		Completer: &fakeCompleter{completion: deepseek.Completion{Text: "answer", FinishReason: deepseek.FinishStop}},
		Poster: &fakePtchanPoster{reply: gateway.ReplyResponse{
			Board: "i", ThreadID: 100, PostID: 109,
		}},
		Logger: discardLogger(),
	}
	event := gateway.WebhookEvent{
		EventID: "Ptchan:post.created:i:109",
		Kind:    gateway.PostCreated,
		Post:    gateway.Post{Board: "i", ThreadID: 100, PostID: 109, Message: "@martie hello"},
	}

	if err := channer.ConsumeGatewayEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	record, ok, err := store.GetEvent(context.Background(), event.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || record.Status != channerstate.EventUnknown || record.ErrorCode != "posted_state_update_failed" {
		t.Fatalf("record = %+v, found %t", record, ok)
	}
}

func TestChannerPrunesExpiredEvents(t *testing.T) {
	store := testChannerStore(t)
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	for _, event := range []channerstate.Event{
		{EventID: "old", Status: channerstate.EventFailedFinal, CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour)},
		{EventID: "recent", Status: channerstate.EventFailedFinal, CreatedAt: now, UpdatedAt: now},
	} {
		if _, err := store.StoreEvent(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	channer := Responder{
		Config:  Config{PruneAfter: 24 * time.Hour},
		Store:   store,
		Logger:  discardLogger(),
		nowFunc: func() time.Time { return now },
	}

	channer.prune(context.Background())
	if _, ok, err := store.GetEvent(context.Background(), "old"); err != nil || ok {
		t.Fatalf("old event found = %t, error = %v", ok, err)
	}
	if _, ok, err := store.GetEvent(context.Background(), "recent"); err != nil || !ok {
		t.Fatalf("recent event found = %t, error = %v", ok, err)
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

type cancelingCompleter struct {
	cancel context.CancelFunc
}

func (f cancelingCompleter) Complete(ctx context.Context, _ string, _ []deepseek.Message) (deepseek.Completion, error) {
	f.cancel()
	return deepseek.Completion{}, ctx.Err()
}

type failingPostedStore struct {
	Store
}

func (failingPostedStore) MarkEventPosted(context.Context, string, time.Time) error {
	return errors.New("database unavailable")
}

type completionRequest struct {
	systemPrompt string
	messages     []deepseek.Message
}

type recordingMetrics struct {
	admissions map[admissionResult]int
	outcomes   map[string]int
}

func (m *recordingMetrics) ObserveChannerAdmission(result string) {
	if m.admissions == nil {
		m.admissions = make(map[admissionResult]int)
	}
	m.admissions[admissionResult(result)]++
}

func (*recordingMetrics) ObserveChannerReply(string) {}

func (*recordingMetrics) ObserveChannerContext(string) {}

func (m *recordingMetrics) ObserveChannerOutcome(outcome string) {
	if m.outcomes == nil {
		m.outcomes = make(map[string]int)
	}
	m.outcomes[outcome]++
}

func (*recordingMetrics) ObserveModelCompletion(string, string, time.Duration, deepseek.Completion, error) {
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

package channer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/time/rate"

	channerstate "martie/internal/apps/channer/state"
	"martie/internal/assistant"
	"martie/internal/deepseek"
	"martie/internal/gateway"
)

const (
	maxChannerReplyRunes = 3000
	maxChannerReplyBytes = 7800

	Surface       = "channer"
	ResultSuccess = "success"
	ResultError   = "error"
)

type Config struct {
	Name            string
	Mentions        []string
	SystemPrompt    string
	MaxInputRunes   int
	RateLimitWindow time.Duration
	RequestLimit    int
	RequestBurst    int
	PtchanContext   assistant.PtchanContextConfig
	Trace           assistant.TraceConfig
}

type Responder struct {
	Config    Config
	Store     Store
	Completer Completer
	ModelName string
	Ptchan    *assistant.PtchanContext
	Poster    Poster
	Traces    *assistant.TraceDumper
	Limit     *rate.Limiter
	Logger    *slog.Logger
	Metrics   Metrics
	nowFunc   func() time.Time
}

type Store interface {
	StoreEvent(context.Context, channerstate.Event) (bool, error)
	GetEvent(context.Context, string) (channerstate.Event, bool, error)
	ClaimEvent(context.Context, string, time.Time) (bool, error)
	MarkEventPosting(context.Context, string, time.Time) error
	MarkEventPosted(context.Context, string, channerstate.Reply, time.Time) error
	PostedReplyExists(context.Context, string, int64, int64) (bool, error)
	MarkEventFailed(context.Context, string, string, string, time.Time) error
	MarkEventUnknown(context.Context, string, string, string, time.Time) error
}

type Completer interface {
	Complete(context.Context, string, []deepseek.Message) (deepseek.Completion, error)
}

type Poster interface {
	PostReply(context.Context, gateway.ThreadRef, string, bool) (*gateway.ReplyResponse, error)
}

type Metrics interface {
	ObserveAssistantAdmission(surface, result string)
	ObserveAssistantReply(surface, result string)
	ObserveAssistantContext(surface, contextType string)
	ObserveModelCompletion(surface, provider, model string, duration time.Duration, completion deepseek.Completion, err error)
}

type admissionResult string

const (
	admissionAccepted    admissionResult = "accepted"
	admissionUnsupported admissionResult = "unsupported"
	admissionBot         admissionResult = "bot"
	admissionUnaddressed admissionResult = "unaddressed"
	admissionEmpty       admissionResult = "empty"
	admissionTooLong     admissionResult = "too_long"
	admissionDuplicate   admissionResult = "duplicate"
	admissionRateLimited admissionResult = "rate_limited"
	admissionReplyToBot  admissionResult = "reply_to_bot"
)

type request struct {
	EventID string
	Thread  gateway.ThreadRef
	PostID  int64
	Text    string
	Mention string
}

func (a *Responder) SetNowFunc(nowFunc func() time.Time) {
	a.nowFunc = nowFunc
}

func (a Responder) Run(ctx context.Context) error {
	a.Logger.Info("channer active", "mentions", a.Config.Mentions)
	<-ctx.Done()
	return nil
}

func (a Responder) ConsumeGatewayEvent(ctx context.Context, event gateway.WebhookEvent) error {
	request, result := a.admit(event)
	if result == admissionAccepted && a.Store == nil {
		return fmt.Errorf("channer store is not configured")
	}
	if result == admissionAccepted {
		// TODO: Prefer referenced-post public identity from ptchan-gateway when
		// webhook events include it; this ledger check is only local loop
		// protection.
		replyToBot, err := a.referencesPostedReply(ctx, event.Post)
		if err != nil {
			return err
		}
		if replyToBot {
			result = admissionReplyToBot
		}
	}
	if a.Metrics != nil {
		a.Metrics.ObserveAssistantAdmission(Surface, string(result))
	}
	if result != admissionAccepted {
		a.Logger.Debug("channer event ignored", "event_id", event.EventID, "reason", result)
		return nil
	}
	now := a.now().UTC()
	created, err := a.Store.StoreEvent(ctx, channerstate.Event{
		EventID:   event.EventID,
		Status:    channerstate.EventAdmitted,
		Board:     request.Thread.Board,
		ThreadID:  request.Thread.ThreadID,
		PostID:    request.PostID,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return err
	}
	if !created {
		return a.consumeExisting(ctx, event)
	}

	a.Logger.Info("channer mention admitted", "event_id", request.EventID, "board", request.Thread.Board, "thread_id", request.Thread.ThreadID, "post_id", request.PostID, "mention", request.Mention)
	return a.handle(ctx, *request)
}

func (a Responder) consumeExisting(ctx context.Context, event gateway.WebhookEvent) error {
	record, ok, err := a.Store.GetEvent(ctx, event.EventID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("channer event insert skipped but record was not found: %s", event.EventID)
	}
	if a.Metrics != nil {
		a.Metrics.ObserveAssistantAdmission(Surface, string(admissionDuplicate))
	}
	a.Logger.Debug("channer event already recorded", "event_id", event.EventID, "status", record.Status)
	return nil
}

func (a Responder) now() time.Time {
	if a.nowFunc != nil {
		return a.nowFunc()
	}
	return time.Now()
}

func (a Responder) handle(ctx context.Context, request request) error {
	startedAt := a.now().UTC()
	claimed, err := a.Store.ClaimEvent(ctx, request.EventID, startedAt)
	if err != nil {
		return err
	}
	if !claimed {
		a.Logger.Debug("channer event was not claimable", "event_id", request.EventID)
		return nil
	}
	if a.Completer == nil || a.Poster == nil {
		return a.markFailed(ctx, request.EventID, "not_configured", "channer completion or posting is not configured")
	}
	if a.Limit != nil && !a.Limit.Allow() {
		return a.markFailed(ctx, request.EventID, "rate_limited", "channer rate limit exceeded")
	}

	var contextText string
	usedPtchanContext := false
	if a.Ptchan != nil {
		if text, ok := a.Ptchan.ForPost(ctx, request.Thread, request.PostID); ok {
			contextText = text
			usedPtchanContext = true
			if a.Metrics != nil {
				a.Metrics.ObserveAssistantContext(Surface, "ptchan")
			}
		}
	}

	messages := []deepseek.Message{{Role: deepseek.RoleUser, Content: formatChannerRequest(request, contextText)}}
	trace := &assistant.Trace{
		Surface:       Surface,
		StartedAt:     startedAt,
		MessageID:     request.PostID,
		ThreadID:      request.Thread.ThreadID,
		UserAlias:     "ptchan",
		UsedPtchan:    usedPtchanContext,
		SystemPrompt:  a.Config.SystemPrompt,
		ModelMessages: append([]deepseek.Message(nil), messages...),
	}
	defer func() { a.dumpTrace(trace) }()

	completion, err := a.Completer.Complete(ctx, a.Config.SystemPrompt, messages)
	trace.Completion = completion
	if a.Metrics != nil {
		a.Metrics.ObserveModelCompletion(Surface, "deepseek", a.ModelName, time.Since(startedAt), completion, err)
	}
	if err != nil {
		trace.Outcome = "completion error"
		trace.Err = err
		return a.markFailed(ctx, request.EventID, "completion_error", err.Error())
	}

	text, ok := ptchanCompletionText(completion)
	if !ok {
		err := fmt.Errorf("completion finish reason %q is not postable", completion.FinishReason)
		trace.Outcome = "completion rejected"
		trace.Err = err
		return a.markFailed(ctx, request.EventID, "completion_rejected", err.Error())
	}
	replyText := formatChannerReply(text)
	if err := a.Store.MarkEventPosting(ctx, request.EventID, a.now().UTC()); err != nil {
		trace.Outcome = "posting state error"
		trace.Err = err
		return err
	}
	reply, err := a.Poster.PostReply(ctx, request.Thread, replyText, false)
	if err != nil {
		if a.Metrics != nil {
			a.Metrics.ObserveAssistantReply(Surface, ResultError)
		}
		trace.Outcome = "posting error"
		trace.Err = err
		postingErr, structured := postingError(err)
		if !structured {
			if markErr := a.Store.MarkEventUnknown(ctx, request.EventID, "posting_state_unknown", err.Error(), a.now().UTC()); markErr != nil {
				return joinErrors(err, markErr)
			}
			return nil
		}
		if postingErr.Code == "reply_state_unknown" {
			if markErr := a.Store.MarkEventUnknown(ctx, request.EventID, postingErr.Code, postingErr.Message, a.now().UTC()); markErr != nil {
				return joinErrors(err, markErr)
			}
			return nil
		}
		return a.markFailed(ctx, request.EventID, postingFailure(postingErr), err.Error())
	}
	if err := a.Store.MarkEventPosted(ctx, request.EventID, channerstate.Reply{Board: reply.Board, ThreadID: reply.ThreadID, PostID: reply.PostID}, a.now().UTC()); err != nil {
		trace.Outcome = "posted but state update failed"
		trace.Err = err
		return err
	}
	trace.Outcome = "posted"
	if a.Metrics != nil {
		a.Metrics.ObserveAssistantReply(Surface, ResultSuccess)
	}
	a.Logger.Info("channer replied", "event_id", request.EventID, "board", reply.Board, "thread_id", reply.ThreadID, "post_id", reply.PostID)
	return nil
}

func (a Responder) markFailed(ctx context.Context, eventID, code, message string) error {
	return a.Store.MarkEventFailed(ctx, eventID, code, message, a.now().UTC())
}

func (a Responder) dumpTrace(trace *assistant.Trace) {
	if a.Traces == nil {
		return
	}
	path, err := a.Traces.Dump(trace)
	if err != nil {
		a.Logger.Warn("channer trace dump failed", "event_id", trace.MessageID, "thread_id", trace.ThreadID, "error", err)
		return
	}
	a.Logger.Info("channer trace dumped", "trace_id", filepath.Base(path), "post_id", trace.MessageID, "thread_id", trace.ThreadID, "path", path)
}

func formatChannerRequest(request request, contextText string) string {
	var b strings.Builder
	if contextText != "" {
		b.WriteString(contextText)
		b.WriteString("\n\n")
	}
	b.WriteString("CURRENT PTCHAN REQUEST\n\n")
	fmt.Fprintf(&b, "Board: /%s/\n", request.Thread.Board)
	fmt.Fprintf(&b, "Thread ID: %d\n", request.Thread.ThreadID)
	fmt.Fprintf(&b, "Post ID: %d\n", request.PostID)
	b.WriteString("Post text after removing the configured mention:\n")
	assistant.WriteFencedBlock(&b, "ptchan-request", request.Text)
	b.WriteString("\n\nReply publicly to the current post. Start from the provided context, not hidden assumptions.")
	b.WriteString("\nUse natural chan style. Say OP when you mean the opening post or original poster. Reference posts as >>123 without Markdown, full URLs, or a blank line before the answer.")
	return b.String()
}

func ptchanCompletionText(completion deepseek.Completion) (string, bool) {
	switch completion.FinishReason {
	case deepseek.FinishStop, deepseek.FinishLength:
		return completion.Text, strings.TrimSpace(completion.Text) != ""
	case deepseek.FinishContentFilter:
		return "I can't help with that request.", true
	default:
		return "", false
	}
}

func formatChannerReply(text string) string {
	text = strings.TrimSpace(text)
	text = sanitizePtchanReplyText(text)
	text = assistant.TruncateRunes(text, maxChannerReplyRunes)
	return truncatePtchanReplyBytes(text, maxChannerReplyBytes)
}

func sanitizePtchanReplyText(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || !isControlRune(r) {
			return r
		}
		return -1
	}, text)
}

func isControlRune(r rune) bool {
	return r >= 0 && r < 0x20 || r == 0x7f
}

func truncatePtchanReplyBytes(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	const suffix = "\n[truncated]"
	bodyLimit := limit - len(suffix)
	if bodyLimit <= 0 {
		used := 0
		for i, r := range text {
			next := used + len(string(r))
			if next > limit {
				return text[:i]
			}
			used = next
		}
		return text
	}
	used := 0
	for i, r := range text {
		next := used + len(string(r))
		if next > bodyLimit {
			return strings.TrimSpace(text[:i]) + suffix
		}
		used = next
	}
	return text
}

func postingFailure(err error) string {
	if postingErr, ok := postingError(err); ok {
		if postingErr.Code != "" {
			return postingErr.Code
		}
	}
	return "posting_error"
}

func postingError(err error) (*gateway.Error, bool) {
	var postingErr *gateway.Error
	if errors.As(err, &postingErr) {
		return postingErr, true
	}
	return nil, false
}

func joinErrors(primary, secondary error) error {
	if secondary == nil {
		return primary
	}
	return fmt.Errorf("%v; additionally: %w", primary, secondary)
}

func (a Responder) admit(event gateway.WebhookEvent) (*request, admissionResult) {
	if event.Kind != gateway.PostCreated {
		return nil, admissionUnsupported
	}
	if isIntegrationPost(event.Post) || assistant.IsSelfTripcode(event.Post.Tripcode, a.Config.PtchanContext.SelfTripcodes) {
		return nil, admissionBot
	}
	text := strings.TrimSpace(event.Post.Message)
	if text == "" {
		return nil, admissionEmpty
	}
	if utf8.RuneCountInString(text) > a.Config.MaxInputRunes {
		return nil, admissionTooLong
	}
	mention, index, ok := firstConfiguredMention(text, a.Config.Mentions)
	if !ok {
		return nil, admissionUnaddressed
	}
	return &request{
		EventID: event.EventID,
		Thread:  event.Post.ThreadRef(),
		PostID:  event.Post.PostID,
		Text:    strings.TrimSpace(removeMentionAt(text, mention, index)),
		Mention: mention,
	}, admissionAccepted
}

func (a Responder) referencesPostedReply(ctx context.Context, post gateway.Post) (bool, error) {
	for _, ref := range post.References {
		if ref.Board == "" || ref.ThreadID <= 0 || ref.PostID <= 0 {
			continue
		}
		found, err := a.Store.PostedReplyExists(ctx, ref.Board, ref.ThreadID, ref.PostID)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

func isIntegrationPost(post gateway.Post) bool {
	return post.Origin != nil && post.Origin.Kind == "integration"
}

func firstConfiguredMention(text string, mentions []string) (string, int, bool) {
	for _, mention := range mentions {
		if index, ok := mentionIndex(text, mention); ok {
			return mention, index, true
		}
	}
	return "", 0, false
}

func containsMention(text, mention string) bool {
	_, ok := mentionIndex(text, mention)
	return ok
}

func mentionIndex(text, mention string) (int, bool) {
	mention = strings.TrimSpace(mention)
	if mention == "" {
		return 0, false
	}
	for index := 0; index+len(mention) <= len(text); index++ {
		if !strings.EqualFold(text[index:index+len(mention)], mention) {
			continue
		}
		beforeOK := index == 0 || !isMentionChar(rune(text[index-1]))
		after := index + len(mention)
		afterOK := after == len(text) || !isMentionChar(rune(text[after]))
		if beforeOK && afterOK {
			return index, true
		}
	}
	return 0, false
}

func removeMentionAt(text, mention string, index int) string {
	mention = strings.TrimSpace(mention)
	if index < 0 || index+len(mention) > len(text) {
		return text
	}
	return text[:index] + text[index+len(mention):]
}

func isMentionChar(r rune) bool {
	return r == '_' || r == '-' || r == '@' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

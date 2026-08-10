package channer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/loynet/ptchan-ai/context/thread"
	"github.com/loynet/ptchan-ai/deepseek"
	"github.com/loynet/ptchan-gateway/clients/go"

	channerstate "martie/internal/channer/state"
)

const (
	maxChannerReplyRunes = 3000
	maxChannerReplyBytes = 7800
	finalizeTimeout      = 5 * time.Second

	ResultSuccess = "success"
	ResultError   = "error"

	outcomePosted             = "posted"
	outcomeGlobalRateLimited  = "local_global_rate_limited"
	outcomeThreadRateLimited  = "local_thread_rate_limited"
	outcomeGatewayRateLimited = "gateway_rate_limited"
	outcomeCompletionError    = "completion_error"
	outcomeCompletionRejected = "completion_rejected"
	outcomePostingRejected    = "posting_rejected"
	outcomePostingUnknown     = "posting_unknown"
	outcomeNotConfigured      = "not_configured"
)

// TerminalOutcomes lists the bounded outcome label values emitted for admitted requests.
func TerminalOutcomes() []string {
	return []string{
		outcomePosted,
		outcomeGlobalRateLimited,
		outcomeThreadRateLimited,
		outcomeGatewayRateLimited,
		outcomeCompletionError,
		outcomeCompletionRejected,
		outcomePostingRejected,
		outcomePostingUnknown,
		outcomeNotConfigured,
	}
}

type Config struct {
	Mentions      []string
	SystemPrompt  string
	MaxInputRunes int
	GlobalPerHour int
	GlobalBurst   int
	ThreadPerHour int
	ThreadBurst   int
	PruneAfter    time.Duration
	ThreadContext thread.Config
}

type Responder struct {
	Config        Config
	Store         Store
	Completer     Completer
	ModelName     string
	ThreadContext *thread.Context
	Poster        Poster
	Limit         *Limiter
	Logger        *slog.Logger
	Metrics       Metrics
	nowFunc       func() time.Time
}

type Store interface {
	StoreEvent(context.Context, channerstate.Event) (bool, error)
	GetEvent(context.Context, string) (channerstate.Event, bool, error)
	ClaimEvent(context.Context, string, time.Time) (bool, error)
	MarkEventPosting(context.Context, string, time.Time) error
	MarkEventPosted(context.Context, string, time.Time) error
	MarkEventFailed(context.Context, string, string, string, time.Time) error
	MarkEventUnknown(context.Context, string, string, string, time.Time) error
	PruneBefore(context.Context, time.Time) (int64, error)
}

type Completer interface {
	Complete(context.Context, string, []deepseek.Message) (deepseek.Completion, error)
}

type Poster interface {
	PostReply(context.Context, gateway.ThreadRef, string, bool) (*gateway.ReplyResponse, error)
}

type Metrics interface {
	ObserveChannerAdmission(result string)
	ObserveChannerReply(result string)
	ObserveChannerContext(contextType string)
	ObserveChannerOutcome(outcome string)
	ObserveModelCompletion(provider, model string, duration time.Duration, completion deepseek.Completion, err error)
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
	if a.Config.PruneAfter == 0 {
		<-ctx.Done()
		return nil
	}

	a.prune(ctx)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			a.prune(ctx)
		}
	}
}

func (a Responder) ConsumeGatewayEvent(ctx context.Context, event gateway.WebhookEvent) error {
	request, result := a.admit(event)
	if result == admissionAccepted && a.Store == nil {
		return fmt.Errorf("channer store is not configured")
	}
	if result != admissionAccepted {
		if a.Metrics != nil {
			a.Metrics.ObserveChannerAdmission(string(result))
		}
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
	if a.Metrics != nil {
		a.Metrics.ObserveChannerAdmission(string(admissionAccepted))
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
		a.Metrics.ObserveChannerAdmission(string(admissionDuplicate))
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
	finalizeCtx, cancel := finalizationContext(ctx)
	claimed, err := a.Store.ClaimEvent(finalizeCtx, request.EventID, startedAt)
	cancel()
	if err != nil {
		return err
	}
	if !claimed {
		a.Logger.Debug("channer event was not claimable", "event_id", request.EventID)
		return nil
	}
	if a.Completer == nil || a.Poster == nil {
		return a.markFailed(ctx, request, "not_configured", "channer completion or posting is not configured")
	}
	if a.Limit != nil {
		switch a.Limit.allow(request.Thread, startedAt) {
		case limitGlobal:
			a.logRateLimit(request, "global")
			return a.markFailed(ctx, request, outcomeGlobalRateLimited, "channer global rate limit exceeded")
		case limitThread:
			a.logRateLimit(request, "thread")
			return a.markFailed(ctx, request, outcomeThreadRateLimited, "channer thread rate limit exceeded")
		}
	}

	var contextText string
	if a.ThreadContext != nil {
		if text, ok := a.ThreadContext.ForPost(ctx, request.Thread, request.PostID); ok {
			contextText = text
			if a.Metrics != nil {
				a.Metrics.ObserveChannerContext("ptchan")
			}
		}
	}

	messages := []deepseek.Message{{Role: deepseek.RoleUser, Content: formatChannerRequest(request, contextText)}}

	completionStarted := time.Now()
	completion, err := a.Completer.Complete(ctx, a.Config.SystemPrompt, messages)
	if a.Metrics != nil {
		a.Metrics.ObserveModelCompletion("deepseek", a.ModelName, time.Since(completionStarted), completion, err)
	}
	if err != nil {
		return a.markFailed(ctx, request, "completion_error", err.Error())
	}

	text, ok := ptchanCompletionText(completion)
	if !ok {
		err := fmt.Errorf("completion finish reason %q is not postable", completion.FinishReason)
		return a.markFailed(ctx, request, "completion_rejected", err.Error())
	}
	replyText := formatChannerReply(request.PostID, text)
	if replyText == "" {
		return a.markFailed(ctx, request, "completion_rejected", "completion contained no reply after removing the triggering post reference")
	}
	finalizeCtx, cancel = finalizationContext(ctx)
	err = a.Store.MarkEventPosting(finalizeCtx, request.EventID, a.now().UTC())
	cancel()
	if err != nil {
		return err
	}
	reply, err := a.Poster.PostReply(ctx, request.Thread, replyText, false)
	if err != nil {
		if a.Metrics != nil {
			a.Metrics.ObserveChannerReply(ResultError)
		}
		postingErr, structured := postingError(err)
		if !structured {
			if markErr := a.markUnknown(ctx, request, "posting_state_unknown", err.Error()); markErr != nil {
				return joinErrors(err, markErr)
			}
			return nil
		}
		if postingErr.Code == "reply_state_unknown" {
			if markErr := a.markUnknown(ctx, request, postingErr.Code, postingErr.Message); markErr != nil {
				return joinErrors(err, markErr)
			}
			return nil
		}
		if postingErr.Code == "rate_limited" {
			a.logRateLimit(request, "gateway")
		}
		return a.markFailed(ctx, request, postingFailure(postingErr), err.Error())
	}
	finalizeCtx, cancel = finalizationContext(ctx)
	err = a.Store.MarkEventPosted(finalizeCtx, request.EventID, a.now().UTC())
	cancel()
	if err != nil {
		if markErr := a.markUnknown(ctx, request, "posted_state_update_failed", err.Error()); markErr != nil {
			return joinErrors(err, markErr)
		}
		if a.Metrics != nil {
			a.Metrics.ObserveChannerReply(ResultSuccess)
		}
		return nil
	}
	if a.Metrics != nil {
		a.Metrics.ObserveChannerReply(ResultSuccess)
		a.Metrics.ObserveChannerOutcome(outcomePosted)
	}
	a.Logger.Info("channer replied", "event_id", request.EventID, "board", reply.Board, "thread_id", reply.ThreadID, "post_id", reply.PostID)
	return nil
}

func (a Responder) logRateLimit(request request, scope string) {
	a.Logger.Warn("channer request rate limited", "scope", scope, "event_id", request.EventID, "board", request.Thread.Board, "thread_id", request.Thread.ThreadID, "post_id", request.PostID)
}

func (a Responder) markFailed(ctx context.Context, request request, code, message string) error {
	finalizeCtx, cancel := finalizationContext(ctx)
	defer cancel()
	if err := a.Store.MarkEventFailed(finalizeCtx, request.EventID, code, message, a.now().UTC()); err != nil {
		return err
	}
	a.logTerminalFailure(request, channerstate.EventFailedFinal, code, message)
	if a.Metrics != nil {
		a.Metrics.ObserveChannerOutcome(failureOutcome(code))
	}
	return nil
}

func (a Responder) markUnknown(ctx context.Context, request request, code, message string) error {
	finalizeCtx, cancel := finalizationContext(ctx)
	defer cancel()
	if err := a.Store.MarkEventUnknown(finalizeCtx, request.EventID, code, message, a.now().UTC()); err != nil {
		return err
	}
	a.logTerminalFailure(request, channerstate.EventUnknown, code, message)
	if a.Metrics != nil {
		a.Metrics.ObserveChannerOutcome(outcomePostingUnknown)
	}
	return nil
}

func (a Responder) logTerminalFailure(request request, status channerstate.EventStatus, code, message string) {
	a.Logger.Warn("channer request failed", "event_id", request.EventID, "board", request.Thread.Board, "thread_id", request.Thread.ThreadID, "post_id", request.PostID, "status", status, "code", code, "error", message)
}

func failureOutcome(code string) string {
	switch code {
	case outcomeGlobalRateLimited, outcomeThreadRateLimited:
		return code
	case "rate_limited":
		return outcomeGatewayRateLimited
	case "completion_error":
		return outcomeCompletionError
	case "completion_rejected":
		return outcomeCompletionRejected
	case "not_configured":
		return outcomeNotConfigured
	default:
		return outcomePostingRejected
	}
}

func finalizationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), finalizeTimeout)
}

func (a Responder) prune(ctx context.Context) {
	cutoff := a.now().UTC().Add(-a.Config.PruneAfter)
	count, err := a.Store.PruneBefore(ctx, cutoff)
	if err != nil {
		a.Logger.Warn("channer event prune failed", "error", err)
		return
	}
	if count > 0 {
		a.Logger.Info("channer events pruned", "count", count, "cutoff", cutoff.Format(time.RFC3339))
	}
}

func formatChannerRequest(request request, contextText string) string {
	var b strings.Builder
	b.WriteString(channerResponseRules())
	b.WriteString("\n\n")
	if contextText != "" {
		b.WriteString(contextText)
		b.WriteString("\n\n")
	}
	b.WriteString("CURRENT PTCHAN REQUEST\n\n")
	fmt.Fprintf(&b, "Focus post: %d\n", request.PostID)
	b.WriteString("Request after removing the configured mention:\n")
	writeFencedBlock(&b, "ptchan-request", request.Text)
	return b.String()
}

func channerResponseRules() string {
	return `CHANNER RESPONSE RULES

- Reply publicly to the focus post using only the provided context.
- The posting layer adds the leading reference to the focus post. Do not add it yourself.
- Use natural chan style. Say OP for the opening post or original poster.
- Use >>123 only for a different post, without Markdown or full URLs.`
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

func formatChannerReply(postID int64, text string) string {
	text = stripUnsafeControls(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = stripLeadingPostReference(text, postID)
	text = removeBlankLineAfterLeadingReferences(text)
	if text == "" {
		return ""
	}
	text = truncateRunes(text, maxChannerReplyRunes)
	prefix := fmt.Sprintf(">>%d\n", postID)
	return prefix + truncatePtchanReplyBytes(text, maxChannerReplyBytes-len(prefix))
}

func stripLeadingPostReference(text string, postID int64) string {
	text = strings.TrimSpace(text)
	reference := fmt.Sprintf(">>%d", postID)
	for {
		line, rest, found := strings.Cut(text, "\n")
		if strings.TrimSpace(line) != reference {
			return text
		}
		if !found {
			return ""
		}
		text = strings.TrimSpace(rest)
	}
}

func removeBlankLineAfterLeadingReferences(text string) string {
	lines := strings.Split(text, "\n")
	references := 0
	for references < len(lines) && isPostReferenceLine(lines[references]) {
		references++
	}
	if references == 0 {
		return text
	}

	body := references
	for body < len(lines) && strings.TrimSpace(lines[body]) == "" {
		body++
	}
	return strings.Join(append(lines[:references], lines[body:]...), "\n")
}

func isPostReferenceLine(line string) bool {
	line = strings.TrimSpace(line)
	if len(line) <= 2 || !strings.HasPrefix(line, ">>") {
		return false
	}
	for _, r := range line[2:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
	if isIntegrationPost(event.Post) {
		return nil, admissionBot
	}
	text := strings.TrimSpace(stripUnsafeControls(event.Post.Message))
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

func isIntegrationPost(post gateway.Post) bool {
	return post.Origin != nil && post.Origin.Kind == gateway.IntegrationOrigin
}

func firstConfiguredMention(text string, mentions []string) (string, int, bool) {
	for _, mention := range mentions {
		if index, ok := mentionIndex(text, mention); ok {
			return mention, index, true
		}
	}
	return "", 0, false
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

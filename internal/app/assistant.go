package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/time/rate"

	"martie/internal/assistant"
	"martie/internal/deepseek"
	"martie/internal/localization"
	"martie/internal/state"
	"martie/internal/telegram"
)

const (
	// These bounds protect Telegram and completion requests. They are protocol
	// safeguards rather than environment policy, so they remain in code.
	typingInterval       = 4 * time.Second
	rejectionReplyWindow = 10 * time.Second
	replyContextRunes    = 1000
)

type cursorStore interface {
	GetCursor(context.Context, string) (int64, bool, error)
	SetCursor(context.Context, string, int64) error
}

// assistant owns the conversational application flow between Telegram and the
// completion engine. It admits requests, constructs bounded context, replaces
// Telegram identities with temporary aliases, and delivers the final reply.
// Conversation history is process-local and intentionally not persisted.
type telegramAssistant struct {
	cfg       TelegramAssistantConfig
	text      localization.Localizer
	store     cursorStore
	client    *telegram.Client
	sender    telegramAssistantSender
	completer assistantCompleter
	modelName string
	metrics   *metrics
	logger    *slog.Logger
	allowed   map[int64]struct{}
	mu        sync.Mutex
	global    *rate.Limiter
	users     map[int64]userRateLimiter
	replies   *rate.Limiter
	history   map[conversationKey]*conversation
	ptchan    *assistant.PtchanContext
	traces    *assistant.TraceDumper
	aliasSeed string
}

type userRateLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type admissionResult string

const (
	admissionAccepted         admissionResult = "accepted"
	admissionUnsupported      admissionResult = "unsupported"
	admissionWrongChat        admissionResult = "wrong_chat"
	admissionAutomaticForward admissionResult = "automatic_forward"
	admissionBot              admissionResult = "bot"
	admissionUnaddressed      admissionResult = "unaddressed"
	admissionEmpty            admissionResult = "empty"
	admissionTooLong          admissionResult = "too_long"
	admissionUnauthorized     admissionResult = "unauthorized"
	admissionRateLimited      admissionResult = "rate_limited"
)

type assistantCompleter interface {
	Complete(context.Context, string, []deepseek.Message) (deepseek.Completion, error)
}

type telegramAssistantSender interface {
	Send(context.Context, telegram.SendRequest) error
	SendTyping(context.Context, int64, int64) error
}

// telegramAssistantRequest is the admitted subset of a Telegram message. Its text and
// identity fields remain untrusted until prompt construction and aliasing.
type telegramAssistantRequest struct {
	MessageID       int64
	MessageThreadID int64
	UserID          int64
	Username        string
	FirstName       string
	ChatTitle       string
	Text            string
	ReplyText       string
	ReplyFromBot    bool
	ReplyUserID     int64
	ReplyUsername   string
	ReplyFirstName  string
	Mentions        []string
	ReplyMentions   []string
}

func newTelegramAssistant(cfg TelegramAssistantConfig, text localization.Localizer, store *state.Store, client *telegram.Client, completer assistantCompleter, metrics *metrics, logger *slog.Logger) *telegramAssistant {
	allowed := make(map[int64]struct{}, len(cfg.AllowedUserIDs))
	for _, userID := range cfg.AllowedUserIDs {
		allowed[userID] = struct{}{}
	}

	return &telegramAssistant{
		cfg:       cfg,
		text:      text,
		store:     store,
		client:    client,
		sender:    client,
		completer: completer,
		metrics:   metrics,
		logger:    logger,
		allowed:   allowed,
		global:    newRateLimiter(cfg.GlobalRequestLimit, cfg.RateLimitWindow, cfg.GlobalRequestBurst),
		users:     make(map[int64]userRateLimiter),
		replies:   rate.NewLimiter(rate.Every(rejectionReplyWindow), 1),
		history:   make(map[conversationKey]*conversation),
		aliasSeed: randomAliasSeed(),
	}
}

func (c *telegramAssistant) run(ctx context.Context) error {
	bot, err := c.client.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("load bot identity: %w", err)
	}
	if bot.Username == "" {
		return fmt.Errorf("telegram bot username is empty")
	}

	cursor := telegramUpdateCursor(bot.ID)
	offset, _, err := c.store.GetCursor(ctx, cursor)
	if err != nil {
		return fmt.Errorf("load update cursor: %w", err)
	}

	c.logger.Info("telegram assistant active", "username", "@"+bot.Username)
	// Updates stay sequential so a stored cursor always means every preceding
	// message has finished processing.
	for {
		startedAt := time.Now()
		updates, err := c.client.GetUpdates(ctx, offset)
		c.metrics.observeComponentRun(string(componentTelegramAssistant), time.Since(startedAt), err)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("receive updates: %w", err)
		}
		c.expireConversations(time.Now())

		for _, update := range updates {
			if err := c.processUpdate(ctx, cursor, update, bot); err != nil {
				return err
			}
			offset = update.ID + 1
		}
	}
}

func (c *telegramAssistant) processUpdate(ctx context.Context, cursor string, update telegram.Update, bot telegram.User) error {
	request, result := c.admit(update.Message, bot)
	if result == admissionWrongChat {
		c.logger.Debug("telegram assistant message ignored", "reason", result, "chat", update.Message.Chat.Title, "chat_id", update.Message.Chat.ID, "configured_chat_id", c.cfg.DiscussionChatID)
	}
	if request != nil {
		c.metrics.observeAssistantAdmission(componentTelegramAssistant, admissionAccepted)
		if !c.handle(ctx, *request) {
			return ctx.Err()
		}
	} else {
		c.metrics.observeAssistantAdmission(componentTelegramAssistant, result)
		c.replyToRejection(ctx, update.Message, result)
	}

	if err := c.store.SetCursor(ctx, cursor, update.ID+1); err != nil {
		return fmt.Errorf("store update cursor: %w", err)
	}
	return nil
}

func telegramUpdateCursor(botID int64) string {
	return fmt.Sprintf("telegram:%d:updates", botID)
}

func (c *telegramAssistant) admit(message *telegram.IncomingMessage, bot telegram.User) (*telegramAssistantRequest, admissionResult) {
	if message == nil {
		return nil, admissionUnsupported
	}
	if message.Chat.ID != c.cfg.DiscussionChatID {
		return nil, admissionWrongChat
	}
	if message.IsAutomaticForward {
		return nil, admissionAutomaticForward
	}
	if message.From == nil || message.From.IsBot {
		return nil, admissionBot
	}
	if !message.Addresses(bot) {
		return nil, admissionUnaddressed
	}
	if strings.TrimSpace(message.Text) == "" {
		return nil, admissionEmpty
	}
	if utf8.RuneCountInString(message.Text) > c.cfg.MaxInputRunes {
		return nil, admissionTooLong
	}
	text := message.TextWithoutMention(bot)
	if text == "" {
		return nil, admissionEmpty
	}
	if !c.cfg.AllowAllUsers {
		if _, ok := c.allowed[message.From.ID]; !ok {
			return nil, admissionUnauthorized
		}
	}
	if !c.allow(message.From.ID) {
		return nil, admissionRateLimited
	}
	var replyText string
	var replyFromBot bool
	if message.ReplyToMessage != nil {
		replyText = assistant.TruncateRunes(strings.TrimSpace(message.ReplyToMessage.Text), replyContextRunes)
		replyFromBot = message.ReplyToMessage.From != nil && message.ReplyToMessage.From.ID == bot.ID
	}
	request := &telegramAssistantRequest{
		MessageID:       message.ID,
		MessageThreadID: message.MessageThreadID,
		UserID:          message.From.ID,
		Username:        message.From.Username,
		FirstName:       message.From.FirstName,
		ChatTitle:       message.Chat.Title,
		Text:            text,
		ReplyText:       replyText,
		ReplyFromBot:    replyFromBot,
	}
	for _, username := range message.Mentions() {
		if !strings.EqualFold(username, bot.Username) {
			request.Mentions = append(request.Mentions, username)
		}
	}
	if message.ReplyToMessage != nil && message.ReplyToMessage.From != nil && !replyFromBot {
		request.ReplyUserID = message.ReplyToMessage.From.ID
		request.ReplyUsername = message.ReplyToMessage.From.Username
		request.ReplyFirstName = message.ReplyToMessage.From.FirstName
	}
	if message.ReplyToMessage != nil {
		for _, username := range message.ReplyToMessage.Mentions() {
			if !strings.EqualFold(username, bot.Username) {
				request.ReplyMentions = append(request.ReplyMentions, username)
			}
		}
	}
	return request, admissionAccepted
}

func (c *telegramAssistant) allow(userID int64) bool {
	return c.allowAt(userID, time.Now())
}

func (c *telegramAssistant) allowAt(userID int64, now time.Time) bool {
	// The lock makes checking and consuming the user and global buckets one
	// decision; concurrent callers cannot spend the same available capacity.
	c.mu.Lock()
	defer c.mu.Unlock()

	for id, user := range c.users {
		if now.Sub(user.lastSeen) >= c.cfg.RateLimitWindow {
			delete(c.users, id)
		}
	}

	user, ok := c.users[userID]
	if !ok {
		user.limiter = newRateLimiter(c.cfg.UserRequestLimit, c.cfg.RateLimitWindow, c.cfg.UserRequestBurst)
	}
	user.lastSeen = now
	c.users[userID] = user
	if user.limiter.TokensAt(now) < 1 || c.global.TokensAt(now) < 1 {
		return false
	}
	return user.limiter.AllowN(now, 1) && c.global.AllowN(now, 1)
}

func newRateLimiter(requests int, window time.Duration, burst int) *rate.Limiter {
	refill := rate.Limit(float64(requests) / window.Seconds())
	return rate.NewLimiter(refill, burst)
}

func (c *telegramAssistant) handle(ctx context.Context, request telegramAssistantRequest) bool {
	// Telegram typing is refreshed independently while completion is in flight,
	// then joined before any result is handled.
	typingCtx, stopTyping := context.WithCancel(ctx)
	typingDone := make(chan struct{})
	go func() {
		defer close(typingDone)
		c.showTyping(typingCtx, request)
	}()

	startedAt := time.Now()
	key := conversationKey{chatID: c.cfg.DiscussionChatID, threadID: request.MessageThreadID}
	c.expireConversations(startedAt)
	current := c.history[key]
	if current == nil {
		current = &conversation{aliasSeed: c.aliasSeed}
		c.history[key] = current
	}
	messages := current.messages()
	storedBefore := append([]deepseek.Message(nil), messages...)
	if len(messages) > 0 {
		c.metrics.observeAssistantContext(componentTelegramAssistant, "history")
	}
	userAlias := current.participantAlias(request.UserID, request.Username, request.FirstName)
	if request.ReplyUserID != 0 {
		current.participantAlias(request.ReplyUserID, request.ReplyUsername, request.ReplyFirstName)
	}
	for _, username := range append(request.Mentions, request.ReplyMentions...) {
		current.mentionAlias(username)
	}
	userMessage, hasReplyContext := current.userMessage(c.cfg.Name, request)
	if hasReplyContext {
		c.metrics.observeAssistantContext(componentTelegramAssistant, "reply")
	}
	var externalContext string
	usedPtchanContext := false
	if contextText, ok := c.ptchan.ForText(ctx, assistant.PtchanContextRequest{Text: request.Text, ReplyText: request.ReplyText}); ok {
		c.metrics.observeAssistantContext(componentTelegramAssistant, "ptchan")
		externalContext = contextText
		usedPtchanContext = true
	}
	messages = append(messages, deepseek.Message{Role: deepseek.RoleUser, Content: formatTelegramCurrentRequest(userAlias, userMessage, len(storedBefore)/2, hasReplyContext, externalContext)})
	systemPrompt := c.cfg.SystemPrompt
	trace := &assistant.Trace{
		StartedAt:     startedAt,
		MessageID:     request.MessageID,
		ThreadID:      request.MessageThreadID,
		UserAlias:     userAlias,
		UsedHistory:   len(storedBefore) > 0,
		UsedReply:     hasReplyContext,
		UsedPtchan:    usedPtchanContext,
		StoredBefore:  storedBefore,
		StoredAfter:   append([]deepseek.Message(nil), storedBefore...),
		SystemPrompt:  systemPrompt,
		ModelMessages: append([]deepseek.Message(nil), messages...),
	}
	defer func() { c.dumpTrace(trace) }()
	if c.cfg.LogMemory {
		c.logger.Debug("telegram assistant memory system prompt", "content", systemPrompt)
	}
	c.logMemory("request", key, messages)
	completion, err := c.completer.Complete(ctx, systemPrompt, messages)
	trace.Completion = completion
	stopTyping()
	<-typingDone
	c.metrics.observeModelCompletion(componentTelegramAssistant, "deepseek", c.modelName, time.Since(startedAt), completion, err)
	if err != nil {
		trace.Outcome = "completion error"
		trace.Err = err
		c.logger.Warn("telegram assistant completion failed", "message_id", request.MessageID, "chat", request.ChatTitle, "chat_id", c.cfg.DiscussionChatID, "error", err)
		if ctx.Err() != nil {
			c.discardEmptyConversation(key)
			return false
		}
		c.sendReply(ctx, request, telegram.TextMessage(c.text.Text(localization.AssistantTemporaryFailure, "I couldn't answer that right now.")))
		c.discardEmptyConversation(key)
		return ctx.Err() == nil
	}

	text, ok := c.completionText(completion)
	generated := completion.FinishReason == deepseek.FinishStop || completion.FinishReason == deepseek.FinishLength
	if !ok {
		c.logger.Warn("telegram assistant completion has unexpected finish reason", "message_id", request.MessageID, "chat", request.ChatTitle, "chat_id", c.cfg.DiscussionChatID, "finish_reason", completion.FinishReason)
		text = c.text.Text(localization.AssistantUnexpectedFailure, "I couldn't answer that right now. Apparently even machines have off days.")
	}
	renderedText := assistant.TruncateRunes(current.renderAliases(text), 4096)
	message := telegram.TextMessage(renderedText)
	if generated {
		message = telegram.MarkdownMessage(renderedText)
	}
	if !c.sendReply(ctx, request, message) {
		trace.Outcome = "delivery error"
		c.discardEmptyConversation(key)
		return ctx.Err() == nil
	}
	removed := current.remember(userAlias, userMessage, text, time.Now(), c.cfg.HistoryExchanges)
	trace.Outcome = "stored"
	trace.StoredAfter = current.messages()
	trace.RemovedExchanges = removed
	c.metrics.setActiveConversations(componentTelegramAssistant, len(c.history))
	if removed > 0 && c.cfg.LogMemory {
		c.logger.Debug("telegram assistant memory evicted", "chat_id", key.chatID, "thread_id", key.threadID, "removed", removed, "remaining", len(current.exchanges), "runes", current.runes())
	}
	c.logMemory("stored", key, current.messages())
	return true
}

func (c *telegramAssistant) dumpTrace(trace *assistant.Trace) {
	if c.traces == nil {
		return
	}
	path, err := c.traces.Dump(trace)
	if err != nil {
		c.logger.Warn("telegram assistant trace dump failed", "message_id", trace.MessageID, "thread_id", trace.ThreadID, "error", err)
		return
	}
	c.logger.Info("telegram assistant trace dumped", "trace_id", filepath.Base(path), "message_id", trace.MessageID, "thread_id", trace.ThreadID, "path", path)
}

func (c *telegramAssistant) discardEmptyConversation(key conversationKey) {
	conversation := c.history[key]
	if conversation == nil || len(conversation.exchanges) == 0 {
		delete(c.history, key)
	}
}

func (c *telegramAssistant) expireConversations(now time.Time) {
	for existingKey, conversation := range c.history {
		removed := conversation.expire(now, c.cfg.ConversationTTL)
		if removed > 0 && c.cfg.LogMemory {
			c.logger.Debug("telegram assistant memory expired", "chat_id", existingKey.chatID, "thread_id", existingKey.threadID, "removed", removed, "remaining", len(conversation.exchanges))
		}
		if len(conversation.exchanges) == 0 {
			delete(c.history, existingKey)
			continue
		}
	}
	c.metrics.setActiveConversations(componentTelegramAssistant, len(c.history))
}

func (c *telegramAssistant) logMemory(stage string, key conversationKey, messages []deepseek.Message) {
	if !c.cfg.LogMemory {
		return
	}
	conversation := c.history[key]
	c.logger.Debug("telegram assistant memory", "stage", stage, "chat_id", key.chatID, "thread_id", key.threadID, "exchanges", len(conversation.exchanges), "participants", len(conversation.participants), "mentions", len(conversation.mentions), "messages", len(messages))
	for i, message := range messages {
		c.logger.Debug("telegram assistant memory message", "stage", stage, "index", i, "role", message.Role, "content", message.Content)
	}
}

func (c *telegramAssistant) showTyping(ctx context.Context, request telegramAssistantRequest) {
	ticker := time.NewTicker(typingInterval)
	defer ticker.Stop()

	for {
		if err := c.sender.SendTyping(ctx, c.cfg.DiscussionChatID, request.MessageThreadID); err != nil && ctx.Err() == nil {
			c.logger.Debug("send typing action failed", "message_id", request.MessageID, "chat", request.ChatTitle, "chat_id", c.cfg.DiscussionChatID, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *telegramAssistant) replyToRejection(ctx context.Context, message *telegram.IncomingMessage, result admissionResult) {
	var text string
	switch result {
	case admissionTooLong:
		text = c.text.Text(localization.AssistantTooLong, "That message is too long. A little restraint won't kill you.")
	case admissionRateLimited:
		text = c.text.Text(localization.AssistantRateLimited, "Slow down. I can only endure so much of you at once.")
	default:
		return
	}
	if !c.replies.Allow() {
		return
	}

	err := c.sender.Send(ctx, telegram.SendRequest{
		ChatID:           c.cfg.DiscussionChatID,
		Message:          telegram.TextMessage(text),
		ReplyToMessageID: message.ID,
		MessageThreadID:  message.MessageThreadID,
	})
	if err != nil && ctx.Err() == nil {
		c.logger.Warn("send telegram assistant rejection failed", "message_id", message.ID, "chat", message.Chat.Title, "chat_id", c.cfg.DiscussionChatID, "error", err)
	}
}

func (c *telegramAssistant) sendReply(ctx context.Context, request telegramAssistantRequest, message telegram.OutgoingMessage) bool {
	err := c.sender.Send(ctx, telegram.SendRequest{
		ChatID:           c.cfg.DiscussionChatID,
		Message:          message,
		ReplyToMessageID: request.MessageID,
		MessageThreadID:  request.MessageThreadID,
	})
	if err != nil {
		c.metrics.observeAssistantReply(componentTelegramAssistant, metricResultError)
		c.logger.Warn("send telegram assistant reply failed", "message_id", request.MessageID, "chat", request.ChatTitle, "chat_id", c.cfg.DiscussionChatID, "error", err)
		return false
	}

	c.metrics.observeAssistantReply(componentTelegramAssistant, metricResultSuccess)
	c.logger.Info("telegram assistant message answered", "message_id", request.MessageID, "chat", request.ChatTitle, "chat_id", c.cfg.DiscussionChatID, "user_id", request.UserID)
	return true
}

func (c *telegramAssistant) completionText(completion deepseek.Completion) (string, bool) {
	switch completion.FinishReason {
	case deepseek.FinishStop, deepseek.FinishLength:
		return completion.Text, completion.Text != ""
	case deepseek.FinishContentFilter:
		return c.text.Text(localization.AssistantFiltered, "I can't help with that request. Yes, even I have standards."), true
	default:
		return "", false
	}
}

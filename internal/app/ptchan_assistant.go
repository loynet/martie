package app

import (
	"context"
	"log/slog"
	"strings"
	"unicode/utf8"

	"martie/internal/gateway"
)

type ptchanAssistant struct {
	cfg             PtchanAssistantConfig
	integrationName string
	logger          *slog.Logger
	metrics         *metrics
}

type ptchanAssistantRequest struct {
	EventID  string
	Board    string
	ThreadID int64
	PostID   int64
	Text     string
	Mention  string
}

func (a ptchanAssistant) run(ctx context.Context) error {
	a.logger.Info("ptchan assistant active", "mentions", a.cfg.Mentions)
	<-ctx.Done()
	return nil
}

func (a ptchanAssistant) consumeGatewayEvent(_ context.Context, event gateway.Event) error {
	request, result := a.admit(event)
	if a.metrics != nil {
		a.metrics.observeAssistantAdmission(componentPtchanAssistant, result)
	}
	if result != admissionAccepted {
		a.logger.Debug("ptchan assistant event ignored", "event_id", event.EventID, "reason", result)
		return nil
	}

	a.logger.Info("ptchan assistant mention admitted", "event_id", request.EventID, "board", request.Board, "thread_id", request.ThreadID, "post_id", request.PostID, "mention", request.Mention)
	return nil
}

func (a ptchanAssistant) admit(event gateway.Event) (*ptchanAssistantRequest, admissionResult) {
	if event.Kind != gateway.KindPostCreated {
		return nil, admissionUnsupported
	}
	if a.integrationName != "" && event.Post.Origin != nil && event.Post.Origin.Kind == "integration" && strings.EqualFold(event.Post.Origin.Name, a.integrationName) {
		return nil, admissionBot
	}
	text := strings.TrimSpace(event.Post.Message)
	if text == "" {
		return nil, admissionEmpty
	}
	if utf8.RuneCountInString(text) > a.cfg.MaxInputRunes {
		return nil, admissionTooLong
	}
	mention, index, ok := firstConfiguredMention(text, a.cfg.Mentions)
	if !ok {
		return nil, admissionUnaddressed
	}
	return &ptchanAssistantRequest{
		EventID:  event.EventID,
		Board:    event.Post.Board,
		ThreadID: event.Post.ThreadID,
		PostID:   event.Post.PostID,
		Text:     strings.TrimSpace(removeMentionAt(text, mention, index)),
		Mention:  mention,
	}, admissionAccepted
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

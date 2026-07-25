package app

import (
	"strings"
	"testing"

	"martie/internal/gateway"
)

func TestPtchanAssistantAdmitsConfiguredMention(t *testing.T) {
	assistant := ptchanAssistant{
		cfg: PtchanAssistantConfig{
			Mentions:      []string{"@martie"},
			MaxInputRunes: 100,
		},
		integrationName: "martie",
		logger:          discardLogger(),
	}

	request, result := assistant.admit(gateway.Event{
		EventID: "event-1",
		Kind:    gateway.KindPostCreated,
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

func TestPtchanAssistantAdmissionRejectsOwnIntegrationPosts(t *testing.T) {
	assistant := ptchanAssistant{
		cfg:             PtchanAssistantConfig{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		integrationName: "martie",
		logger:          discardLogger(),
	}

	_, result := assistant.admit(gateway.Event{
		Kind: gateway.KindPostCreated,
		Post: gateway.Post{
			Message: "@martie hello",
			Origin:  &gateway.PostOrigin{Kind: "integration", Name: "Martie"},
		},
	})
	if result != admissionBot {
		t.Fatalf("admission = %q, want bot", result)
	}
}

func TestPtchanAssistantAdmissionRequiresMentionBoundary(t *testing.T) {
	assistant := ptchanAssistant{
		cfg:    PtchanAssistantConfig{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		logger: discardLogger(),
	}

	_, result := assistant.admit(gateway.Event{
		Kind: gateway.KindPostCreated,
		Post: gateway.Post{Message: "hello @martie_bot"},
	})
	if result != admissionUnaddressed {
		t.Fatalf("admission = %q, want unaddressed", result)
	}
}

func TestPtchanAssistantAdmissionStripsMatchedMention(t *testing.T) {
	assistant := ptchanAssistant{
		cfg:    PtchanAssistantConfig{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		logger: discardLogger(),
	}

	request, result := assistant.admit(gateway.Event{
		Kind: gateway.KindPostCreated,
		Post: gateway.Post{Message: "ignore @martie_bot but @martie answer this"},
	})
	if result != admissionAccepted {
		t.Fatalf("admission = %q", result)
	}
	if request.Text != "ignore @martie_bot but  answer this" {
		t.Fatalf("text = %q", request.Text)
	}
}

func TestPtchanAssistantAdmissionFindsMentionAfterUnicode(t *testing.T) {
	assistant := ptchanAssistant{
		cfg:    PtchanAssistantConfig{Mentions: []string{"@martie"}, MaxInputRunes: 100},
		logger: discardLogger(),
	}

	request, result := assistant.admit(gateway.Event{
		Kind: gateway.KindPostCreated,
		Post: gateway.Post{Message: "olá @Martie responde"},
	})
	if result != admissionAccepted {
		t.Fatalf("admission = %q", result)
	}
	if request.Text != "olá  responde" {
		t.Fatalf("text = %q", request.Text)
	}
}

func TestPtchanAssistantAdmissionRejectsLongPost(t *testing.T) {
	assistant := ptchanAssistant{
		cfg:    PtchanAssistantConfig{Mentions: []string{"@martie"}, MaxInputRunes: 10},
		logger: discardLogger(),
	}

	_, result := assistant.admit(gateway.Event{
		Kind: gateway.KindPostCreated,
		Post: gateway.Post{Message: "@martie " + strings.Repeat("x", 20)},
	})
	if result != admissionTooLong {
		t.Fatalf("admission = %q, want too_long", result)
	}
}

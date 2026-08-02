package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	channerapp "martie/internal/apps/channer"
)

func TestLoadConfigSelectsAppSettings(t *testing.T) {
	t.Setenv("CONFIG_FILE", writeConfig(t, selectedConfig))
	t.Setenv("TELEGRAM_BOT_TOKEN", " token ")
	t.Setenv("DEEPSEEK_API_KEY", " key ")
	t.Setenv("PTCHAN_INTEGRATION_MARTIE_CHATTER_SECRET", " chatter-secret ")
	t.Setenv("PTCHAN_INTEGRATION_MARTIE_CHANNER_SECRET", " channer-secret ")
	t.Setenv("PTCHAN_INTEGRATION_MARTIE_THREAD_SECRET", " thread-secret ")
	t.Setenv("CHANNER_NO_REPLY_ANCHOR", " channer-anchor ")

	tests := []struct {
		app         AppName
		integration string
		secret      string
		check       func(*testing.T, Config)
	}{
		{
			app:         AppChatter,
			integration: "martie-chatter",
			secret:      "chatter-secret",
			check: func(t *testing.T, cfg Config) {
				if cfg.Chatter.Name != "Marta" || cfg.Chatter.SystemPrompt != "Marta chats." {
					t.Fatalf("chatter identity = (%q, %q)", cfg.Chatter.Name, cfg.Chatter.SystemPrompt)
				}
				if cfg.Chatter.ConversationTTL != 15*time.Minute {
					t.Fatalf("chatter durations = %+v", cfg.Chatter)
				}
				if cfg.Chatter.PtchanContext.MaxReplies != 4 {
					t.Fatalf("chatter context = %+v", cfg.Chatter.PtchanContext)
				}
			},
		},
		{
			app:         AppChanner,
			integration: "martie-channer",
			secret:      "channer-secret",
			check: func(t *testing.T, cfg Config) {
				if cfg.Channer.SystemPrompt != "Marta replies." || cfg.Channer.PruneAfter != 48*time.Hour {
					t.Fatalf("channer config = %+v", cfg.Channer)
				}
				if len(cfg.Channer.Mentions) != 2 || cfg.Channer.Mentions[1] != "@Marta" {
					t.Fatalf("channer mentions = %v", cfg.Channer.Mentions)
				}
				if cfg.Channer.NoReplyAnchor != "channer-anchor" {
					t.Fatalf("channer anchor = %q", cfg.Channer.NoReplyAnchor)
				}
				if cfg.Channer.RequestLimit != 18 || cfg.Channer.RequestBurst != 2 ||
					cfg.Channer.ThreadRequestLimit != 7 || cfg.Channer.ThreadRequestBurst != 2 {
					t.Fatalf("channer rate limits = %+v", cfg.Channer)
				}
			},
		},
		{
			app:         AppThreadNotifier,
			integration: "martie-thread",
			secret:      "thread-secret",
			check: func(t *testing.T, cfg Config) {
				if cfg.ThreadNotifier.MinReplyPosts != 6 || cfg.ThreadNotifier.PruneAfter != 24*time.Hour {
					t.Fatalf("threadnotifier config = %+v", cfg.ThreadNotifier)
				}
			},
		},
		{
			app: AppStreamNotifier,
			check: func(t *testing.T, cfg Config) {
				if cfg.StreamNotifier.PollInterval != 2*time.Minute || len(cfg.StreamNotifier.Channels) != 1 {
					t.Fatalf("streamnotifier config = %+v", cfg.StreamNotifier)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(string(test.app), func(t *testing.T) {
			cfg, err := LoadConfig(test.app)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.App != test.app {
				t.Fatalf("selected app = %q", cfg.App)
			}
			if cfg.Ptchan.IntegrationName != test.integration || cfg.Ptchan.Secret != test.secret {
				t.Fatalf("ptchan = %+v", cfg.Ptchan)
			}
			wantDeepSeekKey := ""
			if test.app == AppChatter || test.app == AppChanner {
				wantDeepSeekKey = "key"
			}
			if cfg.Telegram.BotToken != "token" || cfg.DeepSeek.APIKey != wantDeepSeekKey {
				t.Fatalf("selected secrets = telegram %q deepseek %q", cfg.Telegram.BotToken, cfg.DeepSeek.APIKey)
			}
			test.check(t, cfg)
		})
	}
}

func TestLoadConfigIgnoresUnrelatedSemanticValues(t *testing.T) {
	t.Setenv("CONFIG_FILE", writeConfig(t, `
[deepseek]
model = ""

[chatter.memory]
ttl = "also invalid"

[gateway.webhook]
addr = ""

[streamnotifier]
poll_interval = "30s"

[[streamnotifier.channel]]
key = "live"
probe_url = "https://stream.example/live"
page_url = "https://example/live"
`))

	if _, err := LoadConfig(AppStreamNotifier); err != nil {
		t.Fatalf("streamnotifier rejected unrelated settings: %v", err)
	}

	t.Setenv("CONFIG_FILE", writeConfig(t, strings.Replace(selectedConfig, `gateway_url = "http://gateway.example/"`, `gateway_url = ""`, 1)))
	if _, err := LoadConfig(AppThreadNotifier); err != nil {
		t.Fatalf("threadnotifier required unused gateway URL: %v", err)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("CONFIG_FILE", writeConfig(t, `
name = "Martie"

[telegram]
allow_all_users = true

[chatter]
system_prompt = "You are {{name}}."
`))

	cfg, err := LoadConfig(AppChatter)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.App != AppChatter || cfg.Chatter.MaxInputRunes != 4096 || cfg.Chatter.ConversationTTL != 10*time.Minute || cfg.Chatter.HistoryExchanges != 8 {
		t.Fatalf("chatter defaults = %+v", cfg.Chatter)
	}
	if cfg.Chatter.PtchanContext.MaxReplies != 25 {
		t.Fatalf("ptchan context defaults = %+v", cfg.Chatter.PtchanContext)
	}
	if cfg.SQLitePath != "data/martie.db" {
		t.Fatalf("sqlite path = %q", cfg.SQLitePath)
	}
}

func TestLoadConfigIsStrict(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     string
	}{
		{"unknown key", "surprise = true", "surprise"},
		{"duplicate key", "name = \"one\"\nname = \"two\"", "name"},
		{"wrong type", "[streamnotifier]\npoll_interval = 2", "PollInterval"},
		{"malformed", `name = "unterminated`, "decode config"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CONFIG_FILE", writeConfig(t, test.contents))
			if _, err := LoadConfig(AppStreamNotifier); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadConfig() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadConfigRejectsInvalidSelectedValues(t *testing.T) {
	tests := []struct {
		name        string
		app         AppName
		old         string
		replacement string
		want        string
	}{
		{"chatter limit", AppChatter, "max_input_runes = 2000", "max_input_runes = 0", "chatter.max_input_runes"},
		{"chatter duration", AppChatter, `ttl = "15m"`, `ttl = "later"`, "chatter.memory.ttl"},
		{"channer rate", AppChanner, "request_burst = 2", "request_burst = 0", "channer.rate_limit"},
		{"channer thread rate", AppChanner, "thread_burst = 2", "thread_burst = 0", "channer.rate_limit.thread_burst"},
		{"channer retention", AppChanner, `prune_after = "48h"`, `prune_after = "-1s"`, "channer.prune_after"},
		{"thread retention", AppThreadNotifier, `prune_after = "24h"`, `prune_after = "-1s"`, "threadnotifier.prune_after"},
		{"stream interval", AppStreamNotifier, `poll_interval = "2m"`, `poll_interval = "later"`, "streamnotifier.poll_interval"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contents := strings.Replace(selectedConfig, test.old, test.replacement, 1)
			t.Setenv("CONFIG_FILE", writeConfig(t, contents))
			if _, err := LoadConfig(test.app); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadConfig() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateRunUsesOnlySelectedDependencies(t *testing.T) {
	base := Config{
		App:      AppChanner,
		Ptchan:   PtchanConfig{IntegrationName: "martie", Secret: "secret"},
		Channer:  channerConfigForTest(),
		DeepSeek: DeepSeekConfig{APIKey: "key"},
	}
	if err := base.ValidateRun(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		change func(*Config)
		want   string
	}{
		{"channer API key", func(cfg *Config) { cfg.DeepSeek.APIKey = "" }, "DEEPSEEK_API_KEY"},
		{"channer secret", func(cfg *Config) { cfg.Ptchan.Secret = "" }, "PTCHAN_INTEGRATION_MARTIE_SECRET"},
		{"channer no-reply anchor", func(cfg *Config) { cfg.Channer.NoReplyAnchor = "" }, "CHANNER_NO_REPLY_ANCHOR"},
		{"thread Telegram", func(cfg *Config) {
			cfg.App = AppThreadNotifier
			cfg.Telegram = TelegramConfig{NotificationChatID: 1}
		}, "TELEGRAM_BOT_TOKEN"},
		{"stream channels", func(cfg *Config) {
			cfg.App = AppStreamNotifier
			cfg.Telegram = TelegramConfig{BotToken: "token", NotificationChatID: 1}
			cfg.StreamNotifier.Channels = nil
		}, "at least one channel"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			test.change(&cfg)
			if err := cfg.ValidateRun(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateRun() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestExampleConfigLoadsForEveryApp(t *testing.T) {
	t.Setenv("CONFIG_FILE", filepath.Join("..", "..", "config", "example.toml"))
	for _, app := range []AppName{AppChatter, AppChanner, AppThreadNotifier, AppStreamNotifier} {
		t.Run(string(app), func(t *testing.T) {
			cfg, err := LoadConfig(app)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.App != app || cfg.SQLitePath == "" {
				t.Fatalf("config = app %q sqlite path %q", cfg.App, cfg.SQLitePath)
			}
		})
	}
}

func TestLoadConfigRequiresFile(t *testing.T) {
	t.Setenv("CONFIG_FILE", " ")
	if _, err := LoadConfig(AppChatter); err == nil {
		t.Fatal("missing CONFIG_FILE was accepted")
	}
}

func TestLoadConfigRejectsUnknownApp(t *testing.T) {
	if _, err := LoadConfig("unknown"); err == nil {
		t.Fatal("unknown app was accepted")
	}
}

func channerConfigForTest() channerapp.Config {
	return channerapp.Config{Name: "Martie", Mentions: []string{"@martie"}, SystemPrompt: "Be useful.", NoReplyAnchor: "anchor"}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const selectedConfig = `
locale = "pt-PT"
name = "Marta"

[telegram]
notification_chat_id = 123
discussion_chat_id = -456
allowed_user_ids = [7, 8]

[ptchan]
base_url = "https://ptchan.example/"
gateway_url = "http://gateway.example/"
integration_name = "martie"

[ptchan.chatter]
integration_name = "martie-chatter"
[ptchan.channer]
integration_name = "martie-channer"
[ptchan.threadnotifier]
integration_name = "martie-thread"

[chatter]
max_input_runes = 2000
system_prompt = "{{name}} chats."
[chatter.memory]
ttl = "15m"
history_exchanges = 6
[chatter.rate_limit]
user_limit = 20
user_burst = 4
global_limit = 80
global_burst = 10
[chatter.ptchan_context]
max_replies = 4

[channer]
mentions = ["@martie", "@Marta"]
max_input_runes = 1200
prune_after = "48h"
system_prompt = "{{name}} replies."
[channer.rate_limit]
request_limit = 18
request_burst = 2
thread_limit = 7
thread_burst = 2

[deepseek]
model = "deepseek-test"
max_tokens = 300

[gateway.webhook]
addr = ":8082"

[threadnotifier]
min_reply_posts = 6
max_thread_age = "12h"
prune_after = "24h"

[streamnotifier]
poll_interval = "2m"
[[streamnotifier.channel]]
key = "live"
probe_url = "https://stream.example/live"
page_url = "https://example/live"

[runtime.logging]
level = "debug"
format = "json"

[storage]
sqlite_path = "data/base.db"
`

package app

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"martie/internal/localization"
	"martie/internal/miau"
)

func TestLoadConfig(t *testing.T) {
	path := writeConfig(t, `
locale = "pt-PT"
name = "Marta"

[telegram]
notification_chat_id = 123
discussion_chat_id = -456
allowed_user_ids = [7, 8]

[ptchan]
base_url = "https://gateway-links.example.com/"
gateway_url = "http://ptchan-gateway.example.com/"
integration_name = "martie-test"

[assistant]
max_input_runes = 2000
	log_memory = true
	system_prompt = " {{name}} is {{name}}. "

[assistant.memory]
ttl = "15m"
history_exchanges = 6

[assistant.rate_limit]
window = "30m"
user_limit = 20
user_burst = 4
global_limit = 80
global_burst = 10

[assistant.ptchan_context]
timeout = "3s"
max_replies = 4

[assistant.trace]
dir = "data/test-traces"
max_files = 25

[deepseek]
model = "deepseek-test"
max_tokens = 300
timeout = "45s"

[gateway.webhook]
addr = ":8082"
path = "internal/gateway/events"

[gateway.notifications]
min_reply_posts = 6
board_denylist = ["x"]
keyword_denylist = ["y"]
max_thread_age = "12h"
prune_after = "48h"

[runtime]
components = ["gateway", "assistant"]
http_addr = ":9090"

[runtime.logging]
level = "debug"
format = "json"

[streams]
end_miss_threshold = 3
poll_interval = "2m"

[storage]
sqlite_path = "data/from-config.db"

[[streams.channel]]
key = "live"
probe_url = "https://stream.example.com/live"
page_url = "https://example.com/live"
`)
	t.Setenv("CONFIG_FILE", path)
	t.Setenv("TELEGRAM_BOT_TOKEN", " token ")
	t.Setenv("DEEPSEEK_API_KEY", " key ")
	t.Setenv("PTCHAN_INTEGRATION_MARTIE_TEST_SECRET", " gateway-secret ")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Assistant.Name != "Marta" || cfg.Assistant.SystemPrompt != "Marta is Marta." {
		t.Fatalf("identity = (%q, %q)", cfg.Assistant.Name, cfg.Assistant.SystemPrompt)
	}
	if cfg.Locale != localization.PortuguesePortugal {
		t.Fatalf("locale = %q", cfg.Locale)
	}
	if cfg.Telegram.BotToken != "token" || cfg.DeepSeek.APIKey != "key" {
		t.Fatalf("secrets were not loaded from the environment")
	}
	if cfg.Telegram.NotificationChatID != 123 || cfg.Assistant.DiscussionChatID != -456 || cfg.Assistant.AllowAllUsers || len(cfg.Assistant.AllowedUserIDs) != 2 || cfg.Assistant.AllowedUserIDs[0] != 7 || cfg.Assistant.AllowedUserIDs[1] != 8 {
		t.Fatalf("telegram config = %+v, assistant = %+v", cfg.Telegram, cfg.Assistant)
	}
	if cfg.Assistant.RateLimitWindow != 30*time.Minute || cfg.Assistant.ConversationTTL != 15*time.Minute || cfg.DeepSeek.Timeout != 45*time.Second || cfg.Streams.PollInterval != 2*time.Minute {
		t.Fatalf("durations were not parsed: %+v", cfg)
	}
	if len(cfg.Runtime.Components) != 2 || !cfg.runs(componentGateway) || !cfg.runs(componentAssistant) || cfg.runs(componentStreams) {
		t.Fatalf("components = %+v", cfg.Runtime.Components)
	}
	if cfg.Assistant.HistoryExchanges != 6 || cfg.Assistant.MaxInputRunes != 2000 || !cfg.Assistant.LogMemory || cfg.Assistant.UserRequestLimit != 20 || cfg.Assistant.UserRequestBurst != 4 || cfg.Assistant.GlobalRequestLimit != 80 || cfg.Assistant.GlobalRequestBurst != 10 {
		t.Fatalf("assistant config = %+v", cfg.Assistant)
	}
	if !cfg.Assistant.PtchanContext.Enabled || cfg.Assistant.PtchanContext.BaseURL != "https://gateway-links.example.com" || cfg.Assistant.PtchanContext.GatewayURL != "http://ptchan-gateway.example.com" || cfg.Assistant.PtchanContext.Timeout != 3*time.Second || cfg.Assistant.PtchanContext.MaxReplies != 4 {
		t.Fatalf("ptchan context config = %+v", cfg.Assistant.PtchanContext)
	}
	if !cfg.Assistant.Trace.Enabled || cfg.Assistant.Trace.Dir != "data/test-traces" || cfg.Assistant.Trace.MaxFiles != 25 {
		t.Fatalf("assistant trace config = %+v", cfg.Assistant.Trace)
	}
	if cfg.DeepSeek.Model != "deepseek-test" || cfg.DeepSeek.MaxTokens != 300 {
		t.Fatalf("deepseek config = %+v", cfg.DeepSeek)
	}
	if cfg.Ptchan.IntegrationName != "martie-test" || cfg.Ptchan.Secret != "gateway-secret" || cfg.Ptchan.BaseURL != "https://gateway-links.example.com" || cfg.Ptchan.GatewayURL != "http://ptchan-gateway.example.com" {
		t.Fatalf("ptchan config = %+v", cfg.Ptchan)
	}
	if cfg.Gateway.Webhook.Addr != ":8082" || cfg.Gateway.Webhook.Path != "/internal/gateway/events" || cfg.Gateway.Notifications.MinReplyPosts != 6 || cfg.Gateway.Notifications.Filter.MaxThreadAge != 12*time.Hour || cfg.Gateway.Notifications.PruneAfter != 48*time.Hour || len(cfg.Gateway.Notifications.Filter.BoardDenylist) != 1 || cfg.Gateway.Notifications.Filter.BoardDenylist[0] != "x" || len(cfg.Gateway.Notifications.Filter.KeywordDenylist) != 1 || cfg.Gateway.Notifications.Filter.KeywordDenylist[0] != "y" {
		t.Fatalf("gateway config = %+v", cfg.Gateway)
	}
	if cfg.Runtime.HTTPAddr != ":9090" || cfg.Storage.SQLitePath != "data/from-config.db" {
		t.Fatalf("runtime = %+v, storage = %+v", cfg.Runtime, cfg.Storage)
	}
	if cfg.Runtime.Logging.Level != slog.LevelDebug || cfg.Runtime.Logging.Format != LogJSON {
		t.Fatalf("logging = %+v", cfg.Runtime.Logging)
	}
	if cfg.Streams.EndMissThreshold != 3 || len(cfg.Streams.Channels) != 1 || cfg.Streams.Channels[0].Key != "live" || cfg.Streams.Channels[0].ProbeURL != "https://stream.example.com/live" || cfg.Streams.Channels[0].PageURL != "https://example.com/live" {
		t.Fatalf("streams = %+v", cfg.Streams)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	path := writeConfig(t, `
name = "Martie"

[telegram]
allow_all_users = true

[assistant]
	system_prompt = "You are {{name}}."
`)
	t.Setenv("CONFIG_FILE", path)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Locale != localization.English || !cfg.Assistant.AllowAllUsers || cfg.Assistant.MaxInputRunes != 4096 || cfg.Assistant.ConversationTTL != 10*time.Minute || cfg.Assistant.HistoryExchanges != 8 || cfg.Assistant.RateLimitWindow != time.Hour || cfg.Assistant.UserRequestLimit != 25 || cfg.Assistant.UserRequestBurst != 6 || cfg.Assistant.GlobalRequestLimit != 100 || cfg.Assistant.GlobalRequestBurst != 12 {
		t.Fatalf("assistant defaults were not applied: %+v", cfg.Assistant)
	}
	if cfg.Assistant.PtchanContext.Enabled || cfg.Assistant.PtchanContext.BaseURL != "https://ptchan.org" || cfg.Assistant.PtchanContext.GatewayURL != "http://ptchan-gateway:8080" || cfg.Assistant.PtchanContext.Timeout != 5*time.Second || cfg.Assistant.PtchanContext.MaxReplies != defaultPtchanMaxReplies {
		t.Fatalf("ptchan context defaults were not applied: %+v", cfg.Assistant.PtchanContext)
	}
	if cfg.Assistant.Trace.Enabled || cfg.Assistant.Trace.Dir != "data/traces" || cfg.Assistant.Trace.MaxFiles != 100 {
		t.Fatalf("assistant trace defaults were not applied: %+v", cfg.Assistant.Trace)
	}
	if cfg.DeepSeek.Model != "deepseek-v4-flash" || cfg.DeepSeek.MaxTokens != 500 || cfg.DeepSeek.Timeout != time.Minute || cfg.Ptchan.IntegrationName != "martie" || cfg.Ptchan.BaseURL != "https://ptchan.org" || cfg.Ptchan.GatewayURL != "http://ptchan-gateway:8080" || cfg.Gateway.Webhook.Addr != ":8081" || cfg.Gateway.Webhook.Path != "/internal/ptchan/events" || cfg.Gateway.Notifications.MinReplyPosts != 10 || cfg.Gateway.Notifications.Filter.MaxThreadAge != 0 || cfg.Gateway.Notifications.PruneAfter != 720*time.Hour || cfg.Streams.PollInterval != time.Minute || cfg.Runtime.Logging.Level != slog.LevelInfo || cfg.Runtime.Logging.Format != LogText || cfg.Streams.EndMissThreshold != 2 || cfg.Storage.SQLitePath != "data/bot.db" {
		t.Fatalf("defaults were not applied: %+v", cfg)
	}
	if len(cfg.Runtime.Components) != 0 {
		t.Fatalf("default components = %+v, want none", cfg.Runtime.Components)
	}
}

func TestLoadConfigRejectsUnknownKeys(t *testing.T) {
	path := writeConfig(t, `
name = "Martie"
surprise = true

[assistant]
system_prompt = "Hello."
`)
	t.Setenv("CONFIG_FILE", path)

	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "surprise") {
		t.Fatalf("unknown key error = %v", err)
	}
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name        string
		old         string
		replacement string
		want        string
	}{
		{name: "unsupported locale", old: `locale = "en"`, replacement: `locale = "pt"`, want: "locale must be"},
		{name: "zero input limit", old: "max_input_runes = 4096", replacement: "max_input_runes = 0", want: "assistant.max_input_runes"},
		{name: "zero history limit", old: "history_exchanges = 8", replacement: "history_exchanges = 0", want: "assistant.memory.history_exchanges"},
		{name: "zero user limit", old: "user_limit = 25", replacement: "user_limit = 0", want: "assistant.rate_limit.user_burst"},
		{name: "zero user burst", old: "user_burst = 6", replacement: "user_burst = 0", want: "assistant.rate_limit.user_burst"},
		{name: "user burst above limit", old: "user_burst = 6", replacement: "user_burst = 26", want: "assistant.rate_limit.user_burst"},
		{name: "zero global limit", old: "global_limit = 100", replacement: "global_limit = 0", want: "assistant.rate_limit.global_burst"},
		{name: "zero global burst", old: "global_burst = 12", replacement: "global_burst = 0", want: "assistant.rate_limit.global_burst"},
		{name: "global burst above limit", old: "global_burst = 12", replacement: "global_burst = 101", want: "assistant.rate_limit.global_burst"},
		{name: "zero ptchan context replies", old: "max_replies = 25", replacement: "max_replies = 0", want: "assistant.ptchan_context.max_replies"},
		{name: "invalid ptchan context timeout", old: `timeout = "5s"`, replacement: `timeout = "later"`, want: "assistant.ptchan_context.timeout"},
		{name: "empty assistant trace dir", old: `dir = "data/traces"`, replacement: `dir = " "`, want: "assistant.trace.dir"},
		{name: "zero assistant trace files", old: "max_files = 100", replacement: "max_files = 0", want: "assistant.trace.max_files"},
		{name: "empty model", old: `model = "deepseek-v4-flash"`, replacement: `model = " "`, want: "deepseek.model"},
		{name: "zero max tokens", old: "max_tokens = 500", replacement: "max_tokens = 0", want: "deepseek.max_tokens"},
		{name: "empty ptchan integration", old: `integration_name = "martie"`, replacement: `integration_name = " "`, want: "ptchan.integration_name"},
		{name: "empty ptchan base URL", old: `base_url = "https://gateway.example"`, replacement: `base_url = " "`, want: "ptchan.base_url"},
		{name: "empty ptchan gateway URL", old: `gateway_url = "http://ptchan-gateway:8080"`, replacement: `gateway_url = " "`, want: "ptchan.gateway_url"},
		{name: "empty gateway addr", old: `addr = ":8081"`, replacement: `addr = " "`, want: "gateway.webhook.addr"},
		{name: "empty gateway path", old: `path = "/internal/ptchan/events"`, replacement: `path = " "`, want: "gateway.webhook.path"},
		{name: "negative gateway reply posts", old: "min_reply_posts = 11", replacement: "min_reply_posts = -1", want: "gateway.notifications.min_reply_posts"},
		{name: "zero stream misses", old: "end_miss_threshold = 2", replacement: "end_miss_threshold = 0", want: "streams.end_miss_threshold"},
		{name: "empty stream key", old: `key = "oficial"`, replacement: `key = " "`, want: "requires key, probe_url, and page_url"},
		{name: "empty stream probe", old: `probe_url = "https://stream.example.com/live"`, replacement: `probe_url = " "`, want: "requires key, probe_url, and page_url"},
		{name: "empty stream page", old: `page_url = "https://example.com/live"`, replacement: `page_url = " "`, want: "requires key, probe_url, and page_url"},
		{name: "invalid memory TTL", old: `ttl = "10m"`, replacement: `ttl = "later"`, want: "assistant.memory.ttl"},
		{name: "zero memory TTL", old: `ttl = "10m"`, replacement: `ttl = "0s"`, want: "assistant.memory.ttl"},
		{name: "invalid rate window", old: `window = "60m"`, replacement: `window = "hourly"`, want: "assistant.rate_limit.window"},
		{name: "invalid provider timeout", old: `timeout = "60s"`, replacement: `timeout = "soon"`, want: "deepseek.timeout"},
		{name: "invalid stream poll interval", old: `poll_interval = "30s"`, replacement: `poll_interval = "often"`, want: "streams.poll_interval"},
		{name: "invalid log level", old: `level = "info"`, replacement: `level = "verbose"`, want: "runtime.logging.level"},
		{name: "invalid log format", old: `format = "text"`, replacement: `format = "xml"`, want: "runtime.logging.format"},
		{name: "negative gateway maximum age", old: `max_thread_age = "1h"`, replacement: `max_thread_age = "-1s"`, want: "gateway.notifications.max_thread_age"},
		{name: "invalid gateway maximum age", old: `max_thread_age = "1h"`, replacement: `max_thread_age = "old"`, want: "gateway.notifications.max_thread_age"},
		{name: "negative gateway prune duration", old: `prune_after = "48h"`, replacement: `prune_after = "-1s"`, want: "gateway.notifications.prune_after"},
		{name: "invalid gateway prune duration", old: `prune_after = "48h"`, replacement: `prune_after = "eventually"`, want: "gateway.notifications.prune_after"},
		{name: "empty sqlite path", old: `sqlite_path = "data/test.db"`, replacement: `sqlite_path = " "`, want: "storage.sqlite_path"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contents := strings.Replace(validConfig, test.old, test.replacement, 1)
			if contents == validConfig {
				t.Fatalf("test replacement did not match %q", test.old)
			}
			t.Setenv("CONFIG_FILE", writeConfig(t, contents))

			if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadConfig() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadConfigRejectsMalformedDocuments(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     string
	}{
		{name: "invalid TOML", contents: `name = "unterminated`, want: "decode config"},
		{name: "duplicate key", contents: strings.Replace(validConfig, `name = "Martie"`, "name = \"Martie\"\nname = \"Marta\"", 1), want: "name"},
		{name: "wrong scalar type", contents: strings.Replace(validConfig, "max_input_runes = 4096", `max_input_runes = "many"`, 1), want: "MaxInputRunes"},
		{name: "unknown root key", contents: validConfig + "\nsurprise = true\n", want: "surprise"},
		{name: "unknown nested key", contents: strings.Replace(validConfig, "[deepseek]", "[deepseek]\nsurprise = true", 1), want: "surprise"},
		{name: "stale ptchan context enabled flag", contents: strings.Replace(validConfig, "[assistant.ptchan_context]", "[assistant.ptchan_context]\nenabled = true", 1), want: "enabled"},
		{name: "stale trace enabled flag", contents: strings.Replace(validConfig, "[assistant.trace]", "[assistant.trace]\nenabled = false", 1), want: "enabled"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CONFIG_FILE", writeConfig(t, test.contents))

			if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadConfig() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadConfigRequiresFile(t *testing.T) {
	t.Setenv("CONFIG_FILE", " \t ")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("missing CONFIG_FILE was accepted")
	}
}

func TestLoadConfigReportsMissingFile(t *testing.T) {
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "missing.toml"))

	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "open config") {
		t.Fatalf("missing file error = %v", err)
	}
}

func TestLoadConfigRejectsDuplicateStreamKeys(t *testing.T) {
	path := writeConfig(t, `
name = "Martie"

[assistant]
system_prompt = "Hello."

[[streams.channel]]
key = "same"
probe_url = "https://example.com/one"
page_url = "https://example.com/one"

[[streams.channel]]
key = "same"
probe_url = "https://example.com/two"
page_url = "https://example.com/two"
`)
	t.Setenv("CONFIG_FILE", path)

	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate stream error = %v", err)
	}
}

func TestLoadConfigRejectsInvalidComponents(t *testing.T) {
	tests := []struct {
		name       string
		components string
		want       string
	}{
		{name: "old catalog", components: `["catalog"]`, want: `unknown component "catalog"`},
		{name: "unknown", components: `["gateway", "search"]`, want: `unknown component "search"`},
		{name: "duplicate", components: `["gateway", "gateway"]`, want: `duplicate component "gateway"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contents := strings.Replace(validConfig, `["gateway", "streams", "assistant"]`, test.components, 1)
			t.Setenv("CONFIG_FILE", writeConfig(t, contents))
			if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("LoadConfig() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateRunUsesSelectedComponentDependencies(t *testing.T) {
	base := Config{
		Telegram: TelegramConfig{BotToken: "token", NotificationChatID: 1},
		Assistant: AssistantConfig{
			Name:             "Martie",
			DiscussionChatID: 2,
			AllowAllUsers:    true,
			SystemPrompt:     "Be useful.",
		},
		DeepSeek: DeepSeekConfig{APIKey: "key"},
		Ptchan:   PtchanConfig{IntegrationName: "martie", Secret: "gateway-secret"},
		Streams:  StreamsConfig{Channels: []miau.Channel{{Key: "live", ProbeURL: "https://stream.example.com", PageURL: "https://example.com"}}},
	}

	tests := []struct {
		name       string
		components []ComponentName
		change     func(*Config)
		want       string
	}{
		{name: "requires a component", want: "at least one component"},
		{name: "gateway only", components: []ComponentName{componentGateway}, change: func(cfg *Config) {
			cfg.Assistant = AssistantConfig{}
			cfg.DeepSeek.APIKey = ""
		}},
		{name: "streams only", components: []ComponentName{componentStreams}},
		{name: "assistant only", components: []ComponentName{componentAssistant}, change: func(cfg *Config) { cfg.Telegram.NotificationChatID = 0 }},
		{name: "all components require Telegram", components: []ComponentName{componentGateway}, change: func(cfg *Config) { cfg.Telegram.BotToken = "" }, want: "TELEGRAM_BOT_TOKEN"},
		{name: "gateway requires notification chat", components: []ComponentName{componentGateway}, change: func(cfg *Config) { cfg.Telegram.NotificationChatID = 0 }, want: "notification_chat_id"},
		{name: "gateway requires secret", components: []ComponentName{componentGateway}, change: func(cfg *Config) { cfg.Ptchan.Secret = "" }, want: "PTCHAN_INTEGRATION_MARTIE_SECRET"},
		{name: "streams require channels", components: []ComponentName{componentStreams}, change: func(cfg *Config) { cfg.Streams.Channels = nil }, want: "at least one channel"},
		{name: "assistant requires name", components: []ComponentName{componentAssistant}, change: func(cfg *Config) { cfg.Assistant.Name = "" }, want: "name is required"},
		{name: "assistant requires prompt", components: []ComponentName{componentAssistant}, change: func(cfg *Config) { cfg.Assistant.SystemPrompt = "" }, want: "system_prompt"},
		{name: "assistant requires discussion chat", components: []ComponentName{componentAssistant}, change: func(cfg *Config) { cfg.Assistant.DiscussionChatID = 0 }, want: "discussion_chat_id"},
		{name: "assistant requires access policy", components: []ComponentName{componentAssistant}, change: func(cfg *Config) { cfg.Assistant.AllowAllUsers = false }, want: "allowed_user_ids"},
		{name: "assistant requires api key", components: []ComponentName{componentAssistant}, change: func(cfg *Config) { cfg.DeepSeek.APIKey = "" }, want: "DEEPSEEK_API_KEY"},
		{name: "assistant ptchan context requires gateway secret", components: []ComponentName{componentAssistant}, change: func(cfg *Config) {
			cfg.Assistant.PtchanContext.Enabled = true
			cfg.Ptchan.Secret = ""
		}, want: "PTCHAN_INTEGRATION_MARTIE_SECRET"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			cfg.Runtime.Components = test.components
			if test.change != nil {
				test.change(&cfg)
			}
			err := cfg.ValidateRun()
			if test.want == "" && err != nil {
				t.Fatal(err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("ValidateRun() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestExampleConfigLoads(t *testing.T) {
	t.Setenv("CONFIG_FILE", filepath.Join("..", "..", "config", "example.toml"))

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Assistant.PtchanContext.Enabled || !cfg.Assistant.Trace.Enabled {
		t.Fatalf("example config should include optional assistant sections: %+v", cfg.Assistant)
	}
	if cfg.Storage.SQLitePath == "" {
		t.Fatalf("example config should include storage.sqlite_path")
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const validConfig = `
locale = "en"
name = "Martie"

[ptchan]
base_url = "https://gateway.example"
gateway_url = "http://ptchan-gateway:8080"
integration_name = "martie"

	[assistant]
	max_input_runes = 4096
	system_prompt = "Hello {{name}}."

[assistant.memory]
ttl = "10m"
history_exchanges = 8

[assistant.rate_limit]
window = "60m"
user_limit = 25
user_burst = 6
global_limit = 100
global_burst = 12

[assistant.ptchan_context]
timeout = "5s"
max_replies = 25

[assistant.trace]
dir = "data/traces"
max_files = 100

[deepseek]
model = "deepseek-v4-flash"
max_tokens = 500
timeout = "60s"

[gateway.webhook]
addr = ":8081"
path = "/internal/ptchan/events"

[gateway.notifications]
min_reply_posts = 11
max_thread_age = "1h"
prune_after = "48h"

[runtime]
components = ["gateway", "streams", "assistant"]
http_addr = ":9090"

[runtime.logging]
level = "info"
format = "text"

[streams]
end_miss_threshold = 2
poll_interval = "30s"

[storage]
sqlite_path = "data/test.db"

[[streams.channel]]
key = "oficial"
probe_url = "https://stream.example.com/live"
page_url = "https://example.com/live"
`

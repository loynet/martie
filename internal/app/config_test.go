package app

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	assistantpkg "martie/internal/assistant"
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

[telegram_assistant]
max_input_runes = 2000
	log_memory = true
	system_prompt = " {{name}} is {{name}}. "

[telegram_assistant.memory]
ttl = "15m"
history_exchanges = 6

[telegram_assistant.rate_limit]
window = "30m"
user_limit = 20
user_burst = 4
global_limit = 80
global_burst = 10

[telegram_assistant.ptchan_context]
timeout = "3s"
max_replies = 4

[telegram_assistant.trace]
dir = "data/test-traces"
max_files = 25

[ptchan_assistant]
mentions = ["@martie", " @Marta ", "@martie"]
max_input_runes = 1200
log_memory = true
system_prompt = " {{name}} replies on ptchan. "

[ptchan_assistant.ptchan_context]
timeout = "4s"
max_replies = 8

[ptchan_assistant.trace]
dir = "data/ptchan-traces"
max_files = 20

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
components = ["gateway", "telegram_assistant", "ptchan_assistant"]
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
	if cfg.TelegramAssistant.Name != "Marta" || cfg.TelegramAssistant.SystemPrompt != "Marta is Marta." {
		t.Fatalf("identity = (%q, %q)", cfg.TelegramAssistant.Name, cfg.TelegramAssistant.SystemPrompt)
	}
	if cfg.Locale != localization.PortuguesePortugal {
		t.Fatalf("locale = %q", cfg.Locale)
	}
	if cfg.Telegram.BotToken != "token" || cfg.DeepSeek.APIKey != "key" {
		t.Fatalf("secrets were not loaded from the environment")
	}
	if cfg.Telegram.NotificationChatID != 123 || cfg.TelegramAssistant.DiscussionChatID != -456 || cfg.TelegramAssistant.AllowAllUsers || len(cfg.TelegramAssistant.AllowedUserIDs) != 2 || cfg.TelegramAssistant.AllowedUserIDs[0] != 7 || cfg.TelegramAssistant.AllowedUserIDs[1] != 8 {
		t.Fatalf("telegram config = %+v, assistant = %+v", cfg.Telegram, cfg.TelegramAssistant)
	}
	if cfg.TelegramAssistant.RateLimitWindow != 30*time.Minute || cfg.TelegramAssistant.ConversationTTL != 15*time.Minute || cfg.DeepSeek.Timeout != 45*time.Second || cfg.Streams.PollInterval != 2*time.Minute {
		t.Fatalf("durations were not parsed: %+v", cfg)
	}
	if len(cfg.Runtime.Components) != 3 || !cfg.runs(componentGateway) || !cfg.runs(componentTelegramAssistant) || !cfg.runs(componentPtchanAssistant) || cfg.runs(componentStreams) {
		t.Fatalf("components = %+v", cfg.Runtime.Components)
	}
	if cfg.TelegramAssistant.HistoryExchanges != 6 || cfg.TelegramAssistant.MaxInputRunes != 2000 || !cfg.TelegramAssistant.LogMemory || cfg.TelegramAssistant.UserRequestLimit != 20 || cfg.TelegramAssistant.UserRequestBurst != 4 || cfg.TelegramAssistant.GlobalRequestLimit != 80 || cfg.TelegramAssistant.GlobalRequestBurst != 10 {
		t.Fatalf("telegram assistant config = %+v", cfg.TelegramAssistant)
	}
	if !cfg.TelegramAssistant.PtchanContext.Enabled || cfg.TelegramAssistant.PtchanContext.BaseURL != "https://gateway-links.example.com" || cfg.TelegramAssistant.PtchanContext.GatewayURL != "http://ptchan-gateway.example.com" || cfg.TelegramAssistant.PtchanContext.Timeout != 3*time.Second || cfg.TelegramAssistant.PtchanContext.MaxReplies != 4 {
		t.Fatalf("ptchan context config = %+v", cfg.TelegramAssistant.PtchanContext)
	}
	if !cfg.TelegramAssistant.Trace.Enabled || cfg.TelegramAssistant.Trace.Dir != "data/test-traces" || cfg.TelegramAssistant.Trace.MaxFiles != 25 {
		t.Fatalf("telegram assistant trace config = %+v", cfg.TelegramAssistant.Trace)
	}
	if cfg.PtchanAssistant.Name != "Marta" || cfg.PtchanAssistant.SystemPrompt != "Marta replies on ptchan." || cfg.PtchanAssistant.MaxInputRunes != 1200 || !cfg.PtchanAssistant.LogMemory {
		t.Fatalf("ptchan assistant config = %+v", cfg.PtchanAssistant)
	}
	if len(cfg.PtchanAssistant.Mentions) != 2 || cfg.PtchanAssistant.Mentions[0] != "@martie" || cfg.PtchanAssistant.Mentions[1] != "@Marta" {
		t.Fatalf("ptchan assistant mentions = %+v", cfg.PtchanAssistant.Mentions)
	}
	if !cfg.PtchanAssistant.PtchanContext.Enabled || cfg.PtchanAssistant.PtchanContext.BaseURL != "https://gateway-links.example.com" || cfg.PtchanAssistant.PtchanContext.GatewayURL != "http://ptchan-gateway.example.com" || cfg.PtchanAssistant.PtchanContext.Timeout != 4*time.Second || cfg.PtchanAssistant.PtchanContext.MaxReplies != 8 {
		t.Fatalf("ptchan assistant context config = %+v", cfg.PtchanAssistant.PtchanContext)
	}
	if !cfg.PtchanAssistant.Trace.Enabled || cfg.PtchanAssistant.Trace.Dir != "data/ptchan-traces" || cfg.PtchanAssistant.Trace.MaxFiles != 20 {
		t.Fatalf("ptchan assistant trace config = %+v", cfg.PtchanAssistant.Trace)
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

[telegram_assistant]
	system_prompt = "You are {{name}}."
`)
	t.Setenv("CONFIG_FILE", path)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Locale != localization.English || !cfg.TelegramAssistant.AllowAllUsers || cfg.TelegramAssistant.MaxInputRunes != 4096 || cfg.TelegramAssistant.ConversationTTL != 10*time.Minute || cfg.TelegramAssistant.HistoryExchanges != 8 || cfg.TelegramAssistant.RateLimitWindow != time.Hour || cfg.TelegramAssistant.UserRequestLimit != 25 || cfg.TelegramAssistant.UserRequestBurst != 6 || cfg.TelegramAssistant.GlobalRequestLimit != 100 || cfg.TelegramAssistant.GlobalRequestBurst != 12 {
		t.Fatalf("telegram assistant defaults were not applied: %+v", cfg.TelegramAssistant)
	}
	if cfg.TelegramAssistant.PtchanContext.Enabled || cfg.TelegramAssistant.PtchanContext.BaseURL != "https://ptchan.org" || cfg.TelegramAssistant.PtchanContext.GatewayURL != "http://ptchan-gateway:8080" || cfg.TelegramAssistant.PtchanContext.Timeout != 5*time.Second || cfg.TelegramAssistant.PtchanContext.MaxReplies != assistantpkg.DefaultMaxReplies {
		t.Fatalf("ptchan context defaults were not applied: %+v", cfg.TelegramAssistant.PtchanContext)
	}
	if cfg.TelegramAssistant.Trace.Enabled || cfg.TelegramAssistant.Trace.Dir != "data/traces" || cfg.TelegramAssistant.Trace.MaxFiles != 100 {
		t.Fatalf("telegram assistant trace defaults were not applied: %+v", cfg.TelegramAssistant.Trace)
	}
	if cfg.PtchanAssistant.Name != "Martie" || len(cfg.PtchanAssistant.Mentions) != 1 || cfg.PtchanAssistant.Mentions[0] != "@martie" || cfg.PtchanAssistant.MaxInputRunes != 4096 || cfg.PtchanAssistant.PtchanContext.Enabled || cfg.PtchanAssistant.PtchanContext.Timeout != 5*time.Second || cfg.PtchanAssistant.PtchanContext.MaxReplies != assistantpkg.DefaultMaxReplies || cfg.PtchanAssistant.Trace.Enabled || cfg.PtchanAssistant.Trace.Dir != "data/traces" || cfg.PtchanAssistant.Trace.MaxFiles != 100 {
		t.Fatalf("ptchan assistant defaults were not applied: %+v", cfg.PtchanAssistant)
	}
	if cfg.DeepSeek.Model != "deepseek-v4-flash" || cfg.DeepSeek.MaxTokens != 500 || cfg.DeepSeek.Timeout != time.Minute || cfg.Ptchan.IntegrationName != "martie" || cfg.Ptchan.BaseURL != "https://ptchan.org" || cfg.Ptchan.GatewayURL != "http://ptchan-gateway:8080" || cfg.Gateway.Webhook.Addr != ":8081" || cfg.Gateway.Webhook.Path != "/internal/ptchan/events" || cfg.Gateway.Notifications.MinReplyPosts != 10 || cfg.Gateway.Notifications.Filter.MaxThreadAge != 0 || cfg.Gateway.Notifications.PruneAfter != 720*time.Hour || cfg.Streams.PollInterval != time.Minute || cfg.Runtime.Logging.Level != slog.LevelInfo || cfg.Runtime.Logging.Format != LogText || cfg.Streams.EndMissThreshold != 2 || cfg.Storage.SQLitePath != "data/martie.db" {
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

[telegram_assistant]
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
		{name: "zero input limit", old: "max_input_runes = 4096", replacement: "max_input_runes = 0", want: "telegram_assistant.max_input_runes"},
		{name: "zero history limit", old: "history_exchanges = 8", replacement: "history_exchanges = 0", want: "telegram_assistant.memory.history_exchanges"},
		{name: "zero user limit", old: "user_limit = 25", replacement: "user_limit = 0", want: "telegram_assistant.rate_limit.user_burst"},
		{name: "zero user burst", old: "user_burst = 6", replacement: "user_burst = 0", want: "telegram_assistant.rate_limit.user_burst"},
		{name: "user burst above limit", old: "user_burst = 6", replacement: "user_burst = 26", want: "telegram_assistant.rate_limit.user_burst"},
		{name: "zero global limit", old: "global_limit = 100", replacement: "global_limit = 0", want: "telegram_assistant.rate_limit.global_burst"},
		{name: "zero global burst", old: "global_burst = 12", replacement: "global_burst = 0", want: "telegram_assistant.rate_limit.global_burst"},
		{name: "global burst above limit", old: "global_burst = 12", replacement: "global_burst = 101", want: "telegram_assistant.rate_limit.global_burst"},
		{name: "zero ptchan context replies", old: "max_replies = 25", replacement: "max_replies = 0", want: "telegram_assistant.ptchan_context.max_replies"},
		{name: "invalid ptchan context timeout", old: `timeout = "5s"`, replacement: `timeout = "later"`, want: "telegram_assistant.ptchan_context.timeout"},
		{name: "empty telegram assistant trace dir", old: `dir = "data/traces"`, replacement: `dir = " "`, want: "telegram_assistant.trace.dir"},
		{name: "zero telegram assistant trace files", old: "max_files = 100", replacement: "max_files = 0", want: "telegram_assistant.trace.max_files"},
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
		{name: "invalid memory TTL", old: `ttl = "10m"`, replacement: `ttl = "later"`, want: "telegram_assistant.memory.ttl"},
		{name: "zero memory TTL", old: `ttl = "10m"`, replacement: `ttl = "0s"`, want: "telegram_assistant.memory.ttl"},
		{name: "invalid rate window", old: `window = "60m"`, replacement: `window = "hourly"`, want: "telegram_assistant.rate_limit.window"},
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
		{name: "stale ptchan context enabled flag", contents: strings.Replace(validConfig, "[telegram_assistant.ptchan_context]", "[telegram_assistant.ptchan_context]\nenabled = true", 1), want: "enabled"},
		{name: "stale trace enabled flag", contents: strings.Replace(validConfig, "[telegram_assistant.trace]", "[telegram_assistant.trace]\nenabled = false", 1), want: "enabled"},
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

[telegram_assistant]
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
			contents := strings.Replace(validConfig, `["gateway", "streams", "telegram_assistant"]`, test.components, 1)
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
		TelegramAssistant: TelegramAssistantConfig{
			Name:             "Martie",
			DiscussionChatID: 2,
			AllowAllUsers:    true,
			SystemPrompt:     "Be useful.",
		},
		PtchanAssistant: PtchanAssistantConfig{
			Name:         "Martie",
			Mentions:     []string{"@martie"},
			SystemPrompt: "Be useful in public.",
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
			cfg.TelegramAssistant = TelegramAssistantConfig{}
			cfg.DeepSeek.APIKey = ""
		}},
		{name: "streams only", components: []ComponentName{componentStreams}},
		{name: "telegram assistant only", components: []ComponentName{componentTelegramAssistant}, change: func(cfg *Config) { cfg.Telegram.NotificationChatID = 0 }},
		{name: "ptchan assistant only", components: []ComponentName{componentPtchanAssistant}, change: func(cfg *Config) {
			cfg.Telegram.BotToken = ""
			cfg.Telegram.NotificationChatID = 0
			cfg.TelegramAssistant = TelegramAssistantConfig{}
		}},
		{name: "telegram-backed components require Telegram", components: []ComponentName{componentGateway}, change: func(cfg *Config) { cfg.Telegram.BotToken = "" }, want: "TELEGRAM_BOT_TOKEN"},
		{name: "gateway requires notification chat", components: []ComponentName{componentGateway}, change: func(cfg *Config) { cfg.Telegram.NotificationChatID = 0 }, want: "notification_chat_id"},
		{name: "gateway requires secret", components: []ComponentName{componentGateway}, change: func(cfg *Config) { cfg.Ptchan.Secret = "" }, want: "PTCHAN_INTEGRATION_MARTIE_SECRET"},
		{name: "streams require channels", components: []ComponentName{componentStreams}, change: func(cfg *Config) { cfg.Streams.Channels = nil }, want: "at least one channel"},
		{name: "telegram assistant requires name", components: []ComponentName{componentTelegramAssistant}, change: func(cfg *Config) { cfg.TelegramAssistant.Name = "" }, want: "name is required"},
		{name: "telegram assistant requires prompt", components: []ComponentName{componentTelegramAssistant}, change: func(cfg *Config) { cfg.TelegramAssistant.SystemPrompt = "" }, want: "system_prompt"},
		{name: "telegram assistant requires discussion chat", components: []ComponentName{componentTelegramAssistant}, change: func(cfg *Config) { cfg.TelegramAssistant.DiscussionChatID = 0 }, want: "discussion_chat_id"},
		{name: "telegram assistant requires access policy", components: []ComponentName{componentTelegramAssistant}, change: func(cfg *Config) { cfg.TelegramAssistant.AllowAllUsers = false }, want: "allowed_user_ids"},
		{name: "telegram assistant requires api key", components: []ComponentName{componentTelegramAssistant}, change: func(cfg *Config) { cfg.DeepSeek.APIKey = "" }, want: "DEEPSEEK_API_KEY"},
		{name: "telegram assistant ptchan context requires gateway secret", components: []ComponentName{componentTelegramAssistant}, change: func(cfg *Config) {
			cfg.TelegramAssistant.PtchanContext.Enabled = true
			cfg.Ptchan.Secret = ""
		}, want: "PTCHAN_INTEGRATION_MARTIE_SECRET"},
		{name: "ptchan assistant requires name", components: []ComponentName{componentPtchanAssistant}, change: func(cfg *Config) { cfg.PtchanAssistant.Name = "" }, want: "name is required"},
		{name: "ptchan assistant requires prompt", components: []ComponentName{componentPtchanAssistant}, change: func(cfg *Config) { cfg.PtchanAssistant.SystemPrompt = "" }, want: "system_prompt"},
		{name: "ptchan assistant requires api key", components: []ComponentName{componentPtchanAssistant}, change: func(cfg *Config) { cfg.DeepSeek.APIKey = "" }, want: "DEEPSEEK_API_KEY"},
		{name: "ptchan assistant requires gateway secret", components: []ComponentName{componentPtchanAssistant}, change: func(cfg *Config) { cfg.Ptchan.Secret = "" }, want: "PTCHAN_INTEGRATION_MARTIE_SECRET"},
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
	if !cfg.TelegramAssistant.PtchanContext.Enabled || !cfg.TelegramAssistant.Trace.Enabled {
		t.Fatalf("example config should include optional assistant sections: %+v", cfg.TelegramAssistant)
	}
	if !cfg.PtchanAssistant.PtchanContext.Enabled || !cfg.PtchanAssistant.Trace.Enabled || len(cfg.PtchanAssistant.Mentions) == 0 {
		t.Fatalf("example config should include ptchan assistant context: %+v", cfg.PtchanAssistant)
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

	[telegram_assistant]
	max_input_runes = 4096
	system_prompt = "Hello {{name}}."

[telegram_assistant.memory]
ttl = "10m"
history_exchanges = 8

[telegram_assistant.rate_limit]
window = "60m"
user_limit = 25
user_burst = 6
global_limit = 100
global_burst = 12

[telegram_assistant.ptchan_context]
timeout = "5s"
max_replies = 25

[telegram_assistant.trace]
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
components = ["gateway", "streams", "telegram_assistant"]
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

package app

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	channerapp "martie/internal/apps/channer"
	chatterapp "martie/internal/apps/chatter"
	streamnotifierapp "martie/internal/apps/streamnotifier"
	"martie/internal/apps/streamnotifier/probe"
	assistantpkg "martie/internal/assistant"
	"martie/internal/localization"
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
self_tripcodes = ["!martie", " !martie ", "!!prod"]

[ptchan.chatter]
integration_name = "martie-chatter"

[ptchan.channer]
integration_name = "martie-channer"

[ptchan.threadnotifier]
integration_name = "martie-threadnotifier"

[chatter]
max_input_runes = 2000
	log_memory = true
	system_prompt = " {{name}} is {{name}}. "

[chatter.memory]
ttl = "15m"
history_exchanges = 6

[chatter.rate_limit]
window = "30m"
user_limit = 20
user_burst = 4
global_limit = 80
global_burst = 10

[chatter.ptchan_context]
timeout = "3s"
max_replies = 4

[chatter.trace]
dir = "data/test-traces"
max_files = 25

[channer]
mentions = ["@martie", " @Marta ", "@martie"]
max_input_runes = 1200
system_prompt = " {{name}} replies on ptchan. "

[channer.rate_limit]
window = "20m"
request_limit = 18
request_burst = 2

[channer.ptchan_context]
timeout = "4s"
max_replies = 8

[channer.trace]
dir = "data/ptchan-traces"
max_files = 20

[deepseek]
model = "deepseek-test"
max_tokens = 300
timeout = "45s"

[gateway.webhook]
addr = ":8082"
path = "internal/gateway/events"

[threadnotifier]
min_reply_posts = 6
board_denylist = ["x"]
keyword_denylist = ["y"]
max_thread_age = "12h"
prune_after = "48h"

[runtime]
http_addr = ":9090"

[runtime.logging]
level = "debug"
format = "json"

[streamnotifier]
end_miss_threshold = 3
poll_interval = "2m"

[storage]
sqlite_path = "data/from-config.db"

[storage.chatter]
sqlite_path = "data/chatter.db"

[storage.channer]
sqlite_path = "data/channer.db"

[storage.threadnotifier]
sqlite_path = "data/threadnotifier.db"

[storage.streamnotifier]
sqlite_path = "data/streamnotifier.db"

[[streamnotifier.channel]]
key = "live"
probe_url = "https://stream.example.com/live"
page_url = "https://example.com/live"
`)
	t.Setenv("CONFIG_FILE", path)
	t.Setenv("TELEGRAM_BOT_TOKEN", " token ")
	t.Setenv("DEEPSEEK_API_KEY", " key ")
	t.Setenv("PTCHAN_INTEGRATION_MARTIE_TEST_SECRET", " gateway-secret ")
	t.Setenv("PTCHAN_INTEGRATION_MARTIE_CHATTER_SECRET", " chatter-secret ")
	t.Setenv("PTCHAN_INTEGRATION_MARTIE_CHANNER_SECRET", " channer-secret ")
	t.Setenv("PTCHAN_INTEGRATION_MARTIE_THREADNOTIFIER_SECRET", " threadnotifier-secret ")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Chatter.Name != "Marta" || cfg.Chatter.SystemPrompt != "Marta is Marta." {
		t.Fatalf("identity = (%q, %q)", cfg.Chatter.Name, cfg.Chatter.SystemPrompt)
	}
	if cfg.Locale != localization.PortuguesePortugal {
		t.Fatalf("locale = %q", cfg.Locale)
	}
	if cfg.Telegram.BotToken != "token" || cfg.DeepSeek.APIKey != "key" {
		t.Fatalf("secrets were not loaded from the environment")
	}
	if cfg.Telegram.NotificationChatID != 123 || cfg.Chatter.DiscussionChatID != -456 || cfg.Chatter.AllowAllUsers || len(cfg.Chatter.AllowedUserIDs) != 2 || cfg.Chatter.AllowedUserIDs[0] != 7 || cfg.Chatter.AllowedUserIDs[1] != 8 {
		t.Fatalf("telegram config = %+v, chatter = %+v", cfg.Telegram, cfg.Chatter)
	}
	if cfg.Chatter.RateLimitWindow != 30*time.Minute || cfg.Channer.RateLimitWindow != 20*time.Minute || cfg.Chatter.ConversationTTL != 15*time.Minute || cfg.DeepSeek.Timeout != 45*time.Second || cfg.StreamNotifier.PollInterval != 2*time.Minute {
		t.Fatalf("durations were not parsed: %+v", cfg)
	}
	if cfg.App != "" {
		t.Fatalf("app = %q, want none before app selection", cfg.App)
	}
	if cfg.Chatter.HistoryExchanges != 6 || cfg.Chatter.MaxInputRunes != 2000 || !cfg.Chatter.LogMemory || cfg.Chatter.UserRequestLimit != 20 || cfg.Chatter.UserRequestBurst != 4 || cfg.Chatter.GlobalRequestLimit != 80 || cfg.Chatter.GlobalRequestBurst != 10 {
		t.Fatalf("chatter config = %+v", cfg.Chatter)
	}
	if !cfg.Chatter.PtchanContext.Enabled || cfg.Chatter.PtchanContext.BaseURL != "https://gateway-links.example.com" || cfg.Chatter.PtchanContext.GatewayURL != "http://ptchan-gateway.example.com" || cfg.Chatter.PtchanContext.Timeout != 3*time.Second || cfg.Chatter.PtchanContext.MaxReplies != 4 || strings.Join(cfg.Chatter.PtchanContext.SelfTripcodes, ",") != "!martie,!!prod" {
		t.Fatalf("ptchan context config = %+v", cfg.Chatter.PtchanContext)
	}
	if !cfg.Chatter.Trace.Enabled || cfg.Chatter.Trace.Dir != "data/test-traces" || cfg.Chatter.Trace.MaxFiles != 25 {
		t.Fatalf("chatter trace config = %+v", cfg.Chatter.Trace)
	}
	if cfg.Channer.Name != "Marta" || cfg.Channer.SystemPrompt != "Marta replies on ptchan." || cfg.Channer.MaxInputRunes != 1200 || cfg.Channer.RequestLimit != 18 || cfg.Channer.RequestBurst != 2 {
		t.Fatalf("channer config = %+v", cfg.Channer)
	}
	if len(cfg.Channer.Mentions) != 2 || cfg.Channer.Mentions[0] != "@martie" || cfg.Channer.Mentions[1] != "@Marta" {
		t.Fatalf("channer mentions = %+v", cfg.Channer.Mentions)
	}
	if !cfg.Channer.PtchanContext.Enabled || cfg.Channer.PtchanContext.BaseURL != "https://gateway-links.example.com" || cfg.Channer.PtchanContext.GatewayURL != "http://ptchan-gateway.example.com" || cfg.Channer.PtchanContext.Timeout != 4*time.Second || cfg.Channer.PtchanContext.MaxReplies != 8 || strings.Join(cfg.Channer.PtchanContext.SelfTripcodes, ",") != "!martie,!!prod" {
		t.Fatalf("channer context config = %+v", cfg.Channer.PtchanContext)
	}
	if !cfg.Channer.Trace.Enabled || cfg.Channer.Trace.Dir != "data/ptchan-traces" || cfg.Channer.Trace.MaxFiles != 20 {
		t.Fatalf("channer trace config = %+v", cfg.Channer.Trace)
	}
	if cfg.DeepSeek.Model != "deepseek-test" || cfg.DeepSeek.MaxTokens != 300 {
		t.Fatalf("deepseek config = %+v", cfg.DeepSeek)
	}
	if cfg.Ptchan.IntegrationName != "martie-test" || cfg.Ptchan.Secret != "gateway-secret" || cfg.Ptchan.BaseURL != "https://gateway-links.example.com" || cfg.Ptchan.GatewayURL != "http://ptchan-gateway.example.com" {
		t.Fatalf("ptchan config = %+v", cfg.Ptchan)
	}
	if cfg.ChatterPtchan.IntegrationName != "martie-chatter" || cfg.ChatterPtchan.Secret != "chatter-secret" {
		t.Fatalf("chatter integration = %+v", cfg.ChatterPtchan)
	}
	if cfg.ChannerPtchan.IntegrationName != "martie-channer" || cfg.ChannerPtchan.Secret != "channer-secret" {
		t.Fatalf("channer integration = %+v", cfg.ChannerPtchan)
	}
	if cfg.ThreadNotifierPtchan.IntegrationName != "martie-threadnotifier" || cfg.ThreadNotifierPtchan.Secret != "threadnotifier-secret" {
		t.Fatalf("threadnotifier integration = %+v", cfg.ThreadNotifierPtchan)
	}
	if cfg.Gateway.Webhook.Addr != ":8082" || cfg.Gateway.Webhook.Path != "/internal/gateway/events" || cfg.ThreadNotifier.MinReplyPosts != 6 || cfg.ThreadNotifier.Filter.MaxThreadAge != 12*time.Hour || cfg.ThreadNotifier.PruneAfter != 48*time.Hour || len(cfg.ThreadNotifier.Filter.BoardDenylist) != 1 || cfg.ThreadNotifier.Filter.BoardDenylist[0] != "x" || len(cfg.ThreadNotifier.Filter.KeywordDenylist) != 1 || cfg.ThreadNotifier.Filter.KeywordDenylist[0] != "y" {
		t.Fatalf("gateway config = %+v", cfg.Gateway)
	}
	if cfg.Runtime.HTTPAddr != ":9090" || cfg.Storage.SQLitePath != "data/from-config.db" {
		t.Fatalf("runtime = %+v, storage = %+v", cfg.Runtime, cfg.Storage)
	}
	if cfg.ChatterStorage.SQLitePath != "data/chatter.db" || cfg.ChannerStorage.SQLitePath != "data/channer.db" || cfg.ThreadNotifierStorage.SQLitePath != "data/threadnotifier.db" || cfg.StreamNotifierStorage.SQLitePath != "data/streamnotifier.db" {
		t.Fatalf("app storage = %+v %+v %+v %+v", cfg.ChatterStorage, cfg.ChannerStorage, cfg.ThreadNotifierStorage, cfg.StreamNotifierStorage)
	}
	if cfg.Runtime.Logging.Level != slog.LevelDebug || cfg.Runtime.Logging.Format != LogJSON {
		t.Fatalf("logging = %+v", cfg.Runtime.Logging)
	}
	if cfg.StreamNotifier.EndMissThreshold != 3 || len(cfg.StreamNotifier.Channels) != 1 || cfg.StreamNotifier.Channels[0].Key != "live" || cfg.StreamNotifier.Channels[0].ProbeURL != "https://stream.example.com/live" || cfg.StreamNotifier.Channels[0].PageURL != "https://example.com/live" {
		t.Fatalf("streamnotifier = %+v", cfg.StreamNotifier)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	path := writeConfig(t, `
name = "Martie"

[telegram]
allow_all_users = true

[chatter]
	system_prompt = "You are {{name}}."
`)
	t.Setenv("CONFIG_FILE", path)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Locale != localization.English || !cfg.Chatter.AllowAllUsers || cfg.Chatter.MaxInputRunes != 4096 || cfg.Chatter.ConversationTTL != 10*time.Minute || cfg.Chatter.HistoryExchanges != 8 || cfg.Chatter.RateLimitWindow != time.Hour || cfg.Chatter.UserRequestLimit != 25 || cfg.Chatter.UserRequestBurst != 6 || cfg.Chatter.GlobalRequestLimit != 100 || cfg.Chatter.GlobalRequestBurst != 12 {
		t.Fatalf("chatter defaults were not applied: %+v", cfg.Chatter)
	}
	if cfg.Chatter.PtchanContext.Enabled || cfg.Chatter.PtchanContext.BaseURL != "https://ptchan.org" || cfg.Chatter.PtchanContext.GatewayURL != "http://ptchan-gateway:8080" || cfg.Chatter.PtchanContext.Timeout != 5*time.Second || cfg.Chatter.PtchanContext.MaxReplies != assistantpkg.DefaultMaxReplies {
		t.Fatalf("ptchan context defaults were not applied: %+v", cfg.Chatter.PtchanContext)
	}
	if cfg.Chatter.Trace.Enabled || cfg.Chatter.Trace.Dir != "data/traces" || cfg.Chatter.Trace.MaxFiles != 100 {
		t.Fatalf("chatter trace defaults were not applied: %+v", cfg.Chatter.Trace)
	}
	if cfg.Channer.Name != "Martie" || len(cfg.Channer.Mentions) != 1 || cfg.Channer.Mentions[0] != "@martie" || cfg.Channer.MaxInputRunes != 4096 || cfg.Channer.RateLimitWindow != time.Hour || cfg.Channer.RequestLimit != 25 || cfg.Channer.RequestBurst != 3 || cfg.Channer.PtchanContext.Enabled || cfg.Channer.PtchanContext.Timeout != 5*time.Second || cfg.Channer.PtchanContext.MaxReplies != assistantpkg.DefaultMaxReplies || cfg.Channer.Trace.Enabled || cfg.Channer.Trace.Dir != "data/traces" || cfg.Channer.Trace.MaxFiles != 100 {
		t.Fatalf("channer defaults were not applied: %+v", cfg.Channer)
	}
	if cfg.DeepSeek.Model != "deepseek-v4-flash" || cfg.DeepSeek.MaxTokens != 500 || cfg.DeepSeek.Timeout != time.Minute || cfg.Ptchan.IntegrationName != "martie" || cfg.Ptchan.BaseURL != "https://ptchan.org" || cfg.Ptchan.GatewayURL != "http://ptchan-gateway:8080" || cfg.Gateway.Webhook.Addr != ":8081" || cfg.Gateway.Webhook.Path != "/internal/ptchan/events" || cfg.ThreadNotifier.MinReplyPosts != 10 || cfg.ThreadNotifier.Filter.MaxThreadAge != 0 || cfg.ThreadNotifier.PruneAfter != 720*time.Hour || cfg.StreamNotifier.PollInterval != time.Minute || cfg.Runtime.Logging.Level != slog.LevelInfo || cfg.Runtime.Logging.Format != LogText || cfg.StreamNotifier.EndMissThreshold != 2 || cfg.Storage.SQLitePath != "data/martie.db" {
		t.Fatalf("defaults were not applied: %+v", cfg)
	}
	if cfg.App != "" {
		t.Fatalf("default app = %q, want none", cfg.App)
	}
}

func TestLoadConfigRejectsUnknownKeys(t *testing.T) {
	path := writeConfig(t, `
name = "Martie"
surprise = true

[chatter]
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
		{name: "zero input limit", old: "max_input_runes = 4096", replacement: "max_input_runes = 0", want: "chatter.max_input_runes"},
		{name: "zero history limit", old: "history_exchanges = 8", replacement: "history_exchanges = 0", want: "chatter.memory.history_exchanges"},
		{name: "zero user limit", old: "user_limit = 25", replacement: "user_limit = 0", want: "chatter.rate_limit.user_burst"},
		{name: "zero user burst", old: "user_burst = 6", replacement: "user_burst = 0", want: "chatter.rate_limit.user_burst"},
		{name: "user burst above limit", old: "user_burst = 6", replacement: "user_burst = 26", want: "chatter.rate_limit.user_burst"},
		{name: "zero global limit", old: "global_limit = 100", replacement: "global_limit = 0", want: "chatter.rate_limit.global_burst"},
		{name: "zero global burst", old: "global_burst = 12", replacement: "global_burst = 0", want: "chatter.rate_limit.global_burst"},
		{name: "global burst above limit", old: "global_burst = 12", replacement: "global_burst = 101", want: "chatter.rate_limit.global_burst"},
		{name: "zero ptchan context replies", old: "max_replies = 25", replacement: "max_replies = 0", want: "chatter.ptchan_context.max_replies"},
		{name: "invalid ptchan context timeout", old: `timeout = "5s"`, replacement: `timeout = "later"`, want: "chatter.ptchan_context.timeout"},
		{name: "empty chatter trace dir", old: `dir = "data/traces"`, replacement: `dir = " "`, want: "chatter.trace.dir"},
		{name: "zero chatter trace files", old: "max_files = 100", replacement: "max_files = 0", want: "chatter.trace.max_files"},
		{name: "zero channer request limit", old: "request_limit = 25", replacement: "request_limit = 0", want: "channer.rate_limit.request_burst"},
		{name: "zero channer request burst", old: "request_burst = 3", replacement: "request_burst = 0", want: "channer.rate_limit.request_burst"},
		{name: "channer burst above limit", old: "request_burst = 3", replacement: "request_burst = 26", want: "channer.rate_limit.request_burst"},
		{name: "empty model", old: `model = "deepseek-v4-flash"`, replacement: `model = " "`, want: "deepseek.model"},
		{name: "zero max tokens", old: "max_tokens = 500", replacement: "max_tokens = 0", want: "deepseek.max_tokens"},
		{name: "empty ptchan integration", old: `integration_name = "martie"`, replacement: `integration_name = " "`, want: "ptchan.integration_name"},
		{name: "empty ptchan base URL", old: `base_url = "https://gateway.example"`, replacement: `base_url = " "`, want: "ptchan.base_url"},
		{name: "empty ptchan gateway URL", old: `gateway_url = "http://ptchan-gateway:8080"`, replacement: `gateway_url = " "`, want: "ptchan.gateway_url"},
		{name: "empty gateway addr", old: `addr = ":8081"`, replacement: `addr = " "`, want: "gateway.webhook.addr"},
		{name: "empty gateway path", old: `path = "/internal/ptchan/events"`, replacement: `path = " "`, want: "gateway.webhook.path"},
		{name: "negative gateway reply posts", old: "min_reply_posts = 11", replacement: "min_reply_posts = -1", want: "threadnotifier.min_reply_posts"},
		{name: "zero stream misses", old: "end_miss_threshold = 2", replacement: "end_miss_threshold = 0", want: "streamnotifier.end_miss_threshold"},
		{name: "empty stream key", old: `key = "oficial"`, replacement: `key = " "`, want: "requires key, probe_url, and page_url"},
		{name: "empty stream probe", old: `probe_url = "https://stream.example.com/live"`, replacement: `probe_url = " "`, want: "requires key, probe_url, and page_url"},
		{name: "empty stream page", old: `page_url = "https://example.com/live"`, replacement: `page_url = " "`, want: "requires key, probe_url, and page_url"},
		{name: "invalid memory TTL", old: `ttl = "10m"`, replacement: `ttl = "later"`, want: "chatter.memory.ttl"},
		{name: "zero memory TTL", old: `ttl = "10m"`, replacement: `ttl = "0s"`, want: "chatter.memory.ttl"},
		{name: "invalid rate window", old: `window = "60m"`, replacement: `window = "hourly"`, want: "chatter.rate_limit.window"},
		{name: "invalid provider timeout", old: `timeout = "60s"`, replacement: `timeout = "soon"`, want: "deepseek.timeout"},
		{name: "invalid stream poll interval", old: `poll_interval = "30s"`, replacement: `poll_interval = "often"`, want: "streamnotifier.poll_interval"},
		{name: "invalid log level", old: `level = "info"`, replacement: `level = "verbose"`, want: "runtime.logging.level"},
		{name: "invalid log format", old: `format = "text"`, replacement: `format = "xml"`, want: "runtime.logging.format"},
		{name: "negative gateway maximum age", old: `max_thread_age = "1h"`, replacement: `max_thread_age = "-1s"`, want: "threadnotifier.max_thread_age"},
		{name: "invalid gateway maximum age", old: `max_thread_age = "1h"`, replacement: `max_thread_age = "old"`, want: "threadnotifier.max_thread_age"},
		{name: "negative gateway prune duration", old: `prune_after = "48h"`, replacement: `prune_after = "-1s"`, want: "threadnotifier.prune_after"},
		{name: "invalid gateway prune duration", old: `prune_after = "48h"`, replacement: `prune_after = "eventually"`, want: "threadnotifier.prune_after"},
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
		{name: "stale ptchan context enabled flag", contents: strings.Replace(validConfig, "[chatter.ptchan_context]", "[chatter.ptchan_context]\nenabled = true", 1), want: "enabled"},
		{name: "stale trace enabled flag", contents: strings.Replace(validConfig, "[chatter.trace]", "[chatter.trace]\nenabled = false", 1), want: "enabled"},
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

[chatter]
system_prompt = "Hello."

[[streamnotifier.channel]]
key = "same"
probe_url = "https://example.com/one"
page_url = "https://example.com/one"

[[streamnotifier.channel]]
key = "same"
probe_url = "https://example.com/two"
page_url = "https://example.com/two"
`)
	t.Setenv("CONFIG_FILE", path)

	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate stream error = %v", err)
	}
}

func TestValidateRunUsesSelectedAppDependencies(t *testing.T) {
	base := Config{
		Telegram: TelegramConfig{BotToken: "token", NotificationChatID: 1},
		Chatter: chatterapp.Config{
			Name:             "Martie",
			DiscussionChatID: 2,
			AllowAllUsers:    true,
			SystemPrompt:     "Be useful.",
		},
		Channer: channerapp.Config{
			Name:         "Martie",
			Mentions:     []string{"@martie"},
			SystemPrompt: "Be useful in public.",
		},
		DeepSeek:       DeepSeekConfig{APIKey: "key"},
		Ptchan:         PtchanConfig{IntegrationName: "martie", Secret: "gateway-secret"},
		StreamNotifier: streamnotifierapp.Config{Channels: []probe.Channel{{Key: "live", ProbeURL: "https://stream.example.com", PageURL: "https://example.com"}}},
	}

	tests := []struct {
		name   string
		app    AppName
		change func(*Config)
		want   string
	}{
		{name: "requires app selection", want: "app command is required"},
		{name: "threadnotifier only", app: AppThreadNotifier, change: func(cfg *Config) {
			cfg.Chatter = chatterapp.Config{}
			cfg.DeepSeek.APIKey = ""
		}},
		{name: "streamnotifier only", app: AppStreamNotifier},
		{name: "chatter only", app: AppChatter, change: func(cfg *Config) { cfg.Telegram.NotificationChatID = 0 }},
		{name: "channer only", app: AppChanner, change: func(cfg *Config) {
			cfg.Telegram.BotToken = ""
			cfg.Telegram.NotificationChatID = 0
			cfg.Chatter = chatterapp.Config{}
		}},
		{name: "telegram-backed apps require Telegram", app: AppThreadNotifier, change: func(cfg *Config) { cfg.Telegram.BotToken = "" }, want: "TELEGRAM_BOT_TOKEN"},
		{name: "threadnotifier requires notification chat", app: AppThreadNotifier, change: func(cfg *Config) { cfg.Telegram.NotificationChatID = 0 }, want: "notification_chat_id"},
		{name: "threadnotifier requires secret", app: AppThreadNotifier, change: func(cfg *Config) { cfg.Ptchan.Secret = "" }, want: "PTCHAN_INTEGRATION_MARTIE_SECRET"},
		{name: "streamnotifier require channels", app: AppStreamNotifier, change: func(cfg *Config) { cfg.StreamNotifier.Channels = nil }, want: "at least one channel"},
		{name: "chatter requires name", app: AppChatter, change: func(cfg *Config) { cfg.Chatter.Name = "" }, want: "name is required"},
		{name: "chatter requires prompt", app: AppChatter, change: func(cfg *Config) { cfg.Chatter.SystemPrompt = "" }, want: "system_prompt"},
		{name: "chatter requires discussion chat", app: AppChatter, change: func(cfg *Config) { cfg.Chatter.DiscussionChatID = 0 }, want: "discussion_chat_id"},
		{name: "chatter requires access policy", app: AppChatter, change: func(cfg *Config) { cfg.Chatter.AllowAllUsers = false }, want: "allowed_user_ids"},
		{name: "chatter requires api key", app: AppChatter, change: func(cfg *Config) { cfg.DeepSeek.APIKey = "" }, want: "DEEPSEEK_API_KEY"},
		{name: "chatter ptchan context requires gateway secret", app: AppChatter, change: func(cfg *Config) {
			cfg.Chatter.PtchanContext.Enabled = true
			cfg.Ptchan.Secret = ""
		}, want: "PTCHAN_INTEGRATION_MARTIE_SECRET"},
		{name: "channer requires name", app: AppChanner, change: func(cfg *Config) { cfg.Channer.Name = "" }, want: "name is required"},
		{name: "channer requires prompt", app: AppChanner, change: func(cfg *Config) { cfg.Channer.SystemPrompt = "" }, want: "system_prompt"},
		{name: "channer requires api key", app: AppChanner, change: func(cfg *Config) { cfg.DeepSeek.APIKey = "" }, want: "DEEPSEEK_API_KEY"},
		{name: "channer requires gateway secret", app: AppChanner, change: func(cfg *Config) { cfg.Ptchan.Secret = "" }, want: "PTCHAN_INTEGRATION_MARTIE_SECRET"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			cfg.App = test.app
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
	if cfg.Chatter.PtchanContext.Enabled || cfg.Chatter.Trace.Enabled {
		t.Fatalf("example config should keep optional chatter sections disabled: %+v", cfg.Chatter)
	}
	if !cfg.Channer.PtchanContext.Enabled || cfg.Channer.Trace.Enabled || len(cfg.Channer.Mentions) == 0 {
		t.Fatalf("example config should include channer context: %+v", cfg.Channer)
	}
	if cfg.Storage.SQLitePath == "" {
		t.Fatalf("example config should include storage.sqlite_path")
	}
	if cfg.ChatterPtchan.IntegrationName != cfg.Ptchan.IntegrationName || cfg.ChannerPtchan.IntegrationName != cfg.Ptchan.IntegrationName || cfg.ThreadNotifierPtchan.IntegrationName != cfg.Ptchan.IntegrationName {
		t.Fatalf("example config should use one ptchan integration by default: base=%+v chatter=%+v channer=%+v threadnotifier=%+v", cfg.Ptchan, cfg.ChatterPtchan, cfg.ChannerPtchan, cfg.ThreadNotifierPtchan)
	}
	if cfg.ChatterStorage.SQLitePath != cfg.Storage.SQLitePath || cfg.ChannerStorage.SQLitePath != cfg.Storage.SQLitePath || cfg.ThreadNotifierStorage.SQLitePath != cfg.Storage.SQLitePath || cfg.StreamNotifierStorage.SQLitePath != cfg.Storage.SQLitePath {
		t.Fatalf("example config should use one sqlite path by default: base=%+v chatter=%+v channer=%+v threadnotifier=%+v streamnotifier=%+v", cfg.Storage, cfg.ChatterStorage, cfg.ChannerStorage, cfg.ThreadNotifierStorage, cfg.StreamNotifierStorage)
	}
}

func TestConfigForAppSelectsIntegrationAndStorage(t *testing.T) {
	cfg := Config{
		Ptchan:                PtchanConfig{IntegrationName: "martie", Secret: "base"},
		ChatterPtchan:         PtchanConfig{IntegrationName: "martie-chatter", Secret: "chatter"},
		ChannerPtchan:         PtchanConfig{IntegrationName: "martie-channer", Secret: "channer"},
		ThreadNotifierPtchan:  PtchanConfig{IntegrationName: "martie-threadnotifier", Secret: "threadnotifier"},
		Storage:               StorageConfig{SQLitePath: "data/martie.db"},
		ChatterStorage:        StorageConfig{SQLitePath: "data/chatter.db"},
		ChannerStorage:        StorageConfig{SQLitePath: "data/channer.db"},
		ThreadNotifierStorage: StorageConfig{SQLitePath: "data/threadnotifier.db"},
		StreamNotifierStorage: StorageConfig{SQLitePath: "data/streamnotifier.db"},
	}

	chatter, err := cfg.ForApp(AppChatter)
	if err != nil {
		t.Fatal(err)
	}
	if chatter.App != AppChatter {
		t.Fatalf("chatter app = %q", chatter.App)
	}
	if chatter.Ptchan.IntegrationName != "martie-chatter" || chatter.Ptchan.Secret != "chatter" || chatter.Storage.SQLitePath != "data/chatter.db" {
		t.Fatalf("chatter app = %+v storage=%+v", chatter.Ptchan, chatter.Storage)
	}

	channer, err := cfg.ForApp(AppChanner)
	if err != nil {
		t.Fatal(err)
	}
	if channer.App != AppChanner {
		t.Fatalf("channer app = %q", channer.App)
	}
	if channer.Ptchan.IntegrationName != "martie-channer" || channer.Ptchan.Secret != "channer" || channer.Storage.SQLitePath != "data/channer.db" {
		t.Fatalf("channer app = %+v storage=%+v", channer.Ptchan, channer.Storage)
	}

	threadnotifier, err := cfg.ForApp(AppThreadNotifier)
	if err != nil {
		t.Fatal(err)
	}
	if threadnotifier.App != AppThreadNotifier || threadnotifier.Ptchan.IntegrationName != "martie-threadnotifier" || threadnotifier.Storage.SQLitePath != "data/threadnotifier.db" {
		t.Fatalf("threadnotifier app = app=%q ptchan=%+v storage=%+v", threadnotifier.App, threadnotifier.Ptchan, threadnotifier.Storage)
	}

	streamnotifier, err := cfg.ForApp(AppStreamNotifier)
	if err != nil {
		t.Fatal(err)
	}
	if streamnotifier.App != AppStreamNotifier || streamnotifier.Ptchan.IntegrationName != "martie" || streamnotifier.Storage.SQLitePath != "data/streamnotifier.db" {
		t.Fatalf("streamnotifier app = app=%q ptchan=%+v storage=%+v", streamnotifier.App, streamnotifier.Ptchan, streamnotifier.Storage)
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

	[chatter]
	max_input_runes = 4096
	system_prompt = "Hello {{name}}."

[chatter.memory]
ttl = "10m"
history_exchanges = 8

[chatter.rate_limit]
window = "60m"
user_limit = 25
user_burst = 6
global_limit = 100
global_burst = 12

[chatter.ptchan_context]
timeout = "5s"
max_replies = 25

[chatter.trace]
dir = "data/traces"
max_files = 100

[channer.rate_limit]
window = "60m"
request_limit = 25
request_burst = 3

[deepseek]
model = "deepseek-v4-flash"
max_tokens = 500
timeout = "60s"

[gateway.webhook]
addr = ":8081"
path = "/internal/ptchan/events"

[threadnotifier]
min_reply_posts = 11
max_thread_age = "1h"
prune_after = "48h"

[runtime]
http_addr = ":9090"

[runtime.logging]
level = "info"
format = "text"

[streamnotifier]
end_miss_threshold = 2
poll_interval = "30s"

[storage]
sqlite_path = "data/test.db"

[[streamnotifier.channel]]
key = "oficial"
probe_url = "https://stream.example.com/live"
page_url = "https://example.com/live"
`

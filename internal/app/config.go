package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"martie/internal/assistant"
	"martie/internal/localization"
	"martie/internal/miau"
	"martie/internal/ptchan"
)

type Config struct {
	Locale            localization.Locale
	Ptchan            PtchanConfig
	Telegram          TelegramConfig
	TelegramAssistant TelegramAssistantConfig
	PtchanAssistant   PtchanAssistantConfig
	DeepSeek          DeepSeekConfig
	Gateway           GatewayConfig
	Streams           StreamsConfig
	Runtime           RuntimeConfig
	Storage           StorageConfig
}

type PtchanConfig struct {
	BaseURL         string
	GatewayURL      string
	IntegrationName string
	Secret          string
}

type TelegramConfig struct {
	BotToken           string
	NotificationChatID int64
}

type TelegramAssistantConfig struct {
	Name               string
	DiscussionChatID   int64
	AllowAllUsers      bool
	AllowedUserIDs     []int64
	RateLimitWindow    time.Duration
	UserRequestLimit   int
	UserRequestBurst   int
	GlobalRequestLimit int
	GlobalRequestBurst int
	SystemPrompt       string
	MaxInputRunes      int
	LogMemory          bool
	Trace              AssistantTraceConfig
	ConversationTTL    time.Duration
	HistoryExchanges   int
	PtchanContext      PtchanContextConfig
}

type PtchanAssistantConfig struct {
	Name          string
	Mentions      []string
	SystemPrompt  string
	MaxInputRunes int
	LogMemory     bool
	PtchanContext PtchanContextConfig
	Trace         AssistantTraceConfig
}

type AssistantTraceConfig = assistant.TraceConfig

type PtchanContextConfig = assistant.PtchanContextConfig

type DeepSeekConfig struct {
	APIKey    string
	Model     string
	MaxTokens int
	Timeout   time.Duration
}

type GatewayConfig struct {
	Webhook       GatewayWebhookConfig
	Notifications GatewayNotificationConfig
}

type GatewayWebhookConfig struct {
	Addr string
	Path string
}

type GatewayNotificationConfig struct {
	MinReplyPosts int
	Filter        ptchan.Filter
	PruneAfter    time.Duration
}

type StreamsConfig struct {
	Channels         []miau.Channel
	PollInterval     time.Duration
	EndMissThreshold int
}

type RuntimeConfig struct {
	Components []ComponentName
	HTTPAddr   string
	Logging    LoggingConfig
}

type ComponentName string

const (
	componentTelegramAssistant ComponentName = "telegram_assistant"
	componentPtchanAssistant   ComponentName = "ptchan_assistant"
	componentGateway           ComponentName = "gateway"
	componentStreams           ComponentName = "streams"
)

type LoggingConfig struct {
	Level  slog.Level
	Format LogFormat
}

type LogFormat string

const (
	LogText LogFormat = "text"
	LogJSON LogFormat = "json"
)

type StorageConfig struct {
	SQLitePath string
}

type fileConfig struct {
	Locale            string                      `toml:"locale"`
	Name              string                      `toml:"name"`
	Ptchan            filePtchanConfig            `toml:"ptchan"`
	Telegram          fileTelegramConfig          `toml:"telegram"`
	TelegramAssistant fileTelegramAssistantConfig `toml:"telegram_assistant"`
	PtchanAssistant   filePtchanAssistantConfig   `toml:"ptchan_assistant"`
	DeepSeek          fileDeepSeekConfig          `toml:"deepseek"`
	Gateway           fileGatewayConfig           `toml:"gateway"`
	Streams           fileStreamsConfig           `toml:"streams"`
	Runtime           fileRuntimeConfig           `toml:"runtime"`
	Storage           fileStorageConfig           `toml:"storage"`
}

type filePtchanConfig struct {
	BaseURL         string `toml:"base_url"`
	GatewayURL      string `toml:"gateway_url"`
	IntegrationName string `toml:"integration_name"`
}

type fileTelegramConfig struct {
	NotificationChatID int64   `toml:"notification_chat_id"`
	DiscussionChatID   int64   `toml:"discussion_chat_id"`
	AllowedUserIDs     []int64 `toml:"allowed_user_ids"`
	AllowAllUsers      bool    `toml:"allow_all_users"`
}

type fileTelegramAssistantConfig struct {
	MaxInputRunes int                 `toml:"max_input_runes"`
	LogMemory     bool                `toml:"log_memory"`
	SystemPrompt  string              `toml:"system_prompt"`
	RateLimit     fileRateLimitConfig `toml:"rate_limit"`
	Memory        fileMemoryConfig    `toml:"memory"`
	PtchanContext *filePtchanContext  `toml:"ptchan_context"`
	Trace         *fileAssistantTrace `toml:"trace"`
}

type filePtchanAssistantConfig struct {
	Mentions      []string            `toml:"mentions"`
	MaxInputRunes int                 `toml:"max_input_runes"`
	LogMemory     bool                `toml:"log_memory"`
	SystemPrompt  string              `toml:"system_prompt"`
	PtchanContext *filePtchanContext  `toml:"ptchan_context"`
	Trace         *fileAssistantTrace `toml:"trace"`
}

type fileAssistantTrace struct {
	Dir      *string `toml:"dir"`
	MaxFiles *int    `toml:"max_files"`
}

type filePtchanContext struct {
	Timeout    *string `toml:"timeout"`
	MaxReplies *int    `toml:"max_replies"`
}

type fileMemoryConfig struct {
	TTL              string `toml:"ttl"`
	HistoryExchanges int    `toml:"history_exchanges"`
}

type fileRateLimitConfig struct {
	Window      string `toml:"window"`
	UserLimit   int    `toml:"user_limit"`
	UserBurst   int    `toml:"user_burst"`
	GlobalLimit int    `toml:"global_limit"`
	GlobalBurst int    `toml:"global_burst"`
}

type fileDeepSeekConfig struct {
	Model     string `toml:"model"`
	MaxTokens int    `toml:"max_tokens"`
	Timeout   string `toml:"timeout"`
}

type fileGatewayConfig struct {
	Webhook       fileGatewayWebhookConfig      `toml:"webhook"`
	Notifications fileGatewayNotificationConfig `toml:"notifications"`
}

type fileGatewayWebhookConfig struct {
	Addr string `toml:"addr"`
	Path string `toml:"path"`
}

type fileGatewayNotificationConfig struct {
	MinReplyPosts   int      `toml:"min_reply_posts"`
	BoardDenylist   []string `toml:"board_denylist"`
	KeywordDenylist []string `toml:"keyword_denylist"`
	MaxThreadAge    string   `toml:"max_thread_age"`
	PruneAfter      string   `toml:"prune_after"`
}

type fileRuntimeConfig struct {
	Components []string          `toml:"components"`
	HTTPAddr   string            `toml:"http_addr"`
	Logging    fileLoggingConfig `toml:"logging"`
}

type fileStorageConfig struct {
	SQLitePath string `toml:"sqlite_path"`
}

type fileLoggingConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

type fileStreamsConfig struct {
	EndMissThreshold int                `toml:"end_miss_threshold"`
	PollInterval     string             `toml:"poll_interval"`
	Channels         []fileStreamConfig `toml:"channel"`
}

type fileStreamConfig struct {
	Key      string `toml:"key"`
	ProbeURL string `toml:"probe_url"`
	PageURL  string `toml:"page_url"`
}

func defaultFileConfig() fileConfig {
	return fileConfig{
		Locale: string(localization.English),
		Ptchan: filePtchanConfig{
			BaseURL:         "https://ptchan.org",
			GatewayURL:      "http://ptchan-gateway:8080",
			IntegrationName: "martie",
		},
		TelegramAssistant: fileTelegramAssistantConfig{
			MaxInputRunes: 4096,
			Memory: fileMemoryConfig{
				TTL:              "10m",
				HistoryExchanges: 8,
			},
			RateLimit: fileRateLimitConfig{
				Window:      "60m",
				UserLimit:   25,
				UserBurst:   6,
				GlobalLimit: 100,
				GlobalBurst: 12,
			},
		},
		PtchanAssistant: filePtchanAssistantConfig{
			Mentions:      []string{"@martie"},
			MaxInputRunes: 4096,
		},
		DeepSeek: fileDeepSeekConfig{
			Model:     "deepseek-v4-flash",
			MaxTokens: 500,
			Timeout:   "60s",
		},
		Gateway: fileGatewayConfig{
			Webhook: fileGatewayWebhookConfig{
				Addr: ":8081",
				Path: "/internal/ptchan/events",
			},
			Notifications: fileGatewayNotificationConfig{
				MinReplyPosts: 10,
				MaxThreadAge:  "0s",
				PruneAfter:    "720h",
			},
		},
		Runtime: fileRuntimeConfig{
			Logging: fileLoggingConfig{
				Level:  "info",
				Format: string(LogText),
			},
		},
		Storage: fileStorageConfig{SQLitePath: "data/martie.db"},
		Streams: fileStreamsConfig{EndMissThreshold: 2, PollInterval: "60s"},
	}
}

func LoadConfig() (Config, error) {
	path := strings.TrimSpace(os.Getenv("CONFIG_FILE"))
	if path == "" {
		return Config{}, fmt.Errorf("CONFIG_FILE is required")
	}

	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %q: %w", path, err)
	}
	defer file.Close()

	raw := defaultFileConfig()
	decoder := toml.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		var strictError *toml.StrictMissingError
		if errors.As(err, &strictError) {
			return Config{}, fmt.Errorf("decode config %q:\n%s", path, strictError.String())
		}
		return Config{}, fmt.Errorf("decode config %q: %w", path, err)
	}

	locale, err := localization.Parse(strings.TrimSpace(raw.Locale))
	if err != nil {
		return Config{}, err
	}
	telegramPtchanContext := assistantPtchanContextConfig(raw.TelegramAssistant.PtchanContext)
	telegramTrace := assistantTraceConfig(raw.TelegramAssistant.Trace)
	ptchanContext := assistantPtchanContextConfig(raw.PtchanAssistant.PtchanContext)
	ptchanTrace := assistantTraceConfig(raw.PtchanAssistant.Trace)
	cfg := Config{
		Locale: locale,
		Ptchan: PtchanConfig{
			BaseURL:         strings.TrimRight(strings.TrimSpace(raw.Ptchan.BaseURL), "/"),
			GatewayURL:      strings.TrimRight(strings.TrimSpace(raw.Ptchan.GatewayURL), "/"),
			IntegrationName: strings.TrimSpace(raw.Ptchan.IntegrationName),
			Secret:          strings.TrimSpace(os.Getenv(integrationSecretEnv(raw.Ptchan.IntegrationName))),
		},
		Telegram: TelegramConfig{
			BotToken:           strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
			NotificationChatID: raw.Telegram.NotificationChatID,
		},
		TelegramAssistant: TelegramAssistantConfig{
			Name:               strings.TrimSpace(raw.Name),
			DiscussionChatID:   raw.Telegram.DiscussionChatID,
			AllowAllUsers:      raw.Telegram.AllowAllUsers,
			AllowedUserIDs:     raw.Telegram.AllowedUserIDs,
			UserRequestLimit:   raw.TelegramAssistant.RateLimit.UserLimit,
			UserRequestBurst:   raw.TelegramAssistant.RateLimit.UserBurst,
			GlobalRequestLimit: raw.TelegramAssistant.RateLimit.GlobalLimit,
			GlobalRequestBurst: raw.TelegramAssistant.RateLimit.GlobalBurst,
			MaxInputRunes:      raw.TelegramAssistant.MaxInputRunes,
			LogMemory:          raw.TelegramAssistant.LogMemory,
			Trace: AssistantTraceConfig{
				Enabled:  raw.TelegramAssistant.Trace != nil,
				Dir:      filepath.Clean(strings.TrimSpace(telegramTrace.Dir)),
				MaxFiles: telegramTrace.MaxFiles,
			},
			HistoryExchanges: raw.TelegramAssistant.Memory.HistoryExchanges,
			PtchanContext: PtchanContextConfig{
				Enabled:    raw.TelegramAssistant.PtchanContext != nil,
				BaseURL:    strings.TrimRight(strings.TrimSpace(raw.Ptchan.BaseURL), "/"),
				GatewayURL: strings.TrimRight(strings.TrimSpace(raw.Ptchan.GatewayURL), "/"),
				MaxReplies: telegramPtchanContext.MaxReplies,
			},
		},
		PtchanAssistant: PtchanAssistantConfig{
			Name:          strings.TrimSpace(raw.Name),
			Mentions:      cleanMentions(raw.PtchanAssistant.Mentions),
			MaxInputRunes: raw.PtchanAssistant.MaxInputRunes,
			LogMemory:     raw.PtchanAssistant.LogMemory,
			Trace: AssistantTraceConfig{
				Enabled:  raw.PtchanAssistant.Trace != nil,
				Dir:      filepath.Clean(strings.TrimSpace(ptchanTrace.Dir)),
				MaxFiles: ptchanTrace.MaxFiles,
			},
			PtchanContext: PtchanContextConfig{
				Enabled:    raw.PtchanAssistant.PtchanContext != nil,
				BaseURL:    strings.TrimRight(strings.TrimSpace(raw.Ptchan.BaseURL), "/"),
				GatewayURL: strings.TrimRight(strings.TrimSpace(raw.Ptchan.GatewayURL), "/"),
				MaxReplies: ptchanContext.MaxReplies,
			},
		},
		DeepSeek: DeepSeekConfig{
			APIKey:    strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")),
			Model:     strings.TrimSpace(raw.DeepSeek.Model),
			MaxTokens: raw.DeepSeek.MaxTokens,
		},
		Gateway: GatewayConfig{
			Webhook: GatewayWebhookConfig{
				Addr: strings.TrimSpace(raw.Gateway.Webhook.Addr),
				Path: cleanGatewayPath(raw.Gateway.Webhook.Path),
			},
			Notifications: GatewayNotificationConfig{
				MinReplyPosts: raw.Gateway.Notifications.MinReplyPosts,
				Filter: ptchan.Filter{
					BoardDenylist:   raw.Gateway.Notifications.BoardDenylist,
					KeywordDenylist: raw.Gateway.Notifications.KeywordDenylist,
				},
			},
		},
		Streams: StreamsConfig{EndMissThreshold: raw.Streams.EndMissThreshold},
		Runtime: RuntimeConfig{HTTPAddr: strings.TrimSpace(raw.Runtime.HTTPAddr)},
		Storage: StorageConfig{SQLitePath: filepath.Clean(strings.TrimSpace(raw.Storage.SQLitePath))},
	}

	cfg.TelegramAssistant.SystemPrompt = strings.ReplaceAll(strings.TrimSpace(raw.TelegramAssistant.SystemPrompt), "{{name}}", cfg.TelegramAssistant.Name)
	cfg.PtchanAssistant.SystemPrompt = strings.ReplaceAll(strings.TrimSpace(raw.PtchanAssistant.SystemPrompt), "{{name}}", cfg.PtchanAssistant.Name)
	if cfg.Ptchan.BaseURL == "" {
		return Config{}, fmt.Errorf("ptchan.base_url is required")
	}
	if cfg.Ptchan.GatewayURL == "" {
		return Config{}, fmt.Errorf("ptchan.gateway_url is required")
	}
	if cfg.Ptchan.IntegrationName == "" {
		return Config{}, fmt.Errorf("ptchan.integration_name is required")
	}
	if cfg.TelegramAssistant.MaxInputRunes <= 0 {
		return Config{}, fmt.Errorf("telegram_assistant.max_input_runes must be positive")
	}
	if cfg.TelegramAssistant.HistoryExchanges <= 0 {
		return Config{}, fmt.Errorf("telegram_assistant.memory.history_exchanges must be positive")
	}
	if cfg.TelegramAssistant.UserRequestLimit <= 0 || cfg.TelegramAssistant.UserRequestBurst <= 0 || cfg.TelegramAssistant.UserRequestBurst > cfg.TelegramAssistant.UserRequestLimit {
		return Config{}, fmt.Errorf("telegram_assistant.rate_limit.user_burst must be positive and no greater than user_limit")
	}
	if cfg.TelegramAssistant.GlobalRequestLimit <= 0 || cfg.TelegramAssistant.GlobalRequestBurst <= 0 || cfg.TelegramAssistant.GlobalRequestBurst > cfg.TelegramAssistant.GlobalRequestLimit {
		return Config{}, fmt.Errorf("telegram_assistant.rate_limit.global_burst must be positive and no greater than global_limit")
	}
	if cfg.TelegramAssistant.PtchanContext.MaxReplies <= 0 {
		return Config{}, fmt.Errorf("telegram_assistant.ptchan_context.max_replies must be positive")
	}
	if cfg.TelegramAssistant.Trace.MaxFiles <= 0 {
		return Config{}, fmt.Errorf("telegram_assistant.trace.max_files must be positive")
	}
	if cfg.TelegramAssistant.Trace.Enabled && cfg.TelegramAssistant.Trace.Dir == "." {
		return Config{}, fmt.Errorf("telegram_assistant.trace.dir is required when enabled")
	}
	if cfg.PtchanAssistant.MaxInputRunes <= 0 {
		return Config{}, fmt.Errorf("ptchan_assistant.max_input_runes must be positive")
	}
	if len(cfg.PtchanAssistant.Mentions) == 0 {
		return Config{}, fmt.Errorf("ptchan_assistant.mentions requires at least one mention")
	}
	if cfg.PtchanAssistant.PtchanContext.MaxReplies <= 0 {
		return Config{}, fmt.Errorf("ptchan_assistant.ptchan_context.max_replies must be positive")
	}
	if cfg.PtchanAssistant.Trace.MaxFiles <= 0 {
		return Config{}, fmt.Errorf("ptchan_assistant.trace.max_files must be positive")
	}
	if cfg.PtchanAssistant.Trace.Enabled && cfg.PtchanAssistant.Trace.Dir == "." {
		return Config{}, fmt.Errorf("ptchan_assistant.trace.dir is required when enabled")
	}
	if cfg.DeepSeek.Model == "" {
		return Config{}, fmt.Errorf("deepseek.model is required")
	}
	if cfg.DeepSeek.MaxTokens <= 0 {
		return Config{}, fmt.Errorf("deepseek.max_tokens must be positive")
	}
	if cfg.Gateway.Webhook.Addr == "" {
		return Config{}, fmt.Errorf("gateway.webhook.addr is required")
	}
	if cfg.Gateway.Webhook.Path == "" {
		return Config{}, fmt.Errorf("gateway.webhook.path is required")
	}
	if cfg.Gateway.Notifications.MinReplyPosts < 0 {
		return Config{}, fmt.Errorf("gateway.notifications.min_reply_posts must be non-negative")
	}
	if cfg.Streams.EndMissThreshold <= 0 {
		return Config{}, fmt.Errorf("streams.end_miss_threshold must be positive")
	}
	if err := cfg.Runtime.Logging.Level.UnmarshalText([]byte(strings.TrimSpace(raw.Runtime.Logging.Level))); err != nil {
		return Config{}, fmt.Errorf("runtime.logging.level must be debug, info, warn, or error")
	}
	cfg.Runtime.Logging.Format = LogFormat(strings.TrimSpace(raw.Runtime.Logging.Format))
	if cfg.Runtime.Logging.Format != LogText && cfg.Runtime.Logging.Format != LogJSON {
		return Config{}, fmt.Errorf("runtime.logging.format must be %q or %q", LogText, LogJSON)
	}
	seenComponents := make(map[ComponentName]struct{}, len(raw.Runtime.Components))
	for _, value := range raw.Runtime.Components {
		component := ComponentName(strings.TrimSpace(value))
		switch component {
		case componentGateway, componentStreams, componentTelegramAssistant, componentPtchanAssistant:
		default:
			return Config{}, fmt.Errorf("runtime.components contains unknown component %q", value)
		}
		if _, exists := seenComponents[component]; exists {
			return Config{}, fmt.Errorf("runtime.components contains duplicate component %q", component)
		}
		seenComponents[component] = struct{}{}
		cfg.Runtime.Components = append(cfg.Runtime.Components, component)
	}
	streamKeys := make(map[string]struct{}, len(raw.Streams.Channels))
	for i, stream := range raw.Streams.Channels {
		stream.Key = strings.TrimSpace(stream.Key)
		stream.ProbeURL = strings.TrimSpace(stream.ProbeURL)
		stream.PageURL = strings.TrimSpace(stream.PageURL)
		if stream.Key == "" || stream.ProbeURL == "" || stream.PageURL == "" {
			return Config{}, fmt.Errorf("streams.channel[%d] requires key, probe_url, and page_url", i)
		}
		if _, exists := streamKeys[stream.Key]; exists {
			return Config{}, fmt.Errorf("streams.channel key %q is duplicated", stream.Key)
		}
		streamKeys[stream.Key] = struct{}{}
		cfg.Streams.Channels = append(cfg.Streams.Channels, miau.Channel(stream))
	}

	if cfg.TelegramAssistant.ConversationTTL, err = positiveDuration("telegram_assistant.memory.ttl", raw.TelegramAssistant.Memory.TTL); err != nil {
		return Config{}, err
	}
	if cfg.TelegramAssistant.RateLimitWindow, err = positiveDuration("telegram_assistant.rate_limit.window", raw.TelegramAssistant.RateLimit.Window); err != nil {
		return Config{}, err
	}
	if cfg.TelegramAssistant.PtchanContext.Timeout, err = positiveDuration("telegram_assistant.ptchan_context.timeout", telegramPtchanContext.Timeout); err != nil {
		return Config{}, err
	}
	if cfg.PtchanAssistant.PtchanContext.Timeout, err = positiveDuration("ptchan_assistant.ptchan_context.timeout", ptchanContext.Timeout); err != nil {
		return Config{}, err
	}
	if cfg.DeepSeek.Timeout, err = positiveDuration("deepseek.timeout", raw.DeepSeek.Timeout); err != nil {
		return Config{}, err
	}
	if cfg.Streams.PollInterval, err = positiveDuration("streams.poll_interval", raw.Streams.PollInterval); err != nil {
		return Config{}, err
	}
	if cfg.Gateway.Notifications.Filter.MaxThreadAge, err = nonNegativeDuration("gateway.notifications.max_thread_age", raw.Gateway.Notifications.MaxThreadAge); err != nil {
		return Config{}, err
	}
	if cfg.Gateway.Notifications.PruneAfter, err = nonNegativeDuration("gateway.notifications.prune_after", raw.Gateway.Notifications.PruneAfter); err != nil {
		return Config{}, err
	}
	if cfg.Storage.SQLitePath == "." {
		return Config{}, fmt.Errorf("storage.sqlite_path is required")
	}

	return cfg, nil
}

func assistantPtchanContextConfig(raw *filePtchanContext) ptchanContextFileConfig {
	cfg := ptchanContextFileConfig{
		Timeout:    "5s",
		MaxReplies: assistant.DefaultMaxReplies,
	}
	if raw == nil {
		return cfg
	}
	if raw.Timeout != nil {
		cfg.Timeout = *raw.Timeout
	}
	if raw.MaxReplies != nil {
		cfg.MaxReplies = *raw.MaxReplies
	}
	return cfg
}

type ptchanContextFileConfig struct {
	Timeout    string
	MaxReplies int
}

func assistantTraceConfig(raw *fileAssistantTrace) assistantTraceFileConfig {
	cfg := assistantTraceFileConfig{Dir: "data/traces", MaxFiles: 100}
	if raw == nil {
		return cfg
	}
	if raw.Dir != nil {
		cfg.Dir = *raw.Dir
	}
	if raw.MaxFiles != nil {
		cfg.MaxFiles = *raw.MaxFiles
	}
	return cfg
}

type assistantTraceFileConfig struct {
	Dir      string
	MaxFiles int
}

func (c Config) runs(component ComponentName) bool {
	for _, configured := range c.Runtime.Components {
		if configured == component {
			return true
		}
	}
	return false
}

func (c Config) ValidateRun() error {
	if len(c.Runtime.Components) == 0 {
		return fmt.Errorf("runtime.components must contain at least one component")
	}
	if (c.runs(componentGateway) || c.runs(componentStreams) || c.runs(componentTelegramAssistant)) && c.Telegram.BotToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if (c.runs(componentGateway) || c.runs(componentStreams)) && c.Telegram.NotificationChatID == 0 {
		return fmt.Errorf("telegram.notification_chat_id is required for gateway and streams")
	}
	if c.runs(componentStreams) && len(c.Streams.Channels) == 0 {
		return fmt.Errorf("streams requires at least one channel")
	}
	if c.runs(componentGateway) && c.Ptchan.Secret == "" {
		return fmt.Errorf("%s is required for gateway", integrationSecretEnv(c.Ptchan.IntegrationName))
	}
	if c.runs(componentTelegramAssistant) {
		if c.TelegramAssistant.Name == "" {
			return fmt.Errorf("name is required for telegram_assistant")
		}
		if c.TelegramAssistant.SystemPrompt == "" {
			return fmt.Errorf("telegram_assistant.system_prompt is required for telegram_assistant")
		}
		if c.TelegramAssistant.DiscussionChatID == 0 {
			return fmt.Errorf("telegram.discussion_chat_id is required for telegram_assistant")
		}
		if !c.TelegramAssistant.AllowAllUsers && len(c.TelegramAssistant.AllowedUserIDs) == 0 {
			return fmt.Errorf("telegram.allowed_user_ids requires at least one user for telegram_assistant")
		}
		if c.DeepSeek.APIKey == "" {
			return fmt.Errorf("DEEPSEEK_API_KEY is required for telegram_assistant")
		}
		if c.TelegramAssistant.PtchanContext.Enabled && c.Ptchan.Secret == "" {
			return fmt.Errorf("%s is required for telegram_assistant ptchan context", integrationSecretEnv(c.Ptchan.IntegrationName))
		}
	}
	if c.runs(componentPtchanAssistant) {
		if c.PtchanAssistant.Name == "" {
			return fmt.Errorf("name is required for ptchan_assistant")
		}
		if c.PtchanAssistant.SystemPrompt == "" {
			return fmt.Errorf("ptchan_assistant.system_prompt is required for ptchan_assistant")
		}
		if c.DeepSeek.APIKey == "" {
			return fmt.Errorf("DEEPSEEK_API_KEY is required for ptchan_assistant")
		}
		if c.Ptchan.Secret == "" {
			return fmt.Errorf("%s is required for ptchan_assistant", integrationSecretEnv(c.Ptchan.IntegrationName))
		}
	}
	return nil
}

func integrationSecretEnv(name string) string {
	normalized := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r - 'a' + 'A'
		}
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, strings.TrimSpace(name))
	return "PTCHAN_INTEGRATION_" + normalized + "_SECRET"
}

func cleanGatewayPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func cleanMentions(values []string) []string {
	mentions := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		mention := strings.TrimSpace(value)
		if mention == "" {
			continue
		}
		key := strings.ToLower(mention)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		mentions = append(mentions, mention)
	}
	return mentions
}

func positiveDuration(name, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return duration, nil
}

func nonNegativeDuration(name, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, fmt.Errorf("%s must be a non-negative duration", name)
	}
	return duration, nil
}

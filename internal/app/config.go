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

	channerapp "martie/internal/apps/channer"
	chatterapp "martie/internal/apps/chatter"
	streamnotifierapp "martie/internal/apps/streamnotifier"
	"martie/internal/apps/streamnotifier/probe"
	threadnotifierapp "martie/internal/apps/threadnotifier"
	"martie/internal/assistant"
	"martie/internal/localization"
)

type Config struct {
	App            AppName
	Locale         localization.Locale
	Ptchan         PtchanConfig
	Telegram       TelegramConfig
	Chatter        chatterapp.Config
	Channer        channerapp.Config
	DeepSeek       DeepSeekConfig
	GatewayAddr    string
	ThreadNotifier threadnotifierapp.Config
	StreamNotifier streamnotifierapp.Config
	Runtime        RuntimeConfig
	SQLitePath     string
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

type DeepSeekConfig struct {
	APIKey    string
	Model     string
	MaxTokens int
}

type RuntimeConfig struct {
	HTTPAddr string
	Logging  LoggingConfig
}

type AppName string

const (
	AppChatter        AppName = "chatter"
	AppChanner        AppName = "channer"
	AppThreadNotifier AppName = "threadnotifier"
	AppStreamNotifier AppName = "streamnotifier"
)

type WorkerName string

const (
	workerChatter        WorkerName = "chatter"
	workerChanner        WorkerName = "channer"
	workerThreadNotifier WorkerName = "threadnotifier"
	workerStreamNotifier WorkerName = "streamnotifier"
	workerGatewayEvents  WorkerName = "gateway_events"
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

type fileConfig struct {
	Locale         string                   `toml:"locale"`
	Name           string                   `toml:"name"`
	Ptchan         filePtchanConfig         `toml:"ptchan"`
	Telegram       fileTelegramConfig       `toml:"telegram"`
	Chatter        fileChatterConfig        `toml:"chatter"`
	Channer        fileChannerConfig        `toml:"channer"`
	DeepSeek       fileDeepSeekConfig       `toml:"deepseek"`
	Gateway        fileGatewayConfig        `toml:"gateway"`
	ThreadNotifier fileThreadNotifierConfig `toml:"threadnotifier"`
	StreamNotifier fileStreamNotifierConfig `toml:"streamnotifier"`
	Runtime        fileRuntimeConfig        `toml:"runtime"`
	Storage        fileStorageConfig        `toml:"storage"`
}

type filePtchanConfig struct {
	BaseURL         string                      `toml:"base_url"`
	GatewayURL      string                      `toml:"gateway_url"`
	IntegrationName string                      `toml:"integration_name"`
	Chatter         filePtchanIntegrationConfig `toml:"chatter"`
	Channer         filePtchanIntegrationConfig `toml:"channer"`
	ThreadNotifier  filePtchanIntegrationConfig `toml:"threadnotifier"`
}

type filePtchanIntegrationConfig struct {
	IntegrationName string `toml:"integration_name"`
}

type fileTelegramConfig struct {
	NotificationChatID int64   `toml:"notification_chat_id"`
	DiscussionChatID   int64   `toml:"discussion_chat_id"`
	AllowedUserIDs     []int64 `toml:"allowed_user_ids"`
	AllowAllUsers      bool    `toml:"allow_all_users"`
}

type fileChatterConfig struct {
	MaxInputRunes int                 `toml:"max_input_runes"`
	SystemPrompt  string              `toml:"system_prompt"`
	RateLimit     fileRateLimitConfig `toml:"rate_limit"`
	Memory        fileMemoryConfig    `toml:"memory"`
	PtchanContext filePtchanContext   `toml:"ptchan_context"`
}

type fileChannerConfig struct {
	Mentions      []string                   `toml:"mentions"`
	MaxInputRunes int                        `toml:"max_input_runes"`
	SystemPrompt  string                     `toml:"system_prompt"`
	PruneAfter    string                     `toml:"prune_after"`
	RateLimit     fileChannerRateLimitConfig `toml:"rate_limit"`
	PtchanContext filePtchanContext          `toml:"ptchan_context"`
}

type fileChannerRateLimitConfig struct {
	RequestLimit       int `toml:"request_limit"`
	RequestBurst       int `toml:"request_burst"`
	ThreadRequestLimit int `toml:"thread_limit"`
	ThreadRequestBurst int `toml:"thread_burst"`
}

type filePtchanContext struct {
	MaxReplies int `toml:"max_replies"`
}

type fileMemoryConfig struct {
	TTL              string `toml:"ttl"`
	HistoryExchanges int    `toml:"history_exchanges"`
}

type fileRateLimitConfig struct {
	UserLimit   int `toml:"user_limit"`
	UserBurst   int `toml:"user_burst"`
	GlobalLimit int `toml:"global_limit"`
	GlobalBurst int `toml:"global_burst"`
}

type fileDeepSeekConfig struct {
	Model     string `toml:"model"`
	MaxTokens int    `toml:"max_tokens"`
}

type fileGatewayConfig struct {
	Webhook fileGatewayWebhookConfig `toml:"webhook"`
}

type fileGatewayWebhookConfig struct {
	Addr string `toml:"addr"`
}

type fileThreadNotifierConfig struct {
	MinReplyPosts   int      `toml:"min_reply_posts"`
	BoardDenylist   []string `toml:"board_denylist"`
	KeywordDenylist []string `toml:"keyword_denylist"`
	MaxThreadAge    string   `toml:"max_thread_age"`
	PruneAfter      string   `toml:"prune_after"`
}

type fileRuntimeConfig struct {
	HTTPAddr string            `toml:"http_addr"`
	Logging  fileLoggingConfig `toml:"logging"`
}

type fileStorageConfig struct {
	SQLitePath string `toml:"sqlite_path"`
}

type fileLoggingConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

type fileStreamNotifierConfig struct {
	PollInterval string             `toml:"poll_interval"`
	Channels     []fileStreamConfig `toml:"channel"`
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
		Chatter: fileChatterConfig{
			MaxInputRunes: 4096,
			Memory: fileMemoryConfig{
				TTL:              "10m",
				HistoryExchanges: 8,
			},
			PtchanContext: filePtchanContext{MaxReplies: assistant.DefaultMaxReplies},
			RateLimit: fileRateLimitConfig{
				UserLimit:   25,
				UserBurst:   6,
				GlobalLimit: 100,
				GlobalBurst: 12,
			},
		},
		Channer: fileChannerConfig{
			Mentions:      []string{"@martie"},
			MaxInputRunes: 4096,
			PruneAfter:    "720h",
			PtchanContext: filePtchanContext{
				MaxReplies: assistant.DefaultMaxReplies,
			},
			RateLimit: fileChannerRateLimitConfig{
				RequestLimit:       25,
				RequestBurst:       3,
				ThreadRequestLimit: 6,
				ThreadRequestBurst: 2,
			},
		},
		DeepSeek: fileDeepSeekConfig{
			Model:     "deepseek-v4-flash",
			MaxTokens: 500,
		},
		Gateway: fileGatewayConfig{
			Webhook: fileGatewayWebhookConfig{
				Addr: ":8081",
			},
		},
		ThreadNotifier: fileThreadNotifierConfig{
			MinReplyPosts: 10,
			MaxThreadAge:  "0s",
			PruneAfter:    "720h",
		},
		Runtime: fileRuntimeConfig{
			Logging: fileLoggingConfig{
				Level:  "info",
				Format: string(LogText),
			},
		},
		Storage:        fileStorageConfig{SQLitePath: "data/martie.db"},
		StreamNotifier: fileStreamNotifierConfig{PollInterval: "60s"},
	}
}

func LoadConfig(app AppName) (Config, error) {
	if !validApp(app) {
		return Config{}, fmt.Errorf("unknown app %q", app)
	}

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

	var logging LoggingConfig
	if err := logging.Level.UnmarshalText([]byte(strings.TrimSpace(raw.Runtime.Logging.Level))); err != nil {
		return Config{}, fmt.Errorf("runtime.logging.level must be debug, info, warn, or error")
	}
	logging.Format = LogFormat(strings.TrimSpace(raw.Runtime.Logging.Format))
	if logging.Format != LogText && logging.Format != LogJSON {
		return Config{}, fmt.Errorf("runtime.logging.format must be %q or %q", LogText, LogJSON)
	}

	cfg := Config{
		App:    app,
		Locale: locale,
		Telegram: TelegramConfig{
			BotToken:           strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
			NotificationChatID: raw.Telegram.NotificationChatID,
		},
		Runtime: RuntimeConfig{
			HTTPAddr: strings.TrimSpace(raw.Runtime.HTTPAddr),
			Logging:  logging,
		},
		SQLitePath: cleanPath(raw.Storage.SQLitePath),
	}

	switch app {
	case AppChatter:
		cfg.Ptchan = selectedPtchan(raw.Ptchan, raw.Ptchan.Chatter)
		cfg.Chatter = chatterapp.Config{
			Name:               strings.TrimSpace(raw.Name),
			DiscussionChatID:   raw.Telegram.DiscussionChatID,
			AllowAllUsers:      raw.Telegram.AllowAllUsers,
			AllowedUserIDs:     raw.Telegram.AllowedUserIDs,
			UserRequestLimit:   raw.Chatter.RateLimit.UserLimit,
			UserRequestBurst:   raw.Chatter.RateLimit.UserBurst,
			GlobalRequestLimit: raw.Chatter.RateLimit.GlobalLimit,
			GlobalRequestBurst: raw.Chatter.RateLimit.GlobalBurst,
			MaxInputRunes:      raw.Chatter.MaxInputRunes,
			HistoryExchanges:   raw.Chatter.Memory.HistoryExchanges,
			PtchanContext: assistant.PtchanContextConfig{
				BaseURL:         cfg.Ptchan.BaseURL,
				IntegrationName: cfg.Ptchan.IntegrationName,
				MaxReplies:      raw.Chatter.PtchanContext.MaxReplies,
			},
		}
		cfg.Chatter.SystemPrompt = systemPrompt(raw.Chatter.SystemPrompt, cfg.Chatter.Name)
		if cfg.Chatter.ConversationTTL, err = positiveDuration("chatter.memory.ttl", raw.Chatter.Memory.TTL); err != nil {
			return Config{}, err
		}
		cfg.DeepSeek = deepSeekConfig(raw.DeepSeek)

	case AppChanner:
		cfg.Ptchan = selectedPtchan(raw.Ptchan, raw.Ptchan.Channer)
		cfg.Channer = channerapp.Config{
			Name:               strings.TrimSpace(raw.Name),
			Mentions:           cleanMentions(raw.Channer.Mentions),
			MaxInputRunes:      raw.Channer.MaxInputRunes,
			RequestLimit:       raw.Channer.RateLimit.RequestLimit,
			RequestBurst:       raw.Channer.RateLimit.RequestBurst,
			ThreadRequestLimit: raw.Channer.RateLimit.ThreadRequestLimit,
			ThreadRequestBurst: raw.Channer.RateLimit.ThreadRequestBurst,
			PtchanContext: assistant.PtchanContextConfig{
				BaseURL:         cfg.Ptchan.BaseURL,
				IntegrationName: cfg.Ptchan.IntegrationName,
				MaxReplies:      raw.Channer.PtchanContext.MaxReplies,
			},
		}
		cfg.Channer.SystemPrompt = systemPrompt(raw.Channer.SystemPrompt, cfg.Channer.Name)
		if cfg.Channer.PruneAfter, err = nonNegativeDuration("channer.prune_after", raw.Channer.PruneAfter); err != nil {
			return Config{}, err
		}
		cfg.DeepSeek = deepSeekConfig(raw.DeepSeek)
		cfg.GatewayAddr = gatewayAddr(raw.Gateway)

	case AppThreadNotifier:
		cfg.Ptchan = selectedPtchan(raw.Ptchan, raw.Ptchan.ThreadNotifier)
		cfg.GatewayAddr = gatewayAddr(raw.Gateway)
		cfg.ThreadNotifier = threadnotifierapp.Config{
			MinReplyPosts: raw.ThreadNotifier.MinReplyPosts,
			Filter: threadnotifierapp.Filter{
				BoardDenylist:   raw.ThreadNotifier.BoardDenylist,
				KeywordDenylist: raw.ThreadNotifier.KeywordDenylist,
			},
		}
		if cfg.ThreadNotifier.Filter.MaxThreadAge, err = nonNegativeDuration("threadnotifier.max_thread_age", raw.ThreadNotifier.MaxThreadAge); err != nil {
			return Config{}, err
		}
		if cfg.ThreadNotifier.PruneAfter, err = nonNegativeDuration("threadnotifier.prune_after", raw.ThreadNotifier.PruneAfter); err != nil {
			return Config{}, err
		}

	case AppStreamNotifier:
		if cfg.StreamNotifier.PollInterval, err = positiveDuration("streamnotifier.poll_interval", raw.StreamNotifier.PollInterval); err != nil {
			return Config{}, err
		}
		streamKeys := make(map[string]struct{}, len(raw.StreamNotifier.Channels))
		for i, stream := range raw.StreamNotifier.Channels {
			stream.Key = strings.TrimSpace(stream.Key)
			stream.ProbeURL = strings.TrimSpace(stream.ProbeURL)
			stream.PageURL = strings.TrimSpace(stream.PageURL)
			if stream.Key == "" || stream.ProbeURL == "" || stream.PageURL == "" {
				return Config{}, fmt.Errorf("streamnotifier.channel[%d] requires key, probe_url, and page_url", i)
			}
			if _, exists := streamKeys[stream.Key]; exists {
				return Config{}, fmt.Errorf("streamnotifier.channel key %q is duplicated", stream.Key)
			}
			streamKeys[stream.Key] = struct{}{}
			cfg.StreamNotifier.Channels = append(cfg.StreamNotifier.Channels, probe.Channel(stream))
		}
	}
	if err := cfg.validateValues(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) ValidateRun() error {
	if !validApp(c.App) {
		return fmt.Errorf("unknown app %q", c.App)
	}
	if (c.App == AppThreadNotifier || c.App == AppStreamNotifier || c.App == AppChatter) && c.Telegram.BotToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if (c.App == AppThreadNotifier || c.App == AppStreamNotifier) && c.Telegram.NotificationChatID == 0 {
		return fmt.Errorf("telegram.notification_chat_id is required for threadnotifier and streamnotifier")
	}
	if c.App == AppStreamNotifier && len(c.StreamNotifier.Channels) == 0 {
		return fmt.Errorf("streamnotifier requires at least one channel")
	}
	if c.App == AppThreadNotifier && c.Ptchan.Secret == "" {
		return fmt.Errorf("%s is required for threadnotifier", integrationSecretEnv(c.Ptchan.IntegrationName))
	}
	if c.App == AppChatter {
		if c.Chatter.Name == "" {
			return fmt.Errorf("name is required for chatter")
		}
		if c.Chatter.SystemPrompt == "" {
			return fmt.Errorf("chatter.system_prompt is required for chatter")
		}
		if c.Chatter.DiscussionChatID == 0 {
			return fmt.Errorf("telegram.discussion_chat_id is required for chatter")
		}
		if !c.Chatter.AllowAllUsers && len(c.Chatter.AllowedUserIDs) == 0 {
			return fmt.Errorf("telegram.allowed_user_ids requires at least one user for chatter")
		}
		if c.DeepSeek.APIKey == "" {
			return fmt.Errorf("DEEPSEEK_API_KEY is required for chatter")
		}
		if c.Ptchan.Secret == "" {
			return fmt.Errorf("%s is required for chatter ptchan context", integrationSecretEnv(c.Ptchan.IntegrationName))
		}
	}
	if c.App == AppChanner {
		if c.Channer.Name == "" {
			return fmt.Errorf("name is required for channer")
		}
		if c.Channer.SystemPrompt == "" {
			return fmt.Errorf("channer.system_prompt is required for channer")
		}
		if c.DeepSeek.APIKey == "" {
			return fmt.Errorf("DEEPSEEK_API_KEY is required for channer")
		}
		if c.Ptchan.Secret == "" {
			return fmt.Errorf("%s is required for channer", integrationSecretEnv(c.Ptchan.IntegrationName))
		}
	}
	return nil
}

func ParseAppName(value string) (AppName, error) {
	switch AppName(strings.TrimSpace(value)) {
	case AppChatter:
		return AppChatter, nil
	case AppChanner:
		return AppChanner, nil
	case AppThreadNotifier:
		return AppThreadNotifier, nil
	case AppStreamNotifier:
		return AppStreamNotifier, nil
	default:
		return "", fmt.Errorf("unknown app %q", value)
	}
}

func validApp(app AppName) bool {
	switch app {
	case AppChatter, AppChanner, AppThreadNotifier, AppStreamNotifier:
		return true
	default:
		return false
	}
}

func selectedPtchan(raw filePtchanConfig, integration filePtchanIntegrationConfig) PtchanConfig {
	name := strings.TrimSpace(integration.IntegrationName)
	if name == "" {
		name = strings.TrimSpace(raw.IntegrationName)
	}
	cfg := PtchanConfig{
		BaseURL:         strings.TrimRight(strings.TrimSpace(raw.BaseURL), "/"),
		GatewayURL:      strings.TrimRight(strings.TrimSpace(raw.GatewayURL), "/"),
		IntegrationName: name,
	}
	cfg.Secret = strings.TrimSpace(os.Getenv(integrationSecretEnv(name)))
	return cfg
}

func deepSeekConfig(raw fileDeepSeekConfig) DeepSeekConfig {
	return DeepSeekConfig{
		APIKey:    strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")),
		Model:     strings.TrimSpace(raw.Model),
		MaxTokens: raw.MaxTokens,
	}
}

func gatewayAddr(raw fileGatewayConfig) string {
	return strings.TrimSpace(raw.Webhook.Addr)
}

func systemPrompt(prompt, name string) string {
	return strings.ReplaceAll(strings.TrimSpace(prompt), "{{name}}", name)
}

func (c Config) validateValues() error {
	if c.SQLitePath == "" || c.SQLitePath == "." {
		return fmt.Errorf("storage.sqlite_path is required")
	}
	switch c.App {
	case AppChatter:
		if c.Chatter.MaxInputRunes <= 0 {
			return fmt.Errorf("chatter.max_input_runes must be positive")
		}
		if c.Chatter.HistoryExchanges <= 0 {
			return fmt.Errorf("chatter.memory.history_exchanges must be positive")
		}
		if c.Chatter.UserRequestLimit <= 0 || c.Chatter.UserRequestBurst <= 0 || c.Chatter.UserRequestBurst > c.Chatter.UserRequestLimit {
			return fmt.Errorf("chatter.rate_limit.user_burst must be positive and no greater than user_limit")
		}
		if c.Chatter.GlobalRequestLimit <= 0 || c.Chatter.GlobalRequestBurst <= 0 || c.Chatter.GlobalRequestBurst > c.Chatter.GlobalRequestLimit {
			return fmt.Errorf("chatter.rate_limit.global_burst must be positive and no greater than global_limit")
		}
		if c.Chatter.PtchanContext.MaxReplies <= 0 {
			return fmt.Errorf("chatter.ptchan_context.max_replies must be positive")
		}
		if c.DeepSeek.Model == "" {
			return fmt.Errorf("deepseek.model is required")
		}
		if c.DeepSeek.MaxTokens <= 0 {
			return fmt.Errorf("deepseek.max_tokens must be positive")
		}
		return validatePtchan(c.Ptchan, true)
	case AppChanner:
		if c.Channer.MaxInputRunes <= 0 {
			return fmt.Errorf("channer.max_input_runes must be positive")
		}
		if len(c.Channer.Mentions) == 0 {
			return fmt.Errorf("channer.mentions requires at least one mention")
		}
		if c.Channer.RequestLimit <= 0 || c.Channer.RequestBurst <= 0 || c.Channer.RequestBurst > c.Channer.RequestLimit {
			return fmt.Errorf("channer.rate_limit.request_burst must be positive and no greater than request_limit")
		}
		if c.Channer.ThreadRequestLimit <= 0 || c.Channer.ThreadRequestBurst <= 0 || c.Channer.ThreadRequestBurst > c.Channer.ThreadRequestLimit {
			return fmt.Errorf("channer.rate_limit.thread_burst must be positive and no greater than thread_limit")
		}
		if c.Channer.PtchanContext.MaxReplies <= 0 {
			return fmt.Errorf("channer.ptchan_context.max_replies must be positive")
		}
		if c.DeepSeek.Model == "" {
			return fmt.Errorf("deepseek.model is required")
		}
		if c.DeepSeek.MaxTokens <= 0 {
			return fmt.Errorf("deepseek.max_tokens must be positive")
		}
		if err := validatePtchan(c.Ptchan, true); err != nil {
			return err
		}
		return validateGatewayAddr(c.GatewayAddr)
	case AppThreadNotifier:
		if c.ThreadNotifier.MinReplyPosts < 0 {
			return fmt.Errorf("threadnotifier.min_reply_posts must be non-negative")
		}
		if err := validatePtchan(c.Ptchan, false); err != nil {
			return err
		}
		return validateGatewayAddr(c.GatewayAddr)
	}
	return nil
}

func validatePtchan(cfg PtchanConfig, gatewayRequired bool) error {
	if cfg.BaseURL == "" {
		return fmt.Errorf("ptchan.base_url is required")
	}
	if gatewayRequired && cfg.GatewayURL == "" {
		return fmt.Errorf("ptchan.gateway_url is required")
	}
	if cfg.IntegrationName == "" {
		return fmt.Errorf("ptchan.integration_name is required")
	}
	return nil
}

func validateGatewayAddr(addr string) error {
	if addr == "" {
		return fmt.Errorf("gateway.webhook.addr is required")
	}
	return nil
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
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

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
	App                   AppName
	Locale                localization.Locale
	Ptchan                PtchanConfig
	ChatterPtchan         PtchanConfig
	ChannerPtchan         PtchanConfig
	ThreadNotifierPtchan  PtchanConfig
	Telegram              TelegramConfig
	Chatter               chatterapp.Config
	Channer               channerapp.Config
	DeepSeek              DeepSeekConfig
	Gateway               GatewayConfig
	ThreadNotifier        threadnotifierapp.Config
	StreamNotifier        streamnotifierapp.Config
	Runtime               RuntimeConfig
	Storage               StorageConfig
	ChatterStorage        StorageConfig
	ChannerStorage        StorageConfig
	ThreadNotifierStorage StorageConfig
	StreamNotifierStorage StorageConfig
}

type PtchanConfig struct {
	BaseURL         string
	GatewayURL      string
	IntegrationName string
	SelfTripcodes   []string
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
	Timeout   time.Duration
}

type GatewayConfig struct {
	Webhook GatewayWebhookConfig
}

type GatewayWebhookConfig struct {
	Addr string
	Path string
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

type StorageConfig struct {
	SQLitePath string
}

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
	SelfTripcodes   []string                    `toml:"self_tripcodes"`
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
	LogMemory     bool                `toml:"log_memory"`
	SystemPrompt  string              `toml:"system_prompt"`
	RateLimit     fileRateLimitConfig `toml:"rate_limit"`
	Memory        fileMemoryConfig    `toml:"memory"`
	PtchanContext *filePtchanContext  `toml:"ptchan_context"`
	Trace         *fileAssistantTrace `toml:"trace"`
}

type fileChannerConfig struct {
	Mentions      []string                   `toml:"mentions"`
	MaxInputRunes int                        `toml:"max_input_runes"`
	SystemPrompt  string                     `toml:"system_prompt"`
	RateLimit     fileChannerRateLimitConfig `toml:"rate_limit"`
	PtchanContext *filePtchanContext         `toml:"ptchan_context"`
	Trace         *fileAssistantTrace        `toml:"trace"`
}

type fileChannerRateLimitConfig struct {
	Window       string `toml:"window"`
	RequestLimit int    `toml:"request_limit"`
	RequestBurst int    `toml:"request_burst"`
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
	Webhook fileGatewayWebhookConfig `toml:"webhook"`
}

type fileGatewayWebhookConfig struct {
	Addr string `toml:"addr"`
	Path string `toml:"path"`
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
	SQLitePath     string               `toml:"sqlite_path"`
	Chatter        fileAppStorageConfig `toml:"chatter"`
	Channer        fileAppStorageConfig `toml:"channer"`
	ThreadNotifier fileAppStorageConfig `toml:"threadnotifier"`
	StreamNotifier fileAppStorageConfig `toml:"streamnotifier"`
}

type fileAppStorageConfig struct {
	SQLitePath string `toml:"sqlite_path"`
}

type fileLoggingConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

type fileStreamNotifierConfig struct {
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
		Chatter: fileChatterConfig{
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
		Channer: fileChannerConfig{
			Mentions:      []string{"@martie"},
			MaxInputRunes: 4096,
			RateLimit: fileChannerRateLimitConfig{
				Window:       "60m",
				RequestLimit: 25,
				RequestBurst: 3,
			},
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
		StreamNotifier: fileStreamNotifierConfig{EndMissThreshold: 2, PollInterval: "60s"},
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
	chatterPtchanContext := assistantPtchanContextConfig(raw.Chatter.PtchanContext)
	chatterTrace := assistantTraceConfig(raw.Chatter.Trace)
	ptchanContext := assistantPtchanContextConfig(raw.Channer.PtchanContext)
	ptchanTrace := assistantTraceConfig(raw.Channer.Trace)
	basePtchan := PtchanConfig{
		BaseURL:         strings.TrimRight(strings.TrimSpace(raw.Ptchan.BaseURL), "/"),
		GatewayURL:      strings.TrimRight(strings.TrimSpace(raw.Ptchan.GatewayURL), "/"),
		IntegrationName: strings.TrimSpace(raw.Ptchan.IntegrationName),
		SelfTripcodes:   cleanTripcodes(raw.Ptchan.SelfTripcodes),
	}
	basePtchan.Secret = strings.TrimSpace(os.Getenv(integrationSecretEnv(basePtchan.IntegrationName)))
	chatterPtchan := ptchanIntegrationConfig(basePtchan, raw.Ptchan.Chatter)
	channerPtchan := ptchanIntegrationConfig(basePtchan, raw.Ptchan.Channer)
	threadNotifierPtchan := ptchanIntegrationConfig(basePtchan, raw.Ptchan.ThreadNotifier)
	cfg := Config{
		Locale:               locale,
		Ptchan:               basePtchan,
		ChatterPtchan:        chatterPtchan,
		ChannerPtchan:        channerPtchan,
		ThreadNotifierPtchan: threadNotifierPtchan,
		Telegram: TelegramConfig{
			BotToken:           strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
			NotificationChatID: raw.Telegram.NotificationChatID,
		},
		Chatter: chatterapp.Config{
			Name:               strings.TrimSpace(raw.Name),
			DiscussionChatID:   raw.Telegram.DiscussionChatID,
			AllowAllUsers:      raw.Telegram.AllowAllUsers,
			AllowedUserIDs:     raw.Telegram.AllowedUserIDs,
			UserRequestLimit:   raw.Chatter.RateLimit.UserLimit,
			UserRequestBurst:   raw.Chatter.RateLimit.UserBurst,
			GlobalRequestLimit: raw.Chatter.RateLimit.GlobalLimit,
			GlobalRequestBurst: raw.Chatter.RateLimit.GlobalBurst,
			MaxInputRunes:      raw.Chatter.MaxInputRunes,
			LogMemory:          raw.Chatter.LogMemory,
			Trace: assistant.TraceConfig{
				Enabled:  raw.Chatter.Trace != nil,
				Dir:      filepath.Clean(strings.TrimSpace(chatterTrace.Dir)),
				MaxFiles: chatterTrace.MaxFiles,
			},
			HistoryExchanges: raw.Chatter.Memory.HistoryExchanges,
			PtchanContext: assistant.PtchanContextConfig{
				Enabled:       raw.Chatter.PtchanContext != nil,
				BaseURL:       strings.TrimRight(strings.TrimSpace(raw.Ptchan.BaseURL), "/"),
				GatewayURL:    strings.TrimRight(strings.TrimSpace(raw.Ptchan.GatewayURL), "/"),
				MaxReplies:    chatterPtchanContext.MaxReplies,
				SelfTripcodes: cleanTripcodes(raw.Ptchan.SelfTripcodes),
			},
		},
		Channer: channerapp.Config{
			Name:          strings.TrimSpace(raw.Name),
			Mentions:      cleanMentions(raw.Channer.Mentions),
			MaxInputRunes: raw.Channer.MaxInputRunes,
			RequestLimit:  raw.Channer.RateLimit.RequestLimit,
			RequestBurst:  raw.Channer.RateLimit.RequestBurst,
			Trace: assistant.TraceConfig{
				Enabled:  raw.Channer.Trace != nil,
				Dir:      filepath.Clean(strings.TrimSpace(ptchanTrace.Dir)),
				MaxFiles: ptchanTrace.MaxFiles,
			},
			PtchanContext: assistant.PtchanContextConfig{
				Enabled:       raw.Channer.PtchanContext != nil,
				BaseURL:       strings.TrimRight(strings.TrimSpace(raw.Ptchan.BaseURL), "/"),
				GatewayURL:    strings.TrimRight(strings.TrimSpace(raw.Ptchan.GatewayURL), "/"),
				MaxReplies:    ptchanContext.MaxReplies,
				SelfTripcodes: cleanTripcodes(raw.Ptchan.SelfTripcodes),
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
		},
		ThreadNotifier: threadnotifierapp.Config{
			MinReplyPosts: raw.ThreadNotifier.MinReplyPosts,
			Filter: threadnotifierapp.Filter{
				BoardDenylist:   raw.ThreadNotifier.BoardDenylist,
				KeywordDenylist: raw.ThreadNotifier.KeywordDenylist,
			},
		},
		StreamNotifier:        streamnotifierapp.Config{EndMissThreshold: raw.StreamNotifier.EndMissThreshold},
		Runtime:               RuntimeConfig{HTTPAddr: strings.TrimSpace(raw.Runtime.HTTPAddr)},
		Storage:               StorageConfig{SQLitePath: cleanPath(raw.Storage.SQLitePath)},
		ChatterStorage:        appStorageConfig(raw.Storage.Chatter),
		ChannerStorage:        appStorageConfig(raw.Storage.Channer),
		ThreadNotifierStorage: appStorageConfig(raw.Storage.ThreadNotifier),
		StreamNotifierStorage: appStorageConfig(raw.Storage.StreamNotifier),
	}

	cfg.Chatter.SystemPrompt = strings.ReplaceAll(strings.TrimSpace(raw.Chatter.SystemPrompt), "{{name}}", cfg.Chatter.Name)
	cfg.Channer.SystemPrompt = strings.ReplaceAll(strings.TrimSpace(raw.Channer.SystemPrompt), "{{name}}", cfg.Channer.Name)
	if cfg.Ptchan.BaseURL == "" {
		return Config{}, fmt.Errorf("ptchan.base_url is required")
	}
	if cfg.Ptchan.GatewayURL == "" {
		return Config{}, fmt.Errorf("ptchan.gateway_url is required")
	}
	if cfg.Ptchan.IntegrationName == "" {
		return Config{}, fmt.Errorf("ptchan.integration_name is required")
	}
	if cfg.Chatter.MaxInputRunes <= 0 {
		return Config{}, fmt.Errorf("chatter.max_input_runes must be positive")
	}
	if cfg.Chatter.HistoryExchanges <= 0 {
		return Config{}, fmt.Errorf("chatter.memory.history_exchanges must be positive")
	}
	if cfg.Chatter.UserRequestLimit <= 0 || cfg.Chatter.UserRequestBurst <= 0 || cfg.Chatter.UserRequestBurst > cfg.Chatter.UserRequestLimit {
		return Config{}, fmt.Errorf("chatter.rate_limit.user_burst must be positive and no greater than user_limit")
	}
	if cfg.Chatter.GlobalRequestLimit <= 0 || cfg.Chatter.GlobalRequestBurst <= 0 || cfg.Chatter.GlobalRequestBurst > cfg.Chatter.GlobalRequestLimit {
		return Config{}, fmt.Errorf("chatter.rate_limit.global_burst must be positive and no greater than global_limit")
	}
	if cfg.Chatter.PtchanContext.MaxReplies <= 0 {
		return Config{}, fmt.Errorf("chatter.ptchan_context.max_replies must be positive")
	}
	if cfg.Chatter.Trace.MaxFiles <= 0 {
		return Config{}, fmt.Errorf("chatter.trace.max_files must be positive")
	}
	if cfg.Chatter.Trace.Enabled && cfg.Chatter.Trace.Dir == "." {
		return Config{}, fmt.Errorf("chatter.trace.dir is required when enabled")
	}
	if cfg.Channer.MaxInputRunes <= 0 {
		return Config{}, fmt.Errorf("channer.max_input_runes must be positive")
	}
	if len(cfg.Channer.Mentions) == 0 {
		return Config{}, fmt.Errorf("channer.mentions requires at least one mention")
	}
	if cfg.Channer.RequestLimit <= 0 || cfg.Channer.RequestBurst <= 0 || cfg.Channer.RequestBurst > cfg.Channer.RequestLimit {
		return Config{}, fmt.Errorf("channer.rate_limit.request_burst must be positive and no greater than request_limit")
	}
	if cfg.Channer.PtchanContext.MaxReplies <= 0 {
		return Config{}, fmt.Errorf("channer.ptchan_context.max_replies must be positive")
	}
	if cfg.Channer.Trace.MaxFiles <= 0 {
		return Config{}, fmt.Errorf("channer.trace.max_files must be positive")
	}
	if cfg.Channer.Trace.Enabled && cfg.Channer.Trace.Dir == "." {
		return Config{}, fmt.Errorf("channer.trace.dir is required when enabled")
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
	if cfg.ThreadNotifier.MinReplyPosts < 0 {
		return Config{}, fmt.Errorf("threadnotifier.min_reply_posts must be non-negative")
	}
	if cfg.StreamNotifier.EndMissThreshold <= 0 {
		return Config{}, fmt.Errorf("streamnotifier.end_miss_threshold must be positive")
	}
	if err := cfg.Runtime.Logging.Level.UnmarshalText([]byte(strings.TrimSpace(raw.Runtime.Logging.Level))); err != nil {
		return Config{}, fmt.Errorf("runtime.logging.level must be debug, info, warn, or error")
	}
	cfg.Runtime.Logging.Format = LogFormat(strings.TrimSpace(raw.Runtime.Logging.Format))
	if cfg.Runtime.Logging.Format != LogText && cfg.Runtime.Logging.Format != LogJSON {
		return Config{}, fmt.Errorf("runtime.logging.format must be %q or %q", LogText, LogJSON)
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

	if cfg.Chatter.ConversationTTL, err = positiveDuration("chatter.memory.ttl", raw.Chatter.Memory.TTL); err != nil {
		return Config{}, err
	}
	if cfg.Chatter.RateLimitWindow, err = positiveDuration("chatter.rate_limit.window", raw.Chatter.RateLimit.Window); err != nil {
		return Config{}, err
	}
	if cfg.Chatter.PtchanContext.Timeout, err = positiveDuration("chatter.ptchan_context.timeout", chatterPtchanContext.Timeout); err != nil {
		return Config{}, err
	}
	if cfg.Channer.PtchanContext.Timeout, err = positiveDuration("channer.ptchan_context.timeout", ptchanContext.Timeout); err != nil {
		return Config{}, err
	}
	if cfg.Channer.RateLimitWindow, err = positiveDuration("channer.rate_limit.window", raw.Channer.RateLimit.Window); err != nil {
		return Config{}, err
	}
	if cfg.DeepSeek.Timeout, err = positiveDuration("deepseek.timeout", raw.DeepSeek.Timeout); err != nil {
		return Config{}, err
	}
	if cfg.StreamNotifier.PollInterval, err = positiveDuration("streamnotifier.poll_interval", raw.StreamNotifier.PollInterval); err != nil {
		return Config{}, err
	}
	if cfg.ThreadNotifier.Filter.MaxThreadAge, err = nonNegativeDuration("threadnotifier.max_thread_age", raw.ThreadNotifier.MaxThreadAge); err != nil {
		return Config{}, err
	}
	if cfg.ThreadNotifier.PruneAfter, err = nonNegativeDuration("threadnotifier.prune_after", raw.ThreadNotifier.PruneAfter); err != nil {
		return Config{}, err
	}
	if cfg.Storage.SQLitePath == "" || cfg.Storage.SQLitePath == "." {
		return Config{}, fmt.Errorf("storage.sqlite_path is required")
	}
	if cfg.ChatterStorage.SQLitePath == "." {
		return Config{}, fmt.Errorf("storage.chatter.sqlite_path is required when set")
	}
	if cfg.ChannerStorage.SQLitePath == "." {
		return Config{}, fmt.Errorf("storage.channer.sqlite_path is required when set")
	}
	if cfg.ThreadNotifierStorage.SQLitePath == "." {
		return Config{}, fmt.Errorf("storage.threadnotifier.sqlite_path is required when set")
	}
	if cfg.StreamNotifierStorage.SQLitePath == "." {
		return Config{}, fmt.Errorf("storage.streamnotifier.sqlite_path is required when set")
	}
	if cfg.ChatterStorage.SQLitePath == "" {
		cfg.ChatterStorage = cfg.Storage
	}
	if cfg.ChannerStorage.SQLitePath == "" {
		cfg.ChannerStorage = cfg.Storage
	}
	if cfg.ThreadNotifierStorage.SQLitePath == "" {
		cfg.ThreadNotifierStorage = cfg.Storage
	}
	if cfg.StreamNotifierStorage.SQLitePath == "" {
		cfg.StreamNotifierStorage = cfg.Storage
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

func (c Config) ForApp(app AppName) (Config, error) {
	switch app {
	case AppChatter:
		c.App = app
		c.Ptchan = c.ChatterPtchan
		c.Storage = c.ChatterStorage
		return c, nil
	case AppChanner:
		c.App = app
		c.Ptchan = c.ChannerPtchan
		c.Storage = c.ChannerStorage
		return c, nil
	case AppThreadNotifier:
		c.App = app
		c.Ptchan = c.ThreadNotifierPtchan
		c.Storage = c.ThreadNotifierStorage
		return c, nil
	case AppStreamNotifier:
		c.App = app
		c.Storage = c.StreamNotifierStorage
		return c, nil
	default:
		return Config{}, fmt.Errorf("unknown app %q", app)
	}
}

func (c Config) ValidateRun() error {
	if c.App == "" {
		return fmt.Errorf("app command is required")
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
		if c.Chatter.PtchanContext.Enabled && c.Ptchan.Secret == "" {
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

func ptchanIntegrationConfig(base PtchanConfig, raw filePtchanIntegrationConfig) PtchanConfig {
	name := strings.TrimSpace(raw.IntegrationName)
	if name == "" {
		return base
	}
	cfg := base
	cfg.IntegrationName = name
	cfg.Secret = strings.TrimSpace(os.Getenv(integrationSecretEnv(name)))
	return cfg
}

func appStorageConfig(raw fileAppStorageConfig) StorageConfig {
	return StorageConfig{SQLitePath: cleanPath(raw.SQLitePath)}
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

func cleanTripcodes(values []string) []string {
	tripcodes := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		tripcode := strings.TrimSpace(value)
		if tripcode == "" {
			continue
		}
		if _, ok := seen[tripcode]; ok {
			continue
		}
		seen[tripcode] = struct{}{}
		tripcodes = append(tripcodes, tripcode)
	}
	return tripcodes
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

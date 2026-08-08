package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/loynet/ptchan-ai/context/thread"
	"github.com/pelletier/go-toml/v2"

	"martie/internal/channer"
)

type Config struct {
	Ptchan      PtchanConfig
	Channer     channer.Config
	DeepSeek    DeepSeekConfig
	GatewayAddr string
	Runtime     RuntimeConfig
	SQLitePath  string
}

type PtchanConfig struct {
	BaseURL         string
	GatewayURL      string
	IntegrationName string
	Secret          string
}

type DeepSeekConfig struct {
	APIKey    string
	Model     string
	MaxTokens int
}

type RuntimeConfig struct {
	HTTPAddr string
	LogLevel slog.Level
}

type fileConfig struct {
	Name   string `toml:"name"`
	Ptchan struct {
		BaseURL         string `toml:"base_url"`
		GatewayURL      string `toml:"gateway_url"`
		IntegrationName string `toml:"integration_name"`
	} `toml:"ptchan"`
	Channer struct {
		Mentions      []string `toml:"mentions"`
		MaxInputRunes int      `toml:"max_input_runes"`
		SystemPrompt  string   `toml:"system_prompt"`
		PruneAfter    string   `toml:"prune_after"`
		RateLimit     struct {
			GlobalPerHour int `toml:"global_per_hour"`
			GlobalBurst   int `toml:"global_burst"`
			ThreadPerHour int `toml:"thread_per_hour"`
			ThreadBurst   int `toml:"thread_burst"`
		} `toml:"rate_limit"`
		PtchanContext struct {
			MaxReplies int `toml:"max_replies"`
		} `toml:"ptchan_context"`
	} `toml:"channer"`
	DeepSeek struct {
		Model     string `toml:"model"`
		MaxTokens int    `toml:"max_tokens"`
	} `toml:"deepseek"`
	Gateway struct {
		Webhook struct {
			Addr string `toml:"addr"`
		} `toml:"webhook"`
	} `toml:"gateway"`
	Runtime struct {
		HTTPAddr string `toml:"http_addr"`
		Logging  struct {
			Level string `toml:"level"`
		} `toml:"logging"`
	} `toml:"runtime"`
	Storage struct {
		SQLitePath string `toml:"sqlite_path"`
	} `toml:"storage"`
}

func LoadConfig() (Config, error) {
	path := strings.TrimSpace(os.Getenv("CONFIG_FILE"))
	if path == "" {
		return Config{}, fmt.Errorf("CONFIG_FILE is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	var raw fileConfig
	decoder := toml.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		var x *toml.StrictMissingError
		if errors.As(err, &x) {
			return Config{}, fmt.Errorf("decode config %q:\n%s", path, x.String())
		}
		return Config{}, err
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(raw.Runtime.Logging.Level)); err != nil {
		return Config{}, err
	}

	ptchan := PtchanConfig{
		BaseURL:         strings.TrimRight(strings.TrimSpace(raw.Ptchan.BaseURL), "/"),
		GatewayURL:      strings.TrimRight(strings.TrimSpace(raw.Ptchan.GatewayURL), "/"),
		IntegrationName: strings.TrimSpace(raw.Ptchan.IntegrationName),
	}
	ptchan.Secret = strings.TrimSpace(os.Getenv(secretEnv(ptchan.IntegrationName)))

	pruneAfter, err := time.ParseDuration(raw.Channer.PruneAfter)
	if err != nil || pruneAfter < 0 {
		return Config{}, fmt.Errorf("channer.prune_after must be non-negative")
	}
	name := strings.TrimSpace(raw.Name)
	if name == "" {
		return Config{}, fmt.Errorf("name is required")
	}

	config := Config{
		Ptchan:      ptchan,
		GatewayAddr: strings.TrimSpace(raw.Gateway.Webhook.Addr),
		DeepSeek: DeepSeekConfig{
			APIKey:    strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")),
			Model:     strings.TrimSpace(raw.DeepSeek.Model),
			MaxTokens: raw.DeepSeek.MaxTokens,
		},
		Runtime: RuntimeConfig{
			HTTPAddr: strings.TrimSpace(raw.Runtime.HTTPAddr),
			LogLevel: level,
		},
		SQLitePath: filepath.Clean(strings.TrimSpace(raw.Storage.SQLitePath)),
		Channer: channer.Config{
			Mentions:      cleanMentions(raw.Channer.Mentions),
			SystemPrompt:  strings.ReplaceAll(strings.TrimSpace(raw.Channer.SystemPrompt), "{{name}}", name),
			MaxInputRunes: raw.Channer.MaxInputRunes,
			GlobalPerHour: raw.Channer.RateLimit.GlobalPerHour,
			GlobalBurst:   raw.Channer.RateLimit.GlobalBurst,
			ThreadPerHour: raw.Channer.RateLimit.ThreadPerHour,
			ThreadBurst:   raw.Channer.RateLimit.ThreadBurst,
			PruneAfter:    pruneAfter,
			ThreadContext: thread.Config{
				BaseURL:         ptchan.BaseURL,
				IntegrationName: ptchan.IntegrationName,
				MaxReplies:      raw.Channer.PtchanContext.MaxReplies,
			},
		},
	}
	return config, config.ValidateRun()
}

func (c Config) ValidateRun() error {
	switch {
	case c.SQLitePath == "" || c.SQLitePath == ".":
		return fmt.Errorf("storage.sqlite_path is required")
	case c.Ptchan.BaseURL == "":
		return fmt.Errorf("ptchan.base_url is required")
	case c.Ptchan.GatewayURL == "":
		return fmt.Errorf("ptchan.gateway_url is required")
	case c.Ptchan.IntegrationName == "":
		return fmt.Errorf("ptchan.integration_name is required")
	case c.Ptchan.Secret == "":
		return fmt.Errorf("%s is required", secretEnv(c.Ptchan.IntegrationName))
	case c.GatewayAddr == "":
		return fmt.Errorf("gateway.webhook.addr is required")
	case c.DeepSeek.APIKey == "":
		return fmt.Errorf("DEEPSEEK_API_KEY is required")
	case c.DeepSeek.Model == "" || c.DeepSeek.MaxTokens <= 0:
		return fmt.Errorf("valid deepseek configuration is required")
	case c.Channer.SystemPrompt == "":
		return fmt.Errorf("channer.system_prompt is required")
	case len(c.Channer.Mentions) == 0 || c.Channer.MaxInputRunes <= 0:
		return fmt.Errorf("channer mentions and max_input_runes must be configured")
	case c.Channer.GlobalPerHour <= 0 || c.Channer.GlobalBurst <= 0 || c.Channer.GlobalBurst > c.Channer.GlobalPerHour:
		return fmt.Errorf("channer global hourly rate and burst must be positive, with burst no greater than rate")
	case c.Channer.ThreadPerHour <= 0 || c.Channer.ThreadBurst <= 0 || c.Channer.ThreadBurst > c.Channer.ThreadPerHour:
		return fmt.Errorf("channer thread hourly rate and burst must be positive, with burst no greater than rate")
	case c.Channer.ThreadContext.MaxReplies <= 0:
		return fmt.Errorf("channer.ptchan_context.max_replies must be positive")
	}
	return nil
}

func secretEnv(n string) string {
	return "PTCHAN_INTEGRATION_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(n)) + "_SECRET"
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

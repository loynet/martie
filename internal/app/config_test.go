package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/loynet/ptchan-ai/context/thread"

	"martie/internal/channer"
)

func TestLoadExampleConfig(t *testing.T) {
	t.Setenv("CONFIG_FILE", filepath.Join("..", "..", "config", "example.toml"))
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("PTCHAN_INTEGRATION_MARTIE_SECRET", "test-secret")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ptchan.IntegrationName != "martie" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestValidateRunRejectsInvalidRateLimit(t *testing.T) {
	cfg := validConfig()
	cfg.Channer.GlobalBurst = 2
	if err := cfg.ValidateRun(); err == nil {
		t.Fatal("ValidateRun accepted a burst larger than its rate limit")
	}
}

func TestLoadConfigRejectsBlankMentions(t *testing.T) {
	t.Setenv("CONFIG_FILE", writeConfig(t, `
name = "Martie"
[ptchan]
base_url = "https://ptchan.test"
gateway_url = "https://gateway.test"
integration_name = "martie"
[channer]
mentions = [" ", ""]
max_input_runes = 1
system_prompt = "prompt"
prune_after = "0s"
[channer.rate_limit]
global_per_hour = 1
global_burst = 1
thread_per_hour = 1
thread_burst = 1
[channer.ptchan_context]
max_replies = 1
[deepseek]
model = "model"
max_tokens = 1
[gateway.webhook]
addr = ":8081"
[runtime.logging]
level = "info"
[storage]
sqlite_path = "data/martie.db"
`))
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("PTCHAN_INTEGRATION_MARTIE_SECRET", "test-secret")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig accepted blank channer mentions")
	}
}

func validConfig() Config {
	return Config{
		Ptchan:      PtchanConfig{BaseURL: "https://ptchan.test", GatewayURL: "https://gateway.test", IntegrationName: "martie", Secret: "secret"},
		DeepSeek:    DeepSeekConfig{APIKey: "key", Model: "model", MaxTokens: 1},
		GatewayAddr: ":8081",
		SQLitePath:  "data/martie.db",
		Channer: channer.Config{
			Mentions: []string{"@martie"}, SystemPrompt: "prompt", MaxInputRunes: 1,
			GlobalPerHour: 1, GlobalBurst: 1,
			ThreadPerHour: 1, ThreadBurst: 1,
			ThreadContext: thread.Config{MaxReplies: 1},
		},
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

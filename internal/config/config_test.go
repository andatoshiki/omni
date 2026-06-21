package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParamsLoad(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: deepseek
    api_key: sk-test
    models:
      - name: deepseek-chat
        max_context_tokens: 12000
        input_price: 0.27
        output_price: 1.10
global:
  initial_prompt: Be concise.
telegram:
  bot_token: 123:test
  allowed_user_ids: [10, 10]
  admin_user_ids: [20]
  allowed_group_ids: [-100, -100]
`)

	var got Params
	if err := got.Load(filename); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	wantDatabasePath := filepath.Join(filepath.Dir(filename), DefaultDatabasePath)
	if got.Database.SQLite.Path != wantDatabasePath {
		t.Fatalf("DatabasePath = %q, want %q", got.Database.SQLite.Path, wantDatabasePath)
	}
	if got.Temperature != 1.3 || got.MaxReplyTokens != 2048 || got.MaxContextTokens != 8192 || got.HistorySize != 4 {
		t.Fatalf("defaults not applied: %+v", got)
	}
	if !slices.Equal(got.AllowedUserIDs, []int64{10, 20}) {
		t.Fatalf("AllowedUserIDs = %v, want [10 20]", got.AllowedUserIDs)
	}
	if !slices.Equal(got.AllowedGroupIDs, []int64{-100}) {
		t.Fatalf("AllowedGroupIDs = %v, want [-100]", got.AllowedGroupIDs)
	}
	if len(got.Providers) != 1 || got.Providers[0].Name != "deepseek" {
		t.Fatalf("Providers = %v, want 1 deepseek provider", got.Providers)
	}
	if got.Providers[0].Models[0].InputPrice != 0.27 {
		t.Fatalf("InputPrice = %v, want 0.27", got.Providers[0].Models[0].InputPrice)
	}
	if got.Providers[0].Models[0].MaxContextTokens != 12000 {
		t.Fatalf("MaxContextTokens = %v, want 12000", got.Providers[0].Models[0].MaxContextTokens)
	}
}

func TestParamsLoadCustomDatabasePath(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: deepseek
    api_key: sk-test
    models:
      - name: deepseek-chat
database:
  backend: "sqlite"
  sqlite:
    path: data/omni.db
telegram:
  bot_token: 123:test
`)

	var got Params
	if err := got.Load(filename); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := filepath.Join(filepath.Dir(filename), "data", "omni.db")
	if got.Database.SQLite.Path != want {
		t.Fatalf("DatabasePath = %q, want %q", got.Database.SQLite.Path, want)
	}
}

func TestParamsLoadRejectsEmptyDatabasePath(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: deepseek
    api_key: sk-test
    models:
      - name: deepseek-chat
database:
  backend: "sqlite"
  sqlite:
    path: ""
telegram:
  bot_token: 123:test
`)

	var got Params
	err := got.Load(filename)
	if err == nil || !strings.Contains(err.Error(), "database.sqlite.path") {
		t.Fatalf("Load() error = %v, want database path error", err)
	}
}

func TestParamsLoadRejectsContextLimitWithoutReplyRoom(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: deepseek
    api_key: sk-test
    models:
      - name: deepseek-chat
global:
  max_reply_tokens: 2048
  max_context_tokens: 2048
telegram:
  bot_token: 123:test
`)

	var got Params
	err := got.Load(filename)
	if err == nil || !strings.Contains(err.Error(), "max_context_tokens") {
		t.Fatalf("Load() error = %v, want context limit error", err)
	}
}

func TestParamsLoadEnabledDefault(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: deepseek
    api_key: sk-test
    models:
      - name: deepseek-chat
telegram:
  bot_token: 123:test
`)
	var got Params
	if err := got.Load(filename); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !got.Providers[0].IsEnabled() {
		t.Fatal("provider without 'enabled' field should default to true")
	}
}

func TestParamsLoadDisabledProvider(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: deepseek
    enabled: false
    api_key: sk-test
    models:
      - name: deepseek-chat
  - name: openai
    api_key: sk-test2
    models:
      - name: gpt-4o
telegram:
  bot_token: 123:test
`)
	var got Params
	if err := got.Load(filename); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Providers[0].IsEnabled() {
		t.Fatal("provider with enabled: false should be disabled")
	}
}

func TestParamsLoadCustomProvider(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: deepseek
    type: deepseek
    api_key: sk-test
    models:
      - name: deepseek-chat
  - name: your-name
    type: custom
    enabled: false
    api_key: ""
    api_base: ""
    models:
      - name: gpt-4o
        input_price: 2.50
        output_price: 10.00
telegram:
  bot_token: 123:test
`)

	var got Params
	if err := got.Load(filename); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Providers) != 2 || got.Providers[1].EffectiveType() != ProviderTypeCustom {
		t.Fatalf("Providers = %#v, want disabled custom provider", got.Providers)
	}
}

func TestParamsLoadAnthropicProvider(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: anthropic
    api_key: sk-ant-test
    models:
      - name: claude-test
        temperature: 0.7
telegram:
  bot_token: 123:test
`)

	var got Params
	if err := got.Load(filename); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Providers[0].EffectiveType() != ProviderTypeAnthropic {
		t.Fatalf("EffectiveType() = %q, want anthropic", got.Providers[0].EffectiveType())
	}
}

func TestParamsLoadXAIProvider(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: grok
    type: xai
    api_key: xai-test
    models:
      - name: grok-2-latest
telegram:
  bot_token: 123:test
`)

	var got Params
	if err := got.Load(filename); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Providers[0].EffectiveType() != ProviderTypeXAI {
		t.Fatalf("EffectiveType() = %q, want xai", got.Providers[0].EffectiveType())
	}
}

func TestParamsLoadRejectsAnthropicTemperatureAboveOne(t *testing.T) {
	tests := []struct {
		name        string
		temperature string
	}{
		{name: "global", temperature: ""},
		{name: "model override", temperature: "        temperature: 1.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := writeTestConfig(t, `
providers:
  - name: claude-provider
    type: anthropic
    api_key: sk-ant-test
    models:
      - name: claude-test
`+tt.temperature+`
global:
  temperature: 1.3
telegram:
  bot_token: 123:test
`)

			var got Params
			err := got.Load(filename)
			if err == nil || !strings.Contains(err.Error(), "between 0 and 1 for Anthropic") {
				t.Fatalf("Load() error = %v, want Anthropic temperature error", err)
			}
		})
	}
}

func TestParamsLoadRejectsDuplicateProviderNames(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: duplicate
    type: custom
    api_key: sk-one
    models: [{name: model-one}]
  - name: duplicate
    type: custom
    api_key: sk-two
    models: [{name: model-two}]
telegram:
  bot_token: 123:test
`)

	var got Params
	err := got.Load(filename)
	if err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("Load() error = %v, want duplicate provider error", err)
	}
}

func TestConfigPathForExecutable(t *testing.T) {
	got := configPathForExecutable(filepath.Join("opt", "omni", "omni"))
	want := filepath.Join("opt", "omni", "config.yaml")
	if got != want {
		t.Fatalf("configPathForExecutable() = %q, want %q", got, want)
	}
}

func TestParamsLoadRejectsUnknownFields(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: deepseek
    api_key: sk-test
    models:
      - name: deepseek-chat
    unexpected: true
telegram:
  bot_token: 123:test
`)

	var got Params
	err := got.Load(filename)
	if err == nil || !strings.Contains(err.Error(), "field unexpected not found") {
		t.Fatalf("Load() error = %v, want unknown field error", err)
	}
}

func TestParamsLoadValidatesValues(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: deepseek
    api_key: sk-test
    models:
      - name: deepseek-chat
global:
  history_size: 0
telegram:
  bot_token: 123:test
`)

	var got Params
	err := got.Load(filename)
	if err == nil || !strings.Contains(err.Error(), "history_size") {
		t.Fatalf("Load() error = %v, want history size error", err)
	}
}

func TestParamsLoadRejectsLegacyGroups(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: deepseek
    api_key: sk-test
    models:
      - name: deepseek-chat
telegram:
  bot_token: 123:test
groups:
  - id: "@example_group"
    topic: 1
`)

	var got Params
	err := got.Load(filename)
	if err == nil || !strings.Contains(err.Error(), "field groups not found") {
		t.Fatalf("Load() error = %v, want legacy groups field error", err)
	}
}

func TestParamsLoadRejectsLegacyChatCommand(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: deepseek
    api_key: sk-test
    models:
      - name: deepseek-chat
telegram:
  bot_token: 123:test
  chat_command: chat
`)

	var got Params
	err := got.Load(filename)
	if err == nil || !strings.Contains(err.Error(), "field chat_command not found") {
		t.Fatalf("Load() error = %v, want legacy chat_command field error", err)
	}
}

func TestParamsLoadRejectsNoProviders(t *testing.T) {
	filename := writeTestConfig(t, `
providers: []
telegram:
  bot_token: 123:test
`)

	var got Params
	err := got.Load(filename)
	if err == nil || !strings.Contains(err.Error(), "at least one provider") {
		t.Fatalf("Load() error = %v, want no provider error", err)
	}
}

func TestParamsLoadRejectsAllDisabled(t *testing.T) {
	filename := writeTestConfig(t, `
providers:
  - name: deepseek
    enabled: false
    api_key: sk-test
    models:
      - name: deepseek-chat
telegram:
  bot_token: 123:test
`)

	var got Params
	err := got.Load(filename)
	if err == nil || !strings.Contains(err.Error(), "at least one provider must be enabled") {
		t.Fatalf("Load() error = %v, want all disabled error", err)
	}
}

func writeTestConfig(t *testing.T, contents string) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(filename, []byte(contents), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return filename
}

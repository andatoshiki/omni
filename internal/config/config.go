package config

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultDatabasePath    = "omni.db"
)

type Params struct {
	Providers []ProviderConfig

	BotToken string
	Database DatabaseConfig

	InitialPrompt    string
	Temperature      float64
	MaxReplyTokens   int
	MaxContextTokens int
	HistorySize          int
	SenderContext        string
	SessionTimeout       time.Duration
	MaxSessionsDisplayed int
	TitleModel           string

	AllowedUserIDs  []int64
	AdminUserIDs    []int64
	AllowedGroupIDs []int64
}

type configFile struct {
	Providers []ProviderConfig `yaml:"providers"`
	Database  databaseConfig   `yaml:"database"`
	Global    globalConfig     `yaml:"global"`
	Telegram  telegramConfig   `yaml:"telegram"`
}

func (p *Params) Init() error {
	configPath, err := defaultConfigPath()
	if err != nil {
		return err
	}
	flag.StringVar(&configPath, "c", configPath, "path to YAML configuration file")
	flag.StringVar(&configPath, "config", configPath, "path to YAML configuration file")
	flag.Parse()

	return p.Load(configPath)
}

func defaultConfigPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	return configPathForExecutable(executable), nil
}

func configPathForExecutable(executable string) string {
	return filepath.Join(filepath.Dir(executable), "config.yaml")
}

func (p *Params) Load(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read config %q: %w", filename, err)
	}

	cfg := configFile{
		Database: databaseConfig{
			Backend: "sqlite",
			SQLite: SQLiteConfig{
				Path: DefaultDatabasePath,
			},
		},
		Global: globalConfig{
			Temperature:          1.3,
			MaxReplyTokens:       2048,
			MaxContextTokens:     8192,
			HistorySize:          4,
			SenderContext:        "groups",
			SessionTimeout:       "15m",
			MaxSessionsDisplayed: 10,
		},
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return fmt.Errorf("parse config %q: %w", filename, err)
	}
	if err := rejectAdditionalYAMLDocuments(decoder, filename); err != nil {
		return err
	}
	if cfg.Database.Backend == "" {
		cfg.Database.Backend = "sqlite"
	}

	var databaseConfigOut DatabaseConfig
	databaseConfigOut.Backend = cfg.Database.Backend
	databaseConfigOut.MySQL = cfg.Database.MySQL
	databaseConfigOut.Postgres = cfg.Database.Postgres

	if databaseConfigOut.Backend == "sqlite" {
		databasePath, err := resolveDatabasePath(filename, cfg.Database.SQLite.Path)
		if err != nil {
			return err
		}
		databaseConfigOut.SQLite.Path = databasePath
	}

	sessionTimeout, err := time.ParseDuration(cfg.Global.SessionTimeout)
	if err != nil {
		return fmt.Errorf("invalid global.session_timeout: %w", err)
	}

	*p = Params{
		Providers:            cfg.Providers,
		BotToken:             strings.TrimSpace(cfg.Telegram.BotToken),
		Database:             databaseConfigOut,
		InitialPrompt:        cfg.Global.InitialPrompt,
		Temperature:          cfg.Global.Temperature,
		MaxReplyTokens:       cfg.Global.MaxReplyTokens,
		MaxContextTokens:     cfg.Global.MaxContextTokens,
		HistorySize:          cfg.Global.HistorySize,
		SenderContext:        cfg.Global.SenderContext,
		SessionTimeout:       sessionTimeout,
		MaxSessionsDisplayed: cfg.Global.MaxSessionsDisplayed,
		TitleModel:           strings.TrimSpace(cfg.Global.TitleModel),
		AllowedUserIDs:       deduplicateIDs(cfg.Telegram.AllowedUserIDs),
		AdminUserIDs:         deduplicateIDs(cfg.Telegram.AdminUserIDs),
		AllowedGroupIDs:      deduplicateIDs(cfg.Telegram.AllowedGroupIDs),
	}

	for _, id := range p.AdminUserIDs {
		if !slices.Contains(p.AllowedUserIDs, id) {
			p.AllowedUserIDs = append(p.AllowedUserIDs, id)
		}
	}

	return p.validate()
}

func resolveDatabasePath(configFilename, configuredPath string) (string, error) {
	configuredPath = strings.TrimSpace(configuredPath)
	if configuredPath == "" {
		return "", nil
	}
	if filepath.IsAbs(configuredPath) {
		return filepath.Clean(configuredPath), nil
	}
	absoluteConfigPath, err := filepath.Abs(configFilename)
	if err != nil {
		return "", fmt.Errorf("resolve config path %q: %w", configFilename, err)
	}
	return filepath.Join(filepath.Dir(absoluteConfigPath), configuredPath), nil
}

func rejectAdditionalYAMLDocuments(decoder *yaml.Decoder, filename string) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("parse config %q: %w", filename, err)
	}
	return fmt.Errorf("parse config %q: multiple YAML documents are not supported", filename)
}

func deduplicateIDs(ids []int64) []int64 {
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(result, id) {
			result = append(result, id)
		}
	}
	return result
}

func (p *Params) validate() error {
	if len(p.Providers) == 0 {
		return fmt.Errorf("at least one provider must be configured under 'providers'")
	}
	hasEnabled := false
	providerNames := make(map[string]int, len(p.Providers))
	for i, prov := range p.Providers {
		providerName := strings.TrimSpace(prov.Name)
		if providerName == "" {
			return fmt.Errorf("providers[%d].name is required", i)
		}
		if firstIndex, exists := providerNames[providerName]; exists {
			return fmt.Errorf("providers[%d].name duplicates providers[%d].name (%s)", i, firstIndex, providerName)
		}
		providerNames[providerName] = i

		switch prov.EffectiveType() {
		case ProviderTypeDeepSeek, ProviderTypeOpenAI, ProviderTypeCustom, ProviderTypeGoogle, ProviderTypeAnthropic, ProviderTypeXAI, ProviderTypePerplexity, ProviderTypeOllama, ProviderTypeGroq, ProviderTypeTogether, ProviderTypeMistral, ProviderTypeBedrock:
		default:
			return fmt.Errorf("providers[%d].type must be one of deepseek, openai, custom, google, anthropic, xai, perplexity, ollama, groq, together, mistral, bedrock (provider: %s)", i, providerName)
		}

		if !prov.IsEnabled() {
			continue
		}
		hasEnabled = true
		if strings.TrimSpace(prov.APIKey) == "" {
			return fmt.Errorf("providers[%d].api_key is required (provider: %s)", i, providerName)
		}
		if len(prov.Models) == 0 {
			return fmt.Errorf("providers[%d].models must have at least one model (provider: %s)", i, providerName)
		}
		for j, model := range prov.Models {
			if strings.TrimSpace(model.Name) == "" {
				return fmt.Errorf("providers[%d].models[%d].name is required (provider: %s)", i, j, providerName)
			}
			if model.MaxReplyTokens < 0 {
				return fmt.Errorf("providers[%d].models[%d].max_reply_tokens must not be negative (provider: %s)", i, j, providerName)
			}
			if model.MaxContextTokens < 0 {
				return fmt.Errorf("providers[%d].models[%d].max_context_tokens must not be negative (provider: %s)", i, j, providerName)
			}

			effReplyTokens := p.MaxReplyTokens
			if model.MaxReplyTokens > 0 {
				effReplyTokens = model.MaxReplyTokens
			}
			effContextTokens := p.MaxContextTokens
			if model.MaxContextTokens > 0 {
				effContextTokens = model.MaxContextTokens
			}
			if effContextTokens <= effReplyTokens {
				return fmt.Errorf("providers[%d].models[%d] effective max_context_tokens (%d) must be greater than effective max_reply_tokens (%d) (provider: %s)", i, j, effContextTokens, effReplyTokens, providerName)
			}
			if model.Temperature != nil && (*model.Temperature < 0 || *model.Temperature > 2) {
				return fmt.Errorf("providers[%d].models[%d].temperature must be between 0 and 2 (provider: %s)", i, j, providerName)
			}

			effectiveTemperature := p.Temperature
			if model.Temperature != nil {
				effectiveTemperature = float64(*model.Temperature)
			}
			if prov.EffectiveType() == ProviderTypeAnthropic && effectiveTemperature > 1 {
				return fmt.Errorf("providers[%d].models[%d].temperature must be between 0 and 1 for Anthropic (provider: %s)", i, j, providerName)
			}
		}
	}
	if !hasEnabled {
		return fmt.Errorf("at least one provider must be enabled")
	}
	if p.BotToken == "" {
		return fmt.Errorf("telegram.bot_token is required")
	}
	if p.Database.Backend == "sqlite" && p.Database.SQLite.Path == "" {
		return fmt.Errorf("database.sqlite.path is required")
	}
	if p.Temperature < 0 || p.Temperature > 2 {
		return fmt.Errorf("global.temperature must be between 0 and 2")
	}
	if p.MaxReplyTokens <= 0 {
		return fmt.Errorf("global.max_reply_tokens must be greater than 0")
	}
	if p.MaxContextTokens <= p.MaxReplyTokens {
		return fmt.Errorf("global.max_context_tokens must be greater than global.max_reply_tokens")
	}
	if p.HistorySize <= 0 {
		return fmt.Errorf("global.history_size must be greater than 0")
	}
	switch p.SenderContext {
	case "off", "groups", "all":
		// Valid
	default:
		return fmt.Errorf("global.sender_context must be one of: off, groups, all")
	}
	return nil
}

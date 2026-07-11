package config

import (
	"strings"
	"time"
)

const (
	ProviderTypeDeepSeek   = "deepseek"
	ProviderTypeOpenAI     = "openai"
	ProviderTypeCustom     = "custom"
	ProviderTypeGoogle     = "google"
	ProviderTypeAnthropic  = "anthropic"
	ProviderTypeXAI        = "xai"
	ProviderTypePerplexity = "perplexity"
	ProviderTypeOllama     = "ollama"
	ProviderTypeGroq       = "groq"
	ProviderTypeTogether   = "together"
	ProviderTypeMistral    = "mistral"
	ProviderTypeBedrock    = "bedrock"
	ProviderTypeAzure      = "azure"
	ProviderTypeCloudflare = "cloudflare"
	ProviderTypeCohere     = "cohere"
	ProviderTypeHuggingFace= "huggingface"
)

type ProviderConfig struct {
	Name         string         `yaml:"name"`
	Type         string         `yaml:"type"`
	Enabled      *bool          `yaml:"enabled"` // nil = true (default enabled)
	APIKey       string         `yaml:"api_key"`
	APIBase      string         `yaml:"api_base"`
	AWSAccessKey string         `yaml:"aws_access_key"`
	AWSSecretKey string         `yaml:"aws_secret_key"`
	AWSRegion           string         `yaml:"aws_region"`
	APIVersion          string         `yaml:"api_version"`
	CloudflareAccountID string         `yaml:"cloudflare_account_id"`
	Timeout             *time.Duration `yaml:"timeout"`
	Models              []ModelConfig  `yaml:"models"`
}

// IsEnabled returns whether the provider is enabled.
// Defaults to true when the field is omitted.
func (p ProviderConfig) IsEnabled() bool {
	return p.Enabled == nil || *p.Enabled
}

func (p ProviderConfig) EffectiveType() string {
	providerType := strings.ToLower(strings.TrimSpace(p.Type))
	if providerType != "" {
		return providerType
	}

	switch strings.ToLower(strings.TrimSpace(p.Name)) {
	case ProviderTypeDeepSeek:
		return ProviderTypeDeepSeek
	case ProviderTypeOpenAI:
		return ProviderTypeOpenAI
	case ProviderTypeGoogle:
		return ProviderTypeGoogle
	case ProviderTypeAnthropic:
		return ProviderTypeAnthropic
	case ProviderTypeXAI:
		return ProviderTypeXAI
	case ProviderTypePerplexity:
		return ProviderTypePerplexity
	case ProviderTypeOllama:
		return ProviderTypeOllama
	case ProviderTypeGroq:
		return ProviderTypeGroq
	case ProviderTypeTogether:
		return ProviderTypeTogether
	case ProviderTypeMistral:
		return ProviderTypeMistral
	case ProviderTypeBedrock:
		return ProviderTypeBedrock
	case ProviderTypeAzure:
		return ProviderTypeAzure
	case ProviderTypeCloudflare:
		return ProviderTypeCloudflare
	case ProviderTypeCohere:
		return ProviderTypeCohere
	case ProviderTypeHuggingFace:
		return ProviderTypeHuggingFace
	default:
		return ProviderTypeCustom
	}
}

type ModelConfig struct {
	Name             string   `yaml:"name"`
	InputPrice       float64  `yaml:"input_price"`  // USD per 1M input tokens
	OutputPrice      float64  `yaml:"output_price"` // USD per 1M output tokens
	Temperature      *float32 `yaml:"temperature,omitempty"`
	MaxReplyTokens   int      `yaml:"max_reply_tokens"`   // 0 inherits global.max_reply_tokens
	MaxContextTokens int      `yaml:"max_context_tokens"` // 0 inherits global.max_context_tokens
}

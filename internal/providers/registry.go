package providers

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/providers/platforms"
	anthropicplatform "github.com/andatoshiki/omni/internal/providers/platforms/anthropic"
	"github.com/andatoshiki/omni/internal/providers/platforms/bedrock"
	customplatform "github.com/andatoshiki/omni/internal/providers/platforms/custom"
	deepseekplatform "github.com/andatoshiki/omni/internal/providers/platforms/deepseek"
	googleplatform "github.com/andatoshiki/omni/internal/providers/platforms/google"
	groqplatform "github.com/andatoshiki/omni/internal/providers/platforms/groq"
	mistralplatform "github.com/andatoshiki/omni/internal/providers/platforms/mistral"
	ollamaplatform "github.com/andatoshiki/omni/internal/providers/platforms/ollama"
	openaiplatform "github.com/andatoshiki/omni/internal/providers/platforms/openai"
	togetherplatform "github.com/andatoshiki/omni/internal/providers/platforms/together"
	xaiplatform "github.com/andatoshiki/omni/internal/providers/platforms/xai"
)

// Provider holds the runtime configuration for a single AI provider.
type Provider struct {
	Name    string
	Type    string
	APIKey  string
	BaseURL string
	Models  []config.ModelConfig

	adapter Adapter
}

// Adapter is the boundary between bot commands and provider-specific HTTP APIs.
type Adapter interface {
	CreateChatCompletionStream(ctx context.Context, endpoint platforms.Endpoint, request *platforms.ChatCompletionStreamRequest) (platforms.ChatCompletionStream, error)
}

// Registry holds all enabled providers, keyed by provider name.
type Registry struct {
	providers map[string]*Provider
	order     []string
}

var defaultBaseURLs = map[string]string{
	config.ProviderTypeDeepSeek:  "https://api.deepseek.com",
	config.ProviderTypeOpenAI:    "https://api.openai.com/v1",
	config.ProviderTypeCustom:    "https://api.openai.com/v1",
	config.ProviderTypeGoogle:     "https://generativelanguage.googleapis.com/v1beta/openai/",
	config.ProviderTypeAnthropic:  "https://api.anthropic.com",
	config.ProviderTypeXAI:        "https://api.x.ai/v1",
	config.ProviderTypePerplexity: "https://api.perplexity.ai",
	config.ProviderTypeOllama:     "http://localhost:11434/v1",
	config.ProviderTypeGroq:       "https://api.groq.com/openai/v1",
	config.ProviderTypeTogether:   "https://api.together.xyz/v1",
	config.ProviderTypeMistral:    "https://api.mistral.ai/v1",
}

// NewRegistry initializes the provider registry from config.
// Only enabled providers are registered.
func NewRegistry(configs []config.ProviderConfig) (*Registry, error) {
	registry := &Registry{
		providers: make(map[string]*Provider, len(configs)),
		order:     make([]string, 0, len(configs)),
	}

	for _, cfg := range configs {
		if !cfg.IsEnabled() {
			continue
		}

		providerType := cfg.EffectiveType()
		adapter, err := adapterForType(cfg)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", cfg.Name, err)
		}

		baseURL := strings.TrimSpace(cfg.APIBase)
		if baseURL == "" {
			baseURL = defaultBaseURLs[providerType]
		}

		provider := &Provider{
			Name:    strings.TrimSpace(cfg.Name),
			Type:    providerType,
			APIKey:  strings.TrimSpace(cfg.APIKey),
			BaseURL: strings.TrimRight(baseURL, "/"),
			Models:  cfg.Models,
			adapter: adapter,
		}
		registry.providers[provider.Name] = provider
		registry.order = append(registry.order, provider.Name)
	}

	return registry, nil
}

func adapterForType(cfg config.ProviderConfig) (Adapter, error) {
	providerType := cfg.EffectiveType()
	var client *http.Client
	if cfg.Timeout != nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = *cfg.Timeout
		client = &http.Client{Transport: transport}
	}
	switch providerType {
	case config.ProviderTypeDeepSeek:
		return deepseekplatform.Adapter{OpenAI: openaiplatform.Adapter{HTTPClient: client}}, nil
	case config.ProviderTypeOpenAI:
		return openaiplatform.Adapter{HTTPClient: client}, nil
	case config.ProviderTypeCustom:
		return customplatform.Adapter{OpenAI: openaiplatform.Adapter{HTTPClient: client}}, nil
	case config.ProviderTypeGoogle:
		return googleplatform.Adapter{HTTPClient: client}, nil
	case config.ProviderTypeAnthropic:
		return anthropicplatform.Adapter{HTTPClient: client}, nil
	case config.ProviderTypeXAI:
		return xaiplatform.Adapter{OpenAI: openaiplatform.Adapter{HTTPClient: client}}, nil
	case config.ProviderTypePerplexity:
		return openaiplatform.Adapter{HTTPClient: client}, nil
	case config.ProviderTypeOllama:
		return ollamaplatform.Adapter{OpenAI: openaiplatform.Adapter{HTTPClient: client}}, nil
	case config.ProviderTypeGroq:
		return groqplatform.Adapter{OpenAI: openaiplatform.Adapter{HTTPClient: client}}, nil
	case config.ProviderTypeTogether:
		return togetherplatform.Adapter{OpenAI: openaiplatform.Adapter{HTTPClient: client}}, nil
	case config.ProviderTypeMistral:
		return mistralplatform.Adapter{OpenAI: openaiplatform.Adapter{HTTPClient: client}}, nil
	case config.ProviderTypeBedrock:
		ctx := context.Background()
		var awsCfg aws.Config
		var err error

		if cfg.AWSAccessKey != "" && cfg.AWSSecretKey != "" {
			awsCfg, err = awsconfig.LoadDefaultConfig(ctx,
				awsconfig.WithRegion(cfg.AWSRegion),
				awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AWSAccessKey, cfg.AWSSecretKey, "")),
			)
		} else {
			awsCfg, err = awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWSRegion))
		}
		if err != nil {
			return nil, fmt.Errorf("load bedrock aws config: %w", err)
		}

		if client != nil {
			awsCfg.HTTPClient = client
		}

		bedrockClient := bedrockruntime.NewFromConfig(awsCfg)
		return bedrock.Adapter{Client: bedrockClient}, nil
	default:
		return nil, fmt.Errorf("unsupported provider type %q", providerType)
	}
}

// ProviderNames returns enabled provider names in deterministic display order.
func (r *Registry) ProviderNames() []string {
	if r == nil {
		return nil
	}
	names := slices.Clone(r.order)
	slices.Sort(names)
	return names
}

func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.providers)
}

// DefaultModelID returns the first model of the first enabled provider
// in config order.
func (r *Registry) DefaultModelID() ModelID {
	if r == nil {
		return ModelID{}
	}
	for _, providerName := range r.order {
		provider := r.providers[providerName]
		if provider != nil && len(provider.Models) > 0 {
			return ModelID{
				Provider: provider.Name,
				Model:    provider.Models[0].Name,
			}
		}
	}
	return ModelID{}
}

// Resolve returns the Provider for a given ModelID, validating that both
// provider and model are configured and enabled.
func (r *Registry) Resolve(id ModelID) (*Provider, error) {
	if r == nil {
		return nil, fmt.Errorf("provider registry is not initialized")
	}
	provider, ok := r.providers[id.Provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", id.Provider)
	}
	for _, m := range provider.Models {
		if m.Name == id.Model {
			return provider, nil
		}
	}
	return nil, fmt.Errorf("model %q not configured for provider %q", id.Model, id.Provider)
}

// CreateChatCompletionStream opens a streaming chat completion response for
// the selected provider and model.
func (r *Registry) CreateChatCompletionStream(ctx context.Context, id ModelID, request *ChatCompletionStreamRequest) (ChatCompletionStream, error) {
	provider, err := r.Resolve(id)
	if err != nil {
		return nil, err
	}
	return provider.adapter.CreateChatCompletionStream(ctx, platforms.Endpoint{
		APIKey:  provider.APIKey,
		BaseURL: provider.BaseURL,
	}, request)
}

// AllModelIDs returns all configured model IDs from enabled providers
// in config order.
func (r *Registry) AllModelIDs() []ModelID {
	if r == nil {
		return nil
	}

	var ids []ModelID
	for _, providerName := range r.order {
		provider := r.providers[providerName]
		if provider == nil {
			continue
		}
		for _, model := range provider.Models {
			ids = append(ids, ModelID{Provider: provider.Name, Model: model.Name})
		}
	}
	return ids
}

// LookupModelConfig returns the configured pricing for a given ModelID,
// or nil if not found.
func (r *Registry) LookupModelConfig(id ModelID) *config.ModelConfig {
	provider, err := r.Resolve(id)
	if err != nil {
		return nil
	}
	for i := range provider.Models {
		if provider.Models[i].Name == id.Model {
			return &provider.Models[i]
		}
	}
	return nil
}

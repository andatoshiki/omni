package providers

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/providers/platforms"
	anthropicplatform "github.com/andatoshiki/omni/internal/providers/platforms/anthropic"
	customplatform "github.com/andatoshiki/omni/internal/providers/platforms/custom"
	deepseekplatform "github.com/andatoshiki/omni/internal/providers/platforms/deepseek"
	googleplatform "github.com/andatoshiki/omni/internal/providers/platforms/google"
	openaiplatform "github.com/andatoshiki/omni/internal/providers/platforms/openai"
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
	config.ProviderTypeGoogle:    "https://generativelanguage.googleapis.com/v1beta/openai/",
	config.ProviderTypeAnthropic: "https://api.anthropic.com",
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
		adapter, err := adapterForType(providerType, cfg.Timeout)
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

func adapterForType(providerType string, timeout *time.Duration) (Adapter, error) {
	var client *http.Client
	if timeout != nil {
		client = &http.Client{Timeout: *timeout}
	}
	switch providerType {
	case config.ProviderTypeDeepSeek:
		return deepseekplatform.Adapter{OpenAI: openaiplatform.Adapter{HTTPClient: client}}, nil
	case config.ProviderTypeOpenAI:
		return openaiplatform.Adapter{HTTPClient: client}, nil
	case config.ProviderTypeCustom:
		return customplatform.Adapter{OpenAI: openaiplatform.Adapter{HTTPClient: client}}, nil
	case config.ProviderTypeGoogle:
		return googleplatform.Adapter{Timeout: timeout}, nil
	case config.ProviderTypeAnthropic:
		return anthropicplatform.Adapter{HTTPClient: client}, nil
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

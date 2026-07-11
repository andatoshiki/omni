package providers

import (
	"testing"

	"github.com/andatoshiki/omni/internal/config"
	anthropicplatform "github.com/andatoshiki/omni/internal/providers/platforms/anthropic"
	cohereplatform "github.com/andatoshiki/omni/internal/providers/platforms/cohere"
	customplatform "github.com/andatoshiki/omni/internal/providers/platforms/custom"
	openaiplatform "github.com/andatoshiki/omni/internal/providers/platforms/openai"
)

func TestRegistrySupportsMultipleCustomProviders(t *testing.T) {
	registry, err := NewRegistry([]config.ProviderConfig{
		{
			Name: "local-ai", Type: config.ProviderTypeCustom,
			APIKey: "local-key", APIBase: "http://localhost:8080/v1",
			Models: []config.ModelConfig{{Name: "local-model"}},
		},
		{
			Name: "hosted-ai", Type: config.ProviderTypeCustom,
			APIKey: "hosted-key",
			Models: []config.ModelConfig{{Name: "hosted-model"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if registry.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", registry.Len())
	}

	local, err := registry.Resolve(ModelID{Provider: "local-ai", Model: "local-model"})
	if err != nil {
		t.Fatal(err)
	}
	if local.Type != config.ProviderTypeCustom || local.BaseURL != "http://localhost:8080/v1" {
		t.Fatalf("local provider = %#v", local)
	}

	hosted, err := registry.Resolve(ModelID{Provider: "hosted-ai", Model: "hosted-model"})
	if err != nil {
		t.Fatal(err)
	}
	if hosted.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("custom default BaseURL = %q", hosted.BaseURL)
	}
	if _, ok := hosted.adapter.(customplatform.Adapter); !ok {
		t.Fatalf("custom adapter = %T, want custom.Adapter", hosted.adapter)
	}
}

func TestRegistrySupportsAnthropic(t *testing.T) {
	registry, err := NewRegistry([]config.ProviderConfig{{
		Name: "anthropic", APIKey: "sk-ant-test",
		Models: []config.ModelConfig{{Name: "claude-test"}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	provider, err := registry.Resolve(ModelID{Provider: "anthropic", Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Type != config.ProviderTypeAnthropic {
		t.Fatalf("provider type = %q, want anthropic", provider.Type)
	}
	if provider.BaseURL != "https://api.anthropic.com" {
		t.Fatalf("provider BaseURL = %q, want Anthropic default", provider.BaseURL)
	}
	if _, ok := provider.adapter.(anthropicplatform.Adapter); !ok {
		t.Fatalf("provider adapter = %T, want anthropic.Adapter", provider.adapter)
	}
}

func TestRegistryPreservesConfigOrderForDefaultModel(t *testing.T) {
	registry, err := NewRegistry([]config.ProviderConfig{
		{Name: "first", Type: "custom", APIKey: "one", Models: []config.ModelConfig{{Name: "a"}}},
		{Name: "second", Type: "custom", APIKey: "two", Models: []config.ModelConfig{{Name: "b"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.DefaultModelID(); got != (ModelID{Provider: "first", Model: "a"}) {
		t.Fatalf("DefaultModelID() = %#v", got)
	}
}

func TestRegistrySupportsCohereAndHuggingFace(t *testing.T) {
	registry, err := NewRegistry([]config.ProviderConfig{
		{Name: "cohere-main", Type: config.ProviderTypeCohere, APIKey: "cohere-key", Models: []config.ModelConfig{{Name: "command-r"}}},
		{Name: "hf-main", Type: config.ProviderTypeHuggingFace, APIKey: "hf-key", Models: []config.ModelConfig{{Name: "openai/gpt-oss-20b"}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	cohereProvider, err := registry.Resolve(ModelID{Provider: "cohere-main", Model: "command-r"})
	if err != nil {
		t.Fatal(err)
	}
	if cohereProvider.BaseURL != "https://api.cohere.com" {
		t.Fatalf("cohere BaseURL = %q, want https://api.cohere.com", cohereProvider.BaseURL)
	}
	if _, ok := cohereProvider.adapter.(cohereplatform.Adapter); !ok {
		t.Fatalf("cohere adapter = %T, want cohere.Adapter", cohereProvider.adapter)
	}

	huggingFaceProvider, err := registry.Resolve(ModelID{Provider: "hf-main", Model: "openai/gpt-oss-20b"})
	if err != nil {
		t.Fatal(err)
	}
	if huggingFaceProvider.BaseURL != "https://api-inference.huggingface.co/v1" {
		t.Fatalf("huggingface BaseURL = %q, want Hugging Face default", huggingFaceProvider.BaseURL)
	}
	if _, ok := huggingFaceProvider.adapter.(openaiplatform.Adapter); !ok {
		t.Fatalf("huggingface adapter = %T, want openai.Adapter", huggingFaceProvider.adapter)
	}
}

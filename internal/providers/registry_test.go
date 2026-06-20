package providers

import (
	"testing"

	"github.com/andatoshiki/omni/internal/config"
	customplatform "github.com/andatoshiki/omni/internal/providers/platforms/custom"
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

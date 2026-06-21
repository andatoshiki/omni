package bot

import (
	"fmt"
	"testing"

	"github.com/andatoshiki/omni/internal/providers"
)

func TestProviderSelectionViewPaginates(t *testing.T) {
	allModels := make([]providers.ModelID, 0, MaxItemsPerPage+1)
	for i := 0; i < MaxItemsPerPage+1; i++ {
		allModels = append(allModels, providers.ModelID{Provider: fmt.Sprintf("provider-%02d", i), Model: "model"})
	}

	text, keyboard := providerSelectionView(allModels, providers.ModelID{Provider: "provider-00", Model: "model"}, 0)
	if text != "🤖 Select a provider:" {
		t.Fatalf("text = %q", text)
	}
	if got := len(keyboard.InlineKeyboard); got != MaxItemsPerPage+1 {
		t.Fatalf("row count = %d, want %d", got, MaxItemsPerPage+1)
	}
	if got := keyboard.InlineKeyboard[0][0].Text; got != "✅ provider-00" {
		t.Fatalf("current provider label = %q", got)
	}
	navigation := keyboard.InlineKeyboard[len(keyboard.InlineKeyboard)-1]
	if len(navigation) != 1 || navigation[0].CallbackData != "p:1" {
		t.Fatalf("navigation = %#v", navigation)
	}
}

func TestModelSelectionViewPaginatesAndNavigates(t *testing.T) {
	allModels := make([]providers.ModelID, 0, MaxItemsPerPage+2)
	for i := 0; i < MaxItemsPerPage+1; i++ {
		allModels = append(allModels, providers.ModelID{Provider: "openrouter", Model: fmt.Sprintf("model-%02d", i)})
	}
	allModels = append(allModels, providers.ModelID{Provider: "other", Model: "ignored"})

	current := providers.ModelID{Provider: "openrouter", Model: "model-10"}
	text, keyboard, ok := modelSelectionView(allModels, current, "openrouter", 1)
	if !ok {
		t.Fatal("modelSelectionView() rejected a configured provider")
	}
	if text != "🤖 Select a model from openrouter:" {
		t.Fatalf("text = %q", text)
	}
	if got := keyboard.InlineKeyboard[0][0].Text; got != "✅ model-10" {
		t.Fatalf("current model label = %q", got)
	}
	if got := keyboard.InlineKeyboard[0][0].CallbackData; got != "m:openrouter:model-10" {
		t.Fatalf("model callback = %q", got)
	}
	navigation := keyboard.InlineKeyboard[1]
	if len(navigation) != 1 || navigation[0].CallbackData != "l:openrouter:0" {
		t.Fatalf("navigation = %#v", navigation)
	}
	back := keyboard.InlineKeyboard[2][0]
	if back.CallbackData != "p:0" {
		t.Fatalf("back callback = %q", back.CallbackData)
	}
}

func TestModelSelectionViewClampsStalePage(t *testing.T) {
	allModels := []providers.ModelID{{Provider: "openai", Model: "gpt"}}
	_, keyboard, ok := modelSelectionView(allModels, providers.ModelID{}, "openai", 99)
	if !ok {
		t.Fatal("modelSelectionView() rejected a configured provider")
	}
	if got := keyboard.InlineKeyboard[0][0].CallbackData; got != "m:openai:gpt" {
		t.Fatalf("model callback = %q", got)
	}
}

func TestModelSelectionViewRejectsUnknownProvider(t *testing.T) {
	_, _, ok := modelSelectionView([]providers.ModelID{{Provider: "openai", Model: "gpt"}}, providers.ModelID{}, "missing", 0)
	if ok {
		t.Fatal("modelSelectionView() accepted an unknown provider")
	}
}

func TestConfirmationKeyboardIsMinimal(t *testing.T) {
	keyboard := confirmationKeyboard("anthropic")
	if got := len(keyboard.InlineKeyboard); got != 2 {
		t.Fatalf("row count = %d, want 2", got)
	}
	if got := keyboard.InlineKeyboard[0][0].CallbackData; got != "l:anthropic:0" {
		t.Fatalf("back-to-models callback = %q", got)
	}
	if got := keyboard.InlineKeyboard[1][0].CallbackData; got != "p:0" {
		t.Fatalf("back-to-providers callback = %q", got)
	}
}

package bot

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/andatoshiki/omni/internal/providers"
)

func TestCountTokensUsesCL100KBase(t *testing.T) {
	got, err := countTokens("hello world")
	if err != nil {
		t.Skipf("cl100k_base data is unavailable offline: %v", err)
	}
	if got != 2 {
		t.Fatalf("countTokens() = %d, want 2", got)
	}
}

func TestMessagesWithinContextKeepsNewestHistoryThatFits(t *testing.T) {
	system := providers.ChatMessage{Role: providers.RoleSystem, Content: "system"}
	history := []providers.ChatMessage{
		{Role: providers.RoleUser, Content: strings.Repeat("old ", 20)},
		{Role: providers.RoleAssistant, Content: strings.Repeat("middle ", 20)},
		{Role: providers.RoleUser, Content: "newest"},
	}
	current := []providers.ChatMessage{{Role: providers.RoleUser, Content: "now"}}

	mandatoryTokens := chatReplyPrimingTokens
	for _, message := range append([]providers.ChatMessage{system}, current...) {
		count, err := countChatMessageTokensWith(message, runeTokenCounter)
		if err != nil {
			t.Fatal(err)
		}
		mandatoryTokens += count
	}
	newestTokens, err := countChatMessageTokensWith(history[2], runeTokenCounter)
	if err != nil {
		t.Fatal(err)
	}

	const replyTokens = 10
	messages, promptTokens, dropped, err := messagesWithinContextWithCounter(
		system,
		history,
		current,
		replyTokens+mandatoryTokens+newestTokens,
		replyTokens,
		runeTokenCounter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 2 {
		t.Fatalf("dropped = %d, want 2", dropped)
	}
	if len(messages) != 3 || messages[1].Content != "newest" {
		t.Fatalf("messages = %#v, want system + newest history + current", messages)
	}
	if promptTokens != mandatoryTokens+newestTokens {
		t.Fatalf("promptTokens = %d, want %d", promptTokens, mandatoryTokens+newestTokens)
	}
}

func TestMessagesWithinContextRejectsOversizedCurrentPrompt(t *testing.T) {
	_, _, _, err := messagesWithinContextWithCounter(
		providers.ChatMessage{Role: providers.RoleSystem, Content: strings.Repeat("system ", 20)},
		nil,
		[]providers.ChatMessage{{Role: providers.RoleUser, Content: strings.Repeat("prompt ", 20)}},
		20,
		10,
		runeTokenCounter,
	)
	if err == nil || !strings.Contains(err.Error(), "exceeding") {
		t.Fatalf("error = %v, want oversized prompt error", err)
	}
}

func TestCountContentTokensIgnoresImageData(t *testing.T) {
	short, err := countContentTokensWith([]providers.ChatContentPart{
		{Type: "text", Text: "describe this"},
		{Type: "image_url", ImageURL: &providers.ChatImageURL{URL: "data:image/jpeg;base64,AA=="}},
	}, runeTokenCounter)
	if err != nil {
		t.Fatal(err)
	}
	long, err := countContentTokensWith([]providers.ChatContentPart{
		{Type: "text", Text: "describe this"},
		{Type: "image_url", ImageURL: &providers.ChatImageURL{URL: "data:image/jpeg;base64," + strings.Repeat("A", 10000)}},
	}, runeTokenCounter)
	if err != nil {
		t.Fatal(err)
	}
	if short != long {
		t.Fatalf("image bytes affected text token count: short=%d long=%d", short, long)
	}
}

func runeTokenCounter(text string) (int, error) {
	return utf8.RuneCountInString(text), nil
}

package conversation

import (
	"testing"

	"github.com/andatoshiki/omni/internal/providers"
)

func TestRender(t *testing.T) {
	msg1 := Message{
		Role:    providers.RoleUser,
		Content: "Hello",
		Speaker: &Speaker{DisplayName: "Alice"},
	}

	msg2 := Message{
		Role:    providers.RoleUser,
		Content: "Hello again",
	}

	msg3 := Message{
		Role:    providers.RoleUser,
		Content: []providers.ChatContentPart{
			{Type: "image_url"},
		},
		Speaker: &Speaker{DisplayName: "Bob"},
		ReplyTo: &ReplyContext{Speaker: &Speaker{DisplayName: "Alice"}, Text: "Hello"},
	}

	msg4 := Message{
		Role:    providers.RoleUser,
		Content: []providers.ChatContentPart{
			{Type: "image_url"},
			{Type: "text", Text: "Look at this"},
		},
		Speaker: &Speaker{DisplayName: "Bob"},
	}

	messages := []Message{msg1, msg2, msg3, msg4}

	// Test without identity
	res1 := Render(messages, false)
	if len(res1) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(res1))
	}
	if res1[0].Content != "Hello" {
		t.Errorf("expected plain content when identity is off")
	}

	// Test with identity
	res2 := Render(messages, true)

	if res2[0].Content != "[telegram speaker: Alice]\n\nHello" {
		t.Errorf("unexpected msg1 content: %v", res2[0].Content)
	}

	if res2[1].Content != "Hello again" {
		t.Errorf("expected no speaker label for msg2 without speaker, got: %v", res2[1].Content)
	}

	parts3, ok := res2[2].Content.([]providers.ChatContentPart)
	if !ok || len(parts3) != 2 || parts3[0].Type != "text" || parts3[0].Text != "[replying to Alice]\nHello\n\n[telegram speaker: Bob]" {
		t.Errorf("unexpected msg3 content: %v", res2[2].Content)
	}

	parts4, ok := res2[3].Content.([]providers.ChatContentPart)
	if !ok || len(parts4) != 2 || parts4[0].Type != "image_url" || parts4[1].Type != "text" || parts4[1].Text != "[telegram speaker: Bob]\n\nLook at this" {
		t.Errorf("unexpected msg4 content: %v", res2[3].Content)
	}
}

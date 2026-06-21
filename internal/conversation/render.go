package conversation

import (
	"fmt"
	"strings"

	"github.com/andatoshiki/omni/internal/providers"
)

const SystemInstruction = "You are in a Telegram group chat. User messages are prefixed with `[telegram speaker: Name]` to indicate the sender. Do NOT use this prefix or any similar formatting in your own responses."

// Render converts internal conversation messages into provider-neutral chat messages.
// If includeIdentity is true, it injects speaker labels into user messages.
func Render(messages []Message, includeIdentity bool) []providers.ChatMessage {
	var result []providers.ChatMessage

	for _, msg := range messages {
		providerMsg := providers.ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}

		if includeIdentity && msg.Role == providers.RoleUser && msg.Speaker != nil {
			var label string
			if msg.Speaker.DisplayName != "" {
				label = fmt.Sprintf("[telegram speaker: %s]\n\n", msg.Speaker.DisplayName)
			} else {
				label = "[telegram speaker: unknown participant]\n\n"
			}

			if msg.ReplyTo != nil {
				replyLabel := "[replying to unknown]\n"
				if msg.ReplyTo.Speaker != nil && msg.ReplyTo.Speaker.DisplayName != "" {
					replyLabel = fmt.Sprintf("[replying to %s]\n", msg.ReplyTo.Speaker.DisplayName)
				}
				if msg.ReplyTo.Text != "" {
					replyLabel += msg.ReplyTo.Text + "\n\n"
				}
				label = replyLabel + label
			}

			if strContent, ok := msg.Content.(string); ok {
				providerMsg.Content = label + strContent
			} else if parts, ok := msg.Content.([]providers.ChatContentPart); ok && len(parts) > 0 {
				newParts := make([]providers.ChatContentPart, 0, len(parts)+1)
				hasText := false
				for i, part := range parts {
					if part.Type == "text" {
						newPart := part
						newPart.Text = label + newPart.Text
						newParts = append(newParts, newPart)
						newParts = append(newParts, parts[i+1:]...)
						hasText = true
						break
					}
					newParts = append(newParts, part)
				}
				if !hasText {
					newParts = append([]providers.ChatContentPart{{Type: "text", Text: strings.TrimSpace(label)}}, parts...)
				}
				providerMsg.Content = newParts
			}
		}

		result = append(result, providerMsg)
	}

	return result
}

package providers

import (
	"strings"

	"github.com/andatoshiki/omni/internal/providers/platforms"
)

type ChatMessage = platforms.ChatMessage
type ChatContentPart = platforms.ChatContentPart
type ChatImageURL = platforms.ChatImageURL
type ChatCompletionStreamRequest = platforms.ChatCompletionStreamRequest
type ChatCompletionStream = platforms.ChatCompletionStream
type TokenUsage = platforms.TokenUsage

const (
	RoleSystem    = platforms.RoleSystem
	RoleUser      = platforms.RoleUser
	RoleAssistant = platforms.RoleAssistant
)

// ModelID uniquely identifies a model because provider names are user-defined.
type ModelID struct {
	Provider string
	Model    string
}

func (m ModelID) String() string {
	return m.Provider + " / " + m.Model
}

func (m ModelID) CallbackData() string {
	return "m:" + m.Provider + ":" + m.Model
}

func ParseModelCallback(data string) (ModelID, bool) {
	if !strings.HasPrefix(data, "m:") {
		return ModelID{}, false
	}
	parts := strings.SplitN(data[2:], ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ModelID{}, false
	}
	return ModelID{Provider: parts[0], Model: parts[1]}, true
}

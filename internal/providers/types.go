package providers

import (
	"strconv"
	"strings"

	"github.com/andatoshiki/omni/internal/providers/platforms"
)

type ChatMessage = platforms.ChatMessage
type ChatContentPart = platforms.ChatContentPart
type ChatImageURL = platforms.ChatImageURL
type MediaData = platforms.MediaData
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

func ProviderPageCallbackData(page int) string {
	return "p:" + strconv.Itoa(page)
}

func ModelListCallbackData(provider string, page int) string {
	return "l:" + provider + ":" + strconv.Itoa(page)
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

func ParseProviderPageCallback(data string) (int, bool) {
	if !strings.HasPrefix(data, "p:") {
		return 0, false
	}
	return parseCallbackPage(data[2:])
}

func ParseModelListCallback(data string) (string, int, bool) {
	if !strings.HasPrefix(data, "l:") {
		return "", 0, false
	}
	payload := data[2:]
	separator := strings.LastIndexByte(payload, ':')
	if separator <= 0 {
		return "", 0, false
	}
	page, ok := parseCallbackPage(payload[separator+1:])
	if !ok {
		return "", 0, false
	}
	return payload[:separator], page, true
}

func parseCallbackPage(value string) (int, bool) {
	page, err := strconv.Atoi(value)
	if err != nil || page < 0 {
		return 0, false
	}
	return page, true
}

// Package openai implements the OpenAI chat-completions protocol shared by
// OpenAI itself and API-compatible provider platforms.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/andatoshiki/omni/internal/providers/platforms"
)

var defaultHTTPClient = &http.Client{Timeout: 5 * time.Minute}

type Adapter struct {
	HTTPClient *http.Client
	Dialect    ThinkingDialect
}

type ThinkingDialect string

const (
	ThinkingDialectCompatible ThinkingDialect = "compatible"
	ThinkingDialectOpenAI     ThinkingDialect = "openai"
	ThinkingDialectDeepSeek   ThinkingDialect = "deepseek"
)

type chatCompletionRequest struct {
	Model               string                         `json:"model"`
	Messages            []platforms.ChatMessage        `json:"messages"`
	Temperature         *float32                       `json:"temperature,omitempty"`
	MaxTokens           int                            `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                            `json:"max_completion_tokens,omitempty"`
	Stream              bool                           `json:"stream"`
	StreamOptions       platforms.StreamOptions        `json:"stream_options"`
	ReasoningEffort     string                         `json:"reasoning_effort,omitempty"`
	Thinking            *deepSeekThinkingConfiguration `json:"thinking,omitempty"`
}

type deepSeekThinkingConfiguration struct {
	Type string `json:"type"`
}

func (a Adapter) CreateChatCompletionStream(
	ctx context.Context,
	endpoint platforms.Endpoint,
	request *platforms.ChatCompletionStreamRequest,
) (platforms.ChatCompletionStream, error) {
	if endpoint.BaseURL == "" {
		return nil, errors.New("provider base URL is not configured")
	}
	if endpoint.APIKey == "" {
		return nil, errors.New("provider auth token is not configured")
	}
	if request == nil {
		return nil, errors.New("request cannot be nil")
	}

	if unsupported := unsupportedMediaTypes(request.Messages); len(unsupported) > 0 {
		return nil, &platforms.UnsupportedMediaError{Types: unsupported}
	}

	body, err := EncodeChatCompletionRequest(request, a.Dialect)
	if err != nil {
		return nil, fmt.Errorf("encode chat completion request: %w", err)
	}

	url := strings.TrimRight(endpoint.BaseURL, "/") + "/chat/completions"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build chat completion request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")

	client := a.HTTPClient
	if client == nil {
		client = defaultHTTPClient
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("send chat completion request: %w", err)
	}
	if response.StatusCode >= http.StatusBadRequest {
		defer response.Body.Close()
		return nil, platforms.ReadAPIError(response)
	}

	return platforms.NewChatCompletionStream(response), nil
}

// EncodeChatCompletionRequest translates the provider-neutral request to an
// OpenAI-compatible wire payload without exposing the transport DTO.
func EncodeChatCompletionRequest(request *platforms.ChatCompletionStreamRequest, dialect ThinkingDialect) ([]byte, error) {
	if request == nil {
		return nil, errors.New("request cannot be nil")
	}
	return json.Marshal((Adapter{Dialect: dialect}).translateRequest(request))
}

func (a Adapter) translateRequest(request *platforms.ChatCompletionStreamRequest) chatCompletionRequest {
	temperature := request.Temperature
	wire := chatCompletionRequest{
		Model:         request.Model,
		Messages:      request.Messages,
		Temperature:   &temperature,
		MaxTokens:     request.MaxTokens,
		Stream:        true,
		StreamOptions: platforms.StreamOptions{IncludeUsage: true},
	}
	if request.Thinking == nil {
		return wire
	}

	mode := strings.ToLower(strings.TrimSpace(request.Thinking.Mode))
	effort := strings.ToLower(strings.TrimSpace(request.Thinking.Effort))
	if mode == "" {
		mode = "auto"
	}
	dialect := a.Dialect
	if dialect == "" {
		dialect = ThinkingDialectCompatible
	}

	switch dialect {
	case ThinkingDialectDeepSeek:
		if mode == "enabled" || mode == "disabled" {
			wire.Thinking = &deepSeekThinkingConfiguration{Type: mode}
		}
		wire.ReasoningEffort = deepSeekReasoningEffort(effort)
	case ThinkingDialectOpenAI, ThinkingDialectCompatible:
		if mode == "disabled" {
			wire.ReasoningEffort = "none"
		} else {
			wire.ReasoningEffort = effort
		}
	}

	active := mode == "enabled" || (wire.ReasoningEffort != "" && wire.ReasoningEffort != "none")
	if active {
		wire.Temperature = nil
	}
	if dialect == ThinkingDialectOpenAI && active {
		wire.MaxCompletionTokens = wire.MaxTokens
		wire.MaxTokens = 0
	}
	return wire
}

func deepSeekReasoningEffort(effort string) string {
	switch effort {
	case "minimal", "low":
		return "low"
	case "medium", "high", "xhigh":
		return "high"
	case "max":
		return "max"
	case "none":
		return "none"
	default:
		return ""
	}
}

func unsupportedMediaTypes(messages []platforms.ChatMessage) []string {
	types := make(map[string]struct{})
	for _, message := range messages {
		parts, ok := message.Content.([]platforms.ChatContentPart)
		if !ok {
			continue
		}
		for _, part := range parts {
			if part.Type == "text" || part.Type == "image_url" {
				continue
			}
			mediaType := strings.TrimSpace(part.Type)
			if mediaType == "" {
				mediaType = "unknown"
			}
			types[mediaType] = struct{}{}
		}
	}

	result := make([]string, 0, len(types))
	for mediaType := range types {
		result = append(result, mediaType)
	}
	slices.Sort(result)
	return result
}

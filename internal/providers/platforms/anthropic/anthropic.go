// Package anthropic implements Anthropic's native Messages API.
package anthropic

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/andatoshiki/omni/internal/providers/platforms"
)

const maxBase64ImageBytes = 10 << 20

var defaultHTTPClient = &http.Client{Timeout: 10 * time.Minute}

type Adapter struct {
	HTTPClient *http.Client
}

func (a Adapter) CreateChatCompletionStream(
	ctx context.Context,
	endpoint platforms.Endpoint,
	request *platforms.ChatCompletionStreamRequest,
) (platforms.ChatCompletionStream, error) {
	if strings.TrimSpace(endpoint.BaseURL) == "" {
		return nil, errors.New("provider base URL is not configured")
	}
	if strings.TrimSpace(endpoint.APIKey) == "" {
		return nil, errors.New("provider auth token is not configured")
	}
	if request == nil {
		return nil, errors.New("request cannot be nil")
	}

	params, err := buildMessageParams(request)
	if err != nil {
		return nil, err
	}

	httpClient := a.HTTPClient
	if httpClient == nil {
		httpClient = defaultHTTPClient
	}
	client := anthropicsdk.NewClient(
		option.WithAPIKey(strings.TrimSpace(endpoint.APIKey)),
		option.WithBaseURL(strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")),
		option.WithHTTPClient(httpClient),
	)
	stream := client.Messages.NewStreaming(ctx, params)
	if err := stream.Err(); err != nil {
		_ = stream.Close()
		return nil, wrapAnthropicError("start Anthropic message stream", err)
	}

	return &messageStream{stream: stream}, nil
}

func buildMessageParams(request *platforms.ChatCompletionStreamRequest) (anthropicsdk.MessageNewParams, error) {
	if strings.TrimSpace(request.Model) == "" {
		return anthropicsdk.MessageNewParams{}, errors.New("model is not configured")
	}
	if request.MaxTokens <= 0 {
		return anthropicsdk.MessageNewParams{}, errors.New("max tokens must be greater than 0")
	}
	if request.Temperature < 0 || request.Temperature > 1 {
		return anthropicsdk.MessageNewParams{}, errors.New("temperature must be between 0 and 1 for Anthropic")
	}

	var (
		messages         []anthropicsdk.MessageParam
		system           []anthropicsdk.TextBlockParam
		unsupportedTypes = make(map[string]struct{})
	)
	for index, message := range request.Messages {
		if message.Role == platforms.RoleSystem {
			blocks, unsupported, err := systemBlocks(message.Content)
			if err != nil {
				return anthropicsdk.MessageNewParams{}, fmt.Errorf("translate system message %d: %w", index, err)
			}
			system = append(system, blocks...)
			addUnsupportedTypes(unsupportedTypes, unsupported)
			continue
		}

		blocks, unsupported, err := contentBlocks(message.Content)
		if err != nil {
			return anthropicsdk.MessageNewParams{}, fmt.Errorf("translate message %d: %w", index, err)
		}
		addUnsupportedTypes(unsupportedTypes, unsupported)
		if len(blocks) == 0 {
			if len(unsupported) > 0 {
				continue
			}
			return anthropicsdk.MessageNewParams{}, fmt.Errorf("translate message %d: content cannot be empty", index)
		}

		switch message.Role {
		case platforms.RoleUser:
			messages = append(messages, anthropicsdk.NewUserMessage(blocks...))
		case platforms.RoleAssistant:
			messages = append(messages, anthropicsdk.NewAssistantMessage(blocks...))
		default:
			return anthropicsdk.MessageNewParams{}, fmt.Errorf("translate message %d: unsupported role %q", index, message.Role)
		}
	}

	if len(unsupportedTypes) > 0 {
		return anthropicsdk.MessageNewParams{}, &platforms.UnsupportedMediaError{Types: sortedTypes(unsupportedTypes)}
	}
	if len(messages) == 0 {
		return anthropicsdk.MessageNewParams{}, errors.New("at least one user or assistant message is required")
	}

	return anthropicsdk.MessageNewParams{
		MaxTokens:   int64(request.MaxTokens),
		Messages:    messages,
		Model:       anthropicsdk.Model(strings.TrimSpace(request.Model)),
		System:      system,
		Temperature: anthropicsdk.Float(float64(request.Temperature)),
	}, nil
}

func systemBlocks(content any) ([]anthropicsdk.TextBlockParam, []string, error) {
	switch value := content.(type) {
	case string:
		if value == "" {
			return nil, nil, nil
		}
		return []anthropicsdk.TextBlockParam{{Text: value}}, nil, nil
	case []platforms.ChatContentPart:
		var (
			blocks      []anthropicsdk.TextBlockParam
			unsupported []string
		)
		for _, part := range value {
			if part.Type == "text" {
				if part.Text != "" {
					blocks = append(blocks, anthropicsdk.TextBlockParam{Text: part.Text})
				}
				continue
			}
			unsupported = append(unsupported, mediaType(part))
		}
		return blocks, unsupported, nil
	default:
		return nil, nil, fmt.Errorf("unsupported content type %T", content)
	}
}

func contentBlocks(content any) ([]anthropicsdk.ContentBlockParamUnion, []string, error) {
	switch value := content.(type) {
	case string:
		if value == "" {
			return nil, nil, nil
		}
		return []anthropicsdk.ContentBlockParamUnion{anthropicsdk.NewTextBlock(value)}, nil, nil
	case []platforms.ChatContentPart:
		var (
			blocks      []anthropicsdk.ContentBlockParamUnion
			unsupported []string
		)
		for _, part := range value {
			switch part.Type {
			case "text":
				if part.Text != "" {
					blocks = append(blocks, anthropicsdk.NewTextBlock(part.Text))
				}
			case "image_url":
				if part.ImageURL == nil {
					return nil, nil, errors.New("image_url content is missing its URL")
				}
				mediaType, encodedData, err := decodeImageDataURI(part.ImageURL.URL)
				if err != nil {
					return nil, nil, err
				}
				blocks = append(blocks, anthropicsdk.NewImageBlockBase64(mediaType, encodedData))
			default:
				unsupported = append(unsupported, mediaType(part))
			}
		}
		return blocks, unsupported, nil
	default:
		return nil, nil, fmt.Errorf("unsupported content type %T", content)
	}
}

func decodeImageDataURI(uri string) (string, string, error) {
	metadata, encodedData, found := strings.Cut(strings.TrimSpace(uri), ",")
	if !found || !strings.HasPrefix(metadata, "data:") {
		return "", "", errors.New("anthropic images must use a base64 data URI")
	}
	parts := strings.Split(strings.TrimPrefix(metadata, "data:"), ";")
	if len(parts) != 2 || parts[1] != "base64" {
		return "", "", errors.New("anthropic images must use base64 encoding")
	}
	mediaType := strings.ToLower(strings.TrimSpace(parts[0]))
	switch mediaType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		return "", "", fmt.Errorf("anthropic does not support image MIME type %q", mediaType)
	}
	if len(encodedData) > maxBase64ImageBytes {
		return "", "", fmt.Errorf("anthropic base64 image exceeds the 10 MB limit")
	}
	if _, err := base64.StdEncoding.DecodeString(encodedData); err != nil {
		return "", "", fmt.Errorf("decode Anthropic image data: %w", err)
	}
	return mediaType, encodedData, nil
}

func mediaType(part platforms.ChatContentPart) string {
	mediaType := strings.TrimSpace(part.Type)
	if mediaType == "" && part.MediaData != nil {
		mediaType = strings.TrimSpace(part.MediaData.MIMEType)
	}
	if mediaType == "" {
		return "unknown"
	}
	return mediaType
}

func addUnsupportedTypes(destination map[string]struct{}, types []string) {
	for _, mediaType := range types {
		destination[mediaType] = struct{}{}
	}
}

func sortedTypes(types map[string]struct{}) []string {
	result := make([]string, 0, len(types))
	for mediaType := range types {
		result = append(result, mediaType)
	}
	slices.Sort(result)
	return result
}

type messageStream struct {
	stream       *ssestream.Stream[anthropicsdk.MessageStreamEventUnion]
	promptTokens int64
}

func (s *messageStream) Recv() (*platforms.ChatCompletionStreamResponse, error) {
	for s.stream.Next() {
		event := s.stream.Current()
		switch event.Type {
		case "message_start":
			s.promptTokens = inputTokens(event.Message.Usage.InputTokens, event.Message.Usage.CacheCreationInputTokens, event.Message.Usage.CacheReadInputTokens)
		case "content_block_delta":
			if event.Delta.Type != "text_delta" || event.Delta.Text == "" {
				continue
			}
			return &platforms.ChatCompletionStreamResponse{
				Choices: []platforms.StreamChoice{{Delta: platforms.StreamDelta{Content: event.Delta.Text}}},
			}, nil
		case "message_delta":
			promptTokens := inputTokens(event.Usage.InputTokens, event.Usage.CacheCreationInputTokens, event.Usage.CacheReadInputTokens)
			if promptTokens == 0 {
				promptTokens = s.promptTokens
			}
			return &platforms.ChatCompletionStreamResponse{Usage: &platforms.TokenUsage{
				PromptTokens:     promptTokens,
				CompletionTokens: event.Usage.OutputTokens,
				TotalTokens:      promptTokens + event.Usage.OutputTokens,
			}}, nil
		case "message_stop":
			return nil, io.EOF
		}
	}
	if err := s.stream.Err(); err != nil {
		return nil, wrapAnthropicError("read Anthropic message stream", err)
	}
	return nil, io.EOF
}

func (s *messageStream) Close() error {
	if s == nil || s.stream == nil {
		return nil
	}
	return s.stream.Close()
}

func inputTokens(input, cacheCreation, cacheRead int64) int64 {
	return input + cacheCreation + cacheRead
}

func wrapAnthropicError(operation string, err error) error {
	var apiError *anthropicsdk.Error
	if !errors.As(err, &apiError) {
		return fmt.Errorf("%s: %w", operation, err)
	}

	details := "Anthropic API error"
	if apiError.StatusCode >= http.StatusBadRequest {
		details += fmt.Sprintf(" (HTTP %d)", apiError.StatusCode)
	}
	if errorType := apiError.Type(); errorType != "" {
		details += fmt.Sprintf(" type=%s", errorType)
	}
	if apiError.RequestID != "" {
		details += fmt.Sprintf(" request_id=%s", apiError.RequestID)
	}
	return fmt.Errorf("%s: %s", operation, details)
}

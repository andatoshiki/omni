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

	request.Stream = true
	request.StreamOptions.IncludeUsage = true

	body, err := json.Marshal(request)
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

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

	request.Stream = true
	request.StreamOptions.IncludeUsage = true

	// Sanitize messages for OpenAI compatibility
	for i := range request.Messages {
		if parts, ok := request.Messages[i].Content.([]platforms.ChatContentPart); ok {
			var sanitized []platforms.ChatContentPart
			hasUnsupportedMedia := false
			for _, part := range parts {
				if part.Type == "text" || part.Type == "image_url" {
					sanitized = append(sanitized, part)
				} else {
					hasUnsupportedMedia = true
				}
			}
			if len(sanitized) == 0 && hasUnsupportedMedia {
				sanitized = append(sanitized, platforms.ChatContentPart{
					Type: "text",
					Text: "[User attached an audio/video file that this model cannot process]",
				})
			}
			request.Messages[i].Content = sanitized
		}
	}

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

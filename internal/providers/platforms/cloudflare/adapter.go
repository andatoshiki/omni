package cloudflare

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/andatoshiki/omni/internal/providers/platforms"
)

type Adapter struct {
	AccountID  string
	HTTPClient *http.Client
}

// CloudflareRequest maps to Cloudflare's specific payload format
type CloudflareRequest struct {
	Messages []platforms.ChatMessage `json:"messages"`
	Stream   bool                    `json:"stream"`
}

func (a Adapter) CreateChatCompletionStream(ctx context.Context, endpoint platforms.Endpoint, req *platforms.ChatCompletionStreamRequest) (platforms.ChatCompletionStream, error) {
	baseURL := endpoint.BaseURL
	if baseURL == "" {
		baseURL = "https://api.cloudflare.com/client/v4/accounts/%s/ai/run"
	}
	if strings.Contains(baseURL, "%s") {
		baseURL = fmt.Sprintf(baseURL, a.AccountID)
	}
	baseURL = strings.TrimRight(baseURL, "/")

	// Format: https://api.cloudflare.com/client/v4/accounts/{account_id}/ai/run/{model}
	url := fmt.Sprintf("%s/%s", baseURL, req.Model)

	cfReq := CloudflareRequest{
		Messages: req.Messages,
		Stream:   true,
	}

	payloadBytes, err := json.Marshal(cfReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var errResp struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && len(errResp.Errors) > 0 {
			return nil, fmt.Errorf("cloudflare api error: %s", errResp.Errors[0].Message)
		}
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	return &streamReader{
		body:    resp.Body,
		scanner: bufio.NewScanner(resp.Body),
	}, nil
}

type streamReader struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
}

// CloudflareStreamResponse represents Cloudflare's specific SSE payload: `data: {"response": "..."}`
type CloudflareStreamResponse struct {
	Response string `json:"response"`
}

func (s *streamReader) Recv() (*platforms.ChatCompletionStreamResponse, error) {
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if line == "" || line == "\n" {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return nil, io.EOF
			}

			var cfResp CloudflareStreamResponse
			if err := json.Unmarshal([]byte(data), &cfResp); err != nil {
				return nil, fmt.Errorf("unmarshal stream response: %w", err)
			}

			// Map Cloudflare's response into the standard OpenAI structure for the bot's core logic
			standardResp := platforms.ChatCompletionStreamResponse{
				Choices: []platforms.StreamChoice{
					{
						Delta: platforms.StreamDelta{
							Content: cfResp.Response,
						},
					},
				},
			}

			return &standardResp, nil
		}
	}

	if err := s.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func (s *streamReader) Close() error {
	return s.body.Close()
}

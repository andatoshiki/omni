package azureopenai

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
	APIVersion string
	HTTPClient *http.Client
}

func (a Adapter) CreateChatCompletionStream(ctx context.Context, endpoint platforms.Endpoint, req *platforms.ChatCompletionStreamRequest) (platforms.ChatCompletionStream, error) {
	baseURL := endpoint.BaseURL
	if baseURL != "" && !strings.HasPrefix(baseURL, "http") {
		baseURL = fmt.Sprintf("https://%s.openai.azure.com", baseURL)
	}
	baseURL = strings.TrimRight(baseURL, "/")

	// Format: https://{endpoint}/openai/deployments/{deployment-id}/chat/completions?api-version={api-version}
	url := fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s", baseURL, req.Model, a.APIVersion)

	payloadBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	// Azure uses api-key instead of Authorization: Bearer
	httpReq.Header.Set("api-key", endpoint.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("azure api error: %s", errResp.Error.Message)
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

			var resp platforms.ChatCompletionStreamResponse
			if err := json.Unmarshal([]byte(data), &resp); err != nil {
				return nil, fmt.Errorf("unmarshal stream response: %w", err)
			}
			return &resp, nil
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

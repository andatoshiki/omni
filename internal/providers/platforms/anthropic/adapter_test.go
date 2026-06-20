package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/andatoshiki/omni/internal/providers/platforms"
)

func TestAdapterTranslatesAndStreamsAnthropicMessages(t *testing.T) {
	t.Parallel()

	imageData := base64.StdEncoding.EncodeToString([]byte("test image"))
	var received map[string]any
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != "test-key" {
			t.Errorf("X-Api-Key = %q, want test-key", got)
		}
		if got := r.Header.Get("Anthropic-Version"); got == "" {
			t.Error("Anthropic-Version header is empty")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}

		streamBody := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11,\"cache_creation_input_tokens\":2,\"cache_read_input_tokens\":3,\"output_tokens\":0}}}\n\n" +
			"event: ping\ndata: {\"type\":\"ping\"}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"hidden\"}}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":0,\"cache_creation_input_tokens\":0,\"cache_read_input_tokens\":0,\"output_tokens\":7}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(streamBody)),
			Request:    r,
		}, nil
	})}

	stream, err := (Adapter{HTTPClient: client}).CreateChatCompletionStream(context.Background(), platforms.Endpoint{
		APIKey: "test-key", BaseURL: "https://api.example.test",
	}, &platforms.ChatCompletionStreamRequest{
		Model: "claude-test", Temperature: 0.7, MaxTokens: 100,
		Messages: []platforms.ChatMessage{
			{Role: platforms.RoleSystem, Content: "Follow instructions."},
			{Role: platforms.RoleUser, Content: []platforms.ChatContentPart{
				{Type: "text", Text: "Describe this."},
				{Type: "image_url", ImageURL: &platforms.ChatImageURL{URL: "data:image/png;base64," + imageData}},
			}},
			{Role: platforms.RoleAssistant, Content: "Earlier reply."},
		},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletionStream() error = %v", err)
	}
	t.Cleanup(func() { _ = stream.Close() })

	first, err := stream.Recv()
	if err != nil {
		t.Fatalf("first Recv() error = %v", err)
	}
	if len(first.Choices) != 1 || first.Choices[0].Delta.Content != "Hello" {
		t.Fatalf("first Recv() = %#v, want Hello delta", first)
	}

	second, err := stream.Recv()
	if err != nil {
		t.Fatalf("second Recv() error = %v", err)
	}
	wantUsage := &platforms.TokenUsage{PromptTokens: 16, CompletionTokens: 7, TotalTokens: 23}
	if second.Usage == nil || *second.Usage != *wantUsage {
		t.Fatalf("second Recv() usage = %#v, want %#v", second.Usage, wantUsage)
	}
	if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("final Recv() error = %v, want EOF", err)
	}

	if received["model"] != "claude-test" || received["max_tokens"] != float64(100) || received["temperature"] != float64(float32(0.7)) {
		t.Fatalf("request metadata = %#v", received)
	}
	if received["stream"] != true {
		t.Fatalf("stream = %#v, want true", received["stream"])
	}
	system, ok := received["system"].([]any)
	if !ok || len(system) != 1 || system[0].(map[string]any)["text"] != "Follow instructions." {
		t.Fatalf("system = %#v", received["system"])
	}
	messages, ok := received["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v, want two non-system messages", received["messages"])
	}
	if messages[0].(map[string]any)["role"] != "user" || messages[1].(map[string]any)["role"] != "assistant" {
		t.Fatalf("message roles = %#v", messages)
	}
	userContent := messages[0].(map[string]any)["content"].([]any)
	if len(userContent) != 2 || userContent[1].(map[string]any)["type"] != "image" {
		t.Fatalf("user content = %#v", userContent)
	}
}

func TestAdapterRejectsUnsupportedMediaBeforeNetwork(t *testing.T) {
	t.Parallel()

	requestSent := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requestSent = true
		return nil, errors.New("HTTP request must not be sent")
	})}

	_, err := (Adapter{HTTPClient: client}).CreateChatCompletionStream(context.Background(), platforms.Endpoint{
		APIKey: "test-key", BaseURL: "https://api.example.test",
	}, &platforms.ChatCompletionStreamRequest{
		Model: "claude-test", Temperature: 0.5, MaxTokens: 100,
		Messages: []platforms.ChatMessage{{Role: platforms.RoleUser, Content: []platforms.ChatContentPart{
			{Type: "video", MediaData: &platforms.MediaData{MIMEType: "video/mp4", Data: []byte("video")}},
			{Type: "audio", MediaData: &platforms.MediaData{MIMEType: "audio/ogg", Data: []byte("audio")}},
		}}},
	})
	var unsupported *platforms.UnsupportedMediaError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %v, want UnsupportedMediaError", err)
	}
	if !slices.Equal(unsupported.Types, []string{"audio", "video"}) {
		t.Fatalf("unsupported types = %v, want [audio video]", unsupported.Types)
	}
	if requestSent {
		t.Fatal("HTTP request was sent for unsupported media")
	}
}

func TestAdapterBoundsAnthropicAPIError(t *testing.T) {
	t.Parallel()

	secretBody := strings.Repeat("sensitive", 2_000)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Status:     "400 Bad Request",
			Header:     http.Header{"Request-Id": []string{"req-test"}, "Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"invalid_request_error","message":"` + secretBody +
				`"}}`)),
			Request: request,
		}, nil
	})}

	_, err := (Adapter{HTTPClient: client}).CreateChatCompletionStream(context.Background(), platforms.Endpoint{
		APIKey: "test-key", BaseURL: "https://api.example.test",
	}, &platforms.ChatCompletionStreamRequest{
		Model: "claude-test", Temperature: 0.5, MaxTokens: 100,
		Messages: []platforms.ChatMessage{{Role: platforms.RoleUser, Content: "hello"}},
	})
	if err == nil {
		t.Fatal("CreateChatCompletionStream() error = nil")
	}
	if strings.Contains(err.Error(), "sensitive") || len(err.Error()) > 200 {
		t.Fatalf("API error was not bounded: len=%d error=%q", len(err.Error()), err)
	}
	if !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), "invalid_request_error") {
		t.Fatalf("API error = %q, want status and type", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestBuildMessageParamsRejectsInvalidAnthropicInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request platforms.ChatCompletionStreamRequest
		want    string
	}{
		{
			name: "temperature",
			request: platforms.ChatCompletionStreamRequest{Model: "claude", Temperature: 1.3, MaxTokens: 10,
				Messages: []platforms.ChatMessage{{Role: platforms.RoleUser, Content: "hello"}}},
			want: "between 0 and 1",
		},
		{
			name: "remote image URL",
			request: platforms.ChatCompletionStreamRequest{Model: "claude", Temperature: 0.5, MaxTokens: 10,
				Messages: []platforms.ChatMessage{{Role: platforms.RoleUser, Content: []platforms.ChatContentPart{{
					Type: "image_url", ImageURL: &platforms.ChatImageURL{URL: "https://example.com/image.png"},
				}}}}},
			want: "base64 data URI",
		},
		{
			name: "unsupported image MIME",
			request: platforms.ChatCompletionStreamRequest{Model: "claude", Temperature: 0.5, MaxTokens: 10,
				Messages: []platforms.ChatMessage{{Role: platforms.RoleUser, Content: []platforms.ChatContentPart{{
					Type: "image_url", ImageURL: &platforms.ChatImageURL{URL: "data:image/svg+xml;base64,PHN2Zy8+"},
				}}}}},
			want: "does not support image MIME type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := buildMessageParams(&tt.request)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("buildMessageParams() error = %v, want %q", err, tt.want)
			}
		})
	}
}

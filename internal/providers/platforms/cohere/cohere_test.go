package cohere

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/andatoshiki/omni/internal/providers/platforms"
)

func TestCreateChatCompletionStreamTranslatesTextParts(t *testing.T) {
	t.Parallel()

	var payload map[string]any
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v2/chat" {
			t.Fatalf("path = %q, want /v2/chat", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer "+"test-key" {
			t.Fatalf("Authorization = %q, want %q", got, "Bearer "+"test-key")
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    request,
		}, nil
	})}

	stream, err := (Adapter{HTTPClient: client}).CreateChatCompletionStream(context.Background(), platforms.Endpoint{
		APIKey: "test-key", BaseURL: "https://api.example.test",
	}, &platforms.ChatCompletionStreamRequest{
		Model: "command-r",
		Messages: []platforms.ChatMessage{
			{Role: platforms.RoleSystem, Content: "Follow instructions."},
			{Role: platforms.RoleUser, Content: []platforms.ChatContentPart{
				{Type: "text", Text: "Summarize"},
				{Type: "text", Text: "this document."},
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletionStream() error = %v", err)
	}
	t.Cleanup(func() { _ = stream.Close() })

	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v", payload["messages"])
	}
	systemMessage := messages[0].(map[string]any)
	if systemMessage["role"] != platforms.RoleSystem {
		t.Fatalf("system role = %#v, want %q", systemMessage["role"], platforms.RoleSystem)
	}
	system := systemMessage["content"]
	if system != "Follow instructions." {
		t.Fatalf("system content = %#v, want Follow instructions.", system)
	}
	userMessage := messages[1].(map[string]any)
	if userMessage["role"] != platforms.RoleUser {
		t.Fatalf("user role = %#v, want %q", userMessage["role"], platforms.RoleUser)
	}
	user := userMessage["content"]
	if user != "Summarize\nthis document." {
		t.Fatalf("user content = %#v, want joined text parts", user)
	}
}

func TestCreateChatCompletionStreamRejectsUnsupportedMediaBeforeRequest(t *testing.T) {
	t.Parallel()

	requestSent := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requestSent = true
		return nil, errors.New("HTTP request must not be sent")
	})}

	_, err := (Adapter{HTTPClient: client}).CreateChatCompletionStream(context.Background(), platforms.Endpoint{
		APIKey: "test-key", BaseURL: "https://api.example.test",
	}, &platforms.ChatCompletionStreamRequest{
		Model: "command-r",
		Messages: []platforms.ChatMessage{{
			Role: platforms.RoleUser,
			Content: []platforms.ChatContentPart{
				{Type: "text", Text: "Describe this image."},
				{Type: "image_url", ImageURL: &platforms.ChatImageURL{URL: "data:image/png;base64,AA=="}},
			},
		}},
	})
	if err == nil {
		t.Fatal("CreateChatCompletionStream() error = nil")
	}
	var unsupported *platforms.UnsupportedMediaError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %v, want UnsupportedMediaError", err)
	}
	if got := strings.Join(unsupported.Types, ","); got != "image_url" {
		t.Fatalf("unsupported media types = %q, want image_url", got)
	}
	if requestSent {
		t.Fatal("HTTP request was sent for unsupported media")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

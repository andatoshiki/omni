package openai

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

func TestCreateChatCompletionStreamIncludesUsage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		options, ok := payload["stream_options"].(map[string]any)
		if !ok || options["include_usage"] != true {
			t.Errorf("stream_options = %#v", payload["stream_options"])
		}
		if temperature, ok := payload["temperature"]; !ok || temperature != float64(0) {
			t.Errorf("temperature = %#v", temperature)
		}
		messages, ok := payload["messages"].([]any)
		if !ok || len(messages) != 1 {
			t.Fatalf("messages = %#v", payload["messages"])
		}
		content, ok := messages[0].(map[string]any)["content"].([]any)
		if !ok || len(content) != 2 {
			t.Fatalf("multipart content = %#v", messages[0])
		}
		imageURL := content[1].(map[string]any)["image_url"].(map[string]any)["url"]
		if imageURL != "data:image/jpeg;base64,AA==" {
			t.Errorf("image URL = %#v", imageURL)
		}

		stream := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" +
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\n" +
			"data: [DONE]\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(stream)),
			Request:    request,
		}, nil
	})}

	stream, err := (Adapter{HTTPClient: client}).CreateChatCompletionStream(
		context.Background(),
		platforms.Endpoint{BaseURL: "https://api.example.test", APIKey: "test-key"},
		&platforms.ChatCompletionStreamRequest{
			Model: "test-model",
			Messages: []platforms.ChatMessage{{
				Role: platforms.RoleUser,
				Content: []platforms.ChatContentPart{
					{Type: "text", Text: "describe"},
					{Type: "image_url", ImageURL: &platforms.ChatImageURL{URL: "data:image/jpeg;base64,AA=="}},
				},
			}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	first, err := stream.Recv()
	if err != nil || len(first.Choices) != 1 || first.Choices[0].Delta.Content != "hello" {
		t.Fatalf("first chunk = %#v, %v", first, err)
	}
	final, err := stream.Recv()
	if err != nil || final.Usage == nil || final.Usage.TotalTokens != 12 {
		t.Fatalf("usage chunk = %#v, %v", final, err)
	}
	if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("final Recv error = %v", err)
	}
}

func TestCreateChatCompletionStreamBoundsAPIErrorBody(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", 10_000))),
			Request:    request,
		}, nil
	})}

	_, err := (Adapter{HTTPClient: client}).CreateChatCompletionStream(
		context.Background(),
		platforms.Endpoint{BaseURL: "https://api.example.test", APIKey: "test-key"},
		&platforms.ChatCompletionStreamRequest{Model: "test-model"},
	)
	if err == nil {
		t.Fatal("CreateChatCompletionStream() error = nil")
	}
	if len(err.Error()) > platforms.MaxAPIErrorBodyBytes+100 || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("API error was not bounded: len=%d error=%q", len(err.Error()), err)
	}
}

func TestCreateChatCompletionStreamRejectsUnsupportedMediaBeforeRequest(t *testing.T) {
	requestSent := false
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestSent = true
		return nil, errors.New("HTTP request must not be sent")
	})}

	_, err := (Adapter{HTTPClient: client}).CreateChatCompletionStream(
		context.Background(),
		platforms.Endpoint{BaseURL: "https://api.example.test", APIKey: "test-key"},
		&platforms.ChatCompletionStreamRequest{
			Model: "test-model",
			Messages: []platforms.ChatMessage{{
				Role: platforms.RoleUser,
				Content: []platforms.ChatContentPart{
					{Type: "text", Text: "summarize these files"},
					{Type: "image_url", ImageURL: &platforms.ChatImageURL{URL: "data:image/jpeg;base64,AA=="}},
					{Type: "video", MediaData: &platforms.MediaData{MIMEType: "video/mp4"}},
					{Type: "audio", MediaData: &platforms.MediaData{MIMEType: "audio/mpeg"}},
					{Type: "audio", MediaData: &platforms.MediaData{MIMEType: "audio/ogg"}},
				},
			}},
		},
	)
	if err == nil {
		t.Fatal("CreateChatCompletionStream() error = nil")
	}
	var mediaErr *platforms.UnsupportedMediaError
	if !errors.As(err, &mediaErr) {
		t.Fatalf("error type = %T, want *platforms.UnsupportedMediaError", err)
	}
	if got := strings.Join(mediaErr.Types, ","); got != "audio,video" {
		t.Fatalf("unsupported media types = %q, want %q", got, "audio,video")
	}
	if requestSent {
		t.Fatal("HTTP request was sent for unsupported media")
	}
}

func TestUnsupportedMediaTypesReportsUnknownParts(t *testing.T) {
	got := unsupportedMediaTypes([]platforms.ChatMessage{{
		Role: platforms.RoleUser,
		Content: []platforms.ChatContentPart{
			{Type: "text", Text: "prompt"},
			{Type: "image_url", ImageURL: &platforms.ChatImageURL{URL: "data:image/jpeg;base64,AA=="}},
			{Type: " "},
		},
	}})
	if joined := strings.Join(got, ","); joined != "unknown" {
		t.Fatalf("unsupportedMediaTypes() = %q, want %q", joined, "unknown")
	}
}

func TestReasoningRequestAndDelta(t *testing.T) {
	var payload map[string]any
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		stream := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"checking\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"answer\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\n" +
			"data: [DONE]\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(stream)),
			Request:    request,
		}, nil
	})}

	stream, err := (Adapter{HTTPClient: client, Dialect: ThinkingDialectOpenAI}).CreateChatCompletionStream(
		context.Background(),
		platforms.Endpoint{BaseURL: "https://api.example.test", APIKey: "test-key"},
		&platforms.ChatCompletionStreamRequest{
			Model:       "reasoning-model",
			Temperature: 0.7,
			MaxTokens:   500,
			Thinking:    &platforms.ThinkingOptions{Mode: "enabled", Effort: "high"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	if payload["reasoning_effort"] != "high" || payload["max_completion_tokens"] != float64(500) {
		t.Fatalf("reasoning payload = %#v", payload)
	}
	if _, exists := payload["temperature"]; exists {
		t.Fatalf("temperature must be omitted for active reasoning: %#v", payload)
	}
	if _, exists := payload["max_tokens"]; exists {
		t.Fatalf("max_tokens must be replaced for native OpenAI reasoning: %#v", payload)
	}

	reasoning, err := stream.Recv()
	if err != nil || reasoning.Choices[0].Delta.ReasoningContent != "checking" {
		t.Fatalf("reasoning delta = %#v, %v", reasoning, err)
	}
	answer, err := stream.Recv()
	if err != nil || answer.Choices[0].Delta.Content != "answer" {
		t.Fatalf("answer delta = %#v, %v", answer, err)
	}
	finished, err := stream.Recv()
	if err != nil || finished.Choices[0].FinishReason != "length" {
		t.Fatalf("finish reason = %#v, %v", finished, err)
	}
}

func TestDeepSeekThinkingTranslation(t *testing.T) {
	request := &platforms.ChatCompletionStreamRequest{
		Model:       "deepseek-reasoner",
		Temperature: 0.8,
		MaxTokens:   200,
		Thinking:    &platforms.ThinkingOptions{Mode: "enabled", Effort: "medium"},
	}
	wire := (Adapter{Dialect: ThinkingDialectDeepSeek}).translateRequest(request)
	if wire.Thinking == nil || wire.Thinking.Type != "enabled" {
		t.Fatalf("thinking = %#v", wire.Thinking)
	}
	if wire.ReasoningEffort != "high" || wire.Temperature != nil || wire.MaxTokens != 200 {
		t.Fatalf("translated request = %#v", wire)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

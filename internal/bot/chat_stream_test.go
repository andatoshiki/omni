package bot

import (
	"context"
	"io"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/providers"
	"github.com/andatoshiki/omni/internal/providers/platforms"
)

type scriptedChatStream struct {
	responses []*platforms.ChatCompletionStreamResponse
	index     int
}

func (s *scriptedChatStream) Recv() (*platforms.ChatCompletionStreamResponse, error) {
	if s.index >= len(s.responses) {
		return nil, io.EOF
	}
	response := s.responses[s.index]
	s.index++
	return response, nil
}

func (*scriptedChatStream) Close() error { return nil }

func TestReceiveChatStreamSeparatesReasoningFromAnswer(t *testing.T) {
	usage := &platforms.TokenUsage{PromptTokens: 4, CompletionTokens: 6, TotalTokens: 10}
	stream := &scriptedChatStream{responses: []*platforms.ChatCompletionStreamResponse{
		{Choices: []platforms.StreamChoice{{Delta: platforms.StreamDelta{ReasoningContent: "first thought"}}}},
		{Choices: []platforms.StreamChoice{{Delta: platforms.StreamDelta{Content: " answer"}}}},
		{Choices: []platforms.StreamChoice{{Delta: platforms.StreamDelta{ReasoningContent: "late thought"}}}},
		{Usage: usage},
	}}
	message := &models.Message{ID: 1, Chat: models.Chat{ID: -100}}
	handler := &CommandHandler{app: &App{}}
	presenter := newChatStreamPresenter(handler, context.Background(), message)
	presenter.updatesDisabled = true

	text, gotUsage, capped, err := handler.receiveChatStream(
		context.Background(), message, providers.ModelID{Provider: "test", Model: "reasoner"}, stream, presenter,
	)
	if err != nil {
		t.Fatalf("receiveChatStream() error = %v", err)
	}
	if text != " answer" {
		t.Fatalf("text = %q, want answer only", text)
	}
	if gotUsage != usage || capped {
		t.Fatalf("usage/capped = %#v/%t", gotUsage, capped)
	}
	if presenter.reasoning != "first thought" {
		t.Fatalf("reasoning = %q; late reasoning must not reappear", presenter.reasoning)
	}
	if presenter.phase != previewAnswering {
		t.Fatalf("phase = %v, want answering", presenter.phase)
	}
}

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
		{Choices: []platforms.StreamChoice{{Delta: platforms.StreamDelta{Content: "\r\n\r\n answer\r\n"}}}},
		{Choices: []platforms.StreamChoice{{Delta: platforms.StreamDelta{ReasoningContent: "late thought"}}}},
		{Choices: []platforms.StreamChoice{{FinishReason: "stop"}}, Usage: usage},
	}}
	message := &models.Message{ID: 1, Chat: models.Chat{ID: -100}}
	handler := &CommandHandler{app: &App{}}
	presenter := newChatStreamPresenter(handler, context.Background(), message)
	presenter.updatesDisabled = true

	result := handler.receiveChatStream(
		context.Background(), message, providers.ModelID{Provider: "test", Model: "reasoner"}, stream, presenter,
	)
	if result.err != nil {
		t.Fatalf("receiveChatStream() error = %v", result.err)
	}
	if result.text != "answer" {
		t.Fatalf("text = %q, want normalized answer only", result.text)
	}
	if result.usage != usage || result.previewCapped {
		t.Fatalf("usage/capped = %#v/%t", result.usage, result.previewCapped)
	}
	if result.finishReason != "stop" || !result.sawReasoning {
		t.Fatalf("finish reason/reasoning = %q/%t", result.finishReason, result.sawReasoning)
	}
	if presenter.reasoning != "first thought" {
		t.Fatalf("reasoning = %q; late reasoning must not reappear", presenter.reasoning)
	}
	if presenter.phase != previewAnswering {
		t.Fatalf("phase = %v, want answering", presenter.phase)
	}
	if presenter.answer != "answer\n" {
		t.Fatalf("live preview = %q, want normalized line endings without leading whitespace", presenter.answer)
	}
}

func TestNoAnswerErrorExplainsProviderOutcome(t *testing.T) {
	tests := []struct {
		name         string
		finishReason string
		sawReasoning bool
		want         string
	}{
		{name: "thinking exhausted limit", finishReason: "MAX_TOKENS", sawReasoning: true, want: "model used its entire output-token limit while thinking before producing an answer"},
		{name: "plain output exhausted limit", finishReason: "length", want: "model reached its output-token limit before producing an answer"},
		{name: "provider safety block", finishReason: "SAFETY", sawReasoning: true, want: "provider blocked the model's answer"},
		{name: "reasoning without metadata", sawReasoning: true, want: "model finished thinking without producing an answer"},
		{name: "no output or metadata", want: "model completed without producing answer text"},
		{name: "unknown provider reason", finishReason: "other", want: "model stopped without producing answer text (provider reason: other)"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := noAnswerError(test.finishReason, test.sawReasoning).Error(); got != test.want {
				t.Fatalf("noAnswerError() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeAssistantResponsePreservesInternalFormatting(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "outer blank lines", input: "\n\nHello\n\n", want: "Hello"},
		{name: "windows line endings", input: "\r\nHello\r\n\r\nWorld\r\n", want: "Hello\n\nWorld"},
		{name: "internal paragraphs", input: "  Hello\n\nWorld  again  \n", want: "Hello\n\nWorld  again"},
		{name: "indented code", input: "\n\n    first line\n    second line\n", want: "    first line\n    second line"},
		{name: "tab indented code", input: "\r\n\tfirst line\r\n\tsecond line\r\n", want: "\tfirst line\n\tsecond line"},
		{name: "whitespace only", input: " \t\r\n ", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeAssistantResponse(test.input); got != test.want {
				t.Fatalf("normalizeAssistantResponse() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeAssistantPreviewKeepsTrailingWhitespaceUntilCompletion(t *testing.T) {
	input := "\r\n\r\nAnswer starts\r\n"
	if got := normalizeAssistantPreview(input); got != "Answer starts\n" {
		t.Fatalf("normalizeAssistantPreview() = %q", got)
	}
}

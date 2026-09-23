package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/providers"
)

func (c *CommandHandler) executeChatStream(
	ctx context.Context,
	msg *models.Message,
	modelID providers.ModelID,
	request *providers.ChatCompletionStreamRequest,
	requestStartedAt time.Time,
) (string, *providers.TokenUsage, error) {
	presenter := newChatStreamPresenter(c, ctx, msg)
	presenter.start()
	c.app.sendChatActionTyping(ctx, msg)

	stream, err := c.openChatStream(ctx, modelID, request)
	if err != nil {
		c.app.logger.Error("failed to create ai chat stream", append(c.app.messageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", err)...)
		presenter.fail(errorMessage(err))
		return "", nil, err
	}
	defer stream.Close()

	result := c.receiveChatStream(ctx, msg, modelID, stream, presenter)
	if result.text == "" && result.err == nil {
		result.err = noAnswerError(result.finishReason, result.sawReasoning)
	}
	c.logChatCompletion(msg, modelID, requestStartedAt, result)

	if result.text != "" {
		if result.err != nil {
			result.text += fmt.Sprintf("\n\n⚠️ _[Stream interrupted: %v]_", result.err)
		}
		presenter.finish(result.text)
	} else {
		if result.err != nil {
			presenter.fail(errorMessage(result.err))
		}
	}

	return result.text, result.usage, result.err
}

type streamOpenResult struct {
	stream providers.ChatCompletionStream
	err    error
}

func (c *CommandHandler) openChatStream(
	ctx context.Context,
	modelID providers.ModelID,
	request *providers.ChatCompletionStreamRequest,
) (providers.ChatCompletionStream, error) {
	result := make(chan streamOpenResult)
	go func() {
		stream, err := c.app.providers.CreateChatCompletionStream(ctx, modelID, request)
		if ctx.Err() != nil {
			if stream != nil {
				_ = stream.Close()
			}
			return
		}
		select {
		case result <- streamOpenResult{stream: stream, err: err}:
		case <-ctx.Done():
			if stream != nil {
				_ = stream.Close()
			}
		}
	}()

	for {
		select {
		case opened := <-result:
			return opened.stream, opened.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

type streamReceiveResult struct {
	response *providers.ChatCompletionStreamResponse
	err      error
}

type chatStreamResult struct {
	text          string
	usage         *providers.TokenUsage
	previewCapped bool
	finishReason  string
	sawReasoning  bool
	err           error
}

func (c *CommandHandler) receiveChatStream(
	ctx context.Context,
	msg *models.Message,
	modelID providers.ModelID,
	stream providers.ChatCompletionStream,
	presenter *chatStreamPresenter,
) (result chatStreamResult) {
	defer func() {
		result.text = normalizeAssistantResponse(result.text)
	}()

	events := make(chan streamReceiveResult, 1)
	go func() {
		for {
			response, err := stream.Recv()
			select {
			case events <- streamReceiveResult{response: response, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	flushTicker := time.NewTicker(previewFlushInterval)
	defer flushTicker.Stop()
	answerStarted := false
	for {
		select {
		case <-ctx.Done():
			result.previewCapped = presenter.previewCapped
			result.err = ctx.Err()
			return result
		case <-flushTicker.C:
			presenter.flushLatest(false)
		case event := <-events:
			if errors.Is(event.err, io.EOF) {
				result.previewCapped = presenter.previewCapped
				return result
			}
			if event.err != nil {
				attrs := append(c.app.messageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", event.err)
				c.app.logger.Error("ai chat stream receive failed", attrs...)
				result.previewCapped = presenter.previewCapped
				result.err = event.err
				return result
			}
			if event.response == nil {
				continue
			}
			if event.response.Usage != nil {
				result.usage = event.response.Usage
			}
			for _, choice := range event.response.Choices {
				if choice.FinishReason != "" {
					result.finishReason = choice.FinishReason
				}
				reasoning := choice.Delta.ReasoningContent + choice.Delta.Reasoning
				if reasoning != "" {
					result.sawReasoning = true
					presenter.appendReasoning(reasoning)
				}
				if choice.Delta.Content == "" {
					continue
				}
				result.text += choice.Delta.Content
				preview := normalizeAssistantPreview(result.text)
				if !answerStarted && preview == "" {
					continue
				}
				answerStarted = true
				presenter.showAnswer(preview)
			}
		}
	}
}

func noAnswerError(finishReason string, sawReasoning bool) error {
	reason := strings.ToLower(strings.TrimSpace(finishReason))
	switch reason {
	case "length", "max_tokens", "max_output_tokens":
		if sawReasoning {
			return errors.New("model used its entire output-token limit while thinking before producing an answer")
		}
		return errors.New("model reached its output-token limit before producing an answer")
	case "content_filter", "safety", "recitation", "prohibited_content", "blocklist", "refusal":
		return errors.New("provider blocked the model's answer")
	case "tool_use", "function_call", "malformed_function_call", "unexpected_tool_call":
		return fmt.Errorf("model stopped for %s instead of producing a text answer", reason)
	case "", "stop", "end_turn", "stop_sequence":
		if sawReasoning {
			return errors.New("model finished thinking without producing an answer")
		}
		return errors.New("model completed without producing answer text")
	default:
		return fmt.Errorf("model stopped without producing answer text (provider reason: %s)", reason)
	}
}

func normalizeAssistantPreview(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return trimAssistantLeadingWhitespace(text)
}

func normalizeAssistantResponse(text string) string {
	return strings.TrimRightFunc(normalizeAssistantPreview(text), unicode.IsSpace)
}

func trimAssistantLeadingWhitespace(text string) string {
	contentStart := strings.IndexFunc(text, func(character rune) bool {
		return !unicode.IsSpace(character)
	})
	if contentStart < 0 {
		return ""
	}

	// Four leading spaces or a tab are meaningful Markdown indentation. Drop
	// blank lines before them, but preserve the indentation itself.
	prefix := text[:contentStart]
	indentStart := strings.LastIndexByte(prefix, '\n') + 1
	indent := prefix[indentStart:]
	if strings.ContainsRune(indent, '\t') || strings.HasPrefix(indent, "    ") {
		return indent + text[contentStart:]
	}
	return text[contentStart:]
}

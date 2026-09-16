package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

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

	stream, err := c.openChatStream(ctx, modelID, request, presenter)
	if err != nil {
		c.app.logger.Error("failed to create ai chat stream", append(c.app.messageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", err)...)
		presenter.fail(errorMessage(err))
		return "", nil, err
	}
	defer stream.Close()

	text, usage, previewCapped, streamErr := c.receiveChatStream(ctx, msg, modelID, stream, presenter)
	if text == "" && streamErr == nil {
		streamErr = errors.New("model returned an empty response")
	}
	c.logChatCompletion(msg, modelID, requestStartedAt, text, usage, previewCapped)

	if text != "" {
		if streamErr != nil {
			text += fmt.Sprintf("\n\n⚠️ _[Stream interrupted: %v]_", streamErr)
		}
		presenter.finish(text)
	} else {
		if streamErr != nil {
			presenter.fail(errorMessage(streamErr))
		}
	}

	return text, usage, streamErr
}

type streamOpenResult struct {
	stream providers.ChatCompletionStream
	err    error
}

func (c *CommandHandler) openChatStream(
	ctx context.Context,
	modelID providers.ModelID,
	request *providers.ChatCompletionStreamRequest,
	presenter *chatStreamPresenter,
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

	var heartbeat <-chan time.Time
	var ticker *time.Ticker
	if presenter != nil && presenter.kind == previewRichDraft {
		ticker = time.NewTicker(privateDraftHeartbeat)
		heartbeat = ticker.C
		defer ticker.Stop()
	}
	for {
		select {
		case opened := <-result:
			return opened.stream, opened.err
		case <-heartbeat:
			presenter.heartbeat()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

type streamReceiveResult struct {
	response *providers.ChatCompletionStreamResponse
	err      error
}

func (c *CommandHandler) receiveChatStream(
	ctx context.Context,
	msg *models.Message,
	modelID providers.ModelID,
	stream providers.ChatCompletionStream,
	presenter *chatStreamPresenter,
) (text string, usage *providers.TokenUsage, previewCapped bool, streamErr error) {
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

	var heartbeat <-chan time.Time
	var heartbeatTicker *time.Ticker
	if presenter != nil && presenter.kind == previewRichDraft {
		heartbeatTicker = time.NewTicker(privateDraftHeartbeat)
		heartbeat = heartbeatTicker.C
		defer heartbeatTicker.Stop()
	}
	flushTicker := time.NewTicker(previewFlushInterval)
	defer flushTicker.Stop()
	answerStarted := false
	for {
		select {
		case <-ctx.Done():
			return text, usage, presenter.previewCapped, ctx.Err()
		case <-heartbeat:
			presenter.heartbeat()
		case <-flushTicker.C:
			presenter.flushLatest(false)
		case event := <-events:
			if errors.Is(event.err, io.EOF) {
				return text, usage, presenter.previewCapped, streamErr
			}
			if event.err != nil {
				attrs := append(c.app.messageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", event.err)
				c.app.logger.Error("ai chat stream receive failed", attrs...)
				return text, usage, presenter.previewCapped, event.err
			}
			if event.response == nil {
				continue
			}
			if event.response.Usage != nil {
				usage = event.response.Usage
			}
			for _, choice := range event.response.Choices {
				presenter.appendReasoning(choice.Delta.ReasoningContent + choice.Delta.Reasoning)
				if choice.Delta.Content == "" {
					continue
				}
				text += choice.Delta.Content
				if !answerStarted && strings.TrimSpace(text) == "" {
					continue
				}
				answerStarted = true
				presenter.showAnswer(text)
			}
		}
	}
}

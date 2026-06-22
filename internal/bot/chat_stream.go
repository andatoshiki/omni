package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	stream, err := c.app.providers.CreateChatCompletionStream(ctx, modelID, request)
	if err != nil {
		c.app.logger.Error("failed to create ai chat stream", append(c.app.messageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", err)...)
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return "", nil, err
	}
	defer stream.Close()

	c.app.sendChatActionTyping(ctx, msg)
	text, usage, replyMsg, previewCapped, streamErr := c.receiveChatStream(ctx, msg, modelID, stream)
	c.logChatCompletion(msg, modelID, requestStartedAt, text, usage, previewCapped)

	if text != "" {
		if streamErr != nil {
			text += fmt.Sprintf("\n\n⚠️ _[Stream interrupted: %v]_", streamErr)
		}
		c.sendFinalChatReply(ctx, msg, replyMsg, text)
	} else {
		if replyMsg != nil {
			_, _ = c.app.deleteMessage(ctx, replyMsg)
		}
		if streamErr != nil {
			_, _ = c.reply(ctx, msg, errorMessage(streamErr))
		}
	}

	return text, usage, streamErr
}

func (c *CommandHandler) receiveChatStream(
	ctx context.Context,
	msg *models.Message,
	modelID providers.ModelID,
	stream providers.ChatCompletionStream,
) (text string, usage *providers.TokenUsage, replyMsg *models.Message, previewCapped bool, streamErr error) {
	lastReplyEditAt := time.Now()
	minReplyInterval := minReplyIntervalPrivateChat
	if msg.Chat.ID < 0 {
		minReplyInterval = minReplyIntervalGroupChat
	}
	lastSentText := ""

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			attrs := append(c.app.messageLogAttrs(msg), "provider", modelID.Provider, "model", modelID.Model, "error", err)
			c.app.logger.Error("ai chat stream receive failed", attrs...)
			streamErr = err
			break
		}
		if response.Usage != nil {
			usage = response.Usage
		}
		for _, choice := range response.Choices {
			text += choice.Delta.Content
			if time.Since(lastReplyEditAt) <= minReplyInterval || text == "" || text == lastSentText {
				continue
			}
			if len(text) <= streamPreviewLimit {
				replyMsg, _ = c.editReply(ctx, msg, replyMsg, text)
				lastSentText = text
			} else if !previewCapped {
				replyMsg, _ = c.editReply(ctx, msg, replyMsg, truncateUTF8(text, streamPreviewLimit)+"\n\n⏳ _Response is long, sending in full when complete…_")
				previewCapped = true
			}
			lastReplyEditAt = time.Now()
		}
	}

	if time.Since(lastReplyEditAt) < minReplyInterval && text != lastSentText {
		time.Sleep(minReplyInterval - time.Since(lastReplyEditAt))
	}
	return text, usage, replyMsg, previewCapped, streamErr
}

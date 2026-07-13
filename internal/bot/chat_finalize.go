package bot

import (
	"context"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/conversation"
	"github.com/andatoshiki/omni/internal/providers"
)

func (c *CommandHandler) finalizeChatTurn(
	chatID int64,
	sessionID int64,
	userID int64,
	msg *models.Message,
	input ChatInput,
	history []conversation.Message,
	storedUserPrompt string,
	text string,
	usage *providers.TokenUsage,
	streamErr error,
) {
	if text == "" && streamErr != nil {
		return // Do not save history if it completely failed
	}

	history = appendTurnToHistory(history, input, storedUserPrompt, text, c.app.params.HistorySize)
	c.msgHistory.Store(sessionID, history)

	if err := c.app.store.SaveSession(chatID, sessionID, history); err != nil {
		c.app.logger.Warn("failed to save session to database", append(c.app.messageLogAttrs(msg), "history_messages", len(history), "error", err)...)
	}

	if usage != nil {
		if err := c.app.store.SaveTokenUsage(chatID, userID, *usage); err != nil {
			c.app.logger.Warn("failed to save token usage", append(c.app.messageLogAttrs(msg), "error", err)...)
		}
	}
}

func (c *CommandHandler) logChatCompletion(
	msg *models.Message,
	modelID providers.ModelID,
	startedAt time.Time,
	text string,
	usage *providers.TokenUsage,
	previewCapped bool,
) {
	attrs := append(
		c.app.messageLogAttrs(msg),
		"provider", modelID.Provider,
		"model", modelID.Model,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"preview_capped", previewCapped,
		"response_chunks", len(splitText(text, streamPreviewLimit)),
	)
	attrs = append(attrs, c.app.textMetricAttrs("response", text)...)
	if usage != nil {
		attrs = append(
			attrs,
			"prompt_tokens", usage.PromptTokens,
			"completion_tokens", usage.CompletionTokens,
			"total_tokens", usage.TotalTokens,
		)
	}
	c.app.logger.Info("ai chat response completed", attrs...)
}

func (c *CommandHandler) sendFinalChatReply(ctx context.Context, msg, replyMsg *models.Message, text string) {
	if len(text) <= streamPreviewLimit {
		finalReplyMsg, err := c.editReply(ctx, msg, replyMsg, text)
		if err != nil {
			_, _ = c.app.deleteMessage(ctx, finalReplyMsg)
			finalReplyMsg, err = c.app.sendMessageInThread(ctx, msg.Chat.ID, msg.MessageThreadID, text)
		}
		if err == nil {
			c.saveAssistantTranscriptMessage(msg, finalReplyMsg, text)
		}
		return
	}

	_, _ = c.app.deleteMessage(ctx, replyMsg)
	for _, chunk := range splitText(text, streamPreviewLimit) {
		var (
			sent *models.Message
			err  error
		)
		if msg.Chat.ID >= 0 {
			sent, err = c.app.sendMessageInThread(ctx, msg.Chat.ID, msg.MessageThreadID, chunk)
		} else {
			sent, err = c.app.sendReplyToMessage(ctx, msg, chunk)
		}
		if err == nil {
			c.saveAssistantTranscriptMessage(msg, sent, chunk)
		}
	}
}

func appendTurnToHistory(history []conversation.Message, input ChatInput, userPrompt, text string, maxMessages int) []conversation.Message {
	replyRole := providers.RoleAssistant
	if input.Reply != nil && input.Reply.Speaker != nil && input.Messages[0] != nil {
		if input.Reply.Speaker.UserID != 0 {
			replyRole = providers.RoleUser
		}
	}
	if input.Reply != nil && input.Reply.Text != "" {
		history = append(history, conversation.Message{
			Role:    replyRole,
			Content: input.Reply.Text,
			Speaker: input.Reply.Speaker,
		})
	}
	history = append(history,
		conversation.Message{Role: providers.RoleUser, Content: userPrompt, Speaker: input.Sender, ReplyTo: input.Reply},
		conversation.Message{Role: providers.RoleAssistant, Content: text},
	)
	if len(history) > maxMessages {
		return history[len(history)-maxMessages:]
	}
	return history
}

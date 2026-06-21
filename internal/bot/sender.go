package bot

import (
	"context"
	"errors"
	"strings"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/logging"
	"github.com/andatoshiki/omni/internal/telegramhtml"
)

func (a *App) sendMessage(ctx context.Context, chatID int64, text string) (*models.Message, error) {
	return a.sendMessageInThread(ctx, chatID, 0, text)
}

func (a *App) sendMessageInThread(ctx context.Context, chatID int64, threadID int, text string) (*models.Message, error) {
	msg, err := a.client.SendMessage(ctx, &telegram.SendMessageParams{
		ChatID: chatID, MessageThreadID: threadID,
		Text: telegramhtml.RenderMarkdown(text), ParseMode: models.ParseModeHTML,
	})
	if err == nil {
		return msg, nil
	}

	msg, err = a.client.SendMessage(ctx, &telegram.SendMessageParams{
		ChatID: chatID, MessageThreadID: threadID, Text: text,
	})
	if err != nil {
		attrs := append([]any{"chat_id", chatID, "error", err}, a.textMetricAttrs("text", text)...)
		a.logger.Error("telegram send message failed", attrs...)
		return nil, err
	}
	return msg, nil
}

func (a *App) sendLongMessage(ctx context.Context, chatID int64, text string) {
	for _, chunk := range splitText(text, streamPreviewLimit) {
		_, _ = a.sendMessage(ctx, chatID, chunk)
	}
}

func (a *App) sendLongReply(ctx context.Context, replyTo *models.Message, text string) {
	for _, chunk := range splitText(text, streamPreviewLimit) {
		_, _ = a.sendReplyToMessage(ctx, replyTo, chunk)
	}
}

func splitText(text string, maxLen int) []string {
	if maxLen <= 0 || len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}

		splitAt := maxLen
		for splitAt > 0 && !isUTF8Boundary(text[splitAt]) {
			splitAt--
		}
		if splitAt == 0 {
			splitAt = maxLen
		}
		window := text[:splitAt]
		if idx := strings.LastIndex(window, "\n\n"); idx > splitAt/4 {
			splitAt = idx + 2
		} else if idx := strings.LastIndex(window, "\n"); idx > splitAt/4 {
			splitAt = idx + 1
		} else if idx := strings.LastIndex(window, " "); idx > splitAt/4 {
			splitAt = idx + 1
		}
		chunks = append(chunks, strings.TrimRight(text[:splitAt], "\n "))
		text = strings.TrimLeft(text[splitAt:], "\n ")
	}
	return chunks
}

func isUTF8Boundary(b byte) bool {
	return b&0xc0 != 0x80
}

func truncateUTF8(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(text) <= maxBytes {
		return text
	}
	end := maxBytes
	for end > 0 && !isUTF8Boundary(text[end]) {
		end--
	}
	return text[:end]
}

func (a *App) sendMessageWithKeyboard(ctx context.Context, chatID int64, text string, keyboard *models.InlineKeyboardMarkup) (*models.Message, error) {
	msg, err := a.client.SendMessage(ctx, &telegram.SendMessageParams{ChatID: chatID, Text: text, ReplyMarkup: keyboard})
	if err != nil {
		attrs := append([]any{"chat_id", chatID, "error", err}, a.textMetricAttrs("text", text)...)
		a.logger.Error("telegram send message with keyboard failed", attrs...)
	}
	return msg, err
}

func (a *App) sendReplyToMessage(ctx context.Context, replyTo *models.Message, text string) (*models.Message, error) {
	if replyTo == nil {
		return nil, errors.New("cannot reply to a nil message")
	}
	params := &telegram.SendMessageParams{
		ReplyParameters: &models.ReplyParameters{MessageID: replyTo.ID, AllowSendingWithoutReply: true},
		ChatID:          replyTo.Chat.ID,
		MessageThreadID: replyTo.MessageThreadID,
		Text:            telegramhtml.RenderMarkdown(text),
		ParseMode:       models.ParseModeHTML,
	}
	msg, err := a.client.SendMessage(ctx, params)
	if err == nil {
		return msg, nil
	}

	params.Text = text
	params.ParseMode = ""
	msg, err = a.client.SendMessage(ctx, params)
	if err != nil {
		attrs := append(a.messageLogAttrs(replyTo), "error", err)
		attrs = append(attrs, a.textMetricAttrs("text", text)...)
		a.logger.Error("telegram reply send failed", attrs...)
		return nil, err
	}
	return msg, nil
}

func (a *App) sendReplyWithKeyboard(ctx context.Context, replyTo *models.Message, text string, keyboard *models.InlineKeyboardMarkup) (*models.Message, error) {
	if replyTo == nil {
		return nil, errors.New("cannot reply to a nil message")
	}
	msg, err := a.client.SendMessage(ctx, &telegram.SendMessageParams{
		ReplyParameters: &models.ReplyParameters{MessageID: replyTo.ID, AllowSendingWithoutReply: true},
		ChatID:          replyTo.Chat.ID,
		MessageThreadID: replyTo.MessageThreadID,
		Text:            text,
		ReplyMarkup:     keyboard,
	})
	if err != nil {
		attrs := append(a.messageLogAttrs(replyTo), "error", err)
		attrs = append(attrs, a.textMetricAttrs("text", text)...)
		a.logger.Error("telegram reply with keyboard failed", attrs...)
		return nil, err
	}
	return msg, nil
}

func (a *App) editReplyToMessage(ctx context.Context, reply *models.Message, text string) (*models.Message, error) {
	if reply == nil {
		return nil, errors.New("cannot edit a nil message")
	}
	params := &telegram.EditMessageTextParams{
		MessageID: reply.ID, ChatID: reply.Chat.ID,
		Text: telegramhtml.RenderMarkdown(text), ParseMode: models.ParseModeHTML,
	}
	msg, err := a.client.EditMessageText(ctx, params)
	if err == nil {
		return msg, nil
	}

	params.Text = text
	params.ParseMode = ""
	msg, err = a.client.EditMessageText(ctx, params)
	if err != nil {
		attrs := append(a.messageLogAttrs(reply), "error", err)
		attrs = append(attrs, a.textMetricAttrs("text", text)...)
		a.logger.Error("telegram reply edit failed", attrs...)
		return reply, err
	}
	return msg, nil
}

func (a *App) deleteMessage(ctx context.Context, msg *models.Message) (bool, error) {
	if msg == nil {
		return false, nil
	}
	success, err := a.client.DeleteMessage(ctx, &telegram.DeleteMessageParams{ChatID: msg.Chat.ID, MessageID: msg.ID})
	if err != nil {
		a.logger.Error("telegram message delete failed", "chat_id", msg.Chat.ID, "message_id", msg.ID, "error", err)
		return false, err
	}
	return success, nil
}

func (a *App) sendChatActionTyping(ctx context.Context, msg *models.Message) {
	if msg == nil {
		return
	}
	_, err := a.client.SendChatAction(ctx, &telegram.SendChatActionParams{
		ChatID: msg.Chat.ID, MessageThreadID: msg.MessageThreadID, Action: models.ChatActionTyping,
	})
	if err != nil {
		attrs := append(a.messageLogAttrs(msg), "action", models.ChatActionTyping, "error", err)
		a.logger.Error("telegram send chat action failed", attrs...)
	}
}

func (a *App) textMetricAttrs(prefix, text string) []any {
	return logging.TextMetricAttrs(prefix, text)
}

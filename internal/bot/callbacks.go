package bot

import (
	"context"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/providers"
)

func (a *App) handleCallbackQuery(ctx context.Context, query *models.CallbackQuery) {
	modelID, ok := providers.ParseModelCallback(query.Data)
	if !ok {
		return
	}

	chatID, messageID := callbackMessageIDs(query)
	if chatID == 0 {
		return
	}

	if _, err := a.providers.Resolve(modelID); err != nil {
		_, _ = a.client.AnswerCallbackQuery(ctx, &telegram.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID, Text: "❌ " + err.Error(), ShowAlert: true,
		})
		return
	}
	if err := a.store.SaveChatModel(chatID, modelID.Provider, modelID.Model); err != nil {
		a.logger.Error("failed to save model selection", "chat_id", chatID, "provider", modelID.Provider, "model", modelID.Model, "error", err)
		_, _ = a.client.AnswerCallbackQuery(ctx, &telegram.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID, Text: "❌ Failed to save model selection", ShowAlert: true,
		})
		return
	}

	var rows [][]models.InlineKeyboardButton
	for _, candidate := range a.providers.AllModelIDs() {
		label := candidate.String()
		if candidate == modelID {
			label = "✅ " + label
		}
		rows = append(rows, []models.InlineKeyboardButton{{Text: label, CallbackData: candidate.CallbackData()}})
	}
	keyboard := models.InlineKeyboardMarkup{InlineKeyboard: rows}
	_, _ = a.client.EditMessageReplyMarkup(ctx, &telegram.EditMessageReplyMarkupParams{
		ChatID: chatID, MessageID: messageID, ReplyMarkup: &keyboard,
	})
	_, _ = a.client.AnswerCallbackQuery(ctx, &telegram.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID, Text: "✅ Model set to: " + modelID.String(),
	})
}

func callbackMessageIDs(query *models.CallbackQuery) (int64, int) {
	if query == nil {
		return 0, 0
	}
	if query.Message.Message != nil {
		return query.Message.Message.Chat.ID, query.Message.Message.ID
	}
	if query.Message.InaccessibleMessage != nil {
		return query.Message.InaccessibleMessage.Chat.ID, query.Message.InaccessibleMessage.MessageID
	}
	return 0, 0
}

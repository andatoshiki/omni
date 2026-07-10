package bot

import (
	"context"
	"strings"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/command"
	"github.com/andatoshiki/omni/internal/providers"
)

func (a *App) handleCallbackQuery(ctx context.Context, query *models.CallbackQuery) {
	if query == nil {
		return
	}

	chatID, messageID := callbackMessageIDs(query)
	if chatID == 0 {
		return
	}

	if page, ok := providers.ParseProviderPageCallback(query.Data); ok {
		a.showProviderPage(ctx, query, chatID, messageID, page)
		return
	}

	if strings.HasPrefix(query.Data, "session:") {
		parts := strings.Split(query.Data, ":")
		command.HandleSessionCallback(ctx, a.commands, query, chatID, messageID, parts)
		return
	}
	if provider, page, ok := providers.ParseModelListCallback(query.Data); ok {
		a.showModelPage(ctx, query, chatID, messageID, provider, page)
		return
	}
	if modelID, ok := providers.ParseModelCallback(query.Data); ok {
		a.selectModel(ctx, query, chatID, messageID, modelID)
	}
}

func (a *App) showProviderPage(ctx context.Context, query *models.CallbackQuery, chatID int64, messageID, page int) {
	current := a.commands.currentModel(chatID)
	text, keyboard := command.ProviderSelectionView(a.providers.AllModelIDs(), current, page)
	a.editModelMenu(ctx, query, chatID, messageID, text, keyboard)
}

func (a *App) showModelPage(ctx context.Context, query *models.CallbackQuery, chatID int64, messageID int, provider string, page int) {
	current := a.commands.currentModel(chatID)
	text, keyboard, ok := command.ModelSelectionView(a.providers.AllModelIDs(), current, provider, page)
	if !ok {
		a.answerCallback(ctx, query.ID, "❌ Unknown provider", true)
		return
	}
	a.editModelMenu(ctx, query, chatID, messageID, text, keyboard)
}

func (a *App) selectModel(ctx context.Context, query *models.CallbackQuery, chatID int64, messageID int, modelID providers.ModelID) {
	if _, err := a.providers.Resolve(modelID); err != nil {
		a.answerCallback(ctx, query.ID, "❌ "+err.Error(), true)
		return
	}
	if err := a.store.SaveChatModel(chatID, modelID.Provider, modelID.Model); err != nil {
		a.logger.Error("failed to save model selection", "chat_id", chatID, "provider", modelID.Provider, "model", modelID.Model, "error", err)
		a.answerCallback(ctx, query.ID, "❌ Failed to save model selection", true)
		return
	}

	keyboard := command.ConfirmationKeyboard(modelID.Provider)
	text := "✅ Model successfully set to: " + modelID.String()
	if _, err := a.client.EditMessageText(ctx, &telegram.EditMessageTextParams{
		ChatID: chatID, MessageID: messageID, Text: text, ReplyMarkup: &keyboard,
	}); err != nil {
		a.logger.Error("failed to show model selection", "chat_id", chatID, "message_id", messageID, "error", err)
	}
	a.answerCallback(ctx, query.ID, "✅ Model set to: "+modelID.String(), false)
}

func (a *App) editModelMenu(ctx context.Context, query *models.CallbackQuery, chatID int64, messageID int, text string, keyboard models.InlineKeyboardMarkup) {
	if _, err := a.client.EditMessageText(ctx, &telegram.EditMessageTextParams{
		ChatID: chatID, MessageID: messageID, Text: text, ReplyMarkup: &keyboard,
	}); err != nil {
		a.logger.Error("failed to update model menu", "chat_id", chatID, "message_id", messageID, "error", err)
		a.answerCallback(ctx, query.ID, "❌ Failed to update model menu", true)
		return
	}
	a.answerCallback(ctx, query.ID, "", false)
}

func (a *App) answerCallback(ctx context.Context, queryID, text string, showAlert bool) {
	_, _ = a.client.AnswerCallbackQuery(ctx, &telegram.AnswerCallbackQueryParams{
		CallbackQueryID: queryID, Text: text, ShowAlert: showAlert,
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

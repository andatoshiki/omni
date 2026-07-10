package bot

import (
	"context"
	"fmt"
	"strings"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

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
		a.handleSessionCallback(ctx, query, chatID, messageID, parts)
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
	text, keyboard := providerSelectionView(a.providers.AllModelIDs(), current, page)
	a.editModelMenu(ctx, query, chatID, messageID, text, keyboard)
}

func (a *App) showModelPage(ctx context.Context, query *models.CallbackQuery, chatID int64, messageID int, provider string, page int) {
	current := a.commands.currentModel(chatID)
	text, keyboard, ok := modelSelectionView(a.providers.AllModelIDs(), current, provider, page)
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

	keyboard := confirmationKeyboard(modelID.Provider)
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

func providerSelectionView(allModels []providers.ModelID, current providers.ModelID, page int) (string, models.InlineKeyboardMarkup) {
	providerNames := uniqueProviderNames(allModels)
	pageItems, page := paginate(providerNames, page)
	rows := make([][]models.InlineKeyboardButton, 0, len(pageItems)+1)
	for _, provider := range pageItems {
		label := provider
		if provider == current.Provider {
			label = "✅ " + label
		}
		rows = append(rows, []models.InlineKeyboardButton{{
			Text: label, CallbackData: providers.ModelListCallbackData(provider, 0),
		}})
	}
	rows = appendNavigationRow(rows, page, len(providerNames), providers.ProviderPageCallbackData)
	return "🤖 Select a provider:", models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func modelSelectionView(allModels []providers.ModelID, current providers.ModelID, provider string, page int) (string, models.InlineKeyboardMarkup, bool) {
	providerModels := modelsForProvider(allModels, provider)
	if len(providerModels) == 0 {
		return "", models.InlineKeyboardMarkup{}, false
	}

	pageItems, page := paginate(providerModels, page)
	rows := make([][]models.InlineKeyboardButton, 0, len(pageItems)+2)
	for i := 0; i < len(pageItems); i += 2 {
		row := make([]models.InlineKeyboardButton, 0, 2)
		for j := 0; j < 2 && i+j < len(pageItems); j++ {
			modelID := pageItems[i+j]
			label := modelID.Model
			if modelID == current {
				label = "✅ " + label
			}
			row = append(row, models.InlineKeyboardButton{Text: label, CallbackData: modelID.CallbackData()})
		}
		rows = append(rows, row)
	}
	rows = appendNavigationRow(rows, page, len(providerModels), func(targetPage int) string {
		return providers.ModelListCallbackData(provider, targetPage)
	})
	rows = append(rows, []models.InlineKeyboardButton{{
		Text: "🔝 Back to Providers", CallbackData: providers.ProviderPageCallbackData(0),
	}})
	return fmt.Sprintf("🤖 Select a model from %s:", provider), models.InlineKeyboardMarkup{InlineKeyboard: rows}, true
}

func confirmationKeyboard(provider string) models.InlineKeyboardMarkup {
	return models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{{Text: "⬅️ Back to Models", CallbackData: providers.ModelListCallbackData(provider, 0)}},
		{{Text: "🔝 Back to Providers", CallbackData: providers.ProviderPageCallbackData(0)}},
	}}
}

func uniqueProviderNames(allModels []providers.ModelID) []string {
	seen := make(map[string]struct{})
	names := make([]string, 0)
	for _, modelID := range allModels {
		if _, ok := seen[modelID.Provider]; ok {
			continue
		}
		seen[modelID.Provider] = struct{}{}
		names = append(names, modelID.Provider)
	}
	return names
}

func modelsForProvider(allModels []providers.ModelID, provider string) []providers.ModelID {
	result := make([]providers.ModelID, 0)
	for _, modelID := range allModels {
		if modelID.Provider == provider {
			result = append(result, modelID)
		}
	}
	return result
}

func paginate[T any](items []T, page int) ([]T, int) {
	lastPage := 0
	if len(items) > 0 {
		lastPage = (len(items) - 1) / MaxItemsPerPage
	}
	if page < 0 {
		page = 0
	} else if page > lastPage {
		page = lastPage
	}
	start := page * MaxItemsPerPage
	end := min(start+MaxItemsPerPage, len(items))
	return items[start:end], page
}

func appendNavigationRow(rows [][]models.InlineKeyboardButton, page, itemCount int, callbackData func(int) string) [][]models.InlineKeyboardButton {
	navigation := make([]models.InlineKeyboardButton, 0, 2)
	if page > 0 {
		navigation = append(navigation, models.InlineKeyboardButton{Text: "⬅️ Prev", CallbackData: callbackData(page - 1)})
	}
	if (page+1)*MaxItemsPerPage < itemCount {
		navigation = append(navigation, models.InlineKeyboardButton{Text: "Next ➡️", CallbackData: callbackData(page + 1)})
	}
	if len(navigation) > 0 {
		rows = append(rows, navigation)
	}
	return rows
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

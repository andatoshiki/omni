package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/providers"
)

const MaxItemsPerPage = 10

func Model(ctx context.Context, b BotContext, msg *models.Message) {
	current := b.CurrentModel(msg.Chat.ID)
	allModels := b.Providers().AllModelIDs()

	if len(allModels) == 0 {
		_, _ = b.Reply(ctx, msg, errorMessage(errors.New("no models configured")))
		return
	}

	text, keyboard := ProviderSelectionView(allModels, current, 0)

	if msg.Chat.ID >= 0 {
		_, _ = b.SendMessageWithKeyboard(ctx, msg.Chat.ID, text, &keyboard)
	} else {
		_, _ = b.SendReplyWithKeyboard(ctx, msg, text, &keyboard)
	}
}

func ProviderSelectionView(allModels []providers.ModelID, current providers.ModelID, page int) (string, models.InlineKeyboardMarkup) {
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

func ModelSelectionView(allModels []providers.ModelID, current providers.ModelID, provider string, page int) (string, models.InlineKeyboardMarkup, bool) {
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

func ConfirmationKeyboard(provider string) models.InlineKeyboardMarkup {
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
	return paginateWithLimit(items, page, MaxItemsPerPage)
}

func paginateWithLimit[T any](items []T, page int, pageSize int) ([]T, int) {
	if pageSize <= 0 {
		pageSize = MaxItemsPerPage
	}
	lastPage := 0
	if len(items) > 0 {
		lastPage = (len(items) - 1) / pageSize
	}
	if page < 0 {
		page = 0
	} else if page > lastPage {
		page = lastPage
	}
	start := page * pageSize
	end := min(start+pageSize, len(items))
	return items[start:end], page
}

func appendNavigationRow(rows [][]models.InlineKeyboardButton, page, itemCount int, callbackData func(int) string) [][]models.InlineKeyboardButton {
	return appendNavigationRowWithLimit(rows, page, itemCount, MaxItemsPerPage, callbackData)
}

func appendNavigationRowWithLimit(rows [][]models.InlineKeyboardButton, page, itemCount int, pageSize int, callbackData func(int) string) [][]models.InlineKeyboardButton {
	if pageSize <= 0 {
		pageSize = MaxItemsPerPage
	}
	navigation := make([]models.InlineKeyboardButton, 0, 2)
	if page > 0 {
		navigation = append(navigation, models.InlineKeyboardButton{Text: "⬅️ Prev", CallbackData: callbackData(page - 1)})
	}
	if (page+1)*pageSize < itemCount {
		navigation = append(navigation, models.InlineKeyboardButton{Text: "Next ➡️", CallbackData: callbackData(page + 1)})
	}
	if len(navigation) > 0 {
		rows = append(rows, navigation)
	}
	return rows
}

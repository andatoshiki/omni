package command

import (
	"context"
	"fmt"
	"strconv"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func Conversation(ctx context.Context, b BotContext, msg *models.Message) {
	text, keyboard, err := sessionListView(b, msg.Chat.ID, 0)
	if err != nil {
		_, _ = b.Reply(ctx, msg, errorMessage(err))
		return
	}

	if msg.Chat.ID >= 0 {
		_, _ = b.SendMessageWithKeyboard(ctx, msg.Chat.ID, text, &keyboard)
	} else {
		_, _ = b.SendReplyWithKeyboard(ctx, msg, text, &keyboard)
	}
}

func NewSession(ctx context.Context, b BotContext, msg *models.Message) {
	_, err := b.Store().CreateNewSession(msg.Chat.ID, "New Session")
	if err != nil {
		_, _ = b.Reply(ctx, msg, errorMessage(err))
		return
	}
	_, _ = b.Reply(ctx, msg, "✅ A new conversation session has been started!")
}

func HandleSessionCallback(ctx context.Context, b BotContext, query *models.CallbackQuery, chatID int64, messageID int, parts []string) {
	if len(parts) < 2 {
		return
	}
	action := parts[1]

	switch action {
	case "list":
		if len(parts) >= 3 {
			page, _ := strconv.Atoi(parts[2])
			showSessionList(ctx, b, query, chatID, messageID, page)
		}
	case "view":
		if len(parts) >= 4 {
			sessionID, _ := strconv.ParseInt(parts[2], 10, 64)
			page, _ := strconv.Atoi(parts[3])
			showSessionMenu(ctx, b, query, chatID, messageID, sessionID, page)
		}
	case "load":
		if len(parts) >= 3 {
			sessionID, _ := strconv.ParseInt(parts[2], 10, 64)
			if err := b.Store().SetActiveSession(chatID, sessionID); err != nil {
				b.AnswerCallback(ctx, query.ID, "❌ Failed to load session", true)
				return
			}

			text := "✅ Session loaded successfully."
			if _, err := b.Telegram().EditMessageText(ctx, &telegram.EditMessageTextParams{
				ChatID: chatID, MessageID: messageID, Text: text, ReplyMarkup: nil,
			}); err != nil {
				b.Logger().Error("failed to edit message", "error", err)
			}
			b.AnswerCallback(ctx, query.ID, "Session loaded", false)
		}
	case "delete":
		if len(parts) >= 4 {
			sessionID, _ := strconv.ParseInt(parts[2], 10, 64)
			page, _ := strconv.Atoi(parts[3])

			if err := b.Store().DeleteSession(chatID, sessionID); err != nil {
				b.AnswerCallback(ctx, query.ID, "❌ Failed to delete session", true)
				return
			}

			b.DeleteSessionCache(sessionID)

			b.AnswerCallback(ctx, query.ID, "✅ Session deleted", false)
			showSessionList(ctx, b, query, chatID, messageID, page)
		}
	}
}

func sessionListView(b BotContext, chatID int64, page int) (string, models.InlineKeyboardMarkup, error) {
	pageSize := sessionPageSize(b)
	if page < 0 {
		page = 0
	}

	sessions, err := b.Store().ListSessions(chatID, sessionFetchLimit(page, pageSize))
	if err != nil {
		return "", models.InlineKeyboardMarkup{}, err
	}

	if len(sessions) == 0 {
		return "No conversations found.", models.InlineKeyboardMarkup{}, nil
	}

	pageItems, page := paginateWithLimit(sessions, page, pageSize)
	rows := make([][]models.InlineKeyboardButton, 0, len(pageItems)+1)

	for _, s := range pageItems {
		label := s.Title
		rows = append(rows, []models.InlineKeyboardButton{{
			Text: label, CallbackData: fmt.Sprintf("session:view:%d:%d", s.ID, page),
		}})
	}

	rows = appendNavigationRowWithLimit(rows, page, len(sessions), pageSize, func(targetPage int) string {
		return fmt.Sprintf("session:list:%d", targetPage)
	})

	return "📂 Select a conversation session:", models.InlineKeyboardMarkup{InlineKeyboard: rows}, nil
}

func sessionPageSize(b BotContext) int {
	if params := b.Config(); params != nil && params.MaxSessionsDisplayed > 0 {
		return params.MaxSessionsDisplayed
	}
	return MaxItemsPerPage
}

func sessionFetchLimit(page int, pageSize int) int {
	if page < 0 {
		page = 0
	}
	if pageSize <= 0 {
		pageSize = MaxItemsPerPage
	}
	return (page+1)*pageSize + 1
}

func showSessionList(ctx context.Context, b BotContext, query *models.CallbackQuery, chatID int64, messageID int, page int) {
	text, keyboard, err := sessionListView(b, chatID, page)
	if err != nil {
		b.AnswerCallback(ctx, query.ID, "❌ Failed to load sessions", true)
		return
	}

	if _, err := b.Telegram().EditMessageText(ctx, &telegram.EditMessageTextParams{
		ChatID: chatID, MessageID: messageID, Text: text, ReplyMarkup: &keyboard,
	}); err != nil {
		b.Logger().Error("failed to update session menu", "error", err)
	}
	b.AnswerCallback(ctx, query.ID, "", false)
}

func showSessionMenu(ctx context.Context, b BotContext, query *models.CallbackQuery, chatID int64, messageID int, sessionID int64, page int) {
	text := "What would you like to do with this conversation?"
	keyboard := models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "📂 Load", CallbackData: fmt.Sprintf("session:load:%d", sessionID)},
				{Text: "🗑 Delete", CallbackData: fmt.Sprintf("session:delete:%d:%d", sessionID, page)},
			},
			{
				{Text: "⬅️ Back", CallbackData: fmt.Sprintf("session:list:%d", page)},
			},
		},
	}

	if _, err := b.Telegram().EditMessageText(ctx, &telegram.EditMessageTextParams{
		ChatID: chatID, MessageID: messageID, Text: text, ReplyMarkup: &keyboard,
	}); err != nil {
		b.Logger().Error("failed to update session menu", "error", err)
	}
	b.AnswerCallback(ctx, query.ID, "", false)
}

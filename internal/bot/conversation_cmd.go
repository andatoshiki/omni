package bot

import (
	"context"
	"fmt"
	"strconv"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (c *CommandHandler) Conversation(ctx context.Context, msg *models.Message) {
	text, keyboard, err := c.app.sessionListView(msg.Chat.ID, 0)
	if err != nil {
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return
	}

	if msg.Chat.ID >= 0 {
		_, _ = c.app.sendMessageWithKeyboard(ctx, msg.Chat.ID, text, &keyboard)
	} else {
		_, _ = c.app.sendReplyWithKeyboard(ctx, msg, text, &keyboard)
	}
}

func (c *CommandHandler) NewSession(ctx context.Context, msg *models.Message) {
	_, err := c.app.store.CreateNewSession(msg.Chat.ID, "New Session")
	if err != nil {
		_, _ = c.reply(ctx, msg, errorMessage(err))
		return
	}
	_, _ = c.reply(ctx, msg, "✅ A new conversation session has been started!")
}

func (a *App) sessionListView(chatID int64, page int) (string, models.InlineKeyboardMarkup, error) {
	// Let's get up to max + 1 to know if there's a next page
	limit := a.params.MaxSessionsDisplayed
	if limit == 0 {
		limit = 10
	}
	
	sessions, err := a.store.ListSessions(chatID, 100)
	if err != nil {
		return "", models.InlineKeyboardMarkup{}, err
	}

	if len(sessions) == 0 {
		return "No conversations found.", models.InlineKeyboardMarkup{}, nil
	}

	pageItems, page := paginate(sessions, page)
	rows := make([][]models.InlineKeyboardButton, 0, len(pageItems)+1)

	for _, s := range pageItems {
		label := s.Title
		rows = append(rows, []models.InlineKeyboardButton{{
			Text: label, CallbackData: fmt.Sprintf("session:view:%d:%d", s.ID, page),
		}})
	}

	rows = appendNavigationRow(rows, page, len(sessions), func(targetPage int) string {
		return fmt.Sprintf("session:list:%d", targetPage)
	})

	return "📂 Select a conversation session:", models.InlineKeyboardMarkup{InlineKeyboard: rows}, nil
}

func (a *App) showSessionList(ctx context.Context, query *models.CallbackQuery, chatID int64, messageID int, page int) {
	text, keyboard, err := a.sessionListView(chatID, page)
	if err != nil {
		a.answerCallback(ctx, query.ID, "❌ Failed to load sessions", true)
		return
	}
	
	if _, err := a.client.EditMessageText(ctx, &telegram.EditMessageTextParams{
		ChatID: chatID, MessageID: messageID, Text: text, ReplyMarkup: &keyboard,
	}); err != nil {
		a.logger.Error("failed to update session menu", "error", err)
	}
	a.answerCallback(ctx, query.ID, "", false)
}

func (a *App) showSessionMenu(ctx context.Context, query *models.CallbackQuery, chatID int64, messageID int, sessionID int64, page int) {
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
	
	if _, err := a.client.EditMessageText(ctx, &telegram.EditMessageTextParams{
		ChatID: chatID, MessageID: messageID, Text: text, ReplyMarkup: &keyboard,
	}); err != nil {
		a.logger.Error("failed to update session menu", "error", err)
	}
	a.answerCallback(ctx, query.ID, "", false)
}

func (a *App) handleSessionCallback(ctx context.Context, query *models.CallbackQuery, chatID int64, messageID int, parts []string) {
	if len(parts) < 2 {
		return
	}
	action := parts[1]

	switch action {
	case "list":
		if len(parts) >= 3 {
			page, _ := strconv.Atoi(parts[2])
			a.showSessionList(ctx, query, chatID, messageID, page)
		}
	case "view":
		if len(parts) >= 4 {
			sessionID, _ := strconv.ParseInt(parts[2], 10, 64)
			page, _ := strconv.Atoi(parts[3])
			a.showSessionMenu(ctx, query, chatID, messageID, sessionID, page)
		}
	case "load":
		if len(parts) >= 3 {
			sessionID, _ := strconv.ParseInt(parts[2], 10, 64)
			if err := a.store.SetActiveSession(chatID, sessionID); err != nil {
				a.answerCallback(ctx, query.ID, "❌ Failed to load session", true)
				return
			}
			
			text := "✅ Session loaded successfully."
			if _, err := a.client.EditMessageText(ctx, &telegram.EditMessageTextParams{
				ChatID: chatID, MessageID: messageID, Text: text, ReplyMarkup: nil,
			}); err != nil {
				a.logger.Error("failed to edit message", "error", err)
			}
			a.answerCallback(ctx, query.ID, "Session loaded", false)
		}
	case "delete":
		if len(parts) >= 4 {
			sessionID, _ := strconv.ParseInt(parts[2], 10, 64)
			page, _ := strconv.Atoi(parts[3])
			
			if err := a.store.DeleteSession(sessionID); err != nil {
				a.answerCallback(ctx, query.ID, "❌ Failed to delete session", true)
				return
			}
			
			// Also remove from cache
			a.commands.msgHistory.Delete(sessionID)

			a.answerCallback(ctx, query.ID, "✅ Session deleted", false)
			a.showSessionList(ctx, query, chatID, messageID, page)
		}
	}
}

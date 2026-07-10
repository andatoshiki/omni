package command

import (
	"context"
	"log/slog"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/providers"
	"github.com/andatoshiki/omni/internal/storage"
)

// HandlerFunc is the command entrypoint used by the bot router.
type HandlerFunc func(ctx context.Context, b BotContext, msg *models.Message)

// BotContext exposes the bot engine capabilities needed by command handlers.
type BotContext interface {
	Store() storage.Store
	Config() *config.Params
	Providers() *providers.Registry
	Telegram() *telegram.Bot
	Logger() *slog.Logger

	Reply(ctx context.Context, msg *models.Message, text string) (*models.Message, error)
	SendMessage(ctx context.Context, chatID int64, text string) (*models.Message, error)
	SendMessageWithKeyboard(ctx context.Context, chatID int64, text string, keyboard *models.InlineKeyboardMarkup) (*models.Message, error)
	SendReplyToMessage(ctx context.Context, msg *models.Message, text string) (*models.Message, error)
	SendReplyWithKeyboard(ctx context.Context, msg *models.Message, text string, keyboard *models.InlineKeyboardMarkup) (*models.Message, error)

	ClearConversation(chatID int64) error
	CurrentModel(chatID int64) providers.ModelID
	MessageLogAttrs(msg *models.Message) []any
	DeleteSessionCache(sessionID int64)
	AnswerCallback(ctx context.Context, queryID, text string, showAlert bool)
}

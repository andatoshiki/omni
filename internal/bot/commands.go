package bot

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/command"
	"github.com/andatoshiki/omni/internal/config"
	"github.com/andatoshiki/omni/internal/providers"
	"github.com/andatoshiki/omni/internal/storage"
)

type Route struct {
	Handler     command.HandlerFunc
	Description string
	Hidden      bool // True if we shouldn't show it in the Telegram menu
}

type CommandHandler struct {
	app        *App
	msgHistory sync.Map
	chatLocks  sync.Map
	routes     map[string]Route
}

var _ command.BotContext = (*CommandHandler)(nil)

func NewCommandHandler(app *App) *CommandHandler {
	c := &CommandHandler{
		app:    app,
		routes: make(map[string]Route),
	}

	c.routes["ping"] = Route{Handler: command.Ping, Description: "Check bot latency"}
	c.routes["version"] = Route{Handler: command.Version, Description: "Show bot version"}
	c.routes["model"] = Route{Handler: command.Model, Description: "Select AI model"}
	c.routes["clear"] = Route{Handler: command.Clear, Description: "Clear AI chat context"}
	c.routes["new"] = Route{Handler: command.NewSession, Description: "Start a new conversation session"}
	c.routes["usage"] = Route{Handler: command.Usage, Description: "Show token usage"}
	c.routes["conversation"] = Route{Handler: command.Conversation, Description: "Manage conversation sessions"}
	c.routes["setprompt"] = Route{Handler: command.SetPrompt, Description: "Set a custom system prompt"}
	c.routes["clearprompt"] = Route{Handler: command.ClearPrompt, Description: "Clear the custom prompt"}
	c.routes["export"] = Route{Handler: command.Export, Description: "Export conversation data"}
	c.routes["summary"] = Route{Handler: command.Summary, Description: "Summarize recent text messages"}
	c.routes["help"] = Route{Handler: command.Help, Description: "Show help message"}
	c.routes["start"] = Route{Handler: command.Start, Hidden: true}

	return c
}

func (c *CommandHandler) Store() storage.Store {
	return c.app.store
}

func (c *CommandHandler) Config() *config.Params {
	return c.app.params
}

func (c *CommandHandler) Providers() *providers.Registry {
	return c.app.providers
}

func (c *CommandHandler) Telegram() *telegram.Bot {
	return c.app.client
}

func (c *CommandHandler) Logger() *slog.Logger {
	return c.app.logger
}

func (c *CommandHandler) MessageLogAttrs(msg *models.Message) []any {
	return c.app.messageLogAttrs(msg)
}

func (c *CommandHandler) SendMessage(ctx context.Context, chatID int64, text string) (*models.Message, error) {
	return c.app.sendMessage(ctx, chatID, text)
}

func (c *CommandHandler) SendMessageWithKeyboard(ctx context.Context, chatID int64, text string, keyboard *models.InlineKeyboardMarkup) (*models.Message, error) {
	return c.app.sendMessageWithKeyboard(ctx, chatID, text, keyboard)
}

func (c *CommandHandler) SendReplyToMessage(ctx context.Context, msg *models.Message, text string) (*models.Message, error) {
	return c.app.sendReplyToMessage(ctx, msg, text)
}

func (c *CommandHandler) SendReplyWithKeyboard(ctx context.Context, msg *models.Message, text string, keyboard *models.InlineKeyboardMarkup) (*models.Message, error) {
	return c.app.sendReplyWithKeyboard(ctx, msg, text, keyboard)
}

func (c *CommandHandler) Reply(ctx context.Context, msg *models.Message, text string) (replyMsg *models.Message, err error) {
	return c.reply(ctx, msg, text)
}

func (c *CommandHandler) AnswerCallback(ctx context.Context, queryID, text string, showAlert bool) {
	c.app.answerCallback(ctx, queryID, text, showAlert)
}

func (c *CommandHandler) DeleteSessionCache(sessionID int64) {
	c.msgHistory.Delete(sessionID)
}

func (c *CommandHandler) lockChat(chatID int64) func() {
	value, _ := c.chatLocks.LoadOrStore(chatID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}

func (c *CommandHandler) ClearConversation(chatID int64) error {
	unlock := c.lockChat(chatID)
	defer unlock()
	if err := c.app.store.ClearSessions(chatID); err != nil {
		return err
	}
	// Session history is keyed by session ID, while clearing happens by chat ID.
	// Existing cache entries become harmlessly unreachable after new sessions load.
	return nil
}

func (c *CommandHandler) reply(ctx context.Context, msg *models.Message, text string) (replyMsg *models.Message, err error) {
	if msg == nil {
		return nil, errors.New("cannot reply to a nil message")
	}
	if msg.Chat.ID >= 0 {
		return c.app.sendMessage(ctx, msg.Chat.ID, text)
	}
	return c.app.sendReplyToMessage(ctx, msg, text)
}

func (c *CommandHandler) editReply(ctx context.Context, msg *models.Message, replyMsg *models.Message, text string) (replyMessage *models.Message, err error) {
	if replyMsg == nil || msg == nil {
		return c.reply(ctx, msg, text)
	}

	return c.app.editReplyToMessage(ctx, replyMsg, text)
}

func (c *CommandHandler) CurrentModel(chatID int64) providers.ModelID {
	if selected, ok := c.app.store.LoadChatModel(chatID); ok {
		if _, err := c.app.providers.Resolve(selected); err == nil {
			return selected
		}
	}
	return c.app.providers.DefaultModelID()
}

func (c *CommandHandler) currentModel(chatID int64) providers.ModelID {
	return c.CurrentModel(chatID)
}

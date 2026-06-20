package bot

import (
	"context"
	"errors"
	"slices"
	"strings"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (a *App) handleMessage(ctx context.Context, update *models.Update) {
	msg := update.Message
	if msg == nil || (msg.Text == "" && len(msg.Photo) == 0 && msg.Voice == nil && msg.Audio == nil && msg.Video == nil && msg.VideoNote == nil) {
		return
	}
	a.logger.Info("telegram message received", a.messageLogAttrs(msg)...)

	if !a.messageAllowed(msg) {
		if msg.Chat.ID >= 0 {
			a.logger.Warn("telegram message ignored: user not allowed", a.messageLogAttrs(msg)...)
		} else {
			a.logger.Warn("telegram message ignored: group not allowed", a.messageLogAttrs(msg)...)
		}
		return
	}

	commandText := msg.Text
	if commandText == "" && (len(msg.Photo) > 0 || msg.Voice != nil || msg.Audio != nil || msg.Video != nil || msg.VideoNote != nil) {
		commandText = msg.Caption
	}
	if commandText != "" && (commandText[0] == '/' || commandText[0] == '!') {
		msg.Text = commandText
		a.routeCommand(ctx, msg)
		return
	}
	if prompt, mentioned := stripBotMention(commandText, a.botUsername); mentioned {
		msg.Text = prompt
		if len(msg.Photo) > 0 || msg.Voice != nil || msg.Audio != nil || msg.Video != nil || msg.VideoNote != nil {
			msg.Caption = prompt
		}
		a.commands.Chat(ctx, msg)
		return
	}

	if msg.Chat.ID >= 0 || replyTargetsBot(msg, a.client.ID()) {
		a.commands.Chat(ctx, msg)
	}
}

func (a *App) messageAllowed(msg *models.Message) bool {
	if msg.Chat.ID >= 0 {
		return msg.From != nil && slices.Contains(a.params.AllowedUserIDs, msg.From.ID)
	}
	return slices.Contains(a.params.AllowedGroupIDs, msg.Chat.ID)
}

func stripBotMention(text, botUsername string) (string, bool) {
	botUsername = strings.TrimPrefix(strings.TrimSpace(botUsername), "@")
	if botUsername == "" {
		return text, false
	}

	mention := "@" + botUsername
	if len(text) < len(mention) || !strings.EqualFold(text[:len(mention)], mention) {
		return text, false
	}
	if len(text) > len(mention) && isTelegramUsernameCharacter(text[len(mention)]) {
		return text, false
	}
	return strings.TrimSpace(text[len(mention):]), true
}

func isTelegramUsernameCharacter(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_'
}

func replyTargetsBot(msg *models.Message, botID int64) bool {
	return msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil && msg.ReplyToMessage.From.ID == botID
}

func (a *App) routeCommand(ctx context.Context, msg *models.Message) {
	commandToken := strings.Fields(msg.Text)[0]
	commandToken = strings.SplitN(commandToken, "@", 2)[0]
	prefix := string(commandToken[0])
	command := strings.TrimPrefix(commandToken, prefix)
	msg.Text = strings.TrimSpace(strings.TrimPrefix(msg.Text, strings.Fields(msg.Text)[0]))
	if len(msg.Photo) > 0 || msg.Voice != nil || msg.Audio != nil || msg.Video != nil || msg.VideoNote != nil {
		msg.Caption = msg.Text
	}
	attrs := append(a.messageLogAttrs(msg), "command", command, "command_prefix", prefix)

	route := func(handler string) {
		a.logger.Info("telegram command routed", append(attrs, "handler", handler)...)
	}
	switch command {
	case "ping":
		route("ping")
		a.commands.Ping(ctx, msg)
	case "model":
		route("model")
		a.commands.Model(ctx, msg)
	case "usage", "dsusage":
		route("usage")
		a.commands.Usage(ctx, msg)
	case "help", "dshelp":
		route("help")
		a.commands.Help(ctx, msg, prefix)
	case "clear", "dsclear":
		route("clear")
		if err := a.commands.ClearConversation(msg.Chat.ID); err != nil {
			_, _ = a.commands.reply(ctx, msg, errorMessage(err))
			return
		}
		_, _ = a.commands.reply(ctx, msg, "✅ Conversation history cleared")
	case "export", "dsexport":
		route("export")
		if err := a.store.ExportMemory("memory_export.json"); err != nil {
			_, _ = a.commands.reply(ctx, msg, errorMessage(err))
			return
		}
		_, _ = a.commands.reply(ctx, msg, "✅ Memory exported to memory_export.json")
	case "setprompt", "dssetprompt":
		route("setprompt")
		a.commands.SetPrompt(ctx, msg)
	case "clearprompt", "dsclearprompt":
		route("clearprompt")
		a.commands.ClearPrompt(ctx, msg)
	case "start":
		route("start")
		if msg.Chat.ID >= 0 {
			_, _ = a.sendReplyToMessage(ctx, msg, "🤖 Welcome! Send me a message or use /help to see available commands.")
		}
	default:
		a.logger.Warn("telegram command ignored: invalid command", attrs...)
		if msg.Chat.ID >= 0 {
			_, _ = a.sendReplyToMessage(ctx, msg, errorMessage(errors.New("invalid command")))
		}
	}
}

func (a *App) updateHandler(ctx context.Context, _ *telegram.Bot, update *models.Update) {
	if update.CallbackQuery != nil {
		a.handleCallbackQuery(ctx, update.CallbackQuery)
		return
	}
	a.handleMessage(ctx, update)
}

func (a *App) registerCommands(ctx context.Context) {
	commands := []models.BotCommand{
		{Command: "ping", Description: "Check bot latency"},
		{Command: "model", Description: "Select AI model"},
		{Command: "clear", Description: "Clear conversation history"},
		{Command: "usage", Description: "Show token usage"},
		{Command: "setprompt", Description: "Set a custom system prompt"},
		{Command: "clearprompt", Description: "Clear the custom prompt"},
		{Command: "export", Description: "Export conversation data"},
		{Command: "help", Description: "Show help message"},
	}
	_, err := a.client.SetMyCommands(ctx, &telegram.SetMyCommandsParams{Commands: commands})
	if err != nil {
		a.logger.Warn("failed to register bot commands", "error", err)
		return
	}
	a.logger.Info("bot commands registered", "count", len(commands))
}

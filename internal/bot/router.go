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

	if msg.MediaGroupID != "" {
		a.mediaAggregator.Add(ctx, msg)
		return
	}

	a.processMessages(ctx, msg)
}

func (a *App) processMessages(ctx context.Context, msgs ...*models.Message) {
	if len(msgs) == 0 {
		return
	}
	msg := msgs[0]

	commandText := msg.Text
	if commandText == "" {
		for _, m := range msgs {
			if m.Caption != "" {
				commandText = m.Caption
				break
			}
		}
	}

	if commandText != "" && (commandText[0] == '/' || commandText[0] == '!') {
		msg.Text = commandText
		a.routeCommand(ctx, msg)
		return
	}
	if prompt, mentioned := stripBotMention(commandText, a.botUsername); mentioned {
		msg.Text = prompt
		msg.Caption = prompt
		a.commands.Chat(ctx, msgs...)
		return
	}

	if msg.Chat.ID >= 0 || replyTargetsBot(msg, a.client.ID()) {
		a.commands.Chat(ctx, msgs...)
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

	if route, exists := a.commands.routes[command]; exists {
		a.logger.Info("telegram command routed", append(attrs, "handler", command)...)
		route.Handler(ctx, msg)
		return
	}

	a.logger.Warn("telegram command ignored: invalid command", attrs...)
	if msg.Chat.ID >= 0 {
		_, _ = a.sendReplyToMessage(ctx, msg, errorMessage(errors.New("invalid command")))
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
	var commands []models.BotCommand
	for cmd, route := range a.commands.routes {
		if !route.Hidden {
			commands = append(commands, models.BotCommand{
				Command:     cmd,
				Description: route.Description,
			})
		}
	}
	// Sort to keep the menu predictable
	slices.SortFunc(commands, func(a, b models.BotCommand) int {
		return strings.Compare(a.Command, b.Command)
	})

	_, err := a.client.SetMyCommands(ctx, &telegram.SetMyCommandsParams{Commands: commands})
	if err != nil {
		a.logger.Warn("failed to register bot commands", "error", err)
		return
	}
	a.logger.Info("bot commands registered", "count", len(commands))
}

package command

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/storage"
)

func AddUser(ctx context.Context, b BotContext, msg *models.Message) {
	manageUserAccess(ctx, b, msg, true)
}

func DeleteUser(ctx context.Context, b BotContext, msg *models.Message) {
	manageUserAccess(ctx, b, msg, false)
}

func manageUserAccess(ctx context.Context, b BotContext, msg *models.Message, allow bool) {
	commandName := "addusr"
	if !allow {
		commandName = "delusr"
	}
	if msg == nil || msg.From == nil || !b.IsAdmin(msg.From.ID) {
		if msg != nil {
			b.Logger().Warn("telegram user access change denied", append(b.MessageLogAttrs(msg), "command", commandName)...)
			_, _ = b.Reply(ctx, msg, "❌ Only the configured administrator can manage users.")
		}
		return
	}

	arguments := strings.Fields(msg.Text)
	reference, ok := accessTargetReference(msg, arguments)
	if !ok {
		_, _ = b.Reply(ctx, msg, fmt.Sprintf("Usage: /%s <@username|user_id>, or reply to a user's message with /%s", commandName, commandName))
		return
	}

	var user storage.TelegramUser
	var changed bool
	var err error
	if allow {
		user, changed, err = b.AddAllowedUser(reference)
	} else {
		user, changed, err = b.DeleteAllowedUser(reference)
	}
	if err != nil {
		b.Logger().Warn("telegram user access change failed", append(b.MessageLogAttrs(msg), "command", commandName, "target", reference, "error", err)...)
		_, _ = b.Reply(ctx, msg, "❌ "+err.Error())
		return
	}

	target := formatAccessUser(user)
	if !changed {
		if allow {
			_, _ = b.Reply(ctx, msg, "ℹ️ "+target+" is already allowed.")
		} else {
			_, _ = b.Reply(ctx, msg, "ℹ️ "+target+" is already not allowed.")
		}
		return
	}
	if allow {
		b.Logger().Info("telegram user allowed", append(b.MessageLogAttrs(msg), "target_user_id", user.UserID, "target_username", user.Username)...)
		_, _ = b.Reply(ctx, msg, "✅ Allowed "+target+".")
		return
	}
	b.Logger().Info("telegram user removed", append(b.MessageLogAttrs(msg), "target_user_id", user.UserID, "target_username", user.Username)...)
	_, _ = b.Reply(ctx, msg, "✅ Removed "+target+".")
}

func accessTargetReference(msg *models.Message, arguments []string) (string, bool) {
	if len(arguments) == 1 {
		return arguments[0], true
	}
	if len(arguments) != 0 || msg.ReplyToMessage == nil || msg.ReplyToMessage.From == nil || msg.ReplyToMessage.From.ID <= 0 {
		return "", false
	}
	return strconv.FormatInt(msg.ReplyToMessage.From.ID, 10), true
}

func formatAccessUser(user storage.TelegramUser) string {
	id := strconv.FormatInt(user.UserID, 10)
	if user.Username == "" {
		return id
	}
	return "@" + user.Username + " (" + id + ")"
}

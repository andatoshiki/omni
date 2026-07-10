package command

import (
	"context"
	"os"
	"slices"
	"time"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func Export(ctx context.Context, b BotContext, msg *models.Message) {
	params := b.Config()
	if !CanExport(msg, params.AllowedUserIDs, params.AdminUserIDs) {
		b.Logger().Warn("memory export denied", b.MessageLogAttrs(msg)...)
		_, _ = b.Reply(ctx, msg, "❌ You are not authorized to export conversation data")
		return
	}

	filename := time.Now().Format("2006-01-02-15-04-05") + "-memory-export.json"
	if err := b.Store().ExportMemory(filename); err != nil {
		_, _ = b.Reply(ctx, msg, errorMessage(err))
		return
	}
	_, _ = b.Reply(ctx, msg, "✅ Memory exported successfully.")

	file, err := os.Open(filename)
	if err != nil {
		b.Logger().Error("failed to open export file", "error", err)
		return
	}
	defer file.Close()
	defer os.Remove(filename)

	_, err = b.Telegram().SendDocument(ctx, &telegram.SendDocumentParams{
		ChatID: msg.Chat.ID,
		Document: &models.InputFileUpload{
			Filename: filename,
			Data:     file,
		},
	})
	if err != nil {
		b.Logger().Error("failed to send export document", "error", err)
	}
}

func CanExport(msg *models.Message, allowedUserIDs, adminUserIDs []int64) bool {
	if msg == nil || msg.From == nil {
		return false
	}
	return slices.Contains(allowedUserIDs, msg.From.ID) || slices.Contains(adminUserIDs, msg.From.ID)
}

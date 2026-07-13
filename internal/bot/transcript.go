package bot

import (
	"strings"

	"github.com/go-telegram/bot/models"

	"github.com/andatoshiki/omni/internal/providers"
	"github.com/andatoshiki/omni/internal/storage"
)

func telegramMessageText(msg *models.Message) string {
	if msg == nil {
		return ""
	}
	if text := strings.TrimSpace(msg.Text); text != "" {
		return text
	}
	return strings.TrimSpace(msg.Caption)
}

func isTelegramCommandText(text string) bool {
	text = strings.TrimSpace(text)
	return text != "" && (text[0] == '/' || text[0] == '!')
}

func isRoutableTelegramMessage(msg *models.Message) bool {
	if msg == nil {
		return false
	}
	return msg.Text != "" || len(msg.Photo) > 0 || msg.Voice != nil || msg.Audio != nil ||
		msg.Video != nil || msg.VideoNote != nil || msg.Document != nil
}

func (a *App) saveIncomingTranscriptMessage(msg *models.Message) {
	text := telegramMessageText(msg)
	if text == "" || isTelegramCommandText(text) {
		return
	}

	sender := ""
	if speaker := ExtractSpeaker(msg); speaker != nil {
		sender = speaker.DisplayName
	}
	if err := a.store.SaveTranscriptMessage(storage.TranscriptMessage{
		ChatID:    msg.Chat.ID,
		ThreadID:  msg.MessageThreadID,
		MessageID: msg.ID,
		Role:      providers.RoleUser,
		Sender:    sender,
		Text:      text,
	}); err != nil {
		a.logger.Warn("failed to save incoming summary transcript message", append(a.messageLogAttrs(msg), "error", err)...)
	}
}

func (c *CommandHandler) saveAssistantTranscriptMessage(source, sent *models.Message, text string) {
	if source == nil || sent == nil || strings.TrimSpace(text) == "" {
		return
	}
	threadID := sent.MessageThreadID
	if threadID == 0 {
		threadID = source.MessageThreadID
	}
	if err := c.app.store.SaveTranscriptMessage(storage.TranscriptMessage{
		ChatID:    source.Chat.ID,
		ThreadID:  threadID,
		MessageID: sent.ID,
		Role:      providers.RoleAssistant,
		Sender:    c.app.botUsername,
		Text:      text,
	}); err != nil {
		c.app.logger.Warn("failed to save outgoing summary transcript message", append(c.app.messageLogAttrs(sent), "error", err)...)
	}
}

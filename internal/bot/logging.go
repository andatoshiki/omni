package bot

import (
	"unicode/utf8"

	"github.com/go-telegram/bot/models"
)

// messageLogAttrs intentionally records metadata only. Message bodies and AI
// responses can be large and sensitive, so they never go to routine logs.
func (a *App) messageLogAttrs(msg *models.Message) []any {
	if msg == nil {
		return []any{"message_nil", true}
	}
	attrs := []any{
		"chat_id", msg.Chat.ID,
		"chat_type", msg.Chat.Type,
		"message_id", msg.ID,
		"thread_id", msg.MessageThreadID,
		"text_chars", utf8.RuneCountInString(msg.Text),
		"text_bytes", len(msg.Text),
		"caption_chars", utf8.RuneCountInString(msg.Caption),
		"caption_bytes", len(msg.Caption),
		"photo_sizes", len(msg.Photo),
	}
	if msg.From != nil {
		attrs = append(attrs, "user_id", msg.From.ID, "username", msg.From.Username)
	}
	return attrs
}

package bot

import (
	"strings"
	"unicode/utf16"

	"github.com/go-telegram/bot/models"
	"github.com/andatoshiki/omni/internal/conversation"
)

// ExtractSpeaker creates a Speaker object from a Telegram message sender.
func ExtractSpeaker(msg *models.Message) *conversation.Speaker {
	if msg == nil {
		return nil
	}

	if msg.SenderChat != nil {
		return &conversation.Speaker{
			UserID:      msg.SenderChat.ID,
			DisplayName: msg.SenderChat.Title,
			Username:    msg.SenderChat.Username,
		}
	}

	if msg.From != nil {
		name := strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName)
		if name == "" {
			name = msg.From.Username
		}
		if name == "" {
			name = "unknown participant"
		}

		return &conversation.Speaker{
			UserID:      msg.From.ID,
			DisplayName: name,
			Username:    msg.From.Username,
		}
	}

	return nil
}

type Mention struct {
	Username    string
	DisplayName string
}

// ExtractMentions extracts text_mention and mention entities from the message.
func ExtractMentions(msg *models.Message) []Mention {
	if msg == nil {
		return nil
	}

	entities := msg.Entities
	text := msg.Text
	if len(entities) == 0 && len(msg.CaptionEntities) > 0 {
		entities = msg.CaptionEntities
		text = msg.Caption
	}

	if len(entities) == 0 {
		return nil
	}

	encodedText := utf16.Encode([]rune(text))
	var mentions []Mention

	for _, ent := range entities {
		if ent.Type == "mention" {
			if ent.Offset+ent.Length <= len(encodedText) {
				mentionText := string(utf16.Decode(encodedText[ent.Offset : ent.Offset+ent.Length]))
				mentionText = strings.TrimPrefix(mentionText, "@")
				mentions = append(mentions, Mention{
					Username: mentionText,
				})
			}
		} else if ent.Type == "text_mention" && ent.User != nil {
			name := strings.TrimSpace(ent.User.FirstName + " " + ent.User.LastName)
			if name == "" {
				name = ent.User.Username
			}
			mentions = append(mentions, Mention{
				Username:    ent.User.Username,
				DisplayName: name,
			})
		}
	}

	return mentions
}

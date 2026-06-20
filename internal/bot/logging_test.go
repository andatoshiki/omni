package bot

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestMessageLogAttrsExcludeMessageBody(t *testing.T) {
	const secret = "message-body-must-not-be-logged"
	app := &App{}
	attrs := app.messageLogAttrs(&models.Message{
		ID:   7,
		Chat: models.Chat{ID: 42, Type: models.ChatTypePrivate},
		Text: secret,
	})
	if strings.Contains(fmt.Sprint(attrs), secret) {
		t.Fatalf("messageLogAttrs() leaked message body: %#v", attrs)
	}
}

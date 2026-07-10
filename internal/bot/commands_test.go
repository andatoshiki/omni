package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/andatoshiki/omni/internal/command"
	"github.com/go-telegram/bot/models"
)

func TestPingReplyText(t *testing.T) {
	messageTime := time.Unix(1_700_000_000, 0)
	msg := &models.Message{Date: int(messageTime.Unix())}

	got := command.PingReplyText(msg, messageTime.Add(1234*time.Millisecond))
	if got != "Pong! 1234ms" {
		t.Fatalf("pingReplyText() = %q, want %q", got, "Pong! 1234ms")
	}
}

func TestPingReplyTextClampsInvalidLatency(t *testing.T) {
	messageTime := time.Unix(1_700_000_000, 0)
	msg := &models.Message{Date: int(messageTime.Unix())}

	got := command.PingReplyText(msg, messageTime.Add(-time.Second))
	if got != "Pong! 0ms" {
		t.Fatalf("pingReplyText() = %q, want %q", got, "Pong! 0ms")
	}
}

func TestCanExport(t *testing.T) {
	tests := []struct {
		name string
		msg  *models.Message
		want bool
	}{
		{name: "allowed user", msg: &models.Message{From: &models.User{ID: 101}}, want: true},
		{name: "administrator", msg: &models.Message{From: &models.User{ID: 202}}, want: true},
		{name: "group-only user", msg: &models.Message{From: &models.User{ID: 303}}, want: false},
		{name: "missing sender", msg: &models.Message{}, want: false},
		{name: "missing message", msg: nil, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := command.CanExport(test.msg, []int64{101}, []int64{202})
			if got != test.want {
				t.Fatalf("canExport() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCommandRoutesHaveNoDeepSeekAliases(t *testing.T) {
	handler := NewCommandHandler(nil)
	for command := range handler.routes {
		if strings.HasPrefix(command, "ds") {
			t.Fatalf("legacy DeepSeek command alias remains registered: %q", command)
		}
	}
}

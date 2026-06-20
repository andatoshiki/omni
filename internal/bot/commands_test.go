package bot

import (
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

func TestPingReplyText(t *testing.T) {
	messageTime := time.Unix(1_700_000_000, 0)
	msg := &models.Message{Date: int(messageTime.Unix())}

	got := pingReplyText(msg, messageTime.Add(1234*time.Millisecond))
	if got != "Pong! 1234ms" {
		t.Fatalf("pingReplyText() = %q, want %q", got, "Pong! 1234ms")
	}
}

func TestPingReplyTextClampsInvalidLatency(t *testing.T) {
	messageTime := time.Unix(1_700_000_000, 0)
	msg := &models.Message{Date: int(messageTime.Unix())}

	got := pingReplyText(msg, messageTime.Add(-time.Second))
	if got != "Pong! 0ms" {
		t.Fatalf("pingReplyText() = %q, want %q", got, "Pong! 0ms")
	}
}

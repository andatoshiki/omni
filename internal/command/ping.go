package command

import (
	"context"
	"fmt"
	"time"

	"github.com/go-telegram/bot/models"
)

func Ping(ctx context.Context, b BotContext, msg *models.Message) {
	_, _ = b.Reply(ctx, msg, PingReplyText(msg, time.Now()))
}

func PingReplyText(msg *models.Message, now time.Time) string {
	if msg == nil || msg.Date <= 0 {
		return "Pong! 0ms"
	}

	latency := now.Sub(time.Unix(int64(msg.Date), 0))
	if latency < 0 {
		latency = 0
	}
	return fmt.Sprintf("Pong! %dms", latency.Milliseconds())
}

package bot

import (
	"context"
	"strings"

	telegram "github.com/go-telegram/bot"
)

func (a *App) preparePolling(ctx context.Context) {
	webhook, err := a.client.GetWebhookInfo(ctx)
	if err != nil {
		a.logger.Warn("failed to inspect telegram webhook before polling", "error", err)
		return
	}

	attrs := []any{
		"webhook_active", webhook.URL != "",
		"pending_update_count", webhook.PendingUpdateCount,
	}
	if len(webhook.AllowedUpdates) > 0 {
		attrs = append(attrs, "webhook_allowed_updates", strings.Join(webhook.AllowedUpdates, ","))
	}
	if webhook.LastErrorMessage != "" {
		attrs = append(attrs, "last_webhook_error", webhook.LastErrorMessage)
	}

	if webhook.URL == "" {
		a.logger.Info("telegram webhook status", attrs...)
		return
	}

	a.logger.Warn("telegram webhook active before polling; deleting webhook", attrs...)
	deleted, err := a.client.DeleteWebhook(ctx, &telegram.DeleteWebhookParams{DropPendingUpdates: false})
	if err != nil {
		a.logger.Error("failed to delete telegram webhook before polling", "error", err)
		return
	}
	if !deleted {
		a.logger.Warn("telegram webhook delete returned false")
		return
	}

	a.logger.Info("telegram webhook deleted before polling", "drop_pending_updates", false)
}

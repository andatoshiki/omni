package bot

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	telegram "github.com/go-telegram/bot"
)

type webhookHTTPClient struct {
	webhookURL string
	methods    []string
}

func (c *webhookHTTPClient) Do(r *http.Request) (*http.Response, error) {
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	c.methods = append(c.methods, method)

	switch method {
	case "getWebhookInfo":
		body := fmt.Sprintf(
			`{"ok":true,"result":{"url":%q,"pending_update_count":3,"allowed_updates":["callback_query"]}}`,
			c.webhookURL,
		)
		return telegramJSONResponse(body), nil
	case "deleteWebhook":
		return telegramJSONResponse(`{"ok":true,"result":true}`), nil
	default:
		return nil, fmt.Errorf("unexpected Telegram method %q", method)
	}
}

func TestPreparePollingDeletesActiveWebhook(t *testing.T) {
	httpClient := &webhookHTTPClient{webhookURL: "https://example.com/telegram"}
	app := newPollingTestApp(t, httpClient)

	app.preparePolling(context.Background())

	if !slices.Equal(httpClient.methods, []string{"getWebhookInfo", "deleteWebhook"}) {
		t.Fatalf("telegram methods = %v, want getWebhookInfo then deleteWebhook", httpClient.methods)
	}
}

func TestPreparePollingLeavesInactiveWebhookAlone(t *testing.T) {
	httpClient := &webhookHTTPClient{}
	app := newPollingTestApp(t, httpClient)

	app.preparePolling(context.Background())

	if !slices.Equal(httpClient.methods, []string{"getWebhookInfo"}) {
		t.Fatalf("telegram methods = %v, want only getWebhookInfo", httpClient.methods)
	}
}

func newPollingTestApp(t *testing.T, httpClient telegram.HttpClient) *App {
	t.Helper()

	client, err := telegram.New(
		"test-token",
		telegram.WithHTTPClient(time.Second, httpClient),
		telegram.WithSkipGetMe(),
	)
	if err != nil {
		t.Fatalf("telegram.New() error = %v", err)
	}
	return &App{
		client: client,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func telegramJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

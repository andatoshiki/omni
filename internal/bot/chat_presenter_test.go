package bot

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type presenterTelegramClient struct {
	mu       sync.Mutex
	requests []recordedTelegramRequest
}

func (c *presenterTelegramClient) Do(request *http.Request) (*http.Response, error) {
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		return nil, err
	}
	form := make(map[string]string)
	for _, key := range []string{"chat_id", "message_thread_id", "reply_parameters", "message_id", "text", "parse_mode", "draft_id", "rich_message", "can_stop"} {
		form[key] = request.FormValue(key)
	}
	c.mu.Lock()
	c.requests = append(c.requests, recordedTelegramRequest{method: request.URL.Path, form: form})
	c.mu.Unlock()

	status := http.StatusOK
	body := `{"ok":true,"result":true}`
	if strings.HasSuffix(request.URL.Path, "/sendMessage") ||
		strings.HasSuffix(request.URL.Path, "/editMessageText") {
		chatID, _ := strconv.ParseInt(request.FormValue("chat_id"), 10, 64)
		body = fmt.Sprintf(`{"ok":true,"result":{"message_id":99,"message_thread_id":42,"chat":{"id":%d,"type":"private"},"text":"preview"}}`, chatID)
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

func (c *presenterTelegramClient) snapshot() []recordedTelegramRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]recordedTelegramRequest(nil), c.requests...)
}

func newPresenterTestHandler(t *testing.T, httpClient *presenterTelegramClient) *CommandHandler {
	t.Helper()
	client, err := telegram.New(
		"test-token",
		telegram.WithHTTPClient(time.Second, httpClient),
		telegram.WithSkipGetMe(),
	)
	if err != nil {
		t.Fatalf("telegram.New() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewCommandHandler(&App{client: client, logger: logger, store: &transcriptCaptureStore{}})
}

func TestPrivatePresenterReplacesExpandableCallout(t *testing.T) {
	recorder := &presenterTelegramClient{}
	handler := newPresenterTestHandler(t, recorder)
	message := &models.Message{ID: 17, MessageThreadID: 42, Chat: models.Chat{ID: 123, Type: models.ChatTypePrivate}}
	presenter := newChatStreamPresenter(handler, context.Background(), message)

	presenter.start()
	presenter.lastUpdate = time.Now().Add(-2 * time.Second)
	presenter.appendReasoning("check <unsafe> safely")
	presenter.showAnswer("Final <answer>")
	presenter.finish("Final answer")

	requests := recorder.snapshot()
	if len(requests) != 4 {
		t.Fatalf("request count = %d, want 4: %#v", len(requests), requests)
	}
	if !strings.HasSuffix(requests[0].method, "/sendMessage") || !strings.Contains(requests[0].form["text"], "<blockquote expandable>") {
		t.Fatalf("initial private callout = %#v", requests[0])
	}
	assertThinkingCalloutSpacing(t, requests[0].form["text"])
	if requests[0].form["message_thread_id"] != "42" || requests[0].form["parse_mode"] != "HTML" {
		t.Fatalf("private thread metadata = %#v", requests[0].form)
	}
	if !strings.Contains(requests[1].form["text"], "check &lt;unsafe&gt; safely") {
		t.Fatalf("reasoning callout = %q", requests[1].form["text"])
	}
	assertThinkingCalloutSpacing(t, requests[1].form["text"])
	if strings.Contains(requests[2].form["text"], "blockquote") || requests[2].form["text"] != "Final &lt;answer&gt;" {
		t.Fatalf("answer transition = %#v", requests[2].form)
	}
	if requests[3].form["text"] != "Final answer" {
		t.Fatalf("final edit = %#v", requests[3].form)
	}
}

func TestGroupPresenterReplacesExpandableCallout(t *testing.T) {
	recorder := &presenterTelegramClient{}
	handler := newPresenterTestHandler(t, recorder)
	message := &models.Message{ID: 23, MessageThreadID: 42, Chat: models.Chat{ID: -100, Type: models.ChatTypeSupergroup}}
	presenter := newChatStreamPresenter(handler, context.Background(), message)

	presenter.start()
	presenter.lastUpdate = time.Now().Add(-4 * time.Second)
	presenter.appendReasoning("checking <unsafe>")
	presenter.showAnswer("Answer starts")
	presenter.finish("Answer complete")

	requests := recorder.snapshot()
	if len(requests) != 4 {
		t.Fatalf("request count = %d, want 4: %#v", len(requests), requests)
	}
	if !strings.HasSuffix(requests[0].method, "/sendMessage") || !strings.Contains(requests[0].form["text"], "<blockquote expandable>") {
		t.Fatalf("initial group callout = %#v", requests[0])
	}
	assertThinkingCalloutSpacing(t, requests[0].form["text"])
	if requests[0].form["message_thread_id"] != "42" || requests[0].form["reply_parameters"] == "" {
		t.Fatalf("group topic/reply metadata = %#v", requests[0].form)
	}
	if !strings.Contains(requests[1].form["text"], "checking &lt;unsafe&gt;") {
		t.Fatalf("reasoning callout = %q", requests[1].form["text"])
	}
	assertThinkingCalloutSpacing(t, requests[1].form["text"])
	if strings.Contains(requests[2].form["text"], "blockquote") || requests[2].form["text"] != "Answer starts" {
		t.Fatalf("answer transition = %#v", requests[2].form)
	}
	if requests[3].form["text"] != "Answer complete" {
		t.Fatalf("final edit = %#v", requests[3].form)
	}
}

func assertThinkingCalloutSpacing(t *testing.T, text string) {
	t.Helper()
	if !strings.Contains(text, "💭 <b>Thinking…</b>\n\n<blockquote expandable>") {
		t.Fatalf("thinking text = %q, want a blank line before the expandable blockquote", text)
	}
}

func TestTailUTF8PreservesCharacters(t *testing.T) {
	got := tailUTF8(strings.Repeat("你好", 20), 17)
	if !utf8.ValidString(got) || len(got) > 17 {
		t.Fatalf("tailUTF8() = %q (%d bytes)", got, len(got))
	}
}

func TestEscapedPrefixStaysWithinTelegramPreviewLimit(t *testing.T) {
	got, capped := escapedPrefix(strings.Repeat("<", streamPreviewLimit), streamPreviewLimit-100)
	if !capped || len(got) > streamPreviewLimit-100 || strings.Contains(got, "<") {
		t.Fatalf("escapedPrefix() length=%d capped=%t", len(got), capped)
	}
}

package bot

import (
	"context"
	"encoding/json"
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
	mu             sync.Mutex
	requests       []recordedTelegramRequest
	failFirstDraft bool
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
	requestIndex := len(c.requests)
	c.mu.Unlock()

	status := http.StatusOK
	body := `{"ok":true,"result":true}`
	if strings.HasSuffix(request.URL.Path, "/sendRichMessageDraft") && c.failFirstDraft && requestIndex == 1 {
		status = http.StatusBadRequest
		body = `{"ok":false,"error_code":400,"description":"draft unavailable"}`
	} else if strings.HasSuffix(request.URL.Path, "/sendMessage") ||
		strings.HasSuffix(request.URL.Path, "/editMessageText") ||
		strings.HasSuffix(request.URL.Path, "/sendRichMessage") {
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

func requestRichHTML(t *testing.T, request recordedTelegramRequest) string {
	t.Helper()
	var rich models.InputRichMessage
	if err := json.Unmarshal([]byte(request.form["rich_message"]), &rich); err != nil {
		t.Fatalf("decode rich_message: %v", err)
	}
	return rich.HTML
}

func TestPrivatePresenterUsesAnimatedRichDraft(t *testing.T) {
	recorder := &presenterTelegramClient{}
	handler := newPresenterTestHandler(t, recorder)
	message := &models.Message{ID: 17, MessageThreadID: 42, Chat: models.Chat{ID: 123, Type: models.ChatTypePrivate}}
	presenter := newChatStreamPresenter(handler, context.Background(), message)

	presenter.start()
	presenter.lastUpdate = time.Now().Add(-2 * time.Second)
	presenter.appendReasoning("check </tg-thinking> safely")
	presenter.showAnswer("Final <answer>")
	presenter.heartbeat()
	presenter.finish("Final answer")

	requests := recorder.snapshot()
	if len(requests) != 5 {
		t.Fatalf("request count = %d, want 5: %#v", len(requests), requests)
	}
	for _, index := range []int{0, 1, 2, 3} {
		if !strings.HasSuffix(requests[index].method, "/sendRichMessageDraft") {
			t.Fatalf("request %d method = %q", index, requests[index].method)
		}
		if requests[index].form["draft_id"] != "17" || requests[index].form["message_thread_id"] != "42" {
			t.Fatalf("request %d form = %#v", index, requests[index].form)
		}
	}
	if initialHTML := requestRichHTML(t, requests[0]); !strings.Contains(initialHTML, "<tg-thinking>") {
		t.Fatalf("initial draft = %q", initialHTML)
	}
	if reasoningHTML := requestRichHTML(t, requests[1]); !strings.Contains(reasoningHTML, "&lt;/tg-thinking&gt;") {
		t.Fatalf("reasoning was not escaped: %q", reasoningHTML)
	}
	if answerHTML := requestRichHTML(t, requests[2]); strings.Contains(answerHTML, "tg-thinking") || !strings.Contains(answerHTML, "Final &lt;answer&gt;") {
		t.Fatalf("answer transition draft = %q", answerHTML)
	}
	if requestRichHTML(t, requests[3]) != requestRichHTML(t, requests[2]) {
		t.Fatalf("heartbeat changed draft content")
	}
	if !strings.HasSuffix(requests[4].method, "/sendRichMessage") || !strings.Contains(requestRichHTML(t, requests[4]), "Final answer") {
		t.Fatalf("final rich message = %#v", requests[4])
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
	if requests[0].form["message_thread_id"] != "42" || requests[0].form["reply_parameters"] == "" {
		t.Fatalf("group topic/reply metadata = %#v", requests[0].form)
	}
	if !strings.Contains(requests[1].form["text"], "checking &lt;unsafe&gt;") {
		t.Fatalf("reasoning callout = %q", requests[1].form["text"])
	}
	if strings.Contains(requests[2].form["text"], "blockquote") || requests[2].form["text"] != "Answer starts" {
		t.Fatalf("answer transition = %#v", requests[2].form)
	}
	if requests[3].form["text"] != "Answer complete" {
		t.Fatalf("final edit = %#v", requests[3].form)
	}
}

func TestPrivatePresenterFallsBackWhenDraftUnavailable(t *testing.T) {
	recorder := &presenterTelegramClient{failFirstDraft: true}
	handler := newPresenterTestHandler(t, recorder)
	message := &models.Message{ID: 17, Chat: models.Chat{ID: 123, Type: models.ChatTypePrivate}}
	presenter := newChatStreamPresenter(handler, context.Background(), message)

	presenter.start()

	requests := recorder.snapshot()
	if len(requests) != 2 || !strings.HasSuffix(requests[0].method, "/sendRichMessageDraft") || !strings.HasSuffix(requests[1].method, "/sendMessage") {
		t.Fatalf("fallback requests = %#v", requests)
	}
	if !strings.Contains(requests[1].form["text"], "<blockquote expandable>") {
		t.Fatalf("fallback callout = %q", requests[1].form["text"])
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

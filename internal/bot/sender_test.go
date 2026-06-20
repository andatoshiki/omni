package bot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type recordedTelegramRequest struct {
	method string
	form   map[string]string
}

type recordingHTTPClient struct {
	requests chan recordedTelegramRequest
}

func (c *recordingHTTPClient) Do(r *http.Request) (*http.Response, error) {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		return nil, err
	}
	c.requests <- recordedTelegramRequest{
		method: r.URL.Path,
		form: map[string]string{
			"chat_id":           r.FormValue("chat_id"),
			"message_thread_id": r.FormValue("message_thread_id"),
			"reply_parameters":  r.FormValue("reply_parameters"),
		},
	}
	body := `{"ok":true,"result":{"message_id":99,"message_thread_id":42,"chat":{"id":-100,"type":"supergroup"},"text":"hello"}}`
	if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
		body = `{"ok":true,"result":true}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func TestTopicAwareTelegramRequests(t *testing.T) {
	recorder := &recordingHTTPClient{requests: make(chan recordedTelegramRequest, 3)}

	client, err := telegram.New(
		"test-token",
		telegram.WithHTTPClient(time.Second, recorder),
		telegram.WithSkipGetMe(),
	)
	if err != nil {
		t.Fatalf("telegram.New() error = %v", err)
	}
	app := &App{client: client}
	message := &models.Message{
		ID:              17,
		MessageThreadID: 42,
		Chat:            models.Chat{ID: -100, Type: models.ChatTypeSupergroup},
	}

	if _, err := app.sendReplyToMessage(context.Background(), message, "hello"); err != nil {
		t.Fatalf("sendReplyToMessage() error = %v", err)
	}
	app.sendChatActionTyping(context.Background(), message)
	if _, err := app.sendMessageInThread(context.Background(), message.Chat.ID, message.MessageThreadID, "hello"); err != nil {
		t.Fatalf("sendMessageInThread() error = %v", err)
	}

	replyRequest := <-recorder.requests
	if replyRequest.method != "/bottest-token/sendMessage" {
		t.Fatalf("reply method = %q", replyRequest.method)
	}
	assertTopicRequest(t, replyRequest.form)
	var replyParameters models.ReplyParameters
	if err := json.Unmarshal([]byte(replyRequest.form["reply_parameters"]), &replyParameters); err != nil {
		t.Fatalf("decode reply_parameters: %v", err)
	}
	if replyParameters.MessageID != message.ID || !replyParameters.AllowSendingWithoutReply {
		t.Fatalf("reply_parameters = %+v", replyParameters)
	}

	typingRequest := <-recorder.requests
	if typingRequest.method != "/bottest-token/sendChatAction" {
		t.Fatalf("typing method = %q", typingRequest.method)
	}
	assertTopicRequest(t, typingRequest.form)

	fallbackRequest := <-recorder.requests
	if fallbackRequest.method != "/bottest-token/sendMessage" {
		t.Fatalf("fallback method = %q", fallbackRequest.method)
	}
	assertTopicRequest(t, fallbackRequest.form)
}

func assertTopicRequest(t *testing.T, form map[string]string) {
	t.Helper()
	if form["chat_id"] != "-100" {
		t.Errorf("chat_id = %q, want -100", form["chat_id"])
	}
	if form["message_thread_id"] != "42" {
		t.Errorf("message_thread_id = %q, want 42", form["message_thread_id"])
	}
}

func TestSplitTextPreservesUTF8(t *testing.T) {
	input := strings.Repeat("你好 ", 20)
	chunks := splitText(input, 17)
	if len(chunks) < 2 {
		t.Fatalf("splitText() returned %d chunk(s), want multiple", len(chunks))
	}
	for _, chunk := range chunks {
		if !utf8.ValidString(chunk) {
			t.Fatalf("splitText() produced invalid UTF-8: %q", chunk)
		}
	}
}

func TestTruncateUTF8PreservesUTF8(t *testing.T) {
	got := truncateUTF8("你好hello", 4)
	if got != "你" || !utf8.ValidString(got) {
		t.Fatalf("truncateUTF8() = %q", got)
	}
}

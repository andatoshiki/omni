package bot

import (
	"errors"
	"strings"
	"testing"

	"github.com/andatoshiki/omni/internal/telegramhtml"
)

func TestErrorMessageFormatsAPIJSONAsCodeBlock(t *testing.T) {
	message := errorMessage(errors.New(`API error 400: {"error":{"message":"bad image","code":"invalid_request_error"}}`))
	if !strings.Contains(message, "API error 400\n\n```json\n") {
		t.Fatalf("message missing JSON fence: %q", message)
	}
	if !strings.Contains(message, "\n  \"error\": {") {
		t.Fatalf("message JSON was not indented: %q", message)
	}

	rendered := telegramhtml.RenderMarkdown(message)
	if !strings.Contains(rendered, `<pre><code class="language-json">`) {
		t.Fatalf("rendered message missing Telegram code block: %q", rendered)
	}
}

func TestErrorMessageUsesSafeFenceForBackticks(t *testing.T) {
	message := errorMessage(errors.New("upstream returned ``` unexpectedly"))
	if !strings.Contains(message, "````text\nupstream returned ``` unexpectedly\n````") {
		t.Fatalf("message did not select a safe fence: %q", message)
	}
	rendered := telegramhtml.RenderMarkdown(message)
	if !strings.Contains(rendered, "upstream returned ``` unexpectedly") {
		t.Fatalf("rendered message changed error detail: %q", rendered)
	}
}

func TestErrorMessageBoundsLongDetails(t *testing.T) {
	message := errorMessage(errors.New(strings.Repeat("x", errorDetailLimit+100)))
	if !strings.Contains(message, "\n…\n```") {
		t.Fatalf("message missing truncation marker: %q", message[len(message)-40:])
	}
	if len(message) > errorDetailLimit+100 {
		t.Fatalf("message length = %d, want bounded output", len(message))
	}
}

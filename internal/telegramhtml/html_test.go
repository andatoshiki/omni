package telegramhtml

import (
	"strings"
	"testing"
)

func TestRenderTelegramMarkdownFormatting(t *testing.T) {
	t.Parallel()

	input := "# Title\n\n**bold** *italic* ~~gone~~ ||hidden||\n\n" +
		"[site](https://example.com/?a=1&b=2)\n\n> quote\n\n- one\n- two\n\n" +
		"```go\nif a < b {}\n```"
	output := RenderMarkdown(input)

	wantFragments := []string{
		"<b>Title</b>",
		"<b>bold</b>",
		"<i>italic</i>",
		"<s>gone</s>",
		"<tg-spoiler>hidden</tg-spoiler>",
		`<a href="https://example.com/?a=1&amp;b=2">site</a>`,
		"<blockquote>quote",
		"• one",
		"• two",
		`<pre><code class="language-go">if a &lt; b {}`,
	}
	for _, fragment := range wantFragments {
		if !strings.Contains(output, fragment) {
			t.Errorf("output does not contain %q:\n%s", fragment, output)
		}
	}
}

func TestRenderTelegramMarkdownSanitizesRawHTML(t *testing.T) {
	t.Parallel()

	input := `<script>alert(1)</script><b onclick="steal()">safe & sound</b> [bad](javascript:alert(1))`
	output := RenderMarkdown(input)

	for _, forbidden := range []string{"<script", `href="javascript:`, "onclick="} {
		if strings.Contains(output, forbidden) {
			t.Errorf("output contains unsafe value %q: %s", forbidden, output)
		}
	}
	if !strings.Contains(output, "<b>safe &amp; sound</b>") {
		t.Errorf("safe text was not escaped correctly: %s", output)
	}
}

func TestRenderTelegramMarkdownSupportsTelegramHTML(t *testing.T) {
	t.Parallel()

	input := `<u>under</u> <tg-spoiler>secret</tg-spoiler> <blockquote expandable>details</blockquote>`
	output := RenderMarkdown(input)

	for _, fragment := range []string{
		"<u>under</u>",
		"<tg-spoiler>secret</tg-spoiler>",
		"<blockquote expandable>details</blockquote>",
	} {
		if !strings.Contains(output, fragment) {
			t.Errorf("output does not contain %q: %s", fragment, output)
		}
	}
}

func TestRenderTelegramMarkdownSupportsCustomEmoji(t *testing.T) {
	t.Parallel()

	output := RenderMarkdown(`![🙂](tg://emoji?id=5368324170671202286)`)
	if !strings.Contains(output, `<tg-emoji emoji-id="5368324170671202286">🙂</tg-emoji>`) {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestRenderTelegramMarkdownLeavesUnmatchedSpoilerLiteral(t *testing.T) {
	t.Parallel()

	output := RenderMarkdown("unfinished ||spoiler")
	if strings.Contains(output, "<tg-spoiler>") || !strings.Contains(output, "||spoiler") {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestRenderTelegramMarkdownDoesNotParseSpoilersInCode(t *testing.T) {
	t.Parallel()

	output := RenderMarkdown("`||literal||`")
	if strings.Contains(output, "<tg-spoiler>") || !strings.Contains(output, "<code>||literal||</code>") {
		t.Fatalf("unexpected output: %s", output)
	}
}

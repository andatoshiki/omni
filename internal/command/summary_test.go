package command

import (
	"strings"
	"testing"

	"github.com/andatoshiki/omni/internal/conversation"
	"github.com/andatoshiki/omni/internal/providers"
)

func TestParseSummaryMessageCount(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
		ok    bool
	}{
		{name: "default", input: "", want: 20, ok: true},
		{name: "whitespace default", input: "  ", want: 20, ok: true},
		{name: "explicit", input: "5", want: 5, ok: true},
		{name: "maximum", input: "100", want: 100, ok: true},
		{name: "capped", input: "101", want: 100, ok: true},
		{name: "non-integer", input: "abc", ok: false},
		{name: "zero", input: "0", ok: false},
		{name: "negative", input: "-1", ok: false},
		{name: "extra argument", input: "5 now", ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseSummaryMessageCount(test.input)
			if got != test.want || ok != test.ok {
				t.Fatalf("parseSummaryMessageCount(%q) = (%d, %t), want (%d, %t)", test.input, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestSelectSummaryMessagesExcludesSystemAndKeepsNewest(t *testing.T) {
	messages := []conversation.Message{
		{Role: providers.RoleSystem, Content: "identity"},
		{Role: providers.RoleUser, Content: "first"},
		{Role: providers.RoleAssistant, Content: "second"},
		{Role: providers.RoleUser, Content: "third"},
	}

	got := selectSummaryMessages(messages, 2)
	if len(got) != 2 || got[0].Content != "second" || got[1].Content != "third" {
		t.Fatalf("selectSummaryMessages() = %#v, want the newest two non-system messages", got)
	}
}

func TestBuildSummaryPromptExcludesSpeakerIdentity(t *testing.T) {
	messages := []conversation.Message{{
		Role:    providers.RoleUser,
		Content: "hello",
		Speaker: &conversation.Speaker{DisplayName: "Secret Name"},
	}}

	got := buildSummaryPrompt("Summarize this.", messages)
	if !strings.Contains(got, "User: hello") {
		t.Fatalf("buildSummaryPrompt() = %q, want role and content", got)
	}
	if strings.Contains(got, "Secret Name") {
		t.Fatalf("buildSummaryPrompt() = %q, should exclude speaker identity", got)
	}
}

package bedrock

import (
	"testing"

	"github.com/andatoshiki/omni/internal/providers/platforms"
)

func TestThinkingRequestFields(t *testing.T) {
	budget := 2048
	fields := thinkingRequestFields(&platforms.ThinkingOptions{Mode: "enabled", BudgetTokens: &budget})
	thinking, ok := fields["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" || thinking["budget_tokens"] != budget || thinking["display"] != "summarized" {
		t.Fatalf("thinking fields = %#v", fields)
	}

	adaptive := thinkingRequestFields(&platforms.ThinkingOptions{Mode: "auto", Effort: "high"})
	output, ok := adaptive["output_config"].(map[string]any)
	if !ok || output["effort"] != "high" {
		t.Fatalf("adaptive fields = %#v", adaptive)
	}

	disabled := thinkingRequestFields(&platforms.ThinkingOptions{Mode: "disabled", Effort: "none"})
	if _, exists := disabled["output_config"]; exists {
		t.Fatalf("disabled fields must omit output_config: %#v", disabled)
	}
}

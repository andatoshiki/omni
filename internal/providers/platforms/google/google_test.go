package google

import (
	"testing"

	"google.golang.org/genai"

	"github.com/andatoshiki/omni/internal/providers/platforms"
)

func TestApplyThinkingConfig(t *testing.T) {
	budget := 1024
	config := &genai.GenerateContentConfig{}
	applyThinkingConfig(config, &platforms.ChatCompletionStreamRequest{
		CaptureReasoning: true,
		Thinking:         &platforms.ThinkingOptions{Mode: "enabled", BudgetTokens: &budget},
	})
	if config.ThinkingConfig == nil || !config.ThinkingConfig.IncludeThoughts {
		t.Fatalf("ThinkingConfig = %#v", config.ThinkingConfig)
	}
	if config.ThinkingConfig.ThinkingBudget == nil || *config.ThinkingConfig.ThinkingBudget != int32(budget) {
		t.Fatalf("ThinkingBudget = %#v", config.ThinkingConfig.ThinkingBudget)
	}

	disabled := &genai.GenerateContentConfig{}
	applyThinkingConfig(disabled, &platforms.ChatCompletionStreamRequest{
		Thinking: &platforms.ThinkingOptions{Mode: "disabled"},
	})
	if disabled.ThinkingConfig.ThinkingBudget == nil || *disabled.ThinkingConfig.ThinkingBudget != 0 {
		t.Fatalf("disabled ThinkingConfig = %#v", disabled.ThinkingConfig)
	}

	effort := &genai.GenerateContentConfig{}
	applyThinkingConfig(effort, &platforms.ChatCompletionStreamRequest{
		Thinking: &platforms.ThinkingOptions{Mode: "enabled", Effort: "high"},
	})
	if effort.ThinkingConfig.ThinkingLevel != genai.ThinkingLevelHigh {
		t.Fatalf("ThinkingLevel = %q", effort.ThinkingConfig.ThinkingLevel)
	}
}

func TestGeminiStreamSeparatesThoughtParts(t *testing.T) {
	response := translateGeminiResponse(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{
		Content: &genai.Content{Parts: []*genai.Part{
			{Text: "summary", Thought: true},
			{Text: "answer"},
		}},
	}}})
	if len(response.Choices) != 2 || response.Choices[0].Delta.ReasoningContent != "summary" || response.Choices[1].Delta.Content != "answer" {
		t.Fatalf("translated response = %#v", response)
	}
}

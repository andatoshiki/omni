// Package deepseek contains DeepSeek-specific API operations.
package deepseek

import (
	"context"

	"github.com/andatoshiki/omni/internal/providers/platforms"
	"github.com/andatoshiki/omni/internal/providers/platforms/openai"
)

// Adapter uses DeepSeek's OpenAI-compatible chat-completions endpoint.
type Adapter struct {
	OpenAI openai.Adapter
}

func (a Adapter) CreateChatCompletionStream(
	ctx context.Context,
	endpoint platforms.Endpoint,
	request *platforms.ChatCompletionStreamRequest,
) (*platforms.ChatCompletionStream, error) {
	return a.OpenAI.CreateChatCompletionStream(ctx, endpoint, request)
}

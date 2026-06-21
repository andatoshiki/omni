// Package xai contains xAI-specific API operations.
package xai

import (
	"context"

	"github.com/andatoshiki/omni/internal/providers/platforms"
	"github.com/andatoshiki/omni/internal/providers/platforms/openai"
)

// Adapter uses xAI's OpenAI-compatible chat-completions endpoint.
type Adapter struct {
	OpenAI openai.Adapter
}

func (a Adapter) CreateChatCompletionStream(
	ctx context.Context,
	endpoint platforms.Endpoint,
	request *platforms.ChatCompletionStreamRequest,
) (platforms.ChatCompletionStream, error) {
	return a.OpenAI.CreateChatCompletionStream(ctx, endpoint, request)
}

// Package custom adapts named OpenAI-compatible endpoints configured by users.
package custom

import (
	"context"

	"github.com/andatoshiki/omni/internal/providers/platforms"
	"github.com/andatoshiki/omni/internal/providers/platforms/openai"
)

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

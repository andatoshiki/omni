package bedrock

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/andatoshiki/omni/internal/providers/platforms"
)

type bedrockStream struct {
	stream *bedrockruntime.ConverseStreamEventStream
	cancel context.CancelFunc
}

func newBedrockStream(stream *bedrockruntime.ConverseStreamEventStream, cancel context.CancelFunc) *bedrockStream {
	return &bedrockStream{
		stream: stream,
		cancel: cancel,
	}
}

func (s *bedrockStream) Recv() (*platforms.ChatCompletionStreamResponse, error) {
	for {
		msg, ok := <-s.stream.Events()
		if !ok {
			if err := s.stream.Err(); err != nil {
				return nil, err
			}
			return nil, io.EOF
		}

		switch v := msg.(type) {
		case *types.ConverseStreamOutputMemberContentBlockDelta:
			if textDelta, ok := v.Value.Delta.(*types.ContentBlockDeltaMemberText); ok {
				return &platforms.ChatCompletionStreamResponse{
					Choices: []platforms.StreamChoice{
						{
							Delta: platforms.StreamDelta{
								Content: textDelta.Value,
							},
						},
					},
				}, nil
			}
		case *types.ConverseStreamOutputMemberMessageStop:
			// The stream has successfully finished generating content.
			// The overall stream channel will close shortly.
			continue
		case *types.ConverseStreamOutputMemberMetadata:
			// Optionally extract token usage from metadata in the future.
			continue
		case *types.ConverseStreamOutputMemberContentBlockStart:
			continue
		case *types.ConverseStreamOutputMemberContentBlockStop:
			continue
		case *types.ConverseStreamOutputMemberMessageStart:
			continue
		case *types.UnknownUnionMember:
			continue
		}
	}
}

func (s *bedrockStream) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	return s.stream.Close()
}

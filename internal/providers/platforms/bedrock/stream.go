package bedrock

import (
	"context"
	"io"

	"github.com/andatoshiki/omni/internal/providers/platforms"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
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
			switch delta := v.Value.Delta.(type) {
			case *types.ContentBlockDeltaMemberText:
				return &platforms.ChatCompletionStreamResponse{
					Choices: []platforms.StreamChoice{
						{
							Delta: platforms.StreamDelta{
								Content: delta.Value,
							},
						},
					},
				}, nil
			case *types.ContentBlockDeltaMemberReasoningContent:
				if reasoning, ok := delta.Value.(*types.ReasoningContentBlockDeltaMemberText); ok {
					return &platforms.ChatCompletionStreamResponse{
						Choices: []platforms.StreamChoice{{Delta: platforms.StreamDelta{ReasoningContent: reasoning.Value}}},
					}, nil
				}
			}
		case *types.ConverseStreamOutputMemberMessageStop:
			return &platforms.ChatCompletionStreamResponse{
				Choices: []platforms.StreamChoice{{FinishReason: string(v.Value.StopReason)}},
			}, nil
		case *types.ConverseStreamOutputMemberMetadata:
			if v.Value.Usage != nil {
				return &platforms.ChatCompletionStreamResponse{Usage: &platforms.TokenUsage{
					PromptTokens:     int64(aws.ToInt32(v.Value.Usage.InputTokens)),
					CompletionTokens: int64(aws.ToInt32(v.Value.Usage.OutputTokens)),
					TotalTokens:      int64(aws.ToInt32(v.Value.Usage.TotalTokens)),
				}}, nil
			}
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

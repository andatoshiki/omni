package platforms

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ChatCompletionStream interface {
	Recv() (*ChatCompletionStreamResponse, error)
	Close() error
}

type sseStream struct {
	response *http.Response
	scanner  *bufio.Scanner
}

func NewChatCompletionStream(response *http.Response) ChatCompletionStream {
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	return &sseStream{response: response, scanner: scanner}
}

func (s *sseStream) Recv() (*ChatCompletionStreamResponse, error) {
	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil, io.EOF
		}

		var response ChatCompletionStreamResponse
		if err := json.Unmarshal([]byte(data), &response); err != nil {
			return nil, fmt.Errorf("decode streamed chat completion: %w", err)
		}
		return &response, nil
	}
	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("read streamed chat completion: %w", err)
	}
	return nil, io.EOF
}

func (s *sseStream) Close() error {
	if s == nil || s.response == nil || s.response.Body == nil {
		return nil
	}
	return s.response.Body.Close()
}

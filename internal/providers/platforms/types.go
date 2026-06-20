package platforms

// Endpoint contains the connection details shared by provider platforms.
type Endpoint struct {
	APIKey  string
	BaseURL string
}

// ChatMessage represents one OpenAI-compatible chat-completions message.
type ChatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// ChatContentPart is one item in an OpenAI-compatible multimodal message.
// Text is populated for type "text" and ImageURL for type "image_url".
type ChatContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *ChatImageURL `json:"image_url,omitempty"`
}

type ChatImageURL struct {
	URL string `json:"url"`
}

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type ChatCompletionStreamRequest struct {
	Model         string        `json:"model"`
	Messages      []ChatMessage `json:"messages"`
	Temperature   float32       `json:"temperature"`
	MaxTokens     int           `json:"max_tokens,omitempty"`
	Stream        bool          `json:"stream"`
	StreamOptions StreamOptions `json:"stream_options"`
}

type TokenUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type StreamDelta struct {
	Content string `json:"content"`
}

type StreamChoice struct {
	Delta StreamDelta `json:"delta"`
}

type ChatCompletionStreamResponse struct {
	Choices []StreamChoice `json:"choices"`
	Usage   *TokenUsage    `json:"usage,omitempty"`
}

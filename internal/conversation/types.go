package conversation

// Speaker represents the identity of a chat participant.
type Speaker struct {
	UserID      int64  `json:"user_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Username    string `json:"username,omitempty"`
}

// ReplyContext contains information about the message being replied to.
type ReplyContext struct {
	Speaker *Speaker `json:"speaker,omitempty"`
	Text    string   `json:"text,omitempty"`
}

// Message represents one turn in a conversation.
// This is structurally compatible with providers.ChatMessage for JSON serialization.
type Message struct {
	Role    string        `json:"role"`
	Content any           `json:"content"`
	Speaker *Speaker      `json:"speaker,omitempty"`
	ReplyTo *ReplyContext `json:"reply_to,omitempty"`
}

// IsValid returns whether the message has valid basic fields.
func (m Message) IsValid() bool {
	return m.Role != "" && m.Content != nil
}

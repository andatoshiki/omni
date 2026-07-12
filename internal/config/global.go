package config

const DefaultSummaryPrompt = "Please provide a concise summary of the following conversation. Start with a brief 1-2 sentence overview of the main topic, followed by bullet points highlighting key decisions, questions, or action items."

type globalConfig struct {
	InitialPrompt        string  `yaml:"initial_prompt"`
	SummaryPrompt        string  `yaml:"summary_prompt"`
	Temperature          float64 `yaml:"temperature"`
	MaxReplyTokens       int     `yaml:"max_reply_tokens"`
	MaxContextTokens     int     `yaml:"max_context_tokens"`
	HistorySize          int     `yaml:"history_size"`
	SenderContext        string  `yaml:"sender_context"`
	SessionTimeout       string  `yaml:"session_timeout"`
	MaxSessionsDisplayed int     `yaml:"max_sessions_displayed"`
	TitleModel           string  `yaml:"title_model"`
}

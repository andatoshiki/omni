package config

type globalConfig struct {
	InitialPrompt    string  `yaml:"initial_prompt"`
	Temperature      float64 `yaml:"temperature"`
	MaxReplyTokens   int     `yaml:"max_reply_tokens"`
	MaxContextTokens       int     `yaml:"max_context_tokens"`
	HistorySize            int     `yaml:"history_size"`
	SenderContext          string  `yaml:"sender_context"`
	SessionTimeout         string  `yaml:"session_timeout"`
	MaxSessionsDisplayed   int     `yaml:"max_sessions_displayed"`
}

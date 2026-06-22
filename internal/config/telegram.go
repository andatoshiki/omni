package config

type telegramConfig struct {
	BotToken        string  `yaml:"bot_token"`
	AllowedUserIDs  []int64 `yaml:"allowed_user_ids"`
	AdminUserIDs    []int64 `yaml:"admin_user_ids"`
	AllowedGroupIDs []int64 `yaml:"allowed_group_ids"`
}

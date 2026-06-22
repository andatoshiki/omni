package config

type databaseConfig struct {
	Backend string       `yaml:"backend"`
	SQLite  SQLiteConfig `yaml:"sqlite"`
	MySQL   MySQLConfig  `yaml:"mysql"`
}

type DatabaseConfig struct {
	Backend string
	SQLite  SQLiteConfig
	MySQL   MySQLConfig
}

type SQLiteConfig struct {
	Path string `yaml:"path"`
}

type MySQLConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
}

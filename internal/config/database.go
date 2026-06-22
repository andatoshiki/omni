package config

type databaseConfig struct {
	Backend  string         `yaml:"backend"`
	SQLite   SQLiteConfig   `yaml:"sqlite"`
	MySQL    MySQLConfig    `yaml:"mysql"`
	Postgres PostgresConfig `yaml:"postgres"`
}

type DatabaseConfig struct {
	Backend  string
	SQLite   SQLiteConfig
	MySQL    MySQLConfig
	Postgres PostgresConfig
}

type SQLiteConfig struct {
	Path string `yaml:"path"`
}

type MySQLConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"db_name"`
}

type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"db_name"`
	SSLMode  string `yaml:"sslmode"`
}

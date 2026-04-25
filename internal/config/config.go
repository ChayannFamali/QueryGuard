package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Proxy     ProxyConfig     `yaml:"proxy"`
	Postgres  PostgresConfig  `yaml:"postgres"`
	Log       LogConfig       `yaml:"log"`
	Dashboard DashboardConfig `yaml:"dashboard"`
	Policy    PolicyConfig    `yaml:"policy"`
	Metrics   MetricsConfig   `yaml:"metrics"`
}

type ProxyConfig struct {
	ListenAddr string `yaml:"listen_addr"`
	TargetAddr string `yaml:"target_addr"`
}

type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type DashboardConfig struct {
	Enabled    bool   `yaml:"enabled"`
	ListenAddr string `yaml:"listen_addr"`
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
}

type PolicyConfig struct {
	DryRun     bool   `yaml:"dry_run"`
	ConfigPath string `yaml:"config_path"`
}

type MetricsConfig struct {
	Enabled    bool   `yaml:"enabled"`
	ListenAddr string `yaml:"listen_addr"`
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config file: %w", err)
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	return &cfg, nil
}

package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Proxy     ProxyConfig     `yaml:"proxy"`
	Log       LogConfig       `yaml:"log"`
	Dashboard DashboardConfig `yaml:"dashboard"`
	Policy    PolicyConfig    `yaml:"policy"`
	Metrics   MetricsConfig   `yaml:"metrics"`
	Analyzer  AnalyzerConfig  `yaml:"analyzer"`
}

type ProxyConfig struct {
	ListenAddr string `yaml:"listen_addr"`
	TargetAddr string `yaml:"target_addr"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	LogSQL bool   `yaml:"log_sql"`
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
	Username   string `yaml:"username"`
	Password   string `yaml:"password"`
}

type AnalyzerConfig struct {
	N1Threshold      int `yaml:"n1_threshold"`
	ComplexityWarn    int `yaml:"complexity_warn"`
	ComplexityCrit    int `yaml:"complexity_crit"`
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

	cfg.applyDefaults()
	cfg.applyEnvOverrides()

	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Analyzer.N1Threshold <= 0 {
		c.Analyzer.N1Threshold = 5
	}
	if c.Analyzer.ComplexityWarn <= 0 {
		c.Analyzer.ComplexityWarn = 30
	}
	if c.Analyzer.ComplexityCrit <= 0 {
		c.Analyzer.ComplexityCrit = 60
	}
}

func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("QG_DASHBOARD_USERNAME"); v != "" {
		c.Dashboard.Username = v
	}
	if v := os.Getenv("QG_DASHBOARD_PASSWORD"); v != "" {
		c.Dashboard.Password = v
	}
	if v := os.Getenv("QG_METRICS_USERNAME"); v != "" {
		c.Metrics.Username = v
	}
	if v := os.Getenv("QG_METRICS_PASSWORD"); v != "" {
		c.Metrics.Password = v
	}
}

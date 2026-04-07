package config

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Port int `yaml:"port"`
	} `yaml:"server"`
	Upstreams []Upstream `yaml:"upstreams"`
	Fallback  struct {
		MaxRetries    int           `yaml:"max_retries"`
		RetryDelay    time.Duration `yaml:"retry_delay"`
		FallbackOrder []string      `yaml:"fallback_order"`
	} `yaml:"fallback"`
}

type Upstream struct {
	Name    string        `yaml:"name"`
	BaseURL string        `yaml:"base_url"`
	APIKey  string        `yaml:"api_key"`
	Weight  int           `yaml:"weight"`
	Timeout time.Duration `yaml:"timeout"`
	RetryOn []int         `yaml:"retry_on"`
}

// Load 读取 YAML 并替换 ${ENV} 变量
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	// 替换 ${VAR} 为环境变量值
	re := regexp.MustCompile(`\$\{(\w+)\}`)
	content := re.ReplaceAllStringFunc(string(data), func(match string) string {
		key := match[2 : len(match)-1] // 提取 VAR
		if val := os.Getenv(key); val != "" {
			return val
		}
		return match // 未设置则保留原样（上游会报错，便于调试）
	})

	var cfg Config
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	// 应用默认值
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 7722
	}
	if cfg.Fallback.MaxRetries == 0 {
		cfg.Fallback.MaxRetries = 2
	}
	if cfg.Fallback.RetryDelay == 0 {
		cfg.Fallback.RetryDelay = 500 * time.Millisecond
	}

	return &cfg, nil
}

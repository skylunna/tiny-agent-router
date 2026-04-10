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
	Upstreams     []Upstream          `yaml:"upstreams"`
	Fallback      FallbackConfig      `yaml:"fallback"`
	Pricing       map[string]Pricing  `yaml:"pricing"`       // 全局 pricing，按 upstream name 映射
	Observability ObservabilityConfig `yaml:"observability"` // 改为 Observability
	Cache         CacheConfig         `yaml:"cache"`
	Metrics       MetricsConfig       `yaml:"metrics"`
}

type Upstream struct {
	Name         string        `yaml:"name"`
	BaseURL      string        `yaml:"base_url"`
	APIKey       string        `yaml:"api_key"`
	Weight       int           `yaml:"weight"`
	Timeout      time.Duration `yaml:"timeout"`
	RetryOn      []int         `yaml:"retry_on"`
	DefaultModel string        `yaml:"default_model"` // 新增
	Pricing      Pricing       `yaml:"pricing"`       // 新增
}

type FallbackConfig struct {
	MaxRetries    int           `yaml:"max_retries"`
	RetryDelay    time.Duration `yaml:"retry_delay"`
	FallbackOrder []string      `yaml:"fallback_order"`
}

type Pricing struct {
	Prompt     float64 `yaml:"prompt"`     // 输入令牌单价（$ / 1K tokens）
	Completion float64 `yaml:"completion"` // 输出令牌单价（$ / 1K tokens）
}

type ObservabilityConfig struct {
	EnableMetrics  bool   `yaml:"enable_metrics"`
	PrometheusPath string `yaml:"prometheus_path"`
	ServiceName    string `yaml:"service_name"`
}

type CacheConfig struct {
	Enabled             bool    `yaml:"enabled"`
	GrpcAddr            string  `yaml:"grpc_addr"`
	SimilarityThreshold float64 `yaml:"similarity_threshold"`
	TTLSeconds          int64   `yaml:"ttl_seconds"`
}

type MetricsConfig struct {
	EnablePrometheus bool   `yaml:"enable_prometheus"`
	PrometheusPath   string `yaml:"prometheus_path"`
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
		key := match[2 : len(match)-1]
		if val := os.Getenv(key); val != "" {
			return val
		}
		return match
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
	if cfg.Observability.PrometheusPath == "" {
		cfg.Observability.PrometheusPath = "/metrics"
	}
	if cfg.Observability.ServiceName == "" {
		cfg.Observability.ServiceName = "tiny-agent-router"
	}
	if cfg.Metrics.PrometheusPath == "" {
		cfg.Metrics.PrometheusPath = "/metrics"
	}
	// 默认开启 Prometheus（生产建议显式配置）
	if !cfg.Metrics.EnablePrometheus && cfg.Metrics.PrometheusPath != "" {
		cfg.Metrics.EnablePrometheus = true
	}

	return &cfg, nil
}

// GetPricing 安全获取 upstream 的计价规则（不存在返回 0）
func (c *Config) GetPricing(upstreamName string) Pricing {
	if c.Pricing == nil {
		return Pricing{}
	}
	return c.Pricing[upstreamName]
}

// HasPricing 检查是否有任意 upstream 配置了计价
func (c *Config) HasPricing() bool {
	if c.Pricing == nil {
		return false
	}
	for _, p := range c.Pricing {
		if p.Prompt > 0 || p.Completion > 0 {
			return true
		}
	}
	return false
}

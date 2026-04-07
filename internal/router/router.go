package router

import (
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/skylunna/tiny-agent-router/internal/config"
)

type UpstreamInfo struct {
	Name         string
	BaseURL      string
	APIKey       string
	Timeout      time.Duration
	RetryOn      map[int]bool
	DefaultModel string         // 模型映射
	Pricing      config.Pricing // 成本配置
}

type Router struct {
	upstreams     []UpstreamInfo
	weights       []int
	totalWeight   int
	fallbackOrder []string
	mu            sync.RWMutex
}

func NewRouter(cfg *config.Config) *Router {
	r := &Router{
		fallbackOrder: cfg.Fallback.FallbackOrder,
	}

	// 预处理 upstream
	for _, u := range cfg.Upstreams {
		if u.Weight <= 0 {
			continue
		}
		r.upstreams = append(r.upstreams, UpstreamInfo{
			Name:         u.Name,
			BaseURL:      u.BaseURL,
			APIKey:       resolveAPIKey(u.APIKey),
			Timeout:      u.Timeout,
			RetryOn:      sliceToSet(u.RetryOn),
			DefaultModel: u.DefaultModel,
			Pricing:      u.Pricing,
		})
		r.weights = append(r.weights, u.Weight)
		r.totalWeight += u.Weight
	}

	return r
}

// 按权重随机选择一个上游
func (r *Router) Select() *UpstreamInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.upstreams) == 0 {
		return nil
	}
	if r.totalWeight == 0 {
		return &r.upstreams[0]
	}

	// 权重随机
	roll := rand.Intn(r.totalWeight)
	for i, w := range r.weights {
		if roll < w {
			return &r.upstreams[i]
		}
		roll -= w
	}
	return &r.upstreams[len(r.upstreams)-1]
}

// 返回备选上游 (排除当前失败的)
func (r *Router) Fallback(excludeName string) *UpstreamInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, name := range r.fallbackOrder {
		if name == excludeName {
			continue
		}
		for i, u := range r.upstreams {
			if u.Name == name {
				return &r.upstreams[i]
			}
		}
	}

	// 兜底返回任意其他上游
	for i := range r.upstreams {
		if r.upstreams[i].Name != excludeName {
			return &r.upstreams[i]
		}
	}
	return nil
}

// 工具函数
func resolveAPIKey(key string) string {
	if len(key) >= 2 && key[0] == '$' && key[1] == '{' {
		// 简单处理 ${VAR}
		end := -1
		for i, c := range key {
			if c == '}' {
				end = i
				break
			}
		}
		if end > 2 {
			envKey := key[2:end]
			if val := os.Getenv(envKey); val != "" {
				return val
			}
		}
	}
	return key
}

func sliceToSet(nums []int) map[int]bool {
	s := make(map[int]bool)
	for _, n := range nums {
		s[n] = true
	}
	return s
}

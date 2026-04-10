package metrics

import (
	"sync"

	"github.com/pkoukk/tiktoken-go"
	"github.com/skylunna/tiny-agent-router/internal/config"
)

type CostTracker struct {
	mu      sync.RWMutex
	pricing map[string]config.Pricing // name -> pricing
	encoder *tiktoken.Tiktoken
}

// NewCostTracker 接收 []Upstream 并内部构建 pricing 映射
// ✅ 修复：参数类型从 map[string]Pricing 改为 []Upstream，调用方无需预处理
func NewCostTracker(upstreams []config.Upstream) (*CostTracker, error) {
	// 初始化 tokenizer（DeepSeek/Qwen/OpenAI 均兼容 cl100k_base）
	encoding, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		return nil, err
	}

	// 构建 pricing 查找表
	pricing := make(map[string]config.Pricing)
	for _, u := range upstreams {
		// 仅收录配置了价格的模型（本地模型可留空）
		if u.Pricing.Prompt > 0 || u.Pricing.Completion > 0 {
			pricing[u.Name] = u.Pricing
		}
	}

	return &CostTracker{
		pricing: pricing,
		encoder: encoding,
	}, nil
}

// CountTokens 估算输入文本的 token 数（简化版：仅计 content 字段）
func (c *CostTracker) CountTokens(messages []map[string]string) int {
	total := 0
	for _, msg := range messages {
		content := msg["content"]
		if content == "" {
			continue
		}
		tokens := c.encoder.Encode(content, nil, nil)
		total += len(tokens)
	}
	return total
}

// CalculateCost 计算单次请求成本（单位：美元）
// ✅ 线程安全：支持高并发调用
func (c *CostTracker) CalculateCost(upstreamName string, inputTokens, outputTokens int) float64 {
	c.mu.RLock()
	pricing, ok := c.pricing[upstreamName]
	c.mu.RUnlock()

	if !ok {
		return 0 // 无定价配置则成本为 0（如本地 Ollama）
	}

	inputCost := float64(inputTokens) * pricing.Prompt / 1000
	outputCost := float64(outputTokens) * pricing.Completion / 1000
	return inputCost + outputCost
}

// UpdatePricing 动态更新定价（可选：用于多租户/动态调价场景）
func (c *CostTracker) UpdatePricing(upstreamName string, pricing config.Pricing) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pricing[upstreamName] = pricing
}

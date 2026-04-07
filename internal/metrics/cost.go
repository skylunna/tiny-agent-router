package metrics

import (
	"sync"

	"github.com/pkoukk/tiktoken-go"
	"github.com/skylunna/tiny-agent-router/internal/config"
)

type CostTracker struct {
	mu      sync.Mutex
	pricing map[string]config.Pricing // upstream name -> pricing
	encoder *tiktoken.Tiktoken
}

func NewCostTracker(pricing map[string]config.Pricing) (*CostTracker, error) {
	// DeepSeek/Qwen/Ollama 均兼容 cl100k_base
	encoding, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		return nil, err
	}

	return &CostTracker{
		pricing: pricing,
		encoder: encoding,
	}, nil
}

// CountTokens 估算 token 数（简化版：仅计 content 字段）
func (c *CostTracker) CountTokens(messages []map[string]string) int {
	total := 0
	for _, msg := range messages {
		content := msg["content"]
		tokens := c.encoder.Encode(content, nil, nil)
		total += len(tokens)
	}
	return total
}

// CalculateCost 计算单次请求成本（美元）
func (c *CostTracker) CalculateCost(upstreamName string, inputTokens, outputTokens int) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	p, ok := c.pricing[upstreamName]
	if !ok {
		return 0 // 无配置则成本为 0
	}

	// pricing 单位是 $ / 1K tokens
	inputCost := float64(inputTokens) * p.Prompt / 1000
	outputCost := float64(outputTokens) * p.Completion / 1000
	return inputCost + outputCost
}

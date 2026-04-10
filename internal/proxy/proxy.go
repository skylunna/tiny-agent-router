package proxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/skylunna/tiny-agent-router/internal/cache"
	"github.com/skylunna/tiny-agent-router/internal/cache/proto"
	"github.com/skylunna/tiny-agent-router/internal/config"
	"github.com/skylunna/tiny-agent-router/internal/metrics"
	"github.com/skylunna/tiny-agent-router/internal/router"
)

// Proxy 核心代理引擎
type Proxy struct {
	router      *router.Router
	cacheClient *cache.Client
	costTracker *metrics.CostTracker
	httpClient  *http.Client
	maxRetries  int
}

// NewProxy 初始化代理
func NewProxy(r *router.Router, cacheClient *cache.Client, costTracker *metrics.CostTracker, fallbackCfg config.FallbackConfig) *Proxy {
	return &Proxy{
		router:      r,
		cacheClient: cacheClient,
		costTracker: costTracker,
		maxRetries:  fallbackCfg.MaxRetries,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:          200,
				MaxIdleConnsPerHost:   50,
				IdleConnTimeout:       90 * time.Second,
				ResponseHeaderTimeout: 60 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
			},
		},
	}
}

// ServeHTTP 统一入口
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	path := r.URL.Path
	isStream := isStreamRequest(r)

	// 1. 读取并还原请求体（后续可能多次使用）
	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("Failed to read request body", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(reqBody))

	originalModel := extractModel(reqBody)
	slog.Info("Incoming request", "path", path, "model", originalModel, "stream", isStream, "remote", r.RemoteAddr)

	// 2. 缓存拦截（仅非流式）
	if p.cacheClient != nil && !isStream {
		cacheReq := buildCacheRequest(r, originalModel, reqBody)
		if resp, err := p.cacheClient.Get(r.Context(), cacheReq); err == nil && resp != nil && resp.Hit {
			slog.Info("Cache HIT", "model", originalModel, "similarity", resp.Similarity)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			w.Header().Set("X-Cache-Similarity", fmt.Sprintf("%.4f", *resp.Similarity))
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(*resp.ResponseBody))

			// 指标上报
			metrics.Record("cache", originalModel, float64(time.Since(start).Milliseconds()), 0, 0, 0, false)
			metrics.RecordCacheHit(originalModel, 1, resp.Similarity)
			return
		}
	}

	// 3. 路由分发
	if isStream {
		p.handleStreaming(w, r, reqBody, originalModel, start)
	} else {
		p.handleNonStreaming(w, r, reqBody, originalModel, start)
	}
}

// handleNonStreaming 完整拦截：重试、降级、成本、缓存
func (p *Proxy) handleNonStreaming(w http.ResponseWriter, r *http.Request, reqBody []byte, originalModel string, start time.Time) {
	var upstream *router.UpstreamInfo
	var respBody []byte
	var attempts int
	var lastErr error

	for attempts = 0; attempts <= p.maxRetries; attempts++ {
		upstream = p.router.Select()
		if upstream == nil {
			http.Error(w, "No upstream available", http.StatusServiceUnavailable)
			return
		}

		// 模型映射
		targetModel := originalModel
		if upstream.DefaultModel != "" {
			targetModel = upstream.DefaultModel
			reqBody = rewriteModelInBody(reqBody, targetModel)
		}

		// 构造请求
		req, err := http.NewRequestWithContext(r.Context(), r.Method, upstream.BaseURL+r.URL.Path, bytes.NewReader(reqBody))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header = r.Header.Clone()
		if upstream.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+upstream.APIKey)
		}
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(reqBody))

		// 执行
		resp, err := p.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("network: %w", err)
			if p.shouldRetry(upstream, 0) {
				slog.Warn("Network error, retrying", "upstream", upstream.Name, "attempt", attempts+1)
				continue
			}
			break
		}

		respBody, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response: %w", err)
			break
		}

		// 透传响应头与状态码
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)

		if p.shouldRetry(upstream, resp.StatusCode) {
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			slog.Warn("Upstream error, triggering fallback", "upstream", upstream.Name, "status", resp.StatusCode)
			continue
		}

		lastErr = nil
		break
	}

	// 4. 后处理：指标、成本、异步缓存写入
	duration := time.Since(start)
	if lastErr == nil && respBody != nil && upstream != nil {
		inTokens, outTokens := extractUsage(respBody)
		cost := p.costTracker.CalculateCost(upstream.Name, inTokens, outTokens)
		isFallback := attempts > 0

		metrics.Record(upstream.Name, originalModel, float64(duration.Milliseconds()), inTokens, outTokens, cost, isFallback)

		// 成功响应且非降级，异步写入缓存
		if p.cacheClient != nil && respBody != nil {
			go func() {
				respBodyStr := string(respBody)
				inTokensPtr := int32(inTokens)
				outTokensPtr := int32(outTokens)
				p.cacheClient.Put(context.Background(), &proto.CacheRequest{
					RequestId:        uuid.New().String(),
					Model:            originalModel,
					SystemPromptHash: computePromptHash(reqBody),
					ResponseBody:     &respBodyStr,
					InputTokens:      &inTokensPtr,
					OutputTokens:     &outTokensPtr,
					TtlSeconds:       86400,
				})
			}()
		}
	} else if lastErr != nil {
		slog.Error("Request failed after retries", "error", lastErr, "upstream", upstream.Name, "attempts", attempts)
		metrics.Record(upstream.Name, originalModel, float64(duration.Milliseconds()), 0, 0, 0, true)
	}
}

// handleStreaming 直通透传（流式响应不拦截，避免阻塞与内存暴涨）
func (p *Proxy) handleStreaming(w http.ResponseWriter, r *http.Request, reqBody []byte, originalModel string, start time.Time) {
	upstream := p.router.Select()
	if upstream == nil {
		http.Error(w, "No upstream available", http.StatusServiceUnavailable)
		return
	}

	// 模型映射
	targetModel := originalModel
	if upstream.DefaultModel != "" {
		targetModel = upstream.DefaultModel
		reqBody = rewriteModelInBody(reqBody, targetModel)
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstream.BaseURL+r.URL.Path, bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	req.Header = r.Header.Clone()
	if upstream.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+upstream.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		slog.Error("Streaming request failed", "error", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 透传头部
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)

	// 流式拷贝（零缓冲直推客户端）
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		slog.Warn("Streaming interrupted", "error", err)
	}

	duration := time.Since(start)
	slog.Info("Stream completed", "upstream", upstream.Name, "duration_ms", duration.Milliseconds())
	// 注：流式响应默认不含 usage，成本/缓存需客户端开启 stream_options: {"include_usage": true}
}

// ========== 辅助函数 ==========

// extractPromptFromBody 稳健提取用户 prompt（兼容多种 JSON 结构）
func extractPromptFromBody(body []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Debug("Failed to unmarshal request body for cache", "error", err)
		return ""
	}

	// 兼容：messages 可能是 []interface{} 或 []map[string]interface{}
	if msgs, ok := payload["messages"].([]interface{}); ok {
		var prompts []string
		for _, m := range msgs {
			if msg, ok := m.(map[string]interface{}); ok {
				if role, _ := msg["role"].(string); role == "user" {
					if content, _ := msg["content"].(string); content != "" {
						prompts = append(prompts, content)
					}
				}
			}
		}
		if len(prompts) > 0 {
			return strings.Join(prompts, "\n")
		}
	}
	return ""
}

func isStreamRequest(r *http.Request) bool {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))

	var payload map[string]interface{}
	if json.Unmarshal(body, &payload) == nil {
		if stream, ok := payload["stream"].(bool); ok {
			return stream
		}
	}
	return false
}

func extractModel(body []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "unknown"
	}
	if model, ok := payload["model"].(string); ok {
		return model
	}
	return "unknown"
}

func rewriteModelInBody(body []byte, targetModel string) []byte {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	payload["model"] = targetModel
	newBody, _ := json.Marshal(payload)
	return newBody
}

func (p *Proxy) shouldRetry(u *router.UpstreamInfo, statusCode int) bool {
	if statusCode == 0 {
		return true // 网络错误默认重试
	}
	if u.RetryOn != nil {
		return u.RetryOn[statusCode]
	}
	return false
}

func extractUsage(body []byte) (int, int) {
	var resp struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err == nil {
		return resp.Usage.PromptTokens, resp.Usage.CompletionTokens
	}
	return 0, 0
}

func computePromptHash(body []byte) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:8]) // 截断前8字节足够去重
}

// buildCacheRequest 构造缓存请求（强校验非空 + 调试日志）
func buildCacheRequest(r *http.Request, model string, body []byte) *proto.CacheRequest {
	// 1. 提取 prompt（绝不为空）
	prompt := extractPromptFromBody(body)
	if prompt == "" {
		// 兜底：用请求体 hash 作为语义占位，避免空字符串导致 Ollama 报错
		prompt = computePromptHash(body)
		slog.Debug("Prompt empty, using body hash as fallback", "hash", prompt)
	}

	// 2. 确保 RequestId 非空（用于追踪 + 缓存键）
	reqId := r.Header.Get("X-Request-Id")
	if reqId == "" {
		reqId = uuid.New().String()
		slog.Debug("X-Request-Id empty, generated UUID", "id", reqId)
	}

	// 🔍 调试日志：确认传入 gRPC 的值（截取前 30 字符防刷屏）
	promptPreview := prompt
	if len(prompt) > 30 {
		promptPreview = prompt[:30] + "..."
	}
	slog.Debug("Building CacheRequest",
		"request_id", reqId,
		"prompt_text", promptPreview,
		"model", model)

	return &proto.CacheRequest{
		RequestId:        reqId,
		PromptText:       prompt,
		Model:            model,
		SystemPromptHash: computePromptHash(body),
		TtlSeconds:       86400,
		// ResponseBody/InputTokens/OutputTokens 由 PUT 请求时填充，此处无需传
	}
}

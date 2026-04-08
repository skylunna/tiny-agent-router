package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/skylunna/tiny-agent-router/internal/cache"
	"github.com/skylunna/tiny-agent-router/internal/metrics"
	"github.com/skylunna/tiny-agent-router/internal/router"
)

// NewHandler 创建代理处理器（修复版：可编译 + 非流式指标上报）
func NewHandler(r *router.Router, costTracker *metrics.CostTracker, cacheClient *cache.Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()

		// 1. 路由选择
		upstream := r.Select()
		if upstream == nil {
			http.Error(w, "No upstream available", http.StatusServiceUnavailable)
			return
		}

		// 2. 模型名映射（客户端 -> 上游）
		originalModel := extractModel(req)
		if upstream.DefaultModel != "" {
			rewriteModel(req, upstream.DefaultModel)
		}

		// 3. 创建反向代理（标准库实现，支持 ModifyResponse 拦截）
		proxy := createProxy(upstream, costTracker, originalModel, start)

		// 4. 执行转发
		proxy.ServeHTTP(w, req)
	})
}

// createProxy 创建带指标上报的反向代理
func createProxy(
	upstream *router.UpstreamInfo,
	costTracker *metrics.CostTracker,
	originalModel string,
	start time.Time,
) *httputil.ReverseProxy {
	target, _ := url.Parse(upstream.BaseURL)

	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			if upstream.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+upstream.APIKey)
			}
			req.Header.Del("Connection")
		},

		// 响应拦截（仅非流式）
		ModifyResponse: func(resp *http.Response) error {
			duration := time.Since(start)

			// 跳过非 200 或非 JSON 响应
			if resp.StatusCode != 200 {
				return nil
			}
			contentType := resp.Header.Get("Content-Type")
			if contentType != "" && contentType != "application/json" && contentType != "application/json; charset=utf-8" {
				return nil
			}

			// 读取响应体
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil // 读取失败不中断响应
			}
			resp.Body = io.NopCloser(bytes.NewBuffer(body)) // 还原

			// 解析 usage 字段
			var usage struct {
				Usage struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(body, &usage); err != nil {
				return nil
			}

			// 上报指标
			if usage.Usage.PromptTokens > 0 && costTracker != nil {
				input := usage.Usage.PromptTokens
				output := usage.Usage.CompletionTokens
				cost := costTracker.CalculateCost(upstream.Name, input, output)

				metrics.Record(upstream.Name, originalModel,
					float64(duration.Milliseconds()),
					input, output, cost, false)

				slog.Debug("Cost recorded",
					"upstream", upstream.Name,
					"input_tokens", input,
					"output_tokens", output,
					"cost_usd", cost)
			}
			return nil
		},

		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("Proxy error", "error", err, "path", r.URL.Path)
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
		},
	}
}

// 工具函数保持不变（extractModel / rewriteModel）
func extractModel(req *http.Request) string {
	if req.Body == nil {
		return "unknown"
	}
	body, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	var payload map[string]interface{}
	if json.Unmarshal(body, &payload) == nil {
		if model, ok := payload["model"].(string); ok {
			return model
		}
	}
	return "unknown"
}

func rewriteModel(req *http.Request, targetModel string) {
	body, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	var payload map[string]interface{}
	if json.Unmarshal(body, &payload) == nil {
		payload["model"] = targetModel
		newBody, _ := json.Marshal(payload)
		req.Body = io.NopCloser(bytes.NewBuffer(newBody))
		req.ContentLength = int64(len(newBody))
	}
}

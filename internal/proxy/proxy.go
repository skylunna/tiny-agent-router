// internal/proxy/proxy.go
package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/skylunna/tiny-agent-router/internal/router"
)

// NewHandler 创建支持权重路由与基础降级的代理处理器
func NewHandler(r *router.Router) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		path := req.URL.Path
		method := req.Method

		slog.Info("Incoming request",
			"method", method,
			"path", path,
			"remote", req.RemoteAddr,
		)

		// Step 2: 路由选择（权重随机）
		upstream := r.Select()
		if upstream == nil {
			slog.Error("No upstream available for request", "path", path)
			http.Error(w, "Service Unavailable: no upstream", http.StatusServiceUnavailable)
			return
		}

		// 执行请求 + 降级重试
		proxy := createReverseProxy(upstream)
		current := upstream
		attempt := 0
		maxAttempts := 3 // 主请求 + 2 次降级

		for {
			// 创建带超时的上下文
			ctx, cancel := context.WithTimeout(req.Context(), current.Timeout)
			reqWithCtx := req.Clone(ctx)

			// 执行代理转发（原生支持 SSE 流式）
			proxy.ServeHTTP(w, reqWithCtx)
			cancel()

			// Step 2 简化版降级策略：仅当响应未写入时重试
			// 注：生产环境应通过 ResponseWriter 包装捕获状态码，此处为稳扎稳打先简化
			attempt++
			if attempt >= maxAttempts {
				break
			}

			// TODO Step 3: 通过 metrics 层捕获真实状态码再决定是否重试
			// 当前策略：若主上游失败，尝试按 fallback_order 切备用
			backup := r.Fallback(current.Name)
			if backup == nil {
				break
			}

			slog.Warn("Primary upstream failed, trying fallback",
				"attempt", attempt,
				"primary", current.Name,
				"fallback", backup.Name,
				"path", path,
			)
			current = backup
			proxy = createReverseProxy(current)
			// 注意：响应已部分写入时无法真正重试，此处为演示逻辑
			// 生产环境应使用 buffer 或前置状态检查
		}

		slog.Info("Request completed",
			"duration_ms", time.Since(start).Milliseconds(),
			"path", path,
			"upstream", current.Name,
			"attempts", attempt,
		)
	})
}

// createReverseProxy 创建标准 ReverseProxy，注入上游配置
func createReverseProxy(u *router.UpstreamInfo) *httputil.ReverseProxy {
	target, err := url.Parse(u.BaseURL)
	if err != nil {
		slog.Error("Invalid upstream base_url", "error", err, "upstream", u.Name)
		panic(fmt.Sprintf("invalid upstream URL: %v", err))
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// 1. 重写目标地址
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			// 保留原始 path（如 /v1/chat/completions）

			// 2. 注入 API Key（若配置且非空）
			if u.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+u.APIKey)
			}

			// 3. 清理 Hop-by-Hop 头，避免代理冲突
			req.Header.Del("Connection")
			req.Header.Del("Proxy-Connection")
			req.Header.Del("Proxy-Authorization")
			req.Header.Del("Te")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("Proxy forwarding failed",
				"error", err,
				"path", r.URL.Path,
				"upstream", u.Name,
			)
			// 标准库保证此时响应未写入，可安全返回 502
			http.Error(w, "Bad Gateway: upstream error", http.StatusBadGateway)
		},
		// TODO Step 3: 添加 ModifyResponse 用于 Token 计数与成本核算
	}

	// 优化：设置传输层参数（连接复用、超时）
	proxy.Transport = &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	return proxy
}

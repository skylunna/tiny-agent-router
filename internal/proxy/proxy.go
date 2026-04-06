package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// NewHandler 创建透明代理处理器，原生支持 OpenAI 协议与 SSE 流式响应
func NewHandler(upstreamURL string, upstreamAPIKey string) http.Handler {
	target, err := url.Parse(upstreamURL)
	if err != nil {
		slog.Error("Invalid upstream URL", "error", err)
		panic(err)
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// 1. 替换目标地址
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			// req.URL.Path 保持客户端请求的原始路径（如 /v1/chat/completions）

			// 2. 注入/覆盖 API Key
			if upstreamAPIKey != "" {
				req.Header.Set("Authorization", "Bearer "+upstreamAPIKey)
			}

			// 3. 清理可能引起上游解析冲突的 Hop-by-Hop 头
			req.Header.Del("Connection")
			req.Header.Del("Proxy-Authorization")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("Proxy forwarding error", "error", err, "path", r.URL.Path)
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
		},
		// TODO Step 3: ModifyResponse 用于拦截响应做 Token 计数
	}

	// 包装日志与耗时统计
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		slog.Info("Incoming request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
		)

		// 透明转发（Go 原生 ReverseProxy 已完美支持 chunked/SSE）
		proxy.ServeHTTP(w, r)

		slog.Info("Request completed",
			"duration_ms", time.Since(start).Milliseconds(),
			"path", r.URL.Path,
		)
	})
}

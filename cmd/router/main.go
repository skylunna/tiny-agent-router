package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/skylunna/tiny-agent-router/internal/proxy"
)

func main() {
	slog.Info("🚀 Tiny Agent Router starting...")

	// 1. 尝试加载 .env 文件（本地开发友好）
	if err := godotenv.Load(); err != nil {
		slog.Debug(".env file not found, falling back to system env vars")
	}

	// Step 1 阶段: 使用环境变量快速切换上游, Step 2 将改为读取 config.yaml
	upstreamURL := os.Getenv("UPSTREAM_BASE_URL")
	if upstreamURL == "" {
		upstreamURL = "https://api.openai.com/v1" // 默认值
	}
	upstreamKey := os.Getenv("UPSTREAM_API_KEY")
	mux := http.NewServeMux()

	// 健康检查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// 挂载代理: 拦截所有 /v1/ 开头的请求
	mux.Handle("/v1/", proxy.NewHandler(upstreamURL, upstreamKey))

	addr := ":7722"
	slog.Info("Server listening", "addr", addr, "upstream", upstreamURL)

	server := &http.Server{Addr: addr, Handler: mux}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("Shutting down server...")
		server.Shutdown(context.Background())
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}

	slog.Info("Server stopped gracefully")
}

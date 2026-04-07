package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/skylunna/tiny-agent-router/internal/config"
	"github.com/skylunna/tiny-agent-router/internal/proxy"
	"github.com/skylunna/tiny-agent-router/internal/router"
)

func main() {
	slog.Info("🚀 Tiny Agent Router starting...")

	// 1. 加载 .env（本地开发友好，不存在也不报错）
	if err := godotenv.Load(); err != nil {
		slog.Debug(".env file not found, falling back to system env vars")
	}

	// 2. 加载 YAML 配置
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("Failed to load config", "path", cfgPath, "error", err)
		os.Exit(1)
	}
	slog.Info("Config loaded", "upstreams_count", len(cfg.Upstreams))

	// 3. 初始化路由策略（权重 + 降级）
	r := router.NewRouter(cfg)

	// 4. 初始化代理处理器（对接 Router）
	handler := proxy.NewHandler(r)

	// 5. 构建 HTTP 服务
	mux := http.NewServeMux()

	// 健康检查（保持 Step 1 兼容）
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			slog.Warn("Failed to write health response", "error", err)
		}
	})

	// 挂载代理：拦截 /v1/ 所有请求（OpenAI 兼容协议）
	mux.Handle("/v1/", handler)

	// 6. 确定监听端口：.env > config.yaml > 默认 7722
	port := os.Getenv("PORT")
	if port == "" {
		port = fmt.Sprintf(":%d", cfg.Server.Port)
	} else if port[0] != ':' {
		port = ":" + port
	}

	slog.Info("Server initializing",
		"addr", port,
		"config_path", cfgPath,
		"upstreams", getUpstreamNames(cfg))

	server := &http.Server{
		Addr:    port,
		Handler: mux,
		// 可选：生产环境建议设置读/写超时
		// ReadTimeout:  15 * time.Second,
		// WriteTimeout: 30 * time.Second,
	}

	// 7. 优雅关闭（保持 Step 1 专业实践）
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("Received shutdown signal", "signal", sig.String())

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			slog.Error("Server shutdown failed", "error", err)
			os.Exit(1)
		}
		slog.Info("Server stopped gracefully")
	}()

	// 8. 启动服务
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("Server failed to start", "error", err)
		os.Exit(1)
	}
}

// 工具函数：日志友好地打印上游名称列表
func getUpstreamNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Upstreams))
	for _, u := range cfg.Upstreams {
		if u.Weight > 0 {
			names = append(names, fmt.Sprintf("%s(w=%d)", u.Name, u.Weight))
		}
	}
	return names
}

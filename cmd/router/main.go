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

	"github.com/skylunna/tiny-agent-router/internal/cache"
	"github.com/skylunna/tiny-agent-router/internal/config"
	"github.com/skylunna/tiny-agent-router/internal/metrics"
	"github.com/skylunna/tiny-agent-router/internal/proxy"
	"github.com/skylunna/tiny-agent-router/internal/router"
)

func main() {
	// 1. 初始化日志
	slog.SetLogLoggerLevel(slog.LevelInfo)
	slog.Info("🚀 tiny-agent-router starting...")

	// 2. 加载 .env（本地开发友好，不存在也不报错）
	if err := godotenv.Load(); err != nil {
		slog.Debug(".env not found, using env vars only")
	}

	// 3. 加载主配置
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("Failed to load config", "path", cfgPath, "error", err)
		return
	}
	slog.Info("Config loaded", "upstreams", len(cfg.Upstreams))

	// 4. 初始化成本追踪器（可选）
	var costTracker *metrics.CostTracker
	if cfg.HasPricing() {
		costTracker, err = metrics.NewCostTracker(cfg.Pricing)
		if err != nil {
			slog.Warn("Cost tracking disabled", "error", err)
		} else {
			slog.Info("💰 Cost tracking enabled")
		}
	}

	// 5. 【新增】初始化语义缓存客户端（Step 4）
	var cacheClient *cache.Client
	if cfg.Cache.Enabled && cfg.Cache.GrpcAddr != "" {
		slog.Info("🔄 Initializing semantic cache client...", "addr", cfg.Cache.GrpcAddr)

		cacheClient, err = cache.NewClient(cfg.Cache.GrpcAddr)
		if err != nil {
			slog.Warn("⚠️ Failed to connect to semantic-cache, running without cache", "error", err)
			cacheClient = nil // 降级：无缓存模式继续运行
		} else {
			slog.Info("🔗 Connected to semantic-cache", "addr", cfg.Cache.GrpcAddr)
		}
	}

	// 6. 初始化路由策略
	r := router.NewRouter(cfg)

	// 7. 初始化可观测性（Prometheus）
	var metricsHandler http.Handler
	if cfg.Observability.EnableMetrics {
		metricsHandler = metrics.InitPrometheus()
		if metricsHandler != nil {
			slog.Info("📊 Prometheus metrics enabled", "path", cfg.Observability.PrometheusPath)
		}
	}

	// 8. 构建 HTTP 服务
	mux := http.NewServeMux()

	// 健康检查（K8s/负载均衡用）
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			slog.Debug("Health check write failed", "error", err)
		}
	})

	// 挂载指标端点
	if metricsHandler != nil {
		mux.Handle(cfg.Observability.PrometheusPath, metricsHandler)
	}

	// 挂载核心代理（/v1/* 路由）- 传入 cacheClient
	proxyHandler := proxy.NewHandler(r, costTracker, cacheClient)
	mux.Handle("/v1/", proxyHandler)

	// 9. 确定监听端口（优先级：.env PORT > config.yaml > 默认 7722）
	port := os.Getenv("PORT")
	if port == "" {
		port = fmt.Sprintf(":%d", cfg.Server.Port)
	} else if port[0] != ':' {
		// 兼容 "7722" 和 ":7722" 两种写法
		port = ":" + port
	}

	server := &http.Server{
		Addr:         port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 10. 启动服务（阻塞）
	go func() {
		slog.Info("Server listening", "addr", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed to start", "error", err)
		}
	}()

	// 11. 优雅关闭（SIGINT/SIGTERM）
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("Shutdown signal received, draining connections...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// ✅ 修复：原代码此处有笔误
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Forced shutdown", "error", err)
	}

	// 【新增】关闭缓存客户端连接
	if cacheClient != nil {
		slog.Info("🔌 Closing semantic-cache connection...")
		if err := cacheClient.Close(); err != nil {
			slog.Warn("Failed to close cache client", "error", err)
		}
	}

	slog.Info("✅ Server stopped gracefully")
}

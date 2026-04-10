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
	// 1. 加载 .env（本地开发友好）
	_ = godotenv.Load()

	// 2. 加载配置
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("❌ Failed to load config", "error", err)
		os.Exit(1)
	}

	// 3. 初始化成本追踪器
	costTracker, err := metrics.NewCostTracker(cfg.Upstreams)
	if err != nil {
		slog.Warn("⚠️ Cost tracking disabled", "error", err)
	}

	// 4. 初始化路由引擎
	r := router.NewRouter(cfg)

	// 5. 初始化缓存客户端（可选，缓存服务不可用时自动 bypass）
	var cacheClient *cache.Client
	if cfg.Cache.Enabled {
		cacheClient, err = cache.NewClient(cfg.Cache.GrpcAddr)
		if err != nil {
			slog.Warn("⚠️ Cache client disabled", "error", err)
		}
	}

	// 6. 初始化代理处理器 ✅ 使用新构造函数
	proxyHandler := proxy.NewProxy(r, cacheClient, costTracker, cfg.Fallback)

	// 7. 初始化可观测性
	var metricsHandler http.Handler
	if cfg.Metrics.EnablePrometheus {
		metricsHandler = metrics.InitPrometheus()
	}

	// 8. 配置 HTTP 路由
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	if metricsHandler != nil {
		mux.Handle(cfg.Metrics.PrometheusPath, metricsHandler)
	}
	// 挂载核心代理
	mux.Handle("/v1/", proxyHandler)

	// 9. 启动服务
	port := os.Getenv("PORT")
	if port == "" {
		port = fmt.Sprintf(":%d", cfg.Server.Port)
	} else if port[0] != ':' {
		port = ":" + port
	}

	slog.Info("🚀 tiny-agent-router starting...",
		"port", port,
		"upstreams", len(cfg.Upstreams),
		"cache_enabled", cfg.Cache.Enabled,
		"metrics_enabled", cfg.Metrics.EnablePrometheus)

	server := &http.Server{
		Addr:         port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  90 * time.Second,
	}

	// 优雅关闭
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("🛑 Shutting down server...", "signal", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cacheClient != nil {
			cacheClient.Close()
		}
		server.Shutdown(ctx)
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("❌ Server failed", "error", err)
		os.Exit(1)
	}
	slog.Info("✅ Server stopped gracefully")
}

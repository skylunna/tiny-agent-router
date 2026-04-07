package metrics

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"

	// API 包：用于创建 instruments
	metric "go.opentelemetry.io/otel/metric"

	// SDK 包：用于初始化 provider（用别名避免冲突）
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// 全局 instruments
var (
	requestDuration, _ = otel.Meter("tiny-agent-router").Float64Histogram(
		"agent.request.duration.ms",
		metric.WithDescription("Request duration in milliseconds"),
		metric.WithUnit("ms"),
	)
	tokenUsage, _ = otel.Meter("tiny-agent-router").Int64Counter(
		"agent.token.usage",
		metric.WithDescription("Token usage by type"),
		metric.WithUnit("{token}"),
	)
	costUSD, _ = otel.Meter("tiny-agent-router").Float64Counter(
		"agent.cost.usd",
		metric.WithDescription("Accumulated cost in USD"),
		metric.WithUnit("USD"),
	)
	fallbackCount, _ = otel.Meter("tiny-agent-router").Int64Counter(
		"agent.fallback.count",
		metric.WithDescription("Number of fallback attempts"),
	)
)

// InitPrometheus 初始化 Prometheus exporter 并返回 /metrics 处理器
func InitPrometheus() http.Handler {
	exporter, err := prometheus.New()
	if err != nil {
		slog.Error("Failed to create Prometheus exporter", "error", err)
		return nil
	}

	// 使用 sdkmetric 别名调用 SDK 函数
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)

	slog.Info("📊 Prometheus metrics enabled at /metrics")
	return promhttp.Handler()
}

// Record 封装指标上报
func Record(upstream, model string, durationMs float64, inputTokens, outputTokens int, cost float64, isFallback bool) {
	baseAttrs := []attribute.KeyValue{
		attribute.String("upstream", upstream),
		attribute.String("model", model),
	}

	ctx := context.Background()

	requestDuration.Record(ctx, durationMs, metric.WithAttributes(baseAttrs...))
	tokenUsage.Add(ctx, int64(inputTokens), metric.WithAttributes(
		append(baseAttrs, attribute.String("type", "input"))...))
	tokenUsage.Add(ctx, int64(outputTokens), metric.WithAttributes(
		append(baseAttrs, attribute.String("type", "output"))...))
	costUSD.Add(ctx, cost, metric.WithAttributes(baseAttrs...))

	if isFallback {
		fallbackCount.Add(ctx, 1, metric.WithAttributes(baseAttrs...))
	}
}

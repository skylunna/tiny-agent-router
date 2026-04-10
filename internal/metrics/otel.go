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
	// ✅ 新增：缓存命中指标
	cacheHits, _ = otel.Meter("tiny-agent-router").Int64Counter(
		"agent.cache.hits",
		metric.WithDescription("Number of cache hits"),
	)
	cacheSimilarity, _ = otel.Meter("tiny-agent-router").Float64Histogram(
		"agent.cache.similarity",
		metric.WithDescription("Similarity score of cache hits"),
		metric.WithUnit("1"),
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

// ✅ 新增：RecordCacheHit 记录缓存命中指标
// model: 客户端请求的模型名（用于维度隔离）
// count: 命中次数增量（通常传 1，符合 Counter.Add 语义）
// similarity: Protobuf optional float 生成的 *float32 指针，内部安全解引用
func RecordCacheHit(model string, count int64, similarity *float32) {
	ctx := context.Background()
	attrs := []attribute.KeyValue{attribute.String("model", model)}

	// 记录命中次数
	cacheHits.Add(ctx, count, metric.WithAttributes(attrs...))

	// 安全记录相似度（optional 字段可能为 nil）
	if similarity != nil {
		cacheSimilarity.Record(ctx, float64(*similarity), metric.WithAttributes(attrs...))
	}
}

package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
	"go.opentelemetry.io/otel/trace/noop"
)

// ChildOnlyTracerProvider 返回一个只在 ctx 里已有「有效且已采样」的父级时才真正开 span 的
// TracerProvider；ctx 里没有父级（k8s 探针、后台 goroutine、没装 traceparent 提取拦截器的
// gRPC handler）时返回 non-recording span：不记录、不发送。
//
// 语义与 entext 的 otelsql SpanFilter、observability/redisotelv6 一致。给 go-redis v9 官方
// redisotel 这类没有 SpanFilter 的插桩用：它把根 span 交给全局 provider，而全局 provider 的
// 默认采样器 ParentBased(AlwaysSample) 会把根 span 100% 记录并发送——完全绕过 istio 的头采样，
// 每条 Redis 命令都变成一条独立的孤儿 trace。
//
// 底层 tracer 在每次 Start 时从全局 provider 取，所以扩展 Init 早于 otel.SetTracerProvider
// 也不会拿到过期的 provider。
func ChildOnlyTracerProvider() trace.TracerProvider {
	return childOnlyTracerProvider{}
}

type childOnlyTracerProvider struct {
	embedded.TracerProvider
}

func (childOnlyTracerProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return childOnlyTracer{name: name, opts: opts}
}

type childOnlyTracer struct {
	embedded.Tracer
	name string
	opts []trace.TracerOption
}

func (t childOnlyTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() || !sc.IsSampled() {
		return ctx, noop.Span{}
	}
	return otel.GetTracerProvider().Tracer(t.name, t.opts...).Start(ctx, spanName, opts...)
}

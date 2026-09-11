package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// 与线上 initOtel 一致：不设 Sampler，走 SDK 默认的 ParentBased(AlwaysSample)。
// 根 span 在这个 provider 下是会被记录的，这正是孤儿 span 的来源。
func recorder(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})
	return exp
}

var (
	parentTraceID = trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36}
	parentSpanID  = trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7}
)

// 模拟 envoy 通过 traceparent 传进来、已被拦截器 Extract 进 ctx 的远程父级
func withRemoteParent(sampled bool) context.Context {
	var flags trace.TraceFlags
	if sampled {
		flags = trace.FlagsSampled
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    parentTraceID,
		SpanID:     parentSpanID,
		TraceFlags: flags,
		Remote:     true,
	})
	return trace.ContextWithRemoteSpanContext(context.Background(), sc)
}

// 对照组：证明差异来自 ChildOnlyTracerProvider 而不是测试环境——
// 直接用全局 provider 开根 span 是会被导出的。
func TestGlobalProvider_RecordsRootSpan(t *testing.T) {
	exp := recorder(t)
	_, span := otel.GetTracerProvider().Tracer("t").Start(context.Background(), "get")
	span.End()
	if got := len(exp.GetSpans()); got != 1 {
		t.Fatalf("global provider exported %d spans, want 1", got)
	}
}

func TestChildOnlyTracerProvider_RootSpanNotRecorded(t *testing.T) {
	exp := recorder(t)
	_, span := ChildOnlyTracerProvider().Tracer("t").Start(context.Background(), "get")
	if span.IsRecording() {
		t.Error("root span IsRecording() = true, want false")
	}
	span.End()
	if got := len(exp.GetSpans()); got != 0 {
		t.Fatalf("exported %d spans, want 0 (root span must be dropped)", got)
	}
}

func TestChildOnlyTracerProvider_ChildOfSampledRemoteParentRecorded(t *testing.T) {
	exp := recorder(t)
	_, span := ChildOnlyTracerProvider().Tracer("t").Start(withRemoteParent(true), "get")
	span.End()
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("exported %d spans, want 1", len(spans))
	}
	if spans[0].Parent.SpanID() != parentSpanID {
		t.Errorf("parent span id = %s, want %s", spans[0].Parent.SpanID(), parentSpanID)
	}
	if spans[0].SpanContext.TraceID() != parentTraceID {
		t.Errorf("trace id = %s, want %s", spans[0].SpanContext.TraceID(), parentTraceID)
	}
}

func TestChildOnlyTracerProvider_ChildOfUnsampledParentNotRecorded(t *testing.T) {
	exp := recorder(t)
	_, span := ChildOnlyTracerProvider().Tracer("t").Start(withRemoteParent(false), "get")
	span.End()
	if got := len(exp.GetSpans()); got != 0 {
		t.Fatalf("exported %d spans, want 0 (parent not sampled)", got)
	}
}

// 用的是调用时的全局 provider，而不是构造时快照——服务启动顺序里
// 扩展 Init 可能早于 SetTracerProvider。
func TestChildOnlyTracerProvider_FollowsGlobalProviderSetLater(t *testing.T) {
	tp := ChildOnlyTracerProvider()
	exp := recorder(t) // 在拿到 provider 之后才替换全局
	_, span := tp.Tracer("t").Start(withRemoteParent(true), "get")
	span.End()
	if got := len(exp.GetSpans()); got != 1 {
		t.Fatalf("exported %d spans, want 1", got)
	}
}

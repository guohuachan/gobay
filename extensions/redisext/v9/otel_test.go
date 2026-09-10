package redisv9ext_test

import (
	"context"
	"testing"

	"github.com/shanbay/gobay"
	redisv9ext "github.com/shanbay/gobay/extensions/redisext/v9"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// 与线上一致的全局 provider：不设 Sampler → ParentBased(AlwaysSample)
func otelRecorder(t *testing.T) *tracetest.InMemoryExporter {
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

// CreateApp 内部的 observability.Initialize() 在 OTEL_ENABLE=true 时会把全局 provider
// 换成真 OTLP 的，测试的 exporter 就接不到了。所以先关着 OTEL 建 app，再开 OTEL 重新 Init 扩展。
func newExt(t *testing.T) *redisv9ext.RedisExt {
	t.Helper()
	t.Setenv("OTEL_ENABLE", "false")
	ext := &redisv9ext.RedisExt{NS: "redis_"}
	app, err := gobay.CreateApp("../../../testdata/", "redispool", map[gobay.Key]gobay.Extension{"redis": ext})
	if err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}
	_ = ext.Close()
	t.Setenv("OTEL_ENABLE", "true")
	if err := ext.Init(app); err != nil {
		t.Fatalf("re-Init with OTEL_ENABLE=true failed: %v", err)
	}
	t.Cleanup(func() { _ = ext.Close() })
	return ext
}

var parentSpanID = trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7}

func sampledParent() context.Context {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36},
		SpanID:     parentSpanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	return trace.ContextWithRemoteSpanContext(context.Background(), sc)
}

// 探针 / 无拦截器的 gRPC handler 传进来的 ctx 没有 span：不能再产出孤儿 span
func TestOtel_CommandWithoutParentExportsNoSpan(t *testing.T) {
	exp := otelRecorder(t)
	ext := newExt(t)
	exp.Reset()

	_, _ = ext.Client().Get(context.Background(), "gobay-otel-orphan-test").Result()

	if spans := exp.GetSpans(); len(spans) != 0 {
		names := make([]string, 0, len(spans))
		for _, s := range spans {
			names = append(names, s.Name)
		}
		t.Fatalf("exported %d orphan spans %v, want 0", len(spans), names)
	}
}

// 有已采样父级时照常产出，且挂在父级下
func TestOtel_CommandUnderSampledParentExportsChildSpan(t *testing.T) {
	exp := otelRecorder(t)
	ext := newExt(t)
	exp.Reset()

	_, _ = ext.Client().Get(sampledParent(), "gobay-otel-child-test").Result()

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("exported 0 spans, want the redis get span")
	}
	var sawGet bool
	for _, s := range spans {
		if s.Parent.SpanID() != parentSpanID {
			t.Errorf("span %q parent = %s, want %s", s.Name, s.Parent.SpanID(), parentSpanID)
		}
		if s.Name == "get" {
			sawGet = true
		}
	}
	if !sawGet {
		t.Error("no span named \"get\" exported")
	}
}

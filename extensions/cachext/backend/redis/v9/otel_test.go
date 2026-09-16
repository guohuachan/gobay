package redis

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func otelRecorder(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	t.Setenv("OTEL_ENABLE", "true")
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

func newBackend(t *testing.T) *redisBackend {
	t.Helper()
	b := &redisBackend{}
	if err := b.Init(cacheConfig(map[string]interface{}{"addr": "127.0.0.1:6379"})); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
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

// CheckHealth 用的就是探针 ctx（无 span）：不能再产出孤儿 ping
func TestOtel_CheckHealthWithoutParentExportsNoSpan(t *testing.T) {
	exp := otelRecorder(t)
	b := newBackend(t)
	exp.Reset()

	if err := b.CheckHealth(context.Background()); err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}
	if spans := exp.GetSpans(); len(spans) != 0 {
		t.Fatalf("exported %d orphan spans, want 0", len(spans))
	}
}

func TestOtel_GetUnderSampledParentExportsChildSpan(t *testing.T) {
	exp := otelRecorder(t)
	b := newBackend(t)
	exp.Reset()

	_, _ = b.Get(sampledParent(), "gobay-otel-child-test")

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("exported 0 spans, want the redis get span")
	}
	for _, s := range spans {
		if s.Parent.SpanID() != parentSpanID {
			t.Errorf("span %q parent = %s, want %s", s.Name, s.Parent.SpanID(), parentSpanID)
		}
	}
}

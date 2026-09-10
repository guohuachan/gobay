package gobay_grpc

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	tpTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	tpSpanID  = "00f067aa0ba902b7"
)

func useTraceContextPropagator(t *testing.T) {
	t.Helper()
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })
}

func invoke(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	var seen context.Context
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		seen = ctx
		return "ok", nil
	}
	resp, err := UnaryServerTracingInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/t.T/M"}, handler)
	if err != nil || resp != "ok" {
		t.Fatalf("interceptor must pass handler result through, got (%v, %v)", resp, err)
	}
	return seen
}

func TestUnaryServerTracingInterceptor_ExtractsTraceparentIntoContext(t *testing.T) {
	useTraceContextPropagator(t)
	md := metadata.Pairs("traceparent", "00-"+tpTraceID+"-"+tpSpanID+"-01")
	seen := invoke(t, metadata.NewIncomingContext(context.Background(), md))

	sc := trace.SpanContextFromContext(seen)
	if !sc.IsValid() {
		t.Fatal("handler ctx has no span context; traceparent was not extracted")
	}
	if sc.TraceID().String() != tpTraceID {
		t.Errorf("trace id = %s, want %s", sc.TraceID(), tpTraceID)
	}
	if sc.SpanID().String() != tpSpanID {
		t.Errorf("span id = %s, want %s", sc.SpanID(), tpSpanID)
	}
	if !sc.IsSampled() {
		t.Error("sampled flag lost")
	}
	if !sc.IsRemote() {
		t.Error("extracted span context should be marked remote")
	}
}

func TestUnaryServerTracingInterceptor_WithoutTraceparentLeavesContextUntouched(t *testing.T) {
	useTraceContextPropagator(t)
	md := metadata.Pairs("x-request-id", "abc")
	seen := invoke(t, metadata.NewIncomingContext(context.Background(), md))
	if trace.SpanContextFromContext(seen).IsValid() {
		t.Error("no traceparent in metadata, yet handler ctx carries a span context")
	}
}

func TestUnaryServerTracingInterceptor_WithoutMetadataLeavesContextUntouched(t *testing.T) {
	useTraceContextPropagator(t)
	seen := invoke(t, context.Background())
	if trace.SpanContextFromContext(seen).IsValid() {
		t.Error("no incoming metadata, yet handler ctx carries a span context")
	}
}

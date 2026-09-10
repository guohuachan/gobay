package ent_mw

import (
	"context"
	"testing"

	"github.com/shanbay/gobay/extensions/entext"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// 让所有已经在用 entext gRPC 中间件的服务不改代码也能拿到父级：
// 中间件内部先把 traceparent 提取进 ctx，再交给 handler。
func TestGetEntUnaryMw_ExtractsTraceparentIntoContext(t *testing.T) {
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	md := metadata.Pairs("traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	var seen context.Context
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		seen = ctx
		return nil, nil
	}
	// 零值 EntExt 就够了：中间件只在出错时才碰 IsNotFound / IsConstraintFailure
	if _, err := GetEntUnaryMw(&entext.EntExt{})(ctx, nil, &grpc.UnaryServerInfo{}, handler); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sc := trace.SpanContextFromContext(seen)
	if !sc.IsValid() || sc.TraceID().String() != traceID {
		t.Fatalf("handler ctx span context = %v, want trace id %s from traceparent", sc, traceID)
	}
}

package gobay_grpc

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// UnaryServerTracingInterceptor 把入站 gRPC metadata 里的 traceparent / tracestate 提取进 ctx，
// 让 handler 里的 DB / Redis 调用都能挂到本次请求的 trace 上。
//
// 只提取、不建 span：应用侧的 Server span 由 istio sidecar 产出，这里补的是 sidecar 到
// handler 之间断掉的那一环。没有 traceparent 时原样透传。放在拦截器链的最前面。
func UnaryServerTracingInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ExtractTraceContext(ctx), req)
	}
}

// ExtractTraceContext 用全局 propagator 把入站 metadata 里的 trace 上下文提取进 ctx。
// 幂等：ctx 里已经有同一个上下文时再调一次没有副作用，所以别的中间件（如 entext 的
// gRPC 中间件）可以在内部先调它，不必要求业务代码显式装拦截器。
func ExtractTraceContext(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, metadataCarrier(md))
}

// gRPC 会把 metadata 的键统一成小写，而 propagation.HeaderCarrier 走 http.Header 的
// 规范化大小写（Traceparent），直接套上去会查不到，所以自己实现一个。
type metadataCarrier metadata.MD

var _ propagation.TextMapCarrier = metadataCarrier{}

func (c metadataCarrier) Get(key string) string {
	if v := metadata.MD(c).Get(key); len(v) > 0 {
		return v[0]
	}
	return ""
}

func (c metadataCarrier) Set(key, value string) {
	metadata.MD(c).Set(key, value)
}

func (c metadataCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

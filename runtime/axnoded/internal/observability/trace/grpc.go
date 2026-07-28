package trace

import (
	"context"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

func InjectTraceInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	traceID, ok := ctx.Value(ContextKeyTraceId).(string)
	if !ok {
		traceID = GetTraceIdFromContext(ctx).String()
		ctx = context.WithValue(ctx, ContextKeyTraceId, traceID)
	}
	logrus.WithFields(logrus.Fields{
		ContextKeyTraceId: traceID,
		"grpc_method":     info.FullMethod,
	}).Debug("received gRPC request")
	start := time.Now()
	resp, err := handler(ctx, req)
	cost := time.Since(start)
	metrics.RecordActionLatencyMs(nameOfMethod(info.FullMethod), cost.Milliseconds())
	if err != nil {
		metrics.RecordActionResult(nameOfMethod(info.FullMethod), "failed")
	} else {
		metrics.RecordActionResult(nameOfMethod(info.FullMethod), "success")
	}
	return resp, err
}

func nameOfMethod(fullMethod string) string {
	return fullMethod[strings.LastIndex(fullMethod, "/")+1:]
}

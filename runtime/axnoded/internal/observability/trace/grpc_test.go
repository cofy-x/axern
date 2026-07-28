package trace

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

func TestInjectTraceInterceptorDoesNotLogRequestPayload(t *testing.T) {
	logger := logrus.StandardLogger()
	previousOutput := logger.Out
	previousFormatter := logger.Formatter
	previousLevel := logger.Level
	t.Cleanup(func() {
		logger.SetOutput(previousOutput)
		logger.SetFormatter(previousFormatter)
		logger.SetLevel(previousLevel)
	})

	var output bytes.Buffer
	logger.SetOutput(&output)
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.DebugLevel)

	const (
		traceID = "f006b765f81d943aaf9339703587f409"
		method  = "/axern.private.node.lifecycle.v1.NodeLifecycle/CreateAllocation"
		secret  = "registry-credential-must-not-be-logged"
	)
	ctx := context.WithValue(context.Background(), ContextKeyTraceId, traceID)
	response, err := InjectTraceInterceptor(ctx, struct{ Credential string }{Credential: secret}, &grpc.UnaryServerInfo{
		FullMethod: method,
	}, func(ctx context.Context, request interface{}) (interface{}, error) {
		if got := ctx.Value(ContextKeyTraceId); got != traceID {
			t.Fatalf("handler trace id = %v, want %s", got, traceID)
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if response != "ok" {
		t.Fatalf("response = %v, want ok", response)
	}

	logged := output.String()
	if strings.Contains(logged, secret) || strings.Contains(logged, "Credential") {
		t.Fatalf("request payload leaked into log: %s", logged)
	}
	if !strings.Contains(logged, method) || !strings.Contains(logged, traceID) {
		t.Fatalf("request metadata missing from log: %s", logged)
	}
}

func TestNameOfMethod(t *testing.T) {
	const fullMethod = "/runtime.v1.RuntimeService/CreateContainer"
	if got := nameOfMethod(fullMethod); got != "CreateContainer" {
		t.Fatalf("nameOfMethod(%q) = %q, want CreateContainer", fullMethod, got)
	}
}

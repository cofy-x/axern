package trace

import (
	"context"
	"reflect"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestGetContextID(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name  string
		args  args
		want  trace.TraceID
		want1 trace.SpanID
	}{
		{
			name: "generates missing ids",
			args: args{
				ctx: buildContext("", ""),
			},
			want:  trace.TraceID{},
			want1: trace.SpanID{},
		},
		{
			name: "returns valid context ids",
			args: args{
				ctx: buildContext("f006b765f81d943aaf9339703587f409", "f2f938ec1fd47e00"),
			},
			want:  mustTraceID("f006b765f81d943aaf9339703587f409"),
			want1: mustSpanID("f2f938ec1fd47e00"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := GetContextID(tt.args.ctx)

			if tt.want.String() != emptyTraceID && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetContextID() got = %v, want %v", got, tt.want)
			}

			if tt.want1.String() != emptySpanID && !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("GetContextID() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

const (
	emptyTraceID = "00000000000000000000000000000000"
	emptySpanID  = "0000000000000000"
)

func buildContext(traceID, spanID string) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, ContextKeyTraceId, traceID)
	ctx = context.WithValue(ctx, ContextKeySpanId, spanID)
	return ctx
}

func mustTraceID(str string) trace.TraceID {
	tid, _ := trace.TraceIDFromHex(str)
	return tid
}

func mustSpanID(str string) trace.SpanID {
	sid, _ := trace.SpanIDFromHex(str)
	return sid
}

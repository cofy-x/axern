package trace

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"math/rand"
	"sync"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	ContextKeyTraceId = "TRACEID"
	ContextKeySpanId  = "SPANID"
)

var defaultIDGenerator sdktrace.IDGenerator

func init() {
	defaultIDGenerator = newRandomTraceIDGenerator()
}

type randomTraceIDGenerator struct {
	sync.Mutex
	randSource *rand.Rand
}

var _ sdktrace.IDGenerator = &randomTraceIDGenerator{}

// NewSpanID returns a non-zero span ID from a randomly-chosen sequence.
func (gen *randomTraceIDGenerator) NewSpanID(ctx context.Context, traceID trace.TraceID) trace.SpanID {
	gen.Lock()
	defer gen.Unlock()
	sid := trace.SpanID{}
	_, _ = gen.randSource.Read(sid[:])
	return sid
}

// NewIDs returns a non-zero trace ID and a non-zero span ID from a
// randomly-chosen sequence.
func (gen *randomTraceIDGenerator) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	gen.Lock()
	defer gen.Unlock()
	tid := trace.TraceID{}
	_, _ = gen.randSource.Read(tid[:])
	sid := trace.SpanID{}
	_, _ = gen.randSource.Read(sid[:])
	return tid, sid
}

func newRandomTraceIDGenerator() sdktrace.IDGenerator {
	gen := &randomTraceIDGenerator{
		Mutex: sync.Mutex{},
	}
	var rngSeed int64
	_ = binary.Read(crand.Reader, binary.LittleEndian, &rngSeed)
	gen.randSource = rand.New(rand.NewSource(rngSeed))
	return gen
}

func GetContextID(ctx context.Context) (trace.TraceID, trace.SpanID) {
	return GetTraceIdFromContext(ctx), GetSpanIdFromContext(ctx)
}

// GetTraceIdFromContext returns the context trace id or generates one.
func GetTraceIdFromContext(ctx context.Context) trace.TraceID {
	if id, ok := ctx.Value(ContextKeyTraceId).(string); ok {
		if tid, err := trace.TraceIDFromHex(id); err == nil {
			return tid
		}
	}
	tid, _ := defaultIDGenerator.NewIDs(context.Background())
	return tid
}

// GetSpanIdFromContext returns the context span id or generates one.
func GetSpanIdFromContext(ctx context.Context) trace.SpanID {
	if id, ok := ctx.Value(ContextKeySpanId).(string); ok {
		if sid, err := trace.SpanIDFromHex(id); err == nil {
			return sid
		}
	}
	_, sid := defaultIDGenerator.NewIDs(context.Background())
	return sid
}

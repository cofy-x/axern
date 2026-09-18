package logrusotel

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	otelog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

func TestHookLevelsCoversLogrusLevels(t *testing.T) {
	hook := New("test")
	if got, want := len(hook.Levels()), len(logrus.AllLevels); got != want {
		t.Fatalf("levels = %d, want %d", got, want)
	}
}

func TestHookEmitsSanitizedRecord(t *testing.T) {
	exporter := &captureExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown logger provider: %v", err)
		}
	})

	logger := logrus.New()
	logger.SetReportCaller(true)
	hook := &Hook{logger: provider.Logger("test")}
	entry := logrus.NewEntry(logger)
	entry.Context = context.Background()
	entry.Time = time.Unix(1_700_000_000, 0)
	entry.Level = logrus.WarnLevel
	entry.Message = "hello"
	entry.Caller = &runtime.Frame{File: "hook_test.go", Line: 42, Function: "TestHookEmitsSanitizedRecord"}
	entry.Data = logrus.Fields{
		"execution_lease_token": "secret",
		"allocation_id":         "allocation-1",
	}

	if err := hook.Fire(entry); err != nil {
		t.Fatalf("Fire() error = %v", err)
	}
	if got, want := len(exporter.records), 1; got != want {
		t.Fatalf("exported records = %d, want %d", got, want)
	}
	record := exporter.records[0]
	if got, want := record.Body().AsString(), "hello"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if got, want := record.Severity(), otelog.SeverityWarn; got != want {
		t.Fatalf("severity = %v, want %v", got, want)
	}
	if got, want := record.SeverityText(), "warning"; got != want {
		t.Fatalf("severity text = %q, want %q", got, want)
	}
	attrs := make(map[string]attribute.Value)
	record.WalkAttributes(func(attr attribute.KeyValue) bool {
		attrs[string(attr.Key)] = attr.Value
		return true
	})
	assertStringAttribute(t, attrs, "execution_lease_token", "[redacted]")
	assertStringAttribute(t, attrs, "allocation_id", "allocation-1")
	assertStringAttribute(t, attrs, "code.filepath", "hook_test.go")
	assertStringAttribute(t, attrs, "code.function", "TestHookEmitsSanitizedRecord")
	if got, want := attrs["code.lineno"].AsInt64(), int64(42); got != want {
		t.Fatalf("code.lineno = %d, want %d", got, want)
	}
}

type captureExporter struct {
	records []sdklog.Record
}

func (e *captureExporter) Export(_ context.Context, records []sdklog.Record) error {
	for i := range records {
		e.records = append(e.records, records[i].Clone())
	}
	return nil
}

func (*captureExporter) Shutdown(context.Context) error { return nil }

func (*captureExporter) ForceFlush(context.Context) error { return nil }

func assertStringAttribute(t *testing.T, attrs map[string]attribute.Value, key, want string) {
	t.Helper()
	value, ok := attrs[key]
	if !ok {
		t.Fatalf("attribute %q is missing", key)
	}
	if got := value.AsString(); got != want {
		t.Fatalf("attribute %q = %q, want %q", key, got, want)
	}
}

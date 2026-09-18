package logrusotel

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/lib/go/observability"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	otelog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
)

type Hook struct {
	logger otelog.Logger
}

func New(name string) *Hook {
	return &Hook{logger: global.Logger(name)}
}

func (h *Hook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *Hook) Fire(entry *logrus.Entry) error {
	if h == nil || h.logger == nil || entry == nil {
		return nil
	}
	ctx := entry.Context
	if ctx == nil {
		ctx = context.Background()
	}
	record := otelog.Record{}
	record.SetTimestamp(entry.Time)
	record.SetObservedTimestamp(time.Now())
	record.SetSeverity(severity(entry.Level))
	record.SetSeverityText(entry.Level.String())
	record.SetBody(attribute.StringValue(observability.SanitizeLogBody(entry.Message)))
	if entry.HasCaller() && entry.Caller != nil {
		record.AddAttributes(
			attribute.String("code.filepath", entry.Caller.File),
			attribute.Int("code.lineno", entry.Caller.Line),
			attribute.String("code.function", entry.Caller.Function),
		)
	}
	for key, value := range entry.Data {
		if observability.SensitiveKey(key) {
			record.AddAttributes(attribute.String(key, "[redacted]"))
			continue
		}
		record.AddAttributes(attribute.String(key, observability.SanitizeValue(fmt.Sprint(value))))
	}
	h.logger.Emit(ctx, record)
	return nil
}

func severity(level logrus.Level) otelog.Severity {
	switch level {
	case logrus.PanicLevel:
		return otelog.SeverityFatal4
	case logrus.FatalLevel:
		return otelog.SeverityFatal
	case logrus.ErrorLevel:
		return otelog.SeverityError
	case logrus.WarnLevel:
		return otelog.SeverityWarn
	case logrus.InfoLevel:
		return otelog.SeverityInfo
	case logrus.DebugLevel:
		return otelog.SeverityDebug
	case logrus.TraceLevel:
		return otelog.SeverityTrace
	default:
		return otelog.SeverityUndefined
	}
}

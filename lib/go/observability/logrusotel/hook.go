package logrusotel

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/lib/go/observability"
	"github.com/sirupsen/logrus"
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
	record.SetBody(otelog.StringValue(observability.SanitizeLogBody(entry.Message)))
	if entry.HasCaller() && entry.Caller != nil {
		record.AddAttributes(
			otelog.String("code.filepath", entry.Caller.File),
			otelog.Int("code.lineno", entry.Caller.Line),
			otelog.String("code.function", entry.Caller.Function),
		)
	}
	for key, value := range entry.Data {
		if observability.SensitiveKey(key) {
			record.AddAttributes(otelog.String(key, "[redacted]"))
			continue
		}
		record.AddAttributes(otelog.String(key, observability.SanitizeValue(fmt.Sprint(value))))
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

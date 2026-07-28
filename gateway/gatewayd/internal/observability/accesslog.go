package observability

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

type AccessLogRecord struct {
	Method       string
	Path         string
	RouteType    string
	Status       int
	Duration     time.Duration
	ErrorClass   string
	Namespace    string
	ServiceID    string
	Port         string
	AllocationID string
	NodeID       string
}

func LogAccess(record AccessLogRecord) {
	fields := logrus.Fields{
		"method":      record.Method,
		"path":        record.Path,
		"route_type":  record.RouteType,
		"status":      record.Status,
		"duration_ms": record.Duration.Milliseconds(),
		"error_class": normalizeLabel(record.ErrorClass, "none"),
	}
	if record.Namespace != "" {
		fields["namespace"] = record.Namespace
	}
	if record.ServiceID != "" {
		fields["service_id"] = record.ServiceID
	}
	if record.Port != "" {
		fields["port"] = record.Port
	}
	if record.AllocationID != "" {
		fields["allocation_id"] = record.AllocationID
	}
	if record.NodeID != "" {
		fields["node_id"] = record.NodeID
	}
	logrus.WithFields(fields).Info(accessLogMessage(record))
}

func accessLogMessage(record AccessLogRecord) string {
	message := fmt.Sprintf(
		"gateway request %s %s %s status=%d duration_ms=%d",
		normalizeLabel(record.RouteType, "unknown"),
		normalizeLabel(record.Method, "unknown"),
		normalizeLabel(record.Path, "/"),
		record.Status,
		record.Duration.Milliseconds(),
	)
	if record.ErrorClass != "" {
		message += " error_class=" + record.ErrorClass
	}
	return message
}

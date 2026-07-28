package timedtrace

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	imgobs "github.com/cofy-x/axern/runtime/imagemgr/internal/observability"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Config describes how a timed operation should be traced and logged.
type Config struct {
	TracerName      string
	Operation       string
	IdentifierKey   string
	IdentifierValue string
	LogPrefix       string
	Attributes      []attribute.KeyValue
}

// Operation wraps a traced operation and records stage timings.
type Operation struct {
	span            trace.Span
	operation       string
	identifierKey   string
	identifierValue string
	logPrefix       string
	startTime       time.Time
	stages          []stageRecord
	err             error
	ended           bool
}

type stageRecord struct {
	name           string
	canonicalPhase string
	duration       time.Duration
}

// Start begins a timed operation.
func Start(ctx context.Context, cfg Config) (*Operation, context.Context) {
	tracerName := cfg.TracerName
	if tracerName == "" {
		tracerName = "timedtrace"
	}
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer(tracerName)
	ctx, span := tracer.Start(ctx, cfg.Operation, trace.WithAttributes(cfg.Attributes...))

	return &Operation{
		span:            span,
		operation:       cfg.Operation,
		identifierKey:   cfg.IdentifierKey,
		identifierValue: cfg.IdentifierValue,
		logPrefix:       cfg.LogPrefix,
		startTime:       time.Now(),
		stages:          make([]stageRecord, 0, 8),
	}, ctx
}

// Stage records a stage timing and emits a span event.
func (o *Operation) Stage(stageName string, duration time.Duration) {
	canonicalPhase := canonicalPhaseForStage(stageName)
	o.stages = append(o.stages, stageRecord{name: stageName, canonicalPhase: canonicalPhase, duration: duration})
	if o.span.IsRecording() {
		attrs := []attribute.KeyValue{
			attribute.String("stage", stageName),
			attribute.Int64("duration_ms", duration.Milliseconds()),
		}
		if canonicalPhase != "" {
			attrs = append(attrs, attribute.String("canonical_phase", canonicalPhase))
		}
		o.span.AddEvent(stageDisplayName(stageName, canonicalPhase), trace.WithAttributes(attrs...))
	}
}

// RecordError records an error on the span.
func (o *Operation) RecordError(err error) {
	if err != nil {
		o.err = err
		o.span.RecordError(err)
		o.span.SetStatus(codes.Error, sdkobs.ResultError)
	}
}

// End closes the span and logs timing summary.
func (o *Operation) End() {
	if o.ended {
		return
	}
	o.ended = true
	defer o.span.End()

	totalDuration := time.Since(o.startTime)
	summary := o.buildSummary(totalDuration)

	fields := logrus.Fields{
		"operation":        o.operation,
		"duration_seconds": totalDuration.Seconds(),
	}
	if o.identifierKey != "" {
		fields[o.identifierKey] = o.identifierValue
	}
	if o.err != nil {
		logrus.WithError(o.err).WithFields(fields).Warn("timed operation failed")
	} else if o.logPrefix != "" {
		logrus.WithFields(fields).Info(o.logPrefix + " " + summary)
	} else {
		logrus.WithFields(fields).Info(summary)
	}
	result := sdkobs.ResultOK
	if o.err != nil {
		result = sdkobs.ResultError
	}
	o.span.SetAttributes(attribute.Int64("total_duration_ms", totalDuration.Milliseconds()), attribute.String(sdkobs.AttrResult, result))
	attrs := []attribute.KeyValue{
		attribute.String(sdkobs.AttrOperation, o.operation),
		attribute.String(sdkobs.AttrResult, result),
	}
	sdkobs.Int64Counter(imgobs.MetricTimedOperationTotal.Name, imgobs.MetricTimedOperationTotal.Description).Add(context.Background(), 1, attrs...)
	sdkobs.DurationHistogram(imgobs.MetricTimedOperationDuration.Name, imgobs.MetricTimedOperationDuration.Description).RecordDuration(context.Background(), totalDuration, attrs...)
	for _, stage := range o.stages {
		stageAttrs := []attribute.KeyValue{
			attribute.String(sdkobs.AttrOperation, o.operation),
			attribute.String(sdkobs.AttrStage, stage.name),
			attribute.String(sdkobs.AttrPhase, stage.canonicalPhase),
			attribute.String(sdkobs.AttrResult, result),
		}
		sdkobs.DurationHistogram(imgobs.MetricTimedOperationStageDuration.Name, imgobs.MetricTimedOperationStageDuration.Description).RecordDuration(context.Background(), stage.duration, stageAttrs...)
	}
}

// Fail marks the operation as failed. The owner must still call End, normally
// with a defer placed immediately after Start.
func (o *Operation) Fail(err error) {
	o.RecordError(err)
}

func (o *Operation) buildSummary(totalDuration time.Duration) string {
	safeTotal := totalDuration
	if safeTotal <= 0 {
		safeTotal = time.Nanosecond
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("operation=%s", o.operation))
	if o.identifierKey != "" {
		summary.WriteString(fmt.Sprintf(" %s=%s", o.identifierKey, o.identifierValue))
	}
	summary.WriteString(fmt.Sprintf(" total=%v", totalDuration))
	for _, stage := range o.stages {
		percentage := float64(stage.duration) / float64(safeTotal) * 100
		summary.WriteString(fmt.Sprintf(", %s=%v(%.1f%%)", stageDisplayName(stage.name, stage.canonicalPhase), stage.duration, percentage))
	}
	return summary.String()
}

func stageDisplayName(stageName, canonicalPhase string) string {
	if canonicalPhase == "" {
		return stageName
	}
	return fmt.Sprintf("%s[%s]", stageName, canonicalPhase)
}

func canonicalPhaseForStage(stageName string) string {
	switch stageName {
	case "reuse_in_memory_mount",
		"reuse_persisted_mount",
		"check_existing_mount",
		"prepare_mount_id",
		"fetch_image",
		"list_layers",
		"build_chain_ids",
		"extract_layers",
		"prepare_lowerdirs",
		"persist_mount_txn",
		"overlay_mount",
		"persist_mount_record",
		"parse_request",
		"validate_dependencies",
		"check_existing_daemon",
		"mount_existing_daemon",
		"prepare_options",
		"create_daemon",
		"get_daemon",
		"daemon_mount",
		"loop_mount",
		"bootstrap_cache_lookup",
		"bootstrap_cache_hit",
		"bootstrap_cache_store",
		"fetch_env_on_cache_hit",
		"read_image_config",
		"check_nydus_format",
		"extract_bootstrap",
		"list_bootstrap_layers",
		"prepare_bootstrap_output",
		"open_bootstrap_stream",
		"scan_bootstrap_archive",
		"copy_bootstrap_file",
		"fetch_bootstrap",
		"fetch_env_for_existing_daemon",
		"clean_mount_point",
		"apply_config",
		"save_metadata",
		"wait_daemon_running",
		"start_daemon_process",
		"wait_mount_ready":
		return "rootfs_prepare"
	default:
		return ""
	}
}

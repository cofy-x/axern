package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/lib/go/llmproxy"
)

func ImportManagedProxyReport(recorder *Recorder, reportJSON []byte) error {
	if recorder == nil || len(reportJSON) == 0 {
		return nil
	}
	var report llmproxy.Report
	if err := json.Unmarshal(reportJSON, &report); err != nil {
		return fmt.Errorf("decode managed proxy report: %w", err)
	}
	if report.Provider != "" {
		recorder.SetProvider(providerForName(report.Provider))
	}
	if err := recorder.importBodies(report.Bodies); err != nil {
		return err
	}
	for _, event := range report.Events {
		recorder.AppendEvent(recorder.managedProxyEvent(event))
	}
	recorder.mergeManagedProxyCounts(report)
	return nil
}

func (r *Recorder) importBodies(bodies []llmproxy.BodyCapture) error {
	if r == nil || len(bodies) == 0 {
		return nil
	}
	llmDir, ok := r.ensureLLMDir()
	if !ok {
		return nil
	}
	for _, body := range bodies {
		if body.Ref == "" {
			continue
		}
		path := filepath.Join(llmDir, filepath.Base(body.Ref))
		if err := os.WriteFile(path, body.Data, 0o600); err != nil {
			return fmt.Errorf("write managed proxy body %q: %w", body.Ref, err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("protect managed proxy body %q: %w", body.Ref, err)
		}
	}
	return nil
}

func (r *Recorder) managedProxyEvent(event llmproxy.Event) domain.AgentRawEvent {
	timestamp := event.Timestamp
	return domain.AgentRawEvent{
		Type:          domain.AgentRawEventType(event.Type),
		Timestamp:     &timestamp,
		Method:        event.Method,
		Path:          event.Path,
		Model:         event.Model,
		Status:        event.Status,
		LatencyMS:     event.LatencyMS,
		Headers:       event.Headers,
		BodyRef:       r.managedProxyBodyRef(event.BodyRef),
		ChunkRef:      r.managedProxyBodyRef(event.ChunkRef),
		Error:         event.Error,
		DroppedEvents: event.DroppedEvents,
		DroppedBodies: event.DroppedBodies,
		DroppedBytes:  event.DroppedBytes,
		Usage:         managedProxyUsage(event.Usage),
		RequestRef:    event.RequestRef,
		ResponseRef:   event.ResponseRef,
	}
}

func (r *Recorder) managedProxyBodyRef(ref string) string {
	if ref == "" {
		return ""
	}
	path := filepath.Join(r.artifactDir, llmArtifactDirname, filepath.Base(ref))
	return runRelativeArtifactPath(r.artifactDir, path)
}

func managedProxyUsage(usage *llmproxy.Usage) *domain.UsageMetrics {
	if usage == nil {
		return nil
	}
	return &domain.UsageMetrics{
		InputTokens:  int(usage.InputTokens),
		OutputTokens: int(usage.OutputTokens),
		TotalTokens:  int(usage.TotalTokens),
	}
}

func (r *Recorder) mergeManagedProxyCounts(report llmproxy.Report) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests += report.RequestCount
	r.responses += report.ResponseCount
	r.errors += report.ErrorCount
	if report.Usage != nil {
		r.usage.InputTokens += int(report.Usage.InputTokens)
		r.usage.OutputTokens += int(report.Usage.OutputTokens)
		r.usage.TotalTokens += int(report.Usage.TotalTokens)
	}
}

func providerForName(name string) Provider {
	switch name {
	case "anthropic":
		return AnthropicProvider()
	case "openai":
		return OpenAIProvider()
	default:
		return nil
	}
}

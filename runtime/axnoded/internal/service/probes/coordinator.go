package probes

import (
	"strings"
	"sync"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

const ReadinessWaitingMessage = "waiting for readiness probe"

type Options struct {
	Report                func(allocationID string, attempt int64, status commonv1.AllocationStatus, exitCode int32, exitCodeKnown bool, ready bool, readinessMessage string, message string, observedAt time.Time)
	TargetState           func(containerID string) (commonv1.AllocationStatus, bool)
	Execute               func(containerID string, probe *Config) (bool, string)
	ExecuteReadiness      func(containerID string, probe *Config) (bool, string)
	HandleLivenessFailure func(containerID string, attempt int64, detail string)
	Observer              Observer
}

type Observer interface {
	RecordProbeAttempt(kind, probeType, result string, duration time.Duration)
	RecordReadinessProbeStage(probeType, stage, result, errorClass string, duration time.Duration)
	RecordReadinessWait(probeType, result string, duration time.Duration)
}

type Coordinator struct {
	options Options

	readinessMu    sync.Mutex
	readinessStops map[string]chan struct{}
	livenessMu     sync.Mutex
	livenessStops  map[string]chan struct{}
	stateMu        sync.Mutex
	readinessSeen  map[string]bool
}

type kind string

const (
	kindReadiness kind = "readiness"
	kindLiveness  kind = "liveness"
)

func NewCoordinator(options Options) *Coordinator {
	return &Coordinator{
		options:        options,
		readinessStops: make(map[string]chan struct{}),
		livenessStops:  make(map[string]chan struct{}),
		readinessSeen:  make(map[string]bool),
	}
}

func (c *Coordinator) StartReadiness(containerID string, attempt int64, probe *Config) {
	containerID = strings.TrimSpace(containerID)
	if probe != nil {
		c.ResetReadinessGate(containerID)
	} else {
		c.ClearState(containerID)
	}
	c.start(containerID, attempt, probe, kindReadiness)
}

func (c *Coordinator) StartLiveness(containerID string, attempt int64, probe *Config) {
	c.start(strings.TrimSpace(containerID), attempt, probe, kindLiveness)
}

func (c *Coordinator) StopReadiness(containerID string) {
	c.stop(strings.TrimSpace(containerID), kindReadiness)
}

func (c *Coordinator) StopLiveness(containerID string) {
	c.stop(strings.TrimSpace(containerID), kindLiveness)
}

func (c *Coordinator) StopAllReadiness() {
	c.stopAll(kindReadiness)
}

func (c *Coordinator) StopAllLiveness() {
	c.stopAll(kindLiveness)
}

func (c *Coordinator) RequiresReadinessGate(containerID string, probe *Config) bool {
	if c == nil || probe == nil || strings.TrimSpace(containerID) == "" {
		return false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	_, ok := c.readinessSeen[containerID]
	return ok
}

func (c *Coordinator) ResetReadinessGate(containerID string) {
	if c == nil || strings.TrimSpace(containerID) == "" {
		return
	}
	c.stateMu.Lock()
	c.readinessSeen[containerID] = false
	c.stateMu.Unlock()
}

func (c *Coordinator) MarkReadinessGateSatisfied(containerID string) {
	if c == nil || strings.TrimSpace(containerID) == "" {
		return
	}
	c.stateMu.Lock()
	if _, ok := c.readinessSeen[containerID]; ok {
		c.readinessSeen[containerID] = true
	}
	c.stateMu.Unlock()
}

func (c *Coordinator) ReadinessGateSatisfied(containerID string) bool {
	if c == nil || strings.TrimSpace(containerID) == "" {
		return false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	ready, ok := c.readinessSeen[containerID]
	if !ok {
		return true
	}
	return ready
}

func (c *Coordinator) ClearState(containerID string) {
	if c == nil || strings.TrimSpace(containerID) == "" {
		return
	}
	c.stateMu.Lock()
	delete(c.readinessSeen, containerID)
	c.stateMu.Unlock()
}

func (c *Coordinator) ClearAllState() {
	if c == nil {
		return
	}
	c.stateMu.Lock()
	clear(c.readinessSeen)
	c.stateMu.Unlock()
}

func (c *Coordinator) start(containerID string, attempt int64, probe *Config, k kind) {
	if c == nil || c.options.Report == nil || c.options.TargetState == nil || strings.TrimSpace(containerID) == "" || attempt <= 0 || probe == nil || !c.hasExecutor(k) {
		return
	}
	stop := make(chan struct{})
	store := c.stopStore(k)
	mu := c.stopMutex(k)
	mu.Lock()
	if existing, ok := store[containerID]; ok {
		close(existing)
	}
	store[containerID] = stop
	mu.Unlock()
	go c.run(containerID, attempt, probe, k, stop)
}

func (c *Coordinator) hasExecutor(k kind) bool {
	if c == nil {
		return false
	}
	if k == kindReadiness {
		return c.options.ExecuteReadiness != nil || c.options.Execute != nil
	}
	return c.options.Execute != nil
}

func (c *Coordinator) stop(containerID string, k kind) {
	if c == nil {
		return
	}
	store := c.stopStore(k)
	mu := c.stopMutex(k)
	mu.Lock()
	stop, ok := store[containerID]
	if ok {
		delete(store, containerID)
	}
	mu.Unlock()
	if ok {
		close(stop)
	}
	if k == kindReadiness {
		c.ClearState(containerID)
	}
}

func (c *Coordinator) stopAll(k kind) {
	if c == nil {
		return
	}
	store := c.stopStore(k)
	mu := c.stopMutex(k)
	mu.Lock()
	stops := make([]chan struct{}, 0, len(store))
	for id, stop := range store {
		delete(store, id)
		stops = append(stops, stop)
	}
	mu.Unlock()
	for _, stop := range stops {
		close(stop)
	}
	if k == kindReadiness {
		c.ClearAllState()
	}
}

func (c *Coordinator) run(containerID string, attempt int64, probe *Config, k kind, stop <-chan struct{}) {
	if c == nil || probe == nil {
		return
	}
	probeType := classifyProbeType(probe)
	waitStarted := time.Now()
	readinessWaitRecorded := false
	recordReadinessWait := func(result string) {
		if k != kindReadiness || readinessWaitRecorded {
			return
		}
		readinessWaitRecorded = true
		if c.options.Observer != nil {
			c.options.Observer.RecordReadinessWait(probeType, result, time.Since(waitStarted))
		}
	}
	if k == kindReadiness {
		c.options.Report(containerID, attempt, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, 0, false, false, ReadinessWaitingMessage, "", time.Now().UTC())
	}
	if delay := time.Duration(probe.InitialDelayMilliseconds) * time.Millisecond; delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-stop:
			recordReadinessWait("stopped")
			return
		case <-timer.C:
		}
	}

	period := time.Duration(probe.PeriodMilliseconds) * time.Millisecond
	if period <= 0 {
		period = 5 * time.Second
	}
	ticker := time.NewTicker(period)
	defer ticker.Stop()

	successes := 0
	failures := 0
	ready := false
	waitingForReadinessGate := k == kindLiveness && c.RequiresReadinessGate(containerID, probe)

	for {
		containerState, ok := c.options.TargetState(containerID)
		if !ok || containerState != commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING {
			recordReadinessWait("target_not_running")
			return
		}

		attemptStarted := time.Now()
		success, detail := c.executeProbe(containerID, probe, k)
		attemptResult := "error"
		if success {
			attemptResult = "ok"
		}
		if c.options.Observer != nil {
			c.options.Observer.RecordProbeAttempt(string(k), probeType, attemptResult, time.Since(attemptStarted))
		}
		if success {
			successes++
			failures = 0
			if k == kindReadiness && !ready && successes >= thresholdOrDefault(probe.SuccessThreshold, 1) {
				ready = true
				c.MarkReadinessGateSatisfied(containerID)
				recordReadinessWait("ok")
				c.options.Report(containerID, attempt, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, 0, false, true, "", "", time.Now().UTC())
			}
		} else {
			if waitingForReadinessGate && !c.ReadinessGateSatisfied(containerID) {
				successes = 0
				failures = 0
				select {
				case <-stop:
					return
				case <-ticker.C:
					continue
				}
			}
			waitingForReadinessGate = false
			failures++
			successes = 0
			if k == kindReadiness {
				if ready {
					if failures >= thresholdOrDefault(probe.FailureThreshold, 1) {
						ready = false
						c.options.Report(containerID, attempt, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, 0, false, false, detail, "", time.Now().UTC())
					}
				} else if detail != "" {
					c.options.Report(containerID, attempt, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, 0, false, false, detail, "", time.Now().UTC())
				}
			} else if failures >= thresholdOrDefault(probe.FailureThreshold, 1) {
				if strings.TrimSpace(detail) == "" {
					detail = "liveness probe failed"
				}
				if c.options.HandleLivenessFailure != nil {
					c.options.HandleLivenessFailure(containerID, attempt, detail)
				}
				return
			}
		}

		select {
		case <-stop:
			recordReadinessWait("stopped")
			return
		case <-ticker.C:
		}
	}
}

func classifyProbeType(probe *Config) string {
	if probe == nil {
		return "unknown"
	}
	switch {
	case probe.HTTP != nil:
		return "http"
	case probe.TCP != nil:
		return "tcp"
	default:
		return "unknown"
	}
}

func (c *Coordinator) executeProbe(containerID string, probe *Config, k kind) (bool, string) {
	if c == nil {
		return false, "probe coordinator unavailable"
	}
	if k == kindReadiness && c.options.ExecuteReadiness != nil {
		return c.options.ExecuteReadiness(containerID, probe)
	}
	if c.options.Execute != nil {
		return c.options.Execute(containerID, probe)
	}
	return true, ""
}

func (c *Coordinator) stopMutex(k kind) *sync.Mutex {
	switch k {
	case kindLiveness:
		return &c.livenessMu
	default:
		return &c.readinessMu
	}
}

func (c *Coordinator) stopStore(k kind) map[string]chan struct{} {
	switch k {
	case kindLiveness:
		return c.livenessStops
	default:
		return c.readinessStops
	}
}

func thresholdOrDefault(value, fallback int32) int {
	if value <= 0 {
		return int(fallback)
	}
	return int(value)
}

func RequestFromConfig(probe *Config) wire.ProbeRequest {
	if probe == nil {
		return wire.ProbeRequest{}
	}
	request := wire.ProbeRequest{TimeoutMS: int(probe.TimeoutMilliseconds)}
	switch {
	case probe.HTTP != nil:
		request.HTTP = &wire.HTTPProbe{
			Port:   int(probe.HTTP.Port),
			Path:   probe.HTTP.Path,
			Scheme: probe.HTTP.Scheme,
		}
	case probe.TCP != nil:
		request.TCP = &wire.TCPProbe{Port: int(probe.TCP.Port)}
	}
	return request
}

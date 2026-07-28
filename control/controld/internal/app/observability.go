package app

import (
	"fmt"

	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
)

func (a *App) registerClusterMetrics() error {
	if a == nil || a.db == nil {
		return nil
	}
	registrations := []sdkobs.ObservableRegistration{}
	for _, spec := range []struct {
		instrument sdkobs.Instrument
		callback   sdkobs.Int64GaugeCallback
	}{
		{ctrlobs.MetricNodesCurrent, a.observeNodes},
		{ctrlobs.MetricNodeResourceCurrent, a.observeNodeResources},
		{ctrlobs.MetricNodeStorageCurrent, a.observeNodeStorage},
		{ctrlobs.MetricNodeBPFNetCurrent, a.observeNodeBPFNet},
		{ctrlobs.MetricNamespaceResourceCurrent, a.observeNamespaceResources},
		{ctrlobs.MetricNodePoolCurrent, a.observeNodePools},
		{ctrlobs.MetricNodeImagesCurrent, a.observeNodeImages},
		{ctrlobs.MetricServicesCurrent, a.observeServices},
		{ctrlobs.MetricServiceReplicasCurrent, a.observeServiceReplicas},
		{ctrlobs.MetricServiceWatchCurrent, a.observeServiceWatch},
		{ctrlobs.MetricRolloutNotificationCurrent, a.observeRolloutNotifications},
		{ctrlobs.MetricRolloutWorkQueueCurrent, a.observeRolloutWorkQueue},
		{ctrlobs.MetricFunctionInvocationQueueCurrent, a.observeFunctionInvocationQueue},
		{ctrlobs.MetricFunctionInvocationNotificationCurrent, a.observeFunctionInvocationNotifications},
		{ctrlobs.MetricVolumeReclaimQueueCurrent, a.observeVolumeReclaimQueue},
		{ctrlobs.MetricAllocationsCurrent, a.observeAllocations},
		{ctrlobs.MetricNodeAllocationsCurrent, a.observeNodeAllocations},
		{ctrlobs.MetricAllocationReconcileQueueCurrent, a.observeAllocationReconcileQueue},
		{ctrlobs.MetricAllocationReconcileQueueOldestAge, a.observeAllocationReconcileQueueOldestAge},
		{ctrlobs.MetricAllocationReconcileAttemptsCurrent, a.observeAllocationReconcileAttempts},
		{ctrlobs.MetricPostgresPoolConnections, a.observePostgresPoolConnections},
		{ctrlobs.MetricReconcileConsecutiveFailures, a.observeReconcileConsecutiveFailures},
		{ctrlobs.MetricReconcileLastSuccessAge, a.observeReconcileLastSuccessAge},
		{ctrlobs.MetricReconcileLastErrorAge, a.observeReconcileLastErrorAge},
		{ctrlobs.MetricReconcileRunning, a.observeReconcileRunning},
		{ctrlobs.MetricReconcileRunningAge, a.observeReconcileRunningAge},
	} {
		registration, err := sdkobs.RegisterInt64ObservableGauge(spec.instrument.Name, spec.instrument.Description, spec.callback)
		if err != nil {
			return fmt.Errorf("register %s: %w", spec.instrument.Name, err)
		}
		registrations = append(registrations, registration)
	}
	for _, spec := range []struct {
		instrument sdkobs.Instrument
		callback   sdkobs.Float64GaugeCallback
	}{
		{ctrlobs.MetricResourcePolicyCurrent, a.observeResourcePolicy},
		{ctrlobs.MetricRolloutWorkOldestDueAge, a.observeRolloutWorkOldestDueAge},
		{ctrlobs.MetricFunctionInvocationOldestDueAge, a.observeFunctionInvocationOldestDueAge},
		{ctrlobs.MetricVolumeReclaimOldestDueAge, a.observeVolumeReclaimOldestDueAge},
	} {
		registration, err := sdkobs.RegisterFloat64ObservableGauge(spec.instrument.Name, spec.instrument.Description, spec.callback)
		if err != nil {
			return fmt.Errorf("register %s: %w", spec.instrument.Name, err)
		}
		registrations = append(registrations, registration)
	}
	a.metrics = append(a.metrics, registrations...)
	return nil
}

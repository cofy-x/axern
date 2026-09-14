package observability

import sdkobs "github.com/cofy-x/axern/lib/go/observability"

var (
	MetricAuthorizationDecisionTotal = sdkobs.Instrument{
		Name:        "axern.controld_authorization_decision_total",
		Description: "Control-plane authorization decisions by bounded action and result.",
	}
	MetricGatewayTerminalResolveTotal = sdkobs.Instrument{
		Name:        "axern.controld_gateway_terminal_resolve_total",
		Description: "Gateway terminal resolve requests.",
	}
	MetricGatewayTerminalResolveDuration = sdkobs.Instrument{
		Name:        "axern.controld_gateway_terminal_resolve_duration_seconds",
		Description: "Gateway terminal resolve latency.",
	}
	MetricReconcileTotal = sdkobs.Instrument{
		Name:        "axern.controld_reconcile_total",
		Description: "Control-plane reconciliation attempts.",
	}
	MetricReconcileDuration = sdkobs.Instrument{
		Name:        "axern.controld_reconcile_duration_seconds",
		Description: "Control-plane reconciliation latency.",
	}
	MetricReconcileConsecutiveFailures = sdkobs.Instrument{
		Name:        "axern.controld_reconcile_consecutive_failures",
		Description: "Current consecutive background reconcile failures by component.",
	}
	MetricReconcileLastSuccessAge = sdkobs.Instrument{
		Name:        "axern.controld_reconcile_last_success_age_seconds",
		Description: "Seconds since the last successful background reconcile by component.",
	}
	MetricReconcileLastErrorAge = sdkobs.Instrument{
		Name:        "axern.controld_reconcile_last_error_age_seconds",
		Description: "Seconds since the last failed background reconcile by component.",
	}
	MetricReconcileRunning = sdkobs.Instrument{
		Name:        "axern.controld_reconcile_running",
		Description: "Whether a background reconciler is currently running by component.",
	}
	MetricReconcileRunningAge = sdkobs.Instrument{
		Name:        "axern.controld_reconcile_running_age_seconds",
		Description: "Seconds since background reconcile work became continuously active by component.",
	}
	MetricAllocationLifecycleReportTotal = sdkobs.Instrument{
		Name:        "axern.controld_allocation_lifecycle_report_total",
		Description: "Allocation lifecycle reports received by controld.",
	}
	MetricAllocationLifecycleReportDuration = sdkobs.Instrument{
		Name:        "axern.controld_allocation_lifecycle_report_duration_seconds",
		Description: "Allocation lifecycle report handling latency.",
	}
	MetricAllocationLifecycleReportStageDuration = sdkobs.Instrument{
		Name:        "axern.controld_allocation_lifecycle_report_stage_duration_seconds",
		Description: "Allocation lifecycle report validation, authentication, and persistence stage duration.",
	}
	MetricEnvironmentOperationTotal = sdkobs.Instrument{
		Name:        "axern.controld_environment_operation_total",
		Description: "Control-plane environment operation requests.",
	}
	MetricEnvironmentOperationDuration = sdkobs.Instrument{
		Name:        "axern.controld_environment_operation_duration_seconds",
		Description: "Control-plane environment operation latency.",
	}
	MetricRunOperationTotal = sdkobs.Instrument{
		Name:        "axern.controld_run_operation_total",
		Description: "Control-plane run operation requests.",
	}
	MetricRunOperationDuration = sdkobs.Instrument{
		Name:        "axern.controld_run_operation_duration_seconds",
		Description: "Control-plane run operation latency.",
	}
	MetricNamespaceOperationTotal = sdkobs.Instrument{
		Name:        "axern.controld_namespace_operation_total",
		Description: "Control-plane namespace operation requests.",
	}
	MetricNamespaceOperationDuration = sdkobs.Instrument{
		Name:        "axern.controld_namespace_operation_duration_seconds",
		Description: "Control-plane namespace operation latency.",
	}
	MetricTunnelOperationTotal = sdkobs.Instrument{
		Name:        "axern.controld_tunnel_operation_total",
		Description: "Control-plane tunnel operation requests.",
	}
	MetricTunnelOperationDuration = sdkobs.Instrument{
		Name:        "axern.controld_tunnel_operation_duration_seconds",
		Description: "Control-plane tunnel operation latency.",
	}
	MetricQuotaOperationTotal = sdkobs.Instrument{
		Name:        "axern.controld_quota_operation_total",
		Description: "Control-plane namespace quota operation requests.",
	}
	MetricQuotaOperationDuration = sdkobs.Instrument{
		Name:        "axern.controld_quota_operation_duration_seconds",
		Description: "Control-plane namespace quota operation latency.",
	}
	MetricResourceAdmissionTotal = sdkobs.Instrument{
		Name:        "axern.controld_resource_admission_total",
		Description: "Control-plane resource admission decisions by scope, result, and reason.",
	}
	MetricResourceAdmissionStageDuration = sdkobs.Instrument{
		Name:        "axern.controld_resource_admission_stage_duration_seconds",
		Description: "Durable resource admission lock, evaluation, and selection stage duration.",
	}
	MetricPostgresPoolConnections = sdkobs.Instrument{
		Name:        "axern.controld_postgres_pool_connections",
		Description: "Controld Postgres pool connections by state.",
	}
	MetricNodeLifecycleRPCDuration = sdkobs.Instrument{
		Name:        "axern.controld_node_lifecycle_rpc_duration_seconds",
		Description: "Controld node lifecycle request build and RPC duration.",
	}
	MetricNodesCurrent = sdkobs.Instrument{
		Name:        "axern.controld_nodes_current",
		Description: "Current controld node count by state.",
	}
	MetricAllocationsCurrent = sdkobs.Instrument{
		Name:        "axern.controld_allocations_current",
		Description: "Current allocation count by owner, status, and readiness.",
	}
	MetricNodeAllocationsCurrent = sdkobs.Instrument{
		Name:        "axern.controld_node_allocations_current",
		Description: "Current allocation count by node, owner, status, and readiness.",
	}
	MetricAllocationReconcileQueueCurrent = sdkobs.Instrument{
		Name:        "axern.controld_allocation_reconcile_queue_current",
		Description: "Current allocation lifecycle reconcile queue size by allocation lifecycle state.",
	}
	MetricAllocationReconcileQueueOldestAge = sdkobs.Instrument{
		Name:        "axern.controld_allocation_reconcile_queue_oldest_age_seconds",
		Description: "Oldest allocation lifecycle reconcile queue age in seconds by allocation lifecycle state.",
	}
	MetricAllocationReconcileAttemptsCurrent = sdkobs.Instrument{
		Name:        "axern.controld_allocation_reconcile_attempts_current",
		Description: "Maximum current allocation lifecycle reconcile attempts by allocation lifecycle state.",
	}
	MetricCapabilityConditionAllocationsCurrent = sdkobs.Instrument{
		Name:        "axern.controld_capability_condition_allocations_current",
		Description: "Current allocations affected by a non-healthy capability condition.",
	}
	MetricCapabilityAdmissionTotal = sdkobs.Instrument{
		Name:        "axern.controld_capability_admission_total",
		Description: "Capability admission outcomes while current node observations are re-evaluated under the reservation lock.",
	}
	MetricNodeCapabilityChangeTotal = sdkobs.Instrument{
		Name:        "axern.controld_node_capability_change_total",
		Description: "Observed Node capability state changes by bounded capability, state, and reason code.",
	}
	MetricNodeResourceCurrent = sdkobs.Instrument{
		Name:        "axern.controld_node_resource_current",
		Description: "Current aggregate node resource quantity by kind.",
	}
	MetricNodeStorageCurrent = sdkobs.Instrument{
		Name:        "axern.controld_node_storage_current",
		Description: "Current node-local Axern storage filesystem quantity by storage target and state.",
	}
	MetricNodeBPFNetCurrent = sdkobs.Instrument{
		Name:        "axern.controld_node_bpfnet_current",
		Description: "Current bpfnet node dataplane state reported by axnoded node summaries.",
	}
	MetricResourcePolicyCurrent = sdkobs.Instrument{
		Name:        "axern.controld_resource_policy_current",
		Description: "Current global resource admission policy values.",
	}
	MetricNamespaceResourceCurrent = sdkobs.Instrument{
		Name:        "axern.controld_namespace_resource_current",
		Description: "Current namespace quota resource quantity by namespace, resource, and state.",
	}
	MetricNodePoolCurrent = sdkobs.Instrument{
		Name:        "axern.controld_node_pool_current",
		Description: "Current aggregate node resource pool count by resource and state.",
	}
	MetricNodeImagesCurrent = sdkobs.Instrument{
		Name:        "axern.controld_node_images_current",
		Description: "Current node-local workload image inventory reported by imagemgr. State distinguishes imported cache entries from mounted workload rootfs entries.",
	}
	MetricPlacementSelectionTotal = sdkobs.Instrument{
		Name:        "axern.controld_placement_selection_total",
		Description: "Placement selections by result.",
	}
	MetricPlacementCandidateTotal = sdkobs.Instrument{
		Name:        "axern.controld_placement_candidate_total",
		Description: "Placement candidate observations by state.",
	}
	MetricPlacementRequestedResourceTotal = sdkobs.Instrument{
		Name:        "axern.controld_placement_requested_resource_total",
		Description: "Placement requested resource amount by selection result.",
	}
	MetricPlacementRejectionTotal = sdkobs.Instrument{
		Name:        "axern.controld_placement_rejection_total",
		Description: "Placement candidate rejection reasons.",
	}
	MetricRetentionDeletedTotal = sdkobs.Instrument{
		Name:        "axern.controld_retention_deleted_total",
		Description: "Rows deleted by controld retention cleanup.",
	}
	MetricRetentionDuration = sdkobs.Instrument{
		Name:        "axern.controld_retention_duration_seconds",
		Description: "controld retention cleanup latency.",
	}
)

package observability

import sdkobs "github.com/cofy-x/axern/lib/go/observability"

var (
	MetricAuthorizationDecisionTotal = sdkobs.Instrument{
		Name:        "axern.controld_authorization_decision_total",
		Description: "Control-plane authorization decisions by bounded action and result.",
	}
	MetricGatewayResolveTotal = sdkobs.Instrument{
		Name:        "axern.controld_gateway_resolve_total",
		Description: "Gateway route resolve requests.",
	}
	MetricGatewayResolveDuration = sdkobs.Instrument{
		Name:        "axern.controld_gateway_resolve_duration_seconds",
		Description: "Gateway route resolve latency.",
	}
	MetricGatewayTerminalResolveTotal = sdkobs.Instrument{
		Name:        "axern.controld_gateway_terminal_resolve_total",
		Description: "Gateway terminal resolve requests.",
	}
	MetricGatewayTerminalResolveDuration = sdkobs.Instrument{
		Name:        "axern.controld_gateway_terminal_resolve_duration_seconds",
		Description: "Gateway terminal resolve latency.",
	}
	MetricServiceOperationTotal = sdkobs.Instrument{
		Name:        "axern.controld_service_operation_total",
		Description: "Control-plane service operation requests.",
	}
	MetricServiceOperationDuration = sdkobs.Instrument{
		Name:        "axern.controld_service_operation_duration_seconds",
		Description: "Control-plane service operation latency.",
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
	MetricAllocationStatusReportTotal = sdkobs.Instrument{
		Name:        "axern.controld_allocation_status_report_total",
		Description: "Allocation status reports received by controld.",
	}
	MetricAllocationStatusReportDuration = sdkobs.Instrument{
		Name:        "axern.controld_allocation_status_report_duration_seconds",
		Description: "Allocation status report handling latency.",
	}
	MetricAllocationStatusReportStageDuration = sdkobs.Instrument{
		Name:        "axern.controld_allocation_status_report_stage_duration_seconds",
		Description: "Allocation status report validation, authentication, and persistence stage duration.",
	}
	MetricServiceStatusBatchStageDuration = sdkobs.Instrument{
		Name:        "axern.controld_service_status_batch_stage_duration_seconds",
		Description: "Service allocation status batch locking, update, projection, and transaction duration.",
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
	MetricQuotaAdmissionTotal = sdkobs.Instrument{
		Name:        "axern.controld_quota_admission_total",
		Description: "Namespace quota admission decisions by result and reason.",
	}
	MetricResourceAdmissionTotal = sdkobs.Instrument{
		Name:        "axern.controld_resource_admission_total",
		Description: "Control-plane resource admission decisions by scope, result, and reason.",
	}
	MetricResourceAdmissionStageDuration = sdkobs.Instrument{
		Name:        "axern.controld_resource_admission_stage_duration_seconds",
		Description: "Durable resource admission lock, evaluation, and selection stage duration.",
	}
	MetricServiceReadyDuration = sdkobs.Instrument{
		Name:        "axern.controld_service_ready_duration_seconds",
		Description: "Time from service creation to all desired replicas ready.",
	}
	MetricServiceReplicaReadyDuration = sdkobs.Instrument{
		Name:        "axern.controld_service_replica_ready_duration_seconds",
		Description: "Time from service replica admission to ready.",
	}
	MetricServiceReplicaStageDuration = sdkobs.Instrument{
		Name:        "axern.controld_service_replica_stage_duration_seconds",
		Description: "Service replica admission and creation stage duration.",
	}
	MetricServiceAllocationQueueDuration = sdkobs.Instrument{
		Name:        "axern.controld_service_allocation_queue_duration_seconds",
		Description: "Service allocation due lag, eligible claim wait, dispatcher wait, and total queue latency.",
	}
	MetricServiceTransactionStageDuration = sdkobs.Instrument{
		Name:        "axern.controld_service_transaction_stage_duration_seconds",
		Description: "Service durable transaction pool acquisition, body, commit, and total duration.",
	}
	MetricPostgresPoolConnections = sdkobs.Instrument{
		Name:        "axern.controld_postgres_pool_connections",
		Description: "Controld Postgres pool connections by state.",
	}
	MetricServiceReconcileStageDuration = sdkobs.Instrument{
		Name:        "axern.controld_service_reconcile_stage_duration_seconds",
		Description: "Service reconcile event queue wait, worker queue wait, sync, and total duration.",
	}
	MetricServiceReconcileQueueOverflowTotal = sdkobs.Instrument{
		Name:        "axern.controld_service_reconcile_queue_overflow_total",
		Description: "Service reconcile keyed queue overflows that fall back to a full sweep.",
	}
	MetricServiceAllocationDispatcherCurrent = sdkobs.Instrument{
		Name:        "axern.controld_service_allocation_dispatcher_current",
		Description: "Current service allocation dispatcher work by state.",
	}
	MetricNodeLifecycleRPCDuration = sdkobs.Instrument{
		Name:        "axern.controld_node_lifecycle_rpc_duration_seconds",
		Description: "Controld node lifecycle request build and RPC duration.",
	}
	MetricNodesCurrent = sdkobs.Instrument{
		Name:        "axern.controld_nodes_current",
		Description: "Current controld node count by state.",
	}
	MetricServicesCurrent = sdkobs.Instrument{
		Name:        "axern.controld_services_current",
		Description: "Current service count by status.",
	}
	MetricServiceReplicasCurrent = sdkobs.Instrument{
		Name:        "axern.controld_service_replicas_current",
		Description: "Current service replica counts by state.",
	}
	MetricServiceWatchCurrent = sdkobs.Instrument{
		Name:        "axern.controld_service_watch_current",
		Description: "Current service watch streams and PostgreSQL listener readiness by state.",
	}
	MetricRolloutNotificationCurrent = sdkobs.Instrument{
		Name:        "axern.controld_rollout_notification_current",
		Description: "Current rollout event and work waiters plus PostgreSQL listener readiness by state.",
	}
	MetricRolloutWorkNotificationTotal = sdkobs.Instrument{
		Name:        "axern.controld_rollout_work_notification_total",
		Description: "Rollout work notifications consumed by action and bounded dispatch result.",
	}
	MetricRolloutWorkWakeupTotal = sdkobs.Instrument{
		Name:        "axern.controld_rollout_work_wakeup_total",
		Description: "Rollout worker long-poll waiters woken by actionable notification type.",
	}
	MetricRolloutWorkClaimTotal = sdkobs.Instrument{
		Name:        "axern.controld_rollout_work_claim_total",
		Description: "Rollout work claim attempts by bounded result.",
	}
	MetricRolloutWorkClaimDuration = sdkobs.Instrument{
		Name:        "axern.controld_rollout_work_claim_duration_seconds",
		Description: "Rollout work claim transaction latency by bounded result.",
	}
	MetricRolloutWorkClaimLag = sdkobs.Instrument{
		Name:        "axern.controld_rollout_work_claim_lag_seconds",
		Description: "Time claimable rollout work waited past its durable due time.",
	}
	MetricRolloutWorkQueueCurrent = sdkobs.Instrument{
		Name:        "axern.controld_rollout_work_queue_current",
		Description: "Current rollout work rows by bounded scheduling state.",
	}
	MetricRolloutWorkOldestDueAge = sdkobs.Instrument{
		Name:        "axern.controld_rollout_work_oldest_due_age_seconds",
		Description: "Age of the oldest due rollout work row by bounded scheduling state.",
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
		Description: "Current allocation lifecycle reconcile queue size by owner and reason.",
	}
	MetricAllocationReconcileQueueOldestAge = sdkobs.Instrument{
		Name:        "axern.controld_allocation_reconcile_queue_oldest_age_seconds",
		Description: "Oldest allocation lifecycle reconcile queue age in seconds by owner and reason.",
	}
	MetricAllocationReconcileAttemptsCurrent = sdkobs.Instrument{
		Name:        "axern.controld_allocation_reconcile_attempts_current",
		Description: "Maximum current allocation lifecycle reconcile attempts by owner and reason.",
	}
	MetricCapabilityReconcileQueueCurrent = sdkobs.Instrument{
		Name:        "axern.controld_capability_reconcile_queue_current",
		Description: "Current allocation capability reconcile work by bounded scheduling state.",
	}
	MetricCapabilityReconcileQueueOldestAge = sdkobs.Instrument{
		Name:        "axern.controld_capability_reconcile_queue_oldest_age_seconds",
		Description: "Age of the oldest due allocation capability reconcile item.",
	}
	MetricCapabilityReconcileAttemptsCurrent = sdkobs.Instrument{
		Name:        "axern.controld_capability_reconcile_attempts_current",
		Description: "Maximum current allocation capability reconcile attempt count.",
	}
	MetricCapabilityReconcileTotal = sdkobs.Instrument{
		Name:        "axern.controld_capability_reconcile_total",
		Description: "Allocation capability reconciliation outcomes by bounded result.",
	}
	MetricCapabilityFailStopTotal = sdkobs.Instrument{
		Name:        "axern.controld_capability_fail_stop_total",
		Description: "Allocation fail-stop requests caused by capability enforcement loss.",
	}
	MetricNodeCapabilityTransitionTotal = sdkobs.Instrument{
		Name:        "axern.controld_node_capability_transition_total",
		Description: "Committed node capability transitions by bounded capability, state, and reason code.",
	}
	MetricCapabilityAdmissionEvidenceTotal = sdkobs.Instrument{
		Name:        "axern.controld_capability_admission_evidence_total",
		Description: "Capability evidence outcomes while candidates are re-evaluated under the admission lock.",
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
	MetricFunctionOperationTotal = sdkobs.Instrument{
		Name:        "axern.controld_function_operation_total",
		Description: "Control-plane function operation requests.",
	}
	MetricFunctionOperationDuration = sdkobs.Instrument{
		Name:        "axern.controld_function_operation_duration_seconds",
		Description: "Control-plane function operation latency.",
	}
	MetricFunctionInvocationTotal = sdkobs.Instrument{
		Name:        "axern.controld_function_invocation_total",
		Description: "Function invocation attempts by mode and terminal status.",
	}
	MetricFunctionInvocationDuration = sdkobs.Instrument{
		Name:        "axern.controld_function_invocation_duration_seconds",
		Description: "Function invocation dispatch latency.",
	}
	MetricFunctionInvocationQueueCurrent = sdkobs.Instrument{
		Name:        "axern.controld_function_invocation_queue_current",
		Description: "Current asynchronous function invocations by bounded scheduling state.",
	}
	MetricFunctionInvocationOldestDueAge = sdkobs.Instrument{
		Name:        "axern.controld_function_invocation_oldest_due_age_seconds",
		Description: "Age of the oldest due asynchronous function invocation by bounded scheduling state.",
	}
	MetricFunctionInvocationNotificationCurrent = sdkobs.Instrument{
		Name:        "axern.controld_function_invocation_notification_current",
		Description: "Current asynchronous Function invocation PostgreSQL listener readiness.",
	}
	MetricVolumeReclaimTotal = sdkobs.Instrument{
		Name:        "axern.controld_volume_reclaim_total",
		Description: "Durable volume reclaim executions by bounded result.",
	}
	MetricVolumeReclaimDuration = sdkobs.Instrument{
		Name:        "axern.controld_volume_reclaim_duration_seconds",
		Description: "Durable volume reclaim execution latency.",
	}
	MetricVolumeReclaimClaimDuration = sdkobs.Instrument{
		Name:        "axern.controld_volume_reclaim_claim_duration_seconds",
		Description: "Durable volume reclaim claim latency.",
	}
	MetricVolumeReclaimDispatcherCurrent = sdkobs.Instrument{
		Name:        "axern.controld_volume_reclaim_dispatcher_current",
		Description: "Current bounded volume reclaim dispatcher work.",
	}
	MetricVolumeReclaimQueueCurrent = sdkobs.Instrument{
		Name:        "axern.controld_volume_reclaim_queue_current",
		Description: "Current durable volume reclaims by scheduling state.",
	}
	MetricVolumeReclaimOldestDueAge = sdkobs.Instrument{
		Name:        "axern.controld_volume_reclaim_oldest_due_age_seconds",
		Description: "Age of the oldest due durable volume reclaim.",
	}
	MetricRetentionDeletedTotal = sdkobs.Instrument{
		Name:        "axern.controld_retention_deleted_total",
		Description: "Rows deleted by controld retention cleanup.",
	}
	MetricRetentionDuration = sdkobs.Instrument{
		Name:        "axern.controld_retention_duration_seconds",
		Description: "controld retention cleanup latency.",
	}
	MetricArtifactTicketTotal = sdkobs.Instrument{
		Name:        "axern.controld_artifact_ticket_total",
		Description: "Rollout artifact ticket issue and resolve requests by bounded result.",
	}
)

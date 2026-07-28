package output

import (
	"fmt"
	"io"
	"strings"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
)

func RenderConsistencySnapshot(w io.Writer, snapshot *adminv1.ConsistencySnapshot) {
	if snapshot == nil {
		return
	}
	fmt.Fprintf(w, "Status: %s\n", consistencyStatusLabel(snapshot.GetStatus()))
	counts := snapshot.GetCounts()
	if counts != nil {
		fmt.Fprintf(w, "Issues: %d\n", counts.GetIssues())
		fmt.Fprintf(w, "Active Reservations: %d\n", counts.GetActiveReservations())
		fmt.Fprintf(w, "Active Leases: %d\n", counts.GetActiveLeases())
		fmt.Fprintf(w, "Active Tunnels: %d\n", counts.GetActiveTunnels())
		fmt.Fprintf(w, "Allocation Lifecycle Retries: %d\n", counts.GetAllocationLifecycleRetries())
	}
	if snapshot.GetTruncated() {
		fmt.Fprintln(w, "Truncated: true")
	}
	if len(snapshot.GetIssues()) == 0 {
		return
	}
	fmt.Fprintln(w)
	RenderConsistencyIssueTable(w, snapshot.GetIssues())
}

func RenderAdminReliabilityHealth(w io.Writer, health *adminv1.AdminReliabilityHealth) {
	if health == nil {
		return
	}
	fmt.Fprintf(w, "Status: %s\n", adminReliabilityStatusLabel(health.GetStatus()))
	fmt.Fprintf(w, "Allocation Lifecycle Retries: %d\n", health.GetAllocationLifecycleRetries())
	fmt.Fprintf(w, "Due Allocation Lifecycle Retries: %d\n", health.GetDueAllocationLifecycleRetries())
	fmt.Fprintf(w, "Reconcile Unhealthy Components: %d\n", health.GetReconcileUnhealthyComponents())
	if consistency := health.GetConsistency(); consistency != nil && consistency.GetCounts() != nil {
		fmt.Fprintf(w, "Consistency Issues: %d\n", consistency.GetCounts().GetIssues())
	}
	if storageBindings := health.GetStorageBindingHealth(); storageBindings != nil {
		fmt.Fprintf(w, "Storage Binding Failed: %d\n", storageBindings.GetFailedBindings())
		fmt.Fprintf(w, "Storage Binding Releasing: %d\n", storageBindings.GetReleasingBindings())
		fmt.Fprintf(w, "Storage Binding Stuck Releasing: %d\n", storageBindings.GetStuckReleasingBindings())
		fmt.Fprintf(w, "Storage Claims Deleting: %d\n", storageBindings.GetDeletingClaims())
		fmt.Fprintf(w, "Storage Claims Stuck Deleting: %d\n", storageBindings.GetStuckDeletingClaims())
		fmt.Fprintf(w, "Storage Inconsistent Claims: %d\n", storageBindings.GetInconsistentClaims())
		fmt.Fprintf(w, "Storage Invalid Bindings: %d\n", storageBindings.GetInvalidBindings())
		if storageBindings.GetUnavailable() {
			fmt.Fprintln(w, "Storage Binding Health Unavailable: true")
		}
		if strings.TrimSpace(storageBindings.GetError()) != "" {
			fmt.Fprintf(w, "Storage Binding Error: %s\n", storageBindings.GetError())
		}
	}
	if nodeVolumes := health.GetNodeVolumeHealth(); nodeVolumes != nil {
		fmt.Fprintf(w, "Node Volume Unhealthy Nodes: %d\n", nodeVolumes.GetUnhealthyNodes())
		fmt.Fprintf(w, "Node Volume Published Volumes: %d\n", nodeVolumes.GetPublishedVolumes())
		fmt.Fprintf(w, "Node Volume Last Reconcile Stale Allocations: %d\n", nodeVolumes.GetLastReconcileStaleAllocations())
		fmt.Fprintf(w, "Node Volume Last Reconcile Invalid Volumes: %d\n", nodeVolumes.GetLastReconcileInvalidVolumes())
		if strings.TrimSpace(nodeVolumes.GetError()) != "" {
			fmt.Fprintf(w, "Node Volume Error: %s\n", nodeVolumes.GetError())
		}
	}
	if nodes := health.GetNodeFleetHealth(); nodes != nil {
		fmt.Fprintf(w, "Node Fleet Unavailable: %t\n", nodes.GetUnavailable())
		if nodes.GetError() != "" {
			fmt.Fprintf(w, "Node Fleet Error: %s\n", nodes.GetError())
		}
		fmt.Fprintf(w, "Node Fleet Active: %d\n", nodes.GetActiveNodes())
		fmt.Fprintf(w, "Node Fleet Ready: %d\n", nodes.GetReadyNodes())
		fmt.Fprintf(w, "Node Fleet Stale Heartbeat: %d\n", nodes.GetStaleHeartbeatNodes())
		fmt.Fprintf(w, "Node Fleet Stale Summary: %d\n", nodes.GetStaleSummaryNodes())
		fmt.Fprintf(w, "Node Fleet Not Ready: %d\n", nodes.GetNotReadyNodes())
	}
	if len(health.GetReconcileComponents()) > 0 {
		fmt.Fprintln(w)
		rows := make([][]string, 0, len(health.GetReconcileComponents()))
		for _, component := range health.GetReconcileComponents() {
			rows = append(rows, []string{
				component.GetComponent(),
				fmt.Sprintf("%t", component.GetRunning()),
				fmt.Sprintf("%d", component.GetConsecutiveFailures()),
				FormatProtoTimestamp(component.GetLastSuccessAt()),
				FormatProtoTimestamp(component.GetLastErrorAt()),
				ShortMessage(component.GetLastError(), 96),
			})
		}
		RenderTable(w, []string{"COMPONENT", "RUNNING", "FAILURES", "LAST SUCCESS", "LAST ERROR", "ERROR"}, rows)
	}
	if len(health.GetSignals()) == 0 {
		return
	}
	fmt.Fprintln(w)
	rows := make([][]string, 0, len(health.GetSignals()))
	for _, signal := range health.GetSignals() {
		rows = append(rows, []string{
			adminReliabilitySignalCodeLabel(signal.GetCode()),
			ShortMessage(signal.GetMessage(), 96),
		})
	}
	RenderTable(w, []string{"SIGNAL", "MESSAGE"}, rows)
}

func RenderConsistencyIssueTable(w io.Writer, issues []*adminv1.ConsistencyIssue) {
	rows := make([][]string, 0, len(issues))
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		rows = append(rows, []string{
			consistencyIssueCodeLabel(issue.GetCode()),
			consistencyIssueSeverityLabel(issue.GetSeverity()),
			issue.GetAllocationID(),
			issue.GetOwnerType(),
			issue.GetOwnerID(),
			issue.GetNodeID(),
			issue.GetStatus(),
			consistencyRepairOwnerLabel(issue.GetRepairOwner()),
			consistencyRepairActionLabel(issue.GetRepairAction()),
			consistencyRepairTargetLabel(issue.GetRepairTargetType(), issue.GetRepairTargetID()),
			ShortMessage(issue.GetDetail(), 72),
		})
	}
	RenderTable(w, []string{"CODE", "SEVERITY", "ALLOCATION", "OWNER", "OWNER_ID", "NODE", "STATUS", "REPAIR_OWNER", "REPAIR", "REPAIR_TARGET", "DETAIL"}, rows)
}

func adminReliabilityStatusLabel(status adminv1.AdminReliabilityStatus) string {
	return strings.ToLower(trimEnumPrefix(status.String(), "ADMIN_RELIABILITY_STATUS_"))
}

func adminReliabilitySignalCodeLabel(code adminv1.AdminReliabilitySignalCode) string {
	return strings.ToLower(trimEnumPrefix(code.String(), "ADMIN_RELIABILITY_SIGNAL_CODE_"))
}

func consistencyStatusLabel(status adminv1.ConsistencyStatus) string {
	return strings.ToLower(trimEnumPrefix(status.String(), "CONSISTENCY_STATUS_"))
}

func consistencyIssueSeverityLabel(severity adminv1.ConsistencyIssueSeverity) string {
	return strings.ToLower(trimEnumPrefix(severity.String(), "CONSISTENCY_ISSUE_SEVERITY_"))
}

func consistencyIssueCodeLabel(code adminv1.ConsistencyIssueCode) string {
	return strings.ToLower(trimEnumPrefix(code.String(), "CONSISTENCY_ISSUE_CODE_"))
}

func consistencyRepairOwnerLabel(owner adminv1.ConsistencyRepairOwner) string {
	return strings.ToLower(trimEnumPrefix(owner.String(), "CONSISTENCY_REPAIR_OWNER_"))
}

func consistencyRepairActionLabel(action adminv1.ConsistencyRepairAction) string {
	return strings.ToLower(trimEnumPrefix(action.String(), "CONSISTENCY_REPAIR_ACTION_"))
}

func consistencyRepairTargetTypeLabel(targetType adminv1.ConsistencyRepairTargetType) string {
	return strings.ToLower(trimEnumPrefix(targetType.String(), "CONSISTENCY_REPAIR_TARGET_TYPE_"))
}

func consistencyRepairTargetLabel(targetType adminv1.ConsistencyRepairTargetType, targetID string) string {
	label := consistencyRepairTargetTypeLabel(targetType)
	if label == "" || label == "unspecified" {
		return targetID
	}
	if targetID == "" {
		return label
	}
	return label + "/" + targetID
}

type ConsistencySnapshotJSON struct {
	Status    string                  `json:"status"`
	Counts    *ConsistencyCountsJSON  `json:"counts,omitempty"`
	Issues    []*ConsistencyIssueJSON `json:"issues"`
	Truncated bool                    `json:"truncated"`
}

type AdminReliabilityHealthJSON struct {
	Status                        string                          `json:"status"`
	Consistency                   *ConsistencySnapshotJSON        `json:"consistency,omitempty"`
	AllocationLifecycleRetries    int64                           `json:"allocation_lifecycle_retries"`
	DueAllocationLifecycleRetries int64                           `json:"due_allocation_lifecycle_retries"`
	ReconcileUnhealthyComponents  int64                           `json:"reconcile_unhealthy_components"`
	StorageBindingHealth          *AdminStorageBindingHealthJSON  `json:"storage_binding_health,omitempty"`
	NodeVolumeHealth              *AdminNodeVolumeHealthJSON      `json:"node_volume_health,omitempty"`
	NodeFleetHealth               *AdminNodeFleetHealthJSON       `json:"node_fleet_health,omitempty"`
	ReconcileComponents           []*ReconcileComponentHealthJSON `json:"reconcile_components"`
	Signals                       []*AdminReliabilitySignalJSON   `json:"signals"`
}

type ReconcileComponentHealthJSON struct {
	Component           string `json:"component"`
	Running             bool   `json:"running"`
	LastStartedAt       string `json:"last_started_at,omitempty"`
	LastSuccessAt       string `json:"last_success_at,omitempty"`
	LastErrorAt         string `json:"last_error_at,omitempty"`
	LastError           string `json:"last_error,omitempty"`
	ConsecutiveFailures int64  `json:"consecutive_failures"`
}

type ConsistencyCountsJSON struct {
	ActiveReservations         int64 `json:"active_reservations"`
	ActiveLeases               int64 `json:"active_leases"`
	ActiveTunnels              int64 `json:"active_tunnels"`
	AllocationLifecycleRetries int64 `json:"allocation_lifecycle_retries"`
	Issues                     int64 `json:"issues"`
}

type ConsistencyIssueJSON struct {
	Code             string `json:"code"`
	Severity         string `json:"severity"`
	AllocationID     string `json:"allocation_id,omitempty"`
	OwnerType        string `json:"owner_type,omitempty"`
	OwnerID          string `json:"owner_id,omitempty"`
	NodeID           string `json:"node_id,omitempty"`
	Status           string `json:"status,omitempty"`
	Detail           string `json:"detail,omitempty"`
	RepairOwner      string `json:"repair_owner,omitempty"`
	RepairAction     string `json:"repair_action,omitempty"`
	RepairTargetType string `json:"repair_target_type,omitempty"`
	RepairTargetID   string `json:"repair_target_id,omitempty"`
	AutomaticRepair  bool   `json:"automatic_repair"`
}

type AdminReliabilitySignalJSON struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type AdminStorageBindingHealthJSON struct {
	Unavailable            bool   `json:"unavailable"`
	Error                  string `json:"error,omitempty"`
	FailedBindings         int64  `json:"failed_bindings"`
	ReleasingBindings      int64  `json:"releasing_bindings"`
	StuckReleasingBindings int64  `json:"stuck_releasing_bindings"`
	DeletingClaims         int64  `json:"deleting_claims"`
	StuckDeletingClaims    int64  `json:"stuck_deleting_claims"`
	InconsistentClaims     int64  `json:"inconsistent_claims"`
	InvalidBindings        int64  `json:"invalid_bindings"`
}

type AdminNodeVolumeHealthJSON struct {
	UnhealthyNodes                int64  `json:"unhealthy_nodes"`
	PublishedVolumes              int64  `json:"published_volumes"`
	LastReconcileStaleAllocations int64  `json:"last_reconcile_stale_allocations"`
	LastReconcileInvalidVolumes   int64  `json:"last_reconcile_invalid_volumes"`
	Error                         string `json:"error,omitempty"`
}

type AdminNodeFleetHealthJSON struct {
	Unavailable         bool   `json:"unavailable"`
	Error               string `json:"error,omitempty"`
	ActiveNodes         int64  `json:"active_nodes"`
	ReadyNodes          int64  `json:"ready_nodes"`
	StaleHeartbeatNodes int64  `json:"stale_heartbeat_nodes"`
	StaleSummaryNodes   int64  `json:"stale_summary_nodes"`
	NotReadyNodes       int64  `json:"not_ready_nodes"`
}

func PrintConsistencySnapshotJSON(w io.Writer, snapshot *adminv1.ConsistencySnapshot) error {
	return PrintJSON(w, NewConsistencySnapshotJSON(snapshot))
}

func PrintAdminReliabilityHealthJSON(w io.Writer, health *adminv1.AdminReliabilityHealth) error {
	return PrintJSON(w, NewAdminReliabilityHealthJSON(health))
}

func NewAdminReliabilityHealthJSON(health *adminv1.AdminReliabilityHealth) *AdminReliabilityHealthJSON {
	if health == nil {
		return nil
	}
	signals := make([]*AdminReliabilitySignalJSON, 0, len(health.GetSignals()))
	for _, signal := range health.GetSignals() {
		signals = append(signals, &AdminReliabilitySignalJSON{
			Code:    adminReliabilitySignalCodeLabel(signal.GetCode()),
			Message: signal.GetMessage(),
		})
	}
	components := make([]*ReconcileComponentHealthJSON, 0, len(health.GetReconcileComponents()))
	for _, component := range health.GetReconcileComponents() {
		components = append(components, &ReconcileComponentHealthJSON{
			Component:           component.GetComponent(),
			Running:             component.GetRunning(),
			LastStartedAt:       FormatProtoTimestamp(component.GetLastStartedAt()),
			LastSuccessAt:       FormatProtoTimestamp(component.GetLastSuccessAt()),
			LastErrorAt:         FormatProtoTimestamp(component.GetLastErrorAt()),
			LastError:           component.GetLastError(),
			ConsecutiveFailures: component.GetConsecutiveFailures(),
		})
	}
	return &AdminReliabilityHealthJSON{
		Status:                        adminReliabilityStatusLabel(health.GetStatus()),
		Consistency:                   NewConsistencySnapshotJSON(health.GetConsistency()),
		AllocationLifecycleRetries:    health.GetAllocationLifecycleRetries(),
		DueAllocationLifecycleRetries: health.GetDueAllocationLifecycleRetries(),
		ReconcileUnhealthyComponents:  health.GetReconcileUnhealthyComponents(),
		StorageBindingHealth:          NewAdminStorageBindingHealthJSON(health.GetStorageBindingHealth()),
		NodeVolumeHealth:              NewAdminNodeVolumeHealthJSON(health.GetNodeVolumeHealth()),
		NodeFleetHealth:               NewAdminNodeFleetHealthJSON(health.GetNodeFleetHealth()),
		ReconcileComponents:           components,
		Signals:                       signals,
	}
}

func NewAdminNodeFleetHealthJSON(health *adminv1.AdminNodeFleetHealth) *AdminNodeFleetHealthJSON {
	if health == nil {
		return nil
	}
	return &AdminNodeFleetHealthJSON{
		Unavailable:         health.GetUnavailable(),
		Error:               health.GetError(),
		ActiveNodes:         health.GetActiveNodes(),
		ReadyNodes:          health.GetReadyNodes(),
		StaleHeartbeatNodes: health.GetStaleHeartbeatNodes(),
		StaleSummaryNodes:   health.GetStaleSummaryNodes(),
		NotReadyNodes:       health.GetNotReadyNodes(),
	}
}

func NewAdminStorageBindingHealthJSON(health *adminv1.AdminStorageBindingHealth) *AdminStorageBindingHealthJSON {
	if health == nil {
		return nil
	}
	return &AdminStorageBindingHealthJSON{
		Unavailable:            health.GetUnavailable(),
		Error:                  health.GetError(),
		FailedBindings:         health.GetFailedBindings(),
		ReleasingBindings:      health.GetReleasingBindings(),
		StuckReleasingBindings: health.GetStuckReleasingBindings(),
		DeletingClaims:         health.GetDeletingClaims(),
		StuckDeletingClaims:    health.GetStuckDeletingClaims(),
		InconsistentClaims:     health.GetInconsistentClaims(),
		InvalidBindings:        health.GetInvalidBindings(),
	}
}

func NewAdminNodeVolumeHealthJSON(health *adminv1.AdminNodeVolumeHealth) *AdminNodeVolumeHealthJSON {
	if health == nil {
		return nil
	}
	return &AdminNodeVolumeHealthJSON{
		UnhealthyNodes:                health.GetUnhealthyNodes(),
		PublishedVolumes:              health.GetPublishedVolumes(),
		LastReconcileStaleAllocations: health.GetLastReconcileStaleAllocations(),
		LastReconcileInvalidVolumes:   health.GetLastReconcileInvalidVolumes(),
		Error:                         health.GetError(),
	}
}

func NewConsistencySnapshotJSON(snapshot *adminv1.ConsistencySnapshot) *ConsistencySnapshotJSON {
	if snapshot == nil {
		return nil
	}
	issues := make([]*ConsistencyIssueJSON, 0, len(snapshot.GetIssues()))
	for _, issue := range snapshot.GetIssues() {
		issues = append(issues, &ConsistencyIssueJSON{
			Code:             consistencyIssueCodeLabel(issue.GetCode()),
			Severity:         consistencyIssueSeverityLabel(issue.GetSeverity()),
			AllocationID:     issue.GetAllocationID(),
			OwnerType:        issue.GetOwnerType(),
			OwnerID:          issue.GetOwnerID(),
			NodeID:           issue.GetNodeID(),
			Status:           issue.GetStatus(),
			Detail:           issue.GetDetail(),
			RepairOwner:      consistencyRepairOwnerLabel(issue.GetRepairOwner()),
			RepairAction:     consistencyRepairActionLabel(issue.GetRepairAction()),
			RepairTargetType: consistencyRepairTargetTypeLabel(issue.GetRepairTargetType()),
			RepairTargetID:   issue.GetRepairTargetID(),
			AutomaticRepair:  issue.GetAutomaticRepair(),
		})
	}
	var counts *ConsistencyCountsJSON
	if raw := snapshot.GetCounts(); raw != nil {
		counts = &ConsistencyCountsJSON{
			ActiveReservations:         raw.GetActiveReservations(),
			ActiveLeases:               raw.GetActiveLeases(),
			ActiveTunnels:              raw.GetActiveTunnels(),
			AllocationLifecycleRetries: raw.GetAllocationLifecycleRetries(),
			Issues:                     raw.GetIssues(),
		}
	}
	return &ConsistencySnapshotJSON{
		Status:    consistencyStatusLabel(snapshot.GetStatus()),
		Counts:    counts,
		Issues:    issues,
		Truncated: snapshot.GetTruncated(),
	}
}

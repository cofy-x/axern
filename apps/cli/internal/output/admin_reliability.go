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
		fmt.Fprintf(w, "Active Allocations: %d\n", counts.GetActiveAllocations())
		fmt.Fprintf(w, "Active Access Grants: %d\n", counts.GetActiveAccessGrants())
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
			issue.GetRunID(),
			issue.GetNodeID(),
			issue.GetStatus(),
			consistencyRepairOwnerLabel(issue.GetRepairOwner()),
			consistencyRepairActionLabel(issue.GetRepairAction()),
			consistencyRepairTargetLabel(issue.GetRepairTargetType(), issue.GetRepairTargetID()),
			ShortMessage(issue.GetDetail(), 72),
		})
	}
	RenderTable(w, []string{"CODE", "SEVERITY", "ALLOCATION", "RUN", "NODE", "STATUS", "REPAIR_OWNER", "REPAIR", "REPAIR_TARGET", "DETAIL"}, rows)
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
	ActiveAllocations          int64 `json:"active_allocations"`
	ActiveAccessGrants         int64 `json:"active_access_grants"`
	ActiveTunnels              int64 `json:"active_tunnels"`
	AllocationLifecycleRetries int64 `json:"allocation_lifecycle_retries"`
	Issues                     int64 `json:"issues"`
}

type ConsistencyIssueJSON struct {
	Code             string `json:"code"`
	Severity         string `json:"severity"`
	AllocationID     string `json:"allocation_id,omitempty"`
	RunID            string `json:"run_id,omitempty"`
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
			RunID:            issue.GetRunID(),
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
			ActiveAllocations:          raw.GetActiveAllocations(),
			ActiveAccessGrants:         raw.GetActiveAccessGrants(),
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

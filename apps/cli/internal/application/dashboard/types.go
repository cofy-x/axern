package dashboard

type LinksConfig struct {
	ContextName string
	ServiceURL  string
}

type Summary struct {
	Services    ServiceSummary      `json:"services"`
	Tunnels     TunnelSummary       `json:"tunnels"`
	Quotas      QuotaSummary        `json:"quotas"`
	Reliability *ReliabilitySummary `json:"reliability,omitempty"`
}

type ReconcileHealth struct {
	Components []ReconcileComponentHealth `json:"components"`
}

type ReconcileComponentHealth struct {
	Component           string `json:"component"`
	Running             bool   `json:"running"`
	LastStartedAt       string `json:"last_started_at,omitempty"`
	LastSuccessAt       string `json:"last_success_at,omitempty"`
	LastErrorAt         string `json:"last_error_at,omitempty"`
	LastError           string `json:"last_error,omitempty"`
	ConsecutiveFailures int64  `json:"consecutive_failures"`
}

type ServiceSummary struct {
	Total            int `json:"total"`
	Ready            int `json:"ready"`
	Degraded         int `json:"degraded"`
	Failed           int `json:"failed"`
	Reconciling      int `json:"reconciling"`
	AdmissionBlocked int `json:"admission_blocked"`
}

type TunnelSummary struct {
	Total    int `json:"total"`
	Running  int `json:"running"`
	Pending  int `json:"pending"`
	Degraded int `json:"degraded"`
	Failed   int `json:"failed"`
}

type QuotaSummary struct {
	Namespaces        int `json:"namespaces"`
	CPUConstrained    int `json:"cpu_constrained"`
	MemoryConstrained int `json:"memory_constrained"`
	CPUPressure       int `json:"cpu_pressure"`
	MemoryPressure    int `json:"memory_pressure"`
}

type ReliabilitySummary struct {
	Status                        string                     `json:"status,omitempty"`
	ConsistencyStatus             string                     `json:"consistency_status,omitempty"`
	ConsistencyIssues             int64                      `json:"consistency_issues"`
	AllocationLifecycleRetries    int64                      `json:"allocation_lifecycle_retries"`
	DueAllocationLifecycleRetries int64                      `json:"due_allocation_lifecycle_retries"`
	ReconcileUnhealthyComponents  int64                      `json:"reconcile_unhealthy_components"`
	NodeFleet                     *NodeFleetSummary          `json:"node_fleet,omitempty"`
	Signals                       []ReliabilitySignalSummary `json:"signals"`
	Issues                        []ConsistencyIssueSummary  `json:"issues"`
}

type NodeFleetSummary struct {
	Unavailable         bool   `json:"unavailable"`
	Error               string `json:"error,omitempty"`
	ActiveNodes         int64  `json:"active_nodes"`
	ReadyNodes          int64  `json:"ready_nodes"`
	StaleHeartbeatNodes int64  `json:"stale_heartbeat_nodes"`
	StaleSummaryNodes   int64  `json:"stale_summary_nodes"`
	NotReadyNodes       int64  `json:"not_ready_nodes"`
}

type ReliabilitySignalSummary struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ConsistencyIssueSummary struct {
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

type AdminState struct {
	Retries []AllocationLifecycleRetryDTO `json:"retries"`
	Audit   []AdminAuditEventDTO          `json:"audit"`
}

type AdminRetryActionResult struct {
	Retry AllocationLifecycleRetryDTO `json:"retry"`
}

type AllocationLifecycleRetryDTO struct {
	AllocationID       string `json:"allocation_id"`
	OwnerID            string `json:"owner_id,omitempty"`
	OwnerType          string `json:"owner_type,omitempty"`
	EnvironmentID      string `json:"environment_id,omitempty"`
	Reason             string `json:"reason,omitempty"`
	NodeID             string `json:"node_id,omitempty"`
	NodeTarget         string `json:"node_target,omitempty"`
	Attempt            int64  `json:"attempt,omitempty"`
	ReconcileAttempts  int32  `json:"reconcile_attempts,omitempty"`
	LastError          string `json:"last_error,omitempty"`
	NextRunAt          string `json:"next_run_at,omitempty"`
	CreatedAt          string `json:"created_at,omitempty"`
	UpdatedAt          string `json:"updated_at,omitempty"`
	AgeSeconds         int64  `json:"age_seconds,omitempty"`
	Due                bool   `json:"due"`
	Clearable          bool   `json:"clearable"`
	ClearBlockedReason string `json:"clear_blocked_reason,omitempty"`
}

type AdminAuditEventDTO struct {
	EventID        string `json:"event_id"`
	Operation      string `json:"operation"`
	TargetType     string `json:"target_type"`
	TargetID       string `json:"target_id"`
	OperatorReason string `json:"operator_reason"`
	CreatedAt      string `json:"created_at,omitempty"`
}

type QuotaDTO struct {
	Namespace            string `json:"namespace"`
	CPUMilliLimit        *int64 `json:"cpu_milli_limit,omitempty"`
	MemoryBytesLimit     *int64 `json:"memory_bytes_limit,omitempty"`
	ReservedCPUMilli     int64  `json:"reserved_cpu_milli"`
	ReservedMemoryBytes  int64  `json:"reserved_memory_bytes"`
	AvailableCPUMilli    *int64 `json:"available_cpu_milli,omitempty"`
	AvailableMemoryBytes *int64 `json:"available_memory_bytes,omitempty"`
	CPUUsagePercent      *int64 `json:"cpu_usage_percent,omitempty"`
	MemoryUsagePercent   *int64 `json:"memory_usage_percent,omitempty"`
	Version              int64  `json:"version"`
	UpdatedAt            string `json:"updated_at,omitempty"`
}

type QuotaEventDTO struct {
	ID                   string `json:"id"`
	Namespace            string `json:"namespace"`
	Type                 string `json:"type"`
	WorkloadType         string `json:"workload_type,omitempty"`
	WorkloadID           string `json:"workload_id,omitempty"`
	EnvironmentID        string `json:"environment_id,omitempty"`
	Reason               string `json:"reason,omitempty"`
	RequestedCPUMilli    int64  `json:"requested_cpu_milli,omitempty"`
	ReservedCPUMilli     int64  `json:"reserved_cpu_milli,omitempty"`
	CPUMilliLimit        *int64 `json:"cpu_milli_limit,omitempty"`
	AvailableCPUMilli    *int64 `json:"available_cpu_milli,omitempty"`
	RequestedMemoryBytes int64  `json:"requested_memory_bytes,omitempty"`
	ReservedMemoryBytes  int64  `json:"reserved_memory_bytes,omitempty"`
	MemoryBytesLimit     *int64 `json:"memory_bytes_limit,omitempty"`
	AvailableMemoryBytes *int64 `json:"available_memory_bytes,omitempty"`
	Message              string `json:"message,omitempty"`
	CreatedAt            string `json:"created_at,omitempty"`
}

type ServiceDetail struct {
	Service  *ServiceDTO    `json:"service,omitempty"`
	Replicas []ReplicaDTO   `json:"replicas,omitempty"`
	Events   []ServiceEvent `json:"events,omitempty"`
}

type ServiceDTO struct {
	ID                string            `json:"id"`
	Namespace         string            `json:"namespace,omitempty"`
	EnvironmentID     string            `json:"environment_id,omitempty"`
	Status            string            `json:"status"`
	Replicas          int32             `json:"replicas"`
	ReadyReplicas     int32             `json:"ready_replicas"`
	UnhealthyReplicas int32             `json:"unhealthy_replicas"`
	RuntimeClass      string            `json:"runtime_class,omitempty"`
	Resources         *ResourceSpecDTO  `json:"resources,omitempty"`
	Message           string            `json:"message,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	RolloutPhase      string            `json:"rollout_phase,omitempty"`
	DiagnosticCode    string            `json:"diagnostic_code,omitempty"`
	DiagnosticMessage string            `json:"diagnostic_message,omitempty"`
	AdmissionSummary  string            `json:"admission_summary,omitempty"`
	CreatedAt         string            `json:"created_at,omitempty"`
	UpdatedAt         string            `json:"updated_at,omitempty"`
}

type ResourceSpecDTO struct {
	Requests *ResourceQuantityDTO `json:"requests,omitempty"`
	Limits   *ResourceQuantityDTO `json:"limits,omitempty"`
}

type ResourceQuantityDTO struct {
	CPUMilli    int64 `json:"cpu_milli,omitempty"`
	MemoryBytes int64 `json:"memory_bytes,omitempty"`
}

type ReplicaDTO struct {
	ID               string                    `json:"id"`
	ServiceID        string                    `json:"service_id,omitempty"`
	NodeID           string                    `json:"node_id,omitempty"`
	Attempt          int64                     `json:"attempt,omitempty"`
	Status           string                    `json:"status"`
	Ready            bool                      `json:"ready"`
	Ended            bool                      `json:"ended"`
	Outdated         bool                      `json:"outdated"`
	Message          string                    `json:"message,omitempty"`
	ReadinessMessage string                    `json:"readiness_message,omitempty"`
	DiagnosticCode   string                    `json:"diagnostic_code,omitempty"`
	LifecycleRetry   *ReplicaLifecycleRetryDTO `json:"lifecycle_retry,omitempty"`
	CreatedAt        string                    `json:"created_at,omitempty"`
	UpdatedAt        string                    `json:"updated_at,omitempty"`
}

type ReplicaLifecycleRetryDTO struct {
	Reason    string `json:"reason"`
	Attempts  int32  `json:"attempts"`
	LastError string `json:"last_error,omitempty"`
	NextRunAt string `json:"next_run_at,omitempty"`
}

type ServiceEvent struct {
	ID             string `json:"id"`
	ServiceID      string `json:"service_id"`
	Type           string `json:"type"`
	Phase          string `json:"phase,omitempty"`
	DiagnosticCode string `json:"diagnostic_code,omitempty"`
	Message        string `json:"message,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
}

type TunnelDTO struct {
	SessionID       string `json:"session_id"`
	AllocationID    string `json:"allocation_id,omitempty"`
	NodeID          string `json:"node_id,omitempty"`
	Status          string `json:"status"`
	RelayID         string `json:"relay_id,omitempty"`
	BoundAddr       string `json:"bound_addr,omitempty"`
	ClientTarget    string `json:"client_edge_target,omitempty"`
	NodeTarget      string `json:"node_edge_target,omitempty"`
	LocalTarget     string `json:"local_target,omitempty"`
	RemotePort      int32  `json:"remote_port,omitempty"`
	Reason          string `json:"reason,omitempty"`
	BytesIn         int64  `json:"bytes_in,omitempty"`
	BytesOut        int64  `json:"bytes_out,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
	ReadyAt         string `json:"ready_at,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	LastPeerEventAt string `json:"last_peer_event_at,omitempty"`
}

type TunnelEvent struct {
	EventID    int64  `json:"event_id"`
	SessionID  string `json:"session_id"`
	Type       string `json:"type"`
	Status     string `json:"status,omitempty"`
	ReasonCode string `json:"reason_code,omitempty"`
	Reason     string `json:"reason,omitempty"`
	RelayID    string `json:"relay_id,omitempty"`
	PeerKind   string `json:"peer_kind,omitempty"`
	BoundAddr  string `json:"bound_addr,omitempty"`
	BytesIn    int64  `json:"bytes_in,omitempty"`
	BytesOut   int64  `json:"bytes_out,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
}

type TunnelDetail struct {
	Session *TunnelDTO    `json:"session,omitempty"`
	Events  []TunnelEvent `json:"events,omitempty"`
}

type Links struct {
	ContextName string `json:"context_name,omitempty"`
	Links       []Link `json:"links"`
}

type Link struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Kind string `json:"kind"`
}

type TunnelListParams struct {
	AllocationID    string
	NodeID          string
	IncludeTerminal bool
}

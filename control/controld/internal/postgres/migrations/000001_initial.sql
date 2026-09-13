CREATE TABLE principals (
	principal_id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	display_name TEXT NOT NULL,
	kind TEXT NOT NULL CHECK (kind IN ('human', 'service')),
	status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
	version BIGINT NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	CHECK (length(btrim(name)) > 0),
	CHECK (length(btrim(display_name)) > 0)
);

CREATE TABLE principal_credentials (
	credential_id TEXT PRIMARY KEY,
	principal_id TEXT NOT NULL REFERENCES principals(principal_id),
	kind TEXT NOT NULL CHECK (kind = 'x509_sha256'),
	fingerprint BYTEA NOT NULL UNIQUE,
	certificate_not_after TIMESTAMPTZ NOT NULL,
	label TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	revoked_at TIMESTAMPTZ,
	CHECK (octet_length(fingerprint) = 32),
	CHECK (length(btrim(label)) > 0)
);

CREATE TABLE nodes (
	node_id TEXT PRIMARY KEY,
	node_target TEXT NOT NULL DEFAULT '',
	node_auth_token_hash TEXT NOT NULL DEFAULT '',
	registered_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	last_heartbeat_at TIMESTAMPTZ NOT NULL,
	last_summary_at TIMESTAMPTZ,
	lifecycle_status TEXT NOT NULL CHECK (lifecycle_status IN ('active', 'retired')),
	retired_at TIMESTAMPTZ,
	retired_reason TEXT NOT NULL DEFAULT '',
	CHECK (
		(lifecycle_status = 'active' AND retired_at IS NULL AND retired_reason = '') OR
		(lifecycle_status = 'retired' AND retired_at IS NOT NULL AND length(btrim(retired_reason)) > 0)
	),
	version BIGINT NOT NULL DEFAULT 1
);

CREATE TABLE node_summaries (
	node_id TEXT PRIMARY KEY REFERENCES nodes(node_id) ON DELETE CASCADE,
	collected_at TIMESTAMPTZ NOT NULL,
	summary JSONB NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE node_runtime_sets (
	node_id TEXT NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
	runtime_name TEXT NOT NULL,
	PRIMARY KEY (node_id, runtime_name)
);

CREATE INDEX idx_nodes_last_heartbeat_at ON nodes(last_heartbeat_at);
CREATE INDEX idx_nodes_last_summary_at ON nodes(last_summary_at);
CREATE INDEX idx_nodes_lifecycle ON nodes(lifecycle_status, node_id);

CREATE TABLE namespaces (
	namespace TEXT PRIMARY KEY,
	version BIGINT NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE namespace_resource_quotas (
	namespace TEXT PRIMARY KEY REFERENCES namespaces(namespace) ON DELETE CASCADE,
	cpu_milli_limit BIGINT,
	memory_bytes_limit BIGINT,
	ephemeral_storage_bytes_limit BIGINT,
	version BIGINT NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	CHECK (cpu_milli_limit IS NULL OR cpu_milli_limit >= 0),
	CHECK (memory_bytes_limit IS NULL OR memory_bytes_limit >= 0),
	CHECK (ephemeral_storage_bytes_limit IS NULL OR ephemeral_storage_bytes_limit >= 0)
);

CREATE TABLE role_bindings (
	binding_id TEXT PRIMARY KEY,
	principal_id TEXT NOT NULL REFERENCES principals(principal_id),
	scope_type TEXT NOT NULL CHECK (scope_type IN ('platform', 'namespace')),
	namespace TEXT,
	role TEXT NOT NULL CHECK (role IN ('platform_admin', 'namespace_admin', 'namespace_editor', 'namespace_viewer')),
	created_by_principal_id TEXT REFERENCES principals(principal_id),
	created_at TIMESTAMPTZ NOT NULL,
	revoked_by_principal_id TEXT REFERENCES principals(principal_id),
	revoked_at TIMESTAMPTZ,
	CHECK (
		(scope_type = 'platform' AND namespace IS NULL AND role = 'platform_admin') OR
		(scope_type = 'namespace' AND namespace IS NOT NULL AND role IN ('namespace_admin', 'namespace_editor', 'namespace_viewer'))
	),
	CHECK ((revoked_at IS NULL AND revoked_by_principal_id IS NULL) OR (revoked_at IS NOT NULL AND revoked_by_principal_id IS NOT NULL))
);

CREATE TABLE environment_templates (
	template_id TEXT NOT NULL,
	version TEXT NOT NULL,
	template JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (template_id, version)
);

CREATE TABLE environments (
	environment_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	status TEXT NOT NULL,
	spec_hash TEXT NOT NULL,
	spec JSONB NOT NULL,
	resolved_template JSONB NOT NULL,
	labels JSONB NOT NULL,
	version BIGINT NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	message TEXT NOT NULL DEFAULT ''
);

CREATE TABLE secrets (
	secret_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	type TEXT NOT NULL,
	data_keys JSONB NOT NULL,
	encrypted_payload BYTEA NOT NULL,
	labels JSONB NOT NULL,
	version BIGINT NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE runs (
	run_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	status TEXT NOT NULL,
	config JSONB NOT NULL,
	labels JSONB NOT NULL,
	version BIGINT NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	exit_code INTEGER NOT NULL DEFAULT 0,
	exit_code_known BOOLEAN NOT NULL DEFAULT FALSE,
	diagnostic_code TEXT NOT NULL DEFAULT 'WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED',
	message TEXT NOT NULL DEFAULT ''
);

CREATE TABLE allocations (
	allocation_id TEXT PRIMARY KEY,
	run_id TEXT NOT NULL UNIQUE REFERENCES runs(run_id) ON DELETE CASCADE,
	node_id TEXT NOT NULL REFERENCES nodes(node_id) ON DELETE RESTRICT,
	lifecycle_state TEXT NOT NULL,
	workspace_preparation JSONB NOT NULL DEFAULT 'null'::jsonb,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	node_active_at TIMESTAMPTZ,
	CHECK (lifecycle_state IN (
		'ALLOCATION_LIFECYCLE_STATE_BOUND',
		'ALLOCATION_LIFECYCLE_STATE_STARTING',
		'ALLOCATION_LIFECYCLE_STATE_ACTIVE',
		'ALLOCATION_LIFECYCLE_STATE_RELEASING',
		'ALLOCATION_LIFECYCLE_STATE_RELEASED'
	)),
	UNIQUE (allocation_id, node_id)
);

CREATE TABLE node_capability_transitions (
	transition_id TEXT PRIMARY KEY,
	node_id TEXT NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
	snapshot_id TEXT NOT NULL,
	snapshot_sequence BIGINT NOT NULL,
	capability_key JSONB NOT NULL,
	capability_key_id TEXT NOT NULL,
	old_state TEXT NOT NULL,
	new_state TEXT NOT NULL,
	old_evidence JSONB NOT NULL DEFAULT 'null'::jsonb,
	new_evidence JSONB NOT NULL DEFAULT 'null'::jsonb,
	old_reason_code TEXT NOT NULL,
	new_reason_code TEXT NOT NULL,
	reason TEXT NOT NULL DEFAULT '',
	observed_at TIMESTAMPTZ NOT NULL,
	reported_at TIMESTAMPTZ NOT NULL,
	UNIQUE (node_id, snapshot_id, capability_key_id)
);

CREATE TABLE allocation_capability_dependencies (
	allocation_id TEXT NOT NULL,
	node_id TEXT NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
	capability_key_id TEXT NOT NULL,
	capability_key JSONB NOT NULL,
	loss_policy TEXT NOT NULL,
	placement_dependency JSONB NOT NULL,
	admitted_dependency JSONB,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (allocation_id, capability_key_id),
	FOREIGN KEY (allocation_id, node_id)
			REFERENCES allocations(allocation_id, node_id) ON DELETE CASCADE
);

CREATE TABLE allocation_capability_admissions (
	allocation_id TEXT PRIMARY KEY,
	dependency_set_digest TEXT NOT NULL,
	admitted_at TIMESTAMPTZ NOT NULL,
	FOREIGN KEY (allocation_id)
		REFERENCES allocations(allocation_id) ON DELETE CASCADE,
	CHECK (dependency_set_digest ~ '^sha256:[0-9a-f]{64}$')
);

CREATE TABLE allocation_capability_condition_sets (
	allocation_id TEXT PRIMARY KEY,
	revision BIGINT NOT NULL,
	payload_digest TEXT NOT NULL,
	observed_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	UNIQUE (allocation_id, revision),
	FOREIGN KEY (allocation_id)
		REFERENCES allocations(allocation_id) ON DELETE CASCADE,
	CHECK (revision > 0),
	CHECK (payload_digest ~ '^sha256:[0-9a-f]{64}$')
);

CREATE TABLE allocation_capability_conditions (
	allocation_id TEXT NOT NULL,
	capability_key_id TEXT NOT NULL,
	condition_revision BIGINT NOT NULL,
	observed_at TIMESTAMPTZ NOT NULL,
	condition JSONB NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (allocation_id, capability_key_id),
	FOREIGN KEY (allocation_id, capability_key_id)
		REFERENCES allocation_capability_dependencies(allocation_id, capability_key_id) ON DELETE CASCADE,
	FOREIGN KEY (allocation_id, condition_revision)
		REFERENCES allocation_capability_condition_sets(allocation_id, revision) ON DELETE CASCADE,
	CHECK (condition_revision > 0)
);

CREATE TABLE allocation_memory_admission_evidence (
	allocation_id TEXT PRIMARY KEY,
	node_id TEXT NOT NULL,
	sandbox_memory_request_bytes BIGINT NOT NULL,
	sandbox_memory_limit_bytes BIGINT NOT NULL,
	node_memory_budget JSONB NOT NULL,
	summary_collected_at TIMESTAMPTZ NOT NULL,
	node_local_commitment_bytes BIGINT NOT NULL,
	admitted_at TIMESTAMPTZ NOT NULL,
	FOREIGN KEY (allocation_id)
		REFERENCES allocations(allocation_id) ON DELETE CASCADE,
	FOREIGN KEY (allocation_id, node_id)
		REFERENCES allocations(allocation_id, node_id) ON DELETE CASCADE,
	CHECK (sandbox_memory_request_bytes >= 0),
	CHECK (sandbox_memory_limit_bytes >= 0),
	CHECK (sandbox_memory_limit_bytes = 0 OR sandbox_memory_request_bytes <= sandbox_memory_limit_bytes),
	CHECK (node_local_commitment_bytes >= 0),
	-- Node summaries are sampled on the node while admission is committed on the
	-- control plane. Match the publication contract's bounded clock-skew window
	-- instead of rejecting valid evidence from a node whose clock is slightly ahead.
	CHECK (summary_collected_at <= admitted_at + INTERVAL '1 minute'),
	CHECK (jsonb_typeof(node_memory_budget) = 'object'),
	CHECK (
		COALESCE((node_memory_budget->>'physical_capacity_bytes')::BIGINT, -1) > 0
			AND COALESCE((node_memory_budget->>'source_allocatable_bytes')::BIGINT, -1) > 0
			AND (node_memory_budget->>'source_allocatable_bytes')::BIGINT <=
				(node_memory_budget->>'physical_capacity_bytes')::BIGINT
			AND COALESCE(node_memory_budget->>'mode', '') IN (
				'NODE_MEMORY_BUDGET_MODE_CGROUP_V2',
				'NODE_MEMORY_BUDGET_MODE_DISABLED_DEV'
			)
			AND (
				(node_memory_budget->>'mode' = 'NODE_MEMORY_BUDGET_MODE_CGROUP_V2'
				 AND COALESCE((node_memory_budget->>'system_reserve_bytes')::BIGINT, -1) > 0)
				OR
				(node_memory_budget->>'mode' = 'NODE_MEMORY_BUDGET_MODE_DISABLED_DEV'
				 AND COALESCE((node_memory_budget->>'system_reserve_bytes')::BIGINT, 0) = 0
				 AND COALESCE((node_memory_budget->>'internal_current_bytes')::BIGINT, 0) = 0
				 AND NOT COALESCE((node_memory_budget->>'delegated_root_limit_finite')::BOOLEAN, FALSE)
				 AND COALESCE((node_memory_budget->>'delegated_root_limit_bytes')::BIGINT, 0) = 0)
			)
			AND COALESCE((node_memory_budget->>'effective_allocatable_bytes')::BIGINT, -1) > 0
			AND COALESCE((node_memory_budget->>'local_commitment_bytes')::BIGINT, 0) = node_local_commitment_bytes
			AND COALESCE((node_memory_budget->>'cleanup_debt_bytes')::BIGINT, 0) >= 0
			AND COALESCE((node_memory_budget->>'cleanup_debt_bytes')::BIGINT, 0) <=
				COALESCE((node_memory_budget->>'local_commitment_bytes')::BIGINT, 0)
			AND COALESCE((node_memory_budget->>'internal_current_bytes')::BIGINT, 0) >= 0
			AND COALESCE(node_memory_budget->>'capacity_identity', '') <> ''
			AND COALESCE(node_memory_budget->>'sampled_at', '') <> ''
			AND (node_memory_budget->>'sampled_at')::TIMESTAMPTZ <= summary_collected_at
			AND COALESCE((node_memory_budget->>'system_reserve_exhausted')::BOOLEAN, FALSE) = FALSE
			AND (
				(COALESCE((node_memory_budget->>'delegated_root_limit_finite')::BOOLEAN, FALSE)
				 AND COALESCE((node_memory_budget->>'delegated_root_limit_bytes')::BIGINT, -1) > 0)
				OR
				(NOT COALESCE((node_memory_budget->>'delegated_root_limit_finite')::BOOLEAN, FALSE)
				 AND COALESCE((node_memory_budget->>'delegated_root_limit_bytes')::BIGINT, 0) = 0)
			)
			AND COALESCE((node_memory_budget->>'effective_allocatable_bytes')::BIGINT, -1) =
				CASE
					WHEN COALESCE((node_memory_budget->>'delegated_root_limit_finite')::BOOLEAN, FALSE)
					THEN LEAST(
						(node_memory_budget->>'source_allocatable_bytes')::BIGINT,
						(node_memory_budget->>'delegated_root_limit_bytes')::BIGINT
					) - COALESCE((node_memory_budget->>'system_reserve_bytes')::BIGINT, 0)
					ELSE (node_memory_budget->>'source_allocatable_bytes')::BIGINT -
						COALESCE((node_memory_budget->>'system_reserve_bytes')::BIGINT, 0)
				END
		)
	);

CREATE TABLE allocation_memory_observations (
	allocation_id TEXT PRIMARY KEY,
	node_id TEXT NOT NULL,
	revision BIGINT NOT NULL,
	observed_at TIMESTAMPTZ NOT NULL,
	observation JSONB NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	FOREIGN KEY (allocation_id)
		REFERENCES allocations(allocation_id) ON DELETE CASCADE,
	FOREIGN KEY (allocation_id, node_id)
		REFERENCES allocations(allocation_id, node_id) ON DELETE CASCADE,
	CHECK (revision > 0),
	CHECK (jsonb_typeof(observation) = 'object'),
	CHECK (COALESCE(observation->>'allocation_id', '') = allocation_id),
	CHECK (COALESCE((observation->>'revision')::BIGINT, -1) = revision),
	CHECK (COALESCE(observation->>'observed_at', '') <> ''),
	CHECK ((observation->>'observed_at')::TIMESTAMPTZ = observed_at),
	CHECK (COALESCE((observation->>'request_bytes')::BIGINT, 0) >= 0),
	CHECK (COALESCE((observation->>'limit_bytes')::BIGINT, 0) >= 0),
	CHECK (
		COALESCE((observation->>'limit_bytes')::BIGINT, 0) = 0 OR
		COALESCE((observation->>'request_bytes')::BIGINT, 0) <= (observation->>'limit_bytes')::BIGINT
	),
	CHECK (COALESCE((observation->>'current_bytes')::BIGINT, 0) >= 0),
	CHECK (COALESCE((observation->>'peak_bytes')::BIGINT, 0) >= 0),
	CHECK (
		COALESCE((observation->>'peak_bytes')::BIGINT, 0) >=
		COALESCE((observation->>'current_bytes')::BIGINT, 0)
	),
	CHECK (
		COALESCE((observation->>'peak_available')::BOOLEAN, FALSE) = TRUE OR
		COALESCE((observation->>'peak_bytes')::BIGINT, 0) =
		COALESCE((observation->>'current_bytes')::BIGINT, 0)
	),
	CHECK (COALESCE((observation->>'swap_current_bytes')::BIGINT, 0) >= 0),
	CHECK (
		COALESCE((observation->>'limit_bytes')::BIGINT, 0) = 0 OR
		COALESCE((observation->>'swap_current_bytes')::BIGINT, 0) = 0
	),
	CHECK (COALESCE((observation->>'anon_bytes')::BIGINT, 0) >= 0),
	CHECK (COALESCE((observation->>'file_bytes')::BIGINT, 0) >= 0),
	CHECK (COALESCE((observation->>'shmem_bytes')::BIGINT, 0) >= 0),
	CHECK (COALESCE((observation->>'kernel_bytes')::BIGINT, 0) >= 0),
	CHECK (COALESCE((observation->>'dirty_bytes')::BIGINT, 0) >= 0),
	CHECK (COALESCE((observation->>'writeback_bytes')::BIGINT, 0) >= 0),
	CHECK (COALESCE(observation->>'cgroup_identity', '') <> ''),
	CHECK (octet_length(COALESCE(observation->>'cgroup_identity', '')) <= 1024),
	CHECK (COALESCE(observation->>'runtime', '') = 'runsc'),
	CHECK (
		(
			COALESCE((observation->>'limit_bytes')::BIGINT, 0) = 0 AND
			COALESCE((observation->>'parent_controls_verified')::BOOLEAN, FALSE) = FALSE AND
			COALESCE((observation->>'leaf_controls_verified')::BOOLEAN, FALSE) = FALSE
		) OR (
			COALESCE((observation->>'limit_bytes')::BIGINT, 0) > 0 AND
			COALESCE((observation->>'parent_controls_verified')::BOOLEAN, FALSE) = TRUE AND
			(
				COALESCE(observation->>'cleanup_state', '') = 'ALLOCATION_MEMORY_CLEANUP_STATE_RETIRING' OR
				COALESCE((observation->>'leaf_controls_verified')::BOOLEAN, FALSE) = TRUE
			)
		)
	),
	CHECK (COALESCE(observation->>'cleanup_state', '') IN (
		'ALLOCATION_MEMORY_CLEANUP_STATE_ASSIGNED',
		'ALLOCATION_MEMORY_CLEANUP_STATE_RETIRING'
	)),
	CHECK (
		COALESCE(observation->>'cleanup_state', '') <> 'ALLOCATION_MEMORY_CLEANUP_STATE_RETIRING' OR
		COALESCE((observation->>'pid_roles_verified')::BOOLEAN, FALSE) = FALSE
	),
	CHECK (
		COALESCE((observation->>'psi_available')::BOOLEAN, FALSE) = TRUE OR (
			COALESCE((observation->>'psi_some_avg10')::DOUBLE PRECISION, 0) = 0 AND
			COALESCE((observation->>'psi_full_avg10')::DOUBLE PRECISION, 0) = 0 AND
			COALESCE((observation->>'psi_some_total_usec')::BIGINT, 0) = 0 AND
			COALESCE((observation->>'psi_full_total_usec')::BIGINT, 0) = 0
		)
	)
);

CREATE TABLE node_capability_instances (
	node_id TEXT NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
	node_instance_id TEXT NOT NULL,
	first_snapshot_id TEXT NOT NULL,
	last_snapshot_id TEXT NOT NULL,
	last_sequence BIGINT NOT NULL,
	first_seen_at TIMESTAMPTZ NOT NULL,
	last_seen_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (node_id, node_instance_id),
	CHECK (last_sequence > 0)
);

CREATE TABLE allocation_capability_reconcile_queue (
	allocation_id TEXT PRIMARY KEY REFERENCES allocations(allocation_id) ON DELETE CASCADE,
	reconcile_attempts INTEGER NOT NULL DEFAULT 0,
	next_run_at TIMESTAMPTZ NOT NULL,
	lease_owner TEXT NOT NULL DEFAULT '',
	lease_expires_at TIMESTAMPTZ,
	last_error TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE allocation_capability_reconcile_pending_keys (
	allocation_id TEXT NOT NULL REFERENCES allocation_capability_reconcile_queue(allocation_id) ON DELETE CASCADE,
	capability_key_id TEXT NOT NULL,
	snapshot_sequence BIGINT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (allocation_id, capability_key_id),
	FOREIGN KEY (allocation_id, capability_key_id)
		REFERENCES allocation_capability_dependencies(allocation_id, capability_key_id) ON DELETE CASCADE
);

CREATE TABLE reservations (
	allocation_id TEXT PRIMARY KEY,
	node_id TEXT NOT NULL,
	cpu_milli BIGINT NOT NULL DEFAULT 0,
	sandbox_memory_request_bytes BIGINT NOT NULL DEFAULT 0,
	ephemeral_storage_bytes BIGINT NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL,
	released_at TIMESTAMPTZ,
	CHECK (cpu_milli >= 0),
	CHECK (sandbox_memory_request_bytes >= 0),
	CHECK (ephemeral_storage_bytes >= 0),
	FOREIGN KEY (allocation_id, node_id)
		REFERENCES allocations(allocation_id, node_id) ON DELETE CASCADE
);

CREATE TABLE namespace_quota_events (
	event_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	event_type TEXT NOT NULL,
	run_id TEXT NOT NULL DEFAULT '',
	environment_id TEXT NOT NULL DEFAULT '',
	reason TEXT NOT NULL,
	requested_cpu_milli BIGINT NOT NULL DEFAULT 0,
	reserved_cpu_milli BIGINT NOT NULL DEFAULT 0,
	cpu_milli_limit BIGINT,
	available_cpu_milli BIGINT,
	requested_memory_bytes BIGINT NOT NULL DEFAULT 0,
	reserved_memory_bytes BIGINT NOT NULL DEFAULT 0,
	memory_bytes_limit BIGINT,
	available_memory_bytes BIGINT,
	requested_ephemeral_storage_bytes BIGINT NOT NULL DEFAULT 0,
	reserved_ephemeral_storage_bytes BIGINT NOT NULL DEFAULT 0,
	ephemeral_storage_bytes_limit BIGINT,
	available_ephemeral_storage_bytes BIGINT,
	message TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	CHECK (requested_cpu_milli >= 0),
	CHECK (reserved_cpu_milli >= 0),
	CHECK (cpu_milli_limit IS NULL OR cpu_milli_limit >= 0),
	CHECK (available_cpu_milli IS NULL OR available_cpu_milli >= 0),
	CHECK (requested_memory_bytes >= 0),
	CHECK (reserved_memory_bytes >= 0),
	CHECK (memory_bytes_limit IS NULL OR memory_bytes_limit >= 0),
	CHECK (available_memory_bytes IS NULL OR available_memory_bytes >= 0),
	CHECK (requested_ephemeral_storage_bytes >= 0),
	CHECK (reserved_ephemeral_storage_bytes >= 0),
	CHECK (ephemeral_storage_bytes_limit IS NULL OR ephemeral_storage_bytes_limit >= 0),
	CHECK (available_ephemeral_storage_bytes IS NULL OR available_ephemeral_storage_bytes >= 0)
);

CREATE TABLE execution_leases (
	lease_id TEXT PRIMARY KEY,
	allocation_id TEXT NOT NULL,
	node_id TEXT NOT NULL,
	node_target TEXT NOT NULL DEFAULT '',
	lease_type TEXT NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	revision BIGINT NOT NULL,
	revoked BOOLEAN NOT NULL DEFAULT FALSE,
	token_hash TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	FOREIGN KEY (allocation_id, node_id)
		REFERENCES allocations(allocation_id, node_id) ON DELETE CASCADE
);

CREATE TABLE allocation_reconcile_queue (
	allocation_id TEXT PRIMARY KEY REFERENCES allocations(allocation_id) ON DELETE CASCADE,
	reason TEXT NOT NULL,
	next_run_at TIMESTAMPTZ NOT NULL,
	reconcile_attempts INTEGER NOT NULL DEFAULT 0,
	last_error TEXT NOT NULL DEFAULT '',
	lease_owner TEXT NOT NULL DEFAULT '',
	lease_expires_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE admin_audit_events (
	event_id TEXT PRIMARY KEY,
	operation TEXT NOT NULL,
	target_type TEXT NOT NULL,
	target_id TEXT NOT NULL,
	operator_reason TEXT NOT NULL,
	actor_principal_id TEXT REFERENCES principals(principal_id),
	created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE control_revisions (
	name TEXT PRIMARY KEY,
	revision BIGINT NOT NULL
);

INSERT INTO control_revisions(name, revision)
VALUES ('execution_leases', 0), ('access', 0), ('tunnel_sessions', 0);

CREATE TABLE tunnel_sessions (
	session_id TEXT PRIMARY KEY,
	allocation_id TEXT NOT NULL,
	namespace TEXT NOT NULL,
	creator_principal_id TEXT NOT NULL REFERENCES principals(principal_id) ON DELETE RESTRICT,
	node_id TEXT NOT NULL,
	node_target TEXT NOT NULL DEFAULT '',
	remote_port INTEGER NOT NULL,
	local_target TEXT NOT NULL DEFAULT '',
	edge_target TEXT NOT NULL DEFAULT '',
	node_edge_target TEXT NOT NULL DEFAULT '',
	relay_id TEXT NOT NULL DEFAULT '',
	client_edge_target TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	reason TEXT NOT NULL DEFAULT '',
	bound_addr TEXT NOT NULL DEFAULT '',
	revoked BOOLEAN NOT NULL DEFAULT FALSE,
	client_token_hash TEXT NOT NULL,
	node_token_encrypted BYTEA NOT NULL,
	node_token_hash TEXT NOT NULL,
	revision BIGINT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	ready_at TIMESTAMPTZ,
	last_peer_event_at TIMESTAMPTZ,
	bytes_in BIGINT NOT NULL DEFAULT 0,
	bytes_out BIGINT NOT NULL DEFAULT 0,
	FOREIGN KEY (allocation_id, node_id)
		REFERENCES allocations(allocation_id, node_id) ON DELETE CASCADE
);

CREATE TABLE tunnel_session_events (
	event_id BIGSERIAL PRIMARY KEY,
	session_id TEXT NOT NULL REFERENCES tunnel_sessions(session_id) ON DELETE CASCADE,
	event_type TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT '',
	reason_code TEXT NOT NULL DEFAULT '',
	reason TEXT NOT NULL DEFAULT '',
	bound_addr TEXT NOT NULL DEFAULT '',
	relay_id TEXT NOT NULL DEFAULT '',
	peer_kind TEXT NOT NULL DEFAULT '',
	bytes_in BIGINT NOT NULL DEFAULT 0,
	bytes_out BIGINT NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_principal_credentials_principal ON principal_credentials(principal_id, created_at DESC);
CREATE INDEX idx_principal_credentials_active ON principal_credentials(fingerprint) WHERE revoked_at IS NULL;
CREATE INDEX idx_role_bindings_principal ON role_bindings(principal_id, created_at DESC);
CREATE INDEX idx_role_bindings_namespace ON role_bindings(namespace, principal_id) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX idx_role_bindings_active_unique
	ON role_bindings(principal_id, scope_type, COALESCE(namespace, ''), role)
	WHERE revoked_at IS NULL;

CREATE INDEX idx_environments_namespace_created ON environments(namespace, created_at DESC);
CREATE INDEX idx_secrets_namespace_created ON secrets(namespace, created_at DESC);
CREATE INDEX idx_runs_namespace_created ON runs(namespace, created_at DESC);
CREATE INDEX idx_runs_namespace_id ON runs(namespace, run_id);
CREATE INDEX idx_allocations_node_lifecycle ON allocations(node_id, lifecycle_state);
CREATE INDEX idx_allocations_run_lifecycle_updated ON allocations(run_id, lifecycle_state, updated_at);
CREATE INDEX idx_node_capability_transitions_node_reported
	ON node_capability_transitions(node_id, reported_at DESC, transition_id DESC);
CREATE INDEX idx_allocation_capability_dependencies_node_key
	ON allocation_capability_dependencies(node_id, capability_key_id, allocation_id);
CREATE INDEX idx_allocation_capability_conditions_allocation_revision
	ON allocation_capability_conditions(allocation_id, condition_revision);
CREATE INDEX idx_allocation_memory_observations_node_updated
	ON allocation_memory_observations(node_id, updated_at DESC);
CREATE INDEX idx_allocation_capability_reconcile_claimable
	ON allocation_capability_reconcile_queue(next_run_at, lease_expires_at, allocation_id);
CREATE INDEX idx_admin_audit_events_created ON admin_audit_events(created_at DESC, event_id DESC);
CREATE INDEX idx_admin_audit_events_operation_created ON admin_audit_events(operation, created_at DESC, event_id DESC);
CREATE INDEX idx_admin_audit_events_target_created ON admin_audit_events(target_type, target_id, created_at DESC, event_id DESC);
CREATE INDEX idx_reservations_active_node ON reservations(node_id) WHERE released_at IS NULL;
CREATE INDEX idx_reservations_active_created ON reservations(created_at, allocation_id) WHERE released_at IS NULL;
CREATE INDEX idx_namespace_quota_events_namespace_created ON namespace_quota_events(namespace, created_at DESC, event_id DESC);
CREATE INDEX idx_namespace_quota_events_created ON namespace_quota_events(created_at DESC, event_id DESC);
CREATE INDEX idx_execution_leases_node_revision ON execution_leases(node_id, revision);
CREATE INDEX idx_execution_leases_retention ON execution_leases(created_at, expires_at, revoked);
CREATE INDEX idx_execution_leases_active_created ON execution_leases(created_at, allocation_id) WHERE revoked = FALSE;
CREATE INDEX idx_runs_status_updated ON runs(status, updated_at);
CREATE INDEX idx_allocation_reconcile_queue_claimable
	ON allocation_reconcile_queue(next_run_at, lease_expires_at, allocation_id);
CREATE UNIQUE INDEX idx_tunnel_sessions_active_remote_port
	ON tunnel_sessions(allocation_id, remote_port)
	WHERE revoked = FALSE AND status IN (
		'TUNNEL_SESSION_STATUS_PENDING',
		'TUNNEL_SESSION_STATUS_RUNNING',
		'TUNNEL_SESSION_STATUS_DEGRADED'
	);
CREATE INDEX idx_tunnel_sessions_node_revision ON tunnel_sessions(node_id, revision);
CREATE INDEX idx_tunnel_sessions_namespace_created ON tunnel_sessions(namespace, created_at DESC);
CREATE INDEX idx_tunnel_sessions_expiry ON tunnel_sessions(expires_at, revoked);
CREATE INDEX idx_tunnel_sessions_active_created
	ON tunnel_sessions(created_at, allocation_id)
	WHERE revoked = FALSE AND status IN (
		'TUNNEL_SESSION_STATUS_PENDING',
		'TUNNEL_SESSION_STATUS_RUNNING',
		'TUNNEL_SESSION_STATUS_DEGRADED'
	);
CREATE INDEX idx_tunnel_session_events_session_created
	ON tunnel_session_events(session_id, created_at DESC, event_id DESC);

CREATE FUNCTION notify_execution_lease_change()
RETURNS TRIGGER AS $$
BEGIN
	PERFORM pg_notify(
		'axern_execution_lease_changes',
		CASE WHEN TG_OP = 'DELETE' THEN OLD.node_id ELSE NEW.node_id END
	);
	RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER execution_lease_change_notify
AFTER INSERT OR UPDATE OR DELETE ON execution_leases
FOR EACH ROW EXECUTE FUNCTION notify_execution_lease_change();

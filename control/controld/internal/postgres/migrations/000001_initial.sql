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
	role TEXT NOT NULL CHECK (role IN ('platform_admin', 'namespace_admin', 'namespace_editor', 'namespace_viewer', 'rollout_executor')),
	created_by_principal_id TEXT REFERENCES principals(principal_id),
	created_at TIMESTAMPTZ NOT NULL,
	revoked_by_principal_id TEXT REFERENCES principals(principal_id),
	revoked_at TIMESTAMPTZ,
	CHECK (
		(scope_type = 'platform' AND namespace IS NULL AND role IN ('platform_admin', 'rollout_executor')) OR
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
	updated_at TIMESTAMPTZ NOT NULL,
	visibility TEXT NOT NULL DEFAULT 'PUBLIC',
	owner_type TEXT NOT NULL DEFAULT '',
	owner_id TEXT NOT NULL DEFAULT ''
);

CREATE TABLE runs (
	run_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	allocation_id TEXT NOT NULL UNIQUE,
	attempt BIGINT NOT NULL DEFAULT 1,
	status TEXT NOT NULL,
	config JSONB NOT NULL,
	labels JSONB NOT NULL,
	version BIGINT NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	exit_code INTEGER NOT NULL DEFAULT 0,
	exit_code_known BOOLEAN NOT NULL DEFAULT FALSE,
	message TEXT NOT NULL DEFAULT '',
	capability_conditions JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE services (
	service_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	replicas INTEGER NOT NULL,
	ready_replicas INTEGER NOT NULL DEFAULT 0,
	unhealthy_replicas INTEGER NOT NULL DEFAULT 0,
	rollout_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
	readiness_probe JSONB NOT NULL DEFAULT 'null'::jsonb,
	liveness_probe JSONB NOT NULL DEFAULT 'null'::jsonb,
	autoscaling_policy JSONB NOT NULL DEFAULT 'null'::jsonb,
	autoscaling_status JSONB NOT NULL DEFAULT 'null'::jsonb,
	status TEXT NOT NULL,
	config JSONB NOT NULL,
	allocation_ids JSONB NOT NULL,
	labels JSONB NOT NULL,
	version BIGINT NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	message TEXT NOT NULL DEFAULT '',
	deletion_status JSONB NOT NULL DEFAULT 'null'::jsonb
);

CREATE TABLE service_events (
	event_id TEXT PRIMARY KEY,
	service_id TEXT NOT NULL REFERENCES services(service_id) ON DELETE CASCADE,
	replica_id TEXT NOT NULL DEFAULT '',
	event_type TEXT NOT NULL,
	phase TEXT NOT NULL,
	diagnostic_code TEXT NOT NULL,
	message TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE allocations (
	allocation_id TEXT PRIMARY KEY,
	owner_type TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	environment_id TEXT NOT NULL DEFAULT '',
	node_id TEXT NOT NULL,
	attempt BIGINT NOT NULL DEFAULT 1,
	status TEXT NOT NULL,
	ready BOOLEAN NOT NULL DEFAULT FALSE,
	readiness_message TEXT NOT NULL DEFAULT '',
	readiness_probe JSONB NOT NULL DEFAULT 'null'::jsonb,
	liveness_probe JSONB NOT NULL DEFAULT 'null'::jsonb,
	desired_spec_digest TEXT NOT NULL DEFAULT '',
	config JSONB NOT NULL,
	workspace_preparation JSONB NOT NULL DEFAULT 'null'::jsonb,
	capability_dependencies JSONB NOT NULL DEFAULT '{}'::jsonb,
	capability_conditions JSONB NOT NULL DEFAULT '{}'::jsonb,
	admitted_capability_dependencies JSONB NOT NULL DEFAULT '{}'::jsonb,
	version BIGINT NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	node_active_at TIMESTAMPTZ,
	exit_code INTEGER NOT NULL DEFAULT 0,
	exit_code_known BOOLEAN NOT NULL DEFAULT FALSE,
	message TEXT NOT NULL DEFAULT ''
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
	old_evidence_id TEXT NOT NULL DEFAULT '',
	new_evidence_id TEXT NOT NULL DEFAULT '',
	reason_code TEXT NOT NULL,
	reason TEXT NOT NULL DEFAULT '',
	observed_at TIMESTAMPTZ NOT NULL,
	reported_at TIMESTAMPTZ NOT NULL,
	UNIQUE (node_id, snapshot_id, capability_key_id)
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
	pending_dependencies JSONB NOT NULL,
	reconcile_attempts INTEGER NOT NULL DEFAULT 0,
	next_run_at TIMESTAMPTZ NOT NULL,
	lease_owner TEXT NOT NULL DEFAULT '',
	lease_expires_at TIMESTAMPTZ,
	last_error TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE workload_reservations (
	reservation_id TEXT PRIMARY KEY,
	allocation_id TEXT NOT NULL,
	namespace TEXT NOT NULL,
	owner_type TEXT NOT NULL,
	owner_id TEXT NOT NULL,
	node_id TEXT NOT NULL,
	cpu_milli BIGINT NOT NULL DEFAULT 0,
	memory_bytes BIGINT NOT NULL DEFAULT 0,
	ephemeral_storage_bytes BIGINT NOT NULL DEFAULT 0,
	memory_overhead_bytes BIGINT NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL,
	released_at TIMESTAMPTZ,
	CHECK (ephemeral_storage_bytes >= 0),
	CHECK (memory_overhead_bytes >= 0)
);

CREATE TABLE namespace_quota_events (
	event_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	event_type TEXT NOT NULL,
	workload_type TEXT NOT NULL DEFAULT '',
	workload_id TEXT NOT NULL DEFAULT '',
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
	CHECK (ephemeral_storage_bytes_limit IS NULL OR ephemeral_storage_bytes_limit >= 0),
	CHECK (available_ephemeral_storage_bytes IS NULL OR available_ephemeral_storage_bytes >= 0)
);

CREATE TABLE execution_leases (
	lease_id TEXT PRIMARY KEY,
	allocation_id TEXT NOT NULL,
	node_id TEXT NOT NULL,
	node_target TEXT NOT NULL DEFAULT '',
	attempt BIGINT NOT NULL,
	lease_type TEXT NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	revision BIGINT NOT NULL,
	revoked BOOLEAN NOT NULL DEFAULT FALSE,
	token_hash TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE allocation_reconcile_queue (
	allocation_id TEXT PRIMARY KEY,
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
VALUES ('execution_leases', 0), ('access', 0);

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
CREATE INDEX idx_service_events_service_created ON service_events(service_id, created_at DESC);
CREATE INDEX idx_allocations_node_status ON allocations(node_id, status);
CREATE INDEX idx_allocations_owner_status_updated ON allocations(owner_type, owner_id, status, updated_at);
CREATE INDEX idx_node_capability_transitions_node_reported
	ON node_capability_transitions(node_id, reported_at DESC, transition_id DESC);
CREATE INDEX idx_allocation_capability_reconcile_claimable
	ON allocation_capability_reconcile_queue(next_run_at, lease_expires_at, allocation_id);
CREATE INDEX idx_allocations_service_desired_spec
	ON allocations(owner_id, desired_spec_digest)
	WHERE owner_type = 'service';
CREATE INDEX idx_admin_audit_events_created ON admin_audit_events(created_at DESC, event_id DESC);
CREATE INDEX idx_admin_audit_events_operation_created ON admin_audit_events(operation, created_at DESC, event_id DESC);
CREATE INDEX idx_admin_audit_events_target_created ON admin_audit_events(target_type, target_id, created_at DESC, event_id DESC);
CREATE INDEX idx_workload_reservations_active_node ON workload_reservations(node_id) WHERE released_at IS NULL;
CREATE INDEX idx_workload_reservations_active_namespace ON workload_reservations(namespace) WHERE released_at IS NULL;
CREATE INDEX idx_workload_reservations_active_allocation ON workload_reservations(allocation_id) WHERE released_at IS NULL;
CREATE INDEX idx_workload_reservations_active_created ON workload_reservations(created_at, allocation_id) WHERE released_at IS NULL;
CREATE INDEX idx_namespace_quota_events_namespace_created ON namespace_quota_events(namespace, created_at DESC, event_id DESC);
CREATE INDEX idx_namespace_quota_events_created ON namespace_quota_events(created_at DESC, event_id DESC);
CREATE INDEX idx_execution_leases_node_revision ON execution_leases(node_id, revision);
CREATE INDEX idx_execution_leases_retention ON execution_leases(created_at, expires_at, revoked);
CREATE INDEX idx_execution_leases_active_created ON execution_leases(created_at, allocation_id) WHERE revoked = FALSE;
CREATE INDEX idx_services_live_created ON services(created_at, service_id) WHERE status NOT IN ('SERVICE_STATUS_DELETING', 'SERVICE_STATUS_DELETED');
CREATE INDEX idx_runs_status_updated ON runs(status, updated_at);
CREATE INDEX idx_allocation_reconcile_queue_claimable
	ON allocation_reconcile_queue(next_run_at, lease_expires_at, allocation_id);

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

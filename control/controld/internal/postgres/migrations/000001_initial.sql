CREATE TABLE principals (
	principal_id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	display_name TEXT NOT NULL,
	kind TEXT NOT NULL CHECK (kind IN ('human', 'service')),
	status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
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
	node_target TEXT NOT NULL,
	node_auth_token_hash TEXT NOT NULL,
	registered_at TIMESTAMPTZ NOT NULL,
	last_heartbeat_at TIMESTAMPTZ NOT NULL,
	lifecycle_status TEXT NOT NULL CHECK (lifecycle_status IN ('active', 'retired')),
	retired_at TIMESTAMPTZ,
	retired_reason TEXT NOT NULL DEFAULT '',
	CHECK (length(btrim(node_target)) > 0),
	CHECK (length(node_auth_token_hash) = 64),
	CHECK (last_heartbeat_at >= registered_at),
	CHECK (
		(lifecycle_status = 'active' AND retired_at IS NULL AND retired_reason = '') OR
		(lifecycle_status = 'retired' AND retired_at IS NOT NULL AND length(btrim(retired_reason)) > 0)
	)
);

CREATE TABLE node_summaries (
	node_id TEXT PRIMARY KEY REFERENCES nodes(node_id) ON DELETE CASCADE,
	summary JSONB NOT NULL CHECK (jsonb_typeof(summary) = 'object')
);

CREATE INDEX idx_nodes_last_heartbeat_at ON nodes(last_heartbeat_at);
CREATE INDEX idx_nodes_lifecycle ON nodes(lifecycle_status, node_id);

CREATE TABLE namespaces (
	namespace TEXT PRIMARY KEY,
	created_at TIMESTAMPTZ NOT NULL,
	deleted_at TIMESTAMPTZ
);

CREATE TABLE namespace_resource_quotas (
	namespace TEXT PRIMARY KEY REFERENCES namespaces(namespace) ON DELETE CASCADE,
	cpu_milli_limit BIGINT,
	memory_bytes_limit BIGINT,
	ephemeral_storage_bytes_limit BIGINT,
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
	namespace TEXT REFERENCES namespaces(namespace),
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

CREATE TABLE environments (
	environment_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL REFERENCES namespaces(namespace),
	spec JSONB NOT NULL,
	resolved_spec JSONB NOT NULL,
	labels JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	CHECK (jsonb_typeof(spec) = 'object'),
	CHECK (jsonb_typeof(resolved_spec) = 'object'),
	CHECK (jsonb_typeof(labels) = 'object')
);

CREATE UNIQUE INDEX idx_environments_id_namespace
	ON environments(environment_id, namespace);

CREATE TABLE secrets (
	secret_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL REFERENCES namespaces(namespace),
	type TEXT NOT NULL,
	data_keys JSONB NOT NULL,
	encrypted_payload BYTEA NOT NULL,
	labels JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	CHECK (type IN ('SECRET_TYPE_OPAQUE', 'SECRET_TYPE_DOCKER_CONFIG_JSON')),
	CHECK (jsonb_typeof(data_keys) = 'array'),
	CHECK (jsonb_typeof(labels) = 'object')
);

CREATE UNIQUE INDEX idx_secrets_id_namespace
	ON secrets(secret_id, namespace);

CREATE TABLE environment_secret_references (
	environment_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	secret_id TEXT NOT NULL,
	FOREIGN KEY (environment_id, namespace)
		REFERENCES environments(environment_id, namespace) ON DELETE CASCADE,
	FOREIGN KEY (secret_id, namespace)
		REFERENCES secrets(secret_id, namespace) ON DELETE RESTRICT
);

CREATE TABLE runs (
	run_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL REFERENCES namespaces(namespace),
	environment_id TEXT NOT NULL,
	status TEXT NOT NULL,
	config JSONB NOT NULL,
	environment_spec JSONB NOT NULL,
	resolved_environment_spec JSONB NOT NULL,
	labels JSONB NOT NULL,
	version BIGINT NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	exit_code INTEGER,
	diagnostic_code TEXT NOT NULL DEFAULT 'WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED',
	message TEXT NOT NULL DEFAULT '',
	CHECK (status IN (
		'RUN_STATUS_PLACED',
		'RUN_STATUS_STARTING',
		'RUN_STATUS_RUNNING',
		'RUN_STATUS_SUCCEEDED',
		'RUN_STATUS_FAILED',
		'RUN_STATUS_CANCELLED'
	)),
	CHECK (version > 0),
	CHECK (jsonb_typeof(config) = 'object'),
	CHECK (jsonb_typeof(environment_spec) = 'object'),
	CHECK (jsonb_typeof(resolved_environment_spec) = 'object'),
	CHECK (jsonb_typeof(labels) = 'object'),
	CHECK (updated_at >= created_at),
	CHECK (exit_code IS NULL OR status IN ('RUN_STATUS_SUCCEEDED', 'RUN_STATUS_FAILED'))
);

CREATE UNIQUE INDEX idx_runs_id_namespace
	ON runs(run_id, namespace);

CREATE TABLE run_secret_references (
	run_id TEXT NOT NULL,
	namespace TEXT NOT NULL,
	secret_id TEXT NOT NULL,
	PRIMARY KEY (run_id, secret_id),
	FOREIGN KEY (run_id, namespace)
		REFERENCES runs(run_id, namespace) ON DELETE CASCADE,
	FOREIGN KEY (secret_id, namespace)
		REFERENCES secrets(secret_id, namespace) ON DELETE RESTRICT
);

CREATE TABLE allocations (
	allocation_id TEXT PRIMARY KEY,
	run_id TEXT NOT NULL UNIQUE REFERENCES runs(run_id) ON DELETE CASCADE,
	node_id TEXT NOT NULL REFERENCES nodes(node_id) ON DELETE RESTRICT,
	lifecycle_state TEXT NOT NULL,
	cpu_request_milli BIGINT NOT NULL,
	sandbox_memory_request_bytes BIGINT NOT NULL DEFAULT 0,
	ephemeral_storage_request_bytes BIGINT NOT NULL DEFAULT 0,
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
	CHECK (cpu_request_milli > 0),
	CHECK (sandbox_memory_request_bytes >= 0),
	CHECK (ephemeral_storage_request_bytes >= 0),
	CHECK (updated_at >= created_at),
	UNIQUE (allocation_id, node_id)
);

CREATE TABLE allocation_capability_requirements (
	allocation_id TEXT NOT NULL REFERENCES allocations(allocation_id) ON DELETE CASCADE,
	capability_key_id TEXT NOT NULL,
	capability_key JSONB NOT NULL,
	loss_policy TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (allocation_id, capability_key_id),
	CHECK (length(btrim(capability_key_id)) > 0),
	CHECK (jsonb_typeof(capability_key) = 'object')
);

CREATE TABLE allocation_capability_conditions (
	allocation_id TEXT PRIMARY KEY REFERENCES allocations(allocation_id) ON DELETE CASCADE,
	observed_at TIMESTAMPTZ NOT NULL,
	conditions JSONB NOT NULL CHECK (jsonb_typeof(conditions) = 'object')
);

-- Durable ordering fence for capability observations from a concrete node
-- process. This is not an observation cache: only the greatest accepted
-- sequence is retained so duplicate or delayed reports cannot replace newer
-- facts after a controld restart.
CREATE TABLE node_capability_instances (
	node_id TEXT NOT NULL REFERENCES nodes(node_id) ON DELETE CASCADE,
	node_instance_id TEXT NOT NULL,
	last_sequence BIGINT NOT NULL,
	PRIMARY KEY (node_id, node_instance_id),
	CHECK (last_sequence > 0)
);

CREATE TABLE namespace_quota_events (
	event_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL REFERENCES namespaces(namespace),
	event_type TEXT NOT NULL,
	environment_id TEXT NOT NULL,
	reason TEXT NOT NULL,
	requested_cpu_milli BIGINT NOT NULL DEFAULT 0,
	used_cpu_milli BIGINT NOT NULL DEFAULT 0,
	cpu_milli_limit BIGINT,
	available_cpu_milli BIGINT,
	requested_memory_bytes BIGINT NOT NULL DEFAULT 0,
	used_memory_bytes BIGINT NOT NULL DEFAULT 0,
	memory_bytes_limit BIGINT,
	available_memory_bytes BIGINT,
	requested_ephemeral_storage_bytes BIGINT NOT NULL DEFAULT 0,
	used_ephemeral_storage_bytes BIGINT NOT NULL DEFAULT 0,
	ephemeral_storage_bytes_limit BIGINT,
	available_ephemeral_storage_bytes BIGINT,
	created_at TIMESTAMPTZ NOT NULL,
	CHECK (event_type = 'admission_rejected'),
	CHECK (reason IN ('insufficient_cpu', 'insufficient_memory', 'insufficient_cpu_memory', 'insufficient_ephemeral_storage')),
	CHECK (length(btrim(environment_id)) > 0),
	CHECK (requested_cpu_milli >= 0),
	CHECK (used_cpu_milli >= 0),
	CHECK (cpu_milli_limit IS NULL OR cpu_milli_limit >= 0),
	CHECK (available_cpu_milli IS NULL OR available_cpu_milli >= 0),
	CHECK (requested_memory_bytes >= 0),
	CHECK (used_memory_bytes >= 0),
	CHECK (memory_bytes_limit IS NULL OR memory_bytes_limit >= 0),
	CHECK (available_memory_bytes IS NULL OR available_memory_bytes >= 0),
	CHECK (requested_ephemeral_storage_bytes >= 0),
	CHECK (used_ephemeral_storage_bytes >= 0),
	CHECK (ephemeral_storage_bytes_limit IS NULL OR ephemeral_storage_bytes_limit >= 0),
	CHECK (available_ephemeral_storage_bytes IS NULL OR available_ephemeral_storage_bytes >= 0)
);

CREATE TABLE allocation_access_grants (
	grant_id TEXT PRIMARY KEY,
	allocation_id TEXT NOT NULL,
	node_id TEXT NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	revision BIGINT NOT NULL,
	revoked BOOLEAN NOT NULL DEFAULT FALSE,
	token_hash TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	CHECK (revision > 0),
	CHECK (expires_at > created_at),
	CHECK (length(btrim(token_hash)) > 0),
	FOREIGN KEY (allocation_id, node_id)
		REFERENCES allocations(allocation_id, node_id) ON DELETE CASCADE
);

CREATE TABLE allocation_reconcile_queue (
	allocation_id TEXT PRIMARY KEY REFERENCES allocations(allocation_id) ON DELETE CASCADE,
	next_run_at TIMESTAMPTZ NOT NULL,
	reconcile_attempts INTEGER NOT NULL DEFAULT 0,
	last_error TEXT NOT NULL DEFAULT '',
	claim_owner TEXT NOT NULL DEFAULT '',
	claim_expires_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	CHECK (reconcile_attempts >= 0),
	CHECK (updated_at >= created_at),
	CHECK (
		(claim_owner = '' AND claim_expires_at IS NULL) OR
		(length(btrim(claim_owner)) > 0 AND claim_expires_at IS NOT NULL)
	)
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
	revision BIGINT NOT NULL CHECK (revision >= 0)
);

INSERT INTO control_revisions(name, revision)
VALUES ('allocation_access_grants', 0), ('tunnel_sessions', 0);

CREATE TABLE tunnel_sessions (
	session_id TEXT PRIMARY KEY,
	allocation_id TEXT NOT NULL REFERENCES allocations(allocation_id) ON DELETE CASCADE,
	creator_principal_id TEXT NOT NULL REFERENCES principals(principal_id) ON DELETE RESTRICT,
	remote_port INTEGER NOT NULL,
	node_edge_target TEXT NOT NULL DEFAULT '',
	relay_id TEXT NOT NULL DEFAULT '',
	client_edge_target TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	reason TEXT NOT NULL DEFAULT '',
	bound_addr TEXT NOT NULL DEFAULT '',
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
	CHECK (remote_port > 0 AND remote_port <= 65535),
	CHECK (status IN (
		'TUNNEL_SESSION_STATUS_PENDING',
		'TUNNEL_SESSION_STATUS_RUNNING',
		'TUNNEL_SESSION_STATUS_DEGRADED',
		'TUNNEL_SESSION_STATUS_REVOKED',
		'TUNNEL_SESSION_STATUS_EXPIRED',
		'TUNNEL_SESSION_STATUS_FAILED'
	)),
	CHECK (expires_at > created_at),
	CHECK (updated_at >= created_at),
	CHECK (ready_at IS NULL OR ready_at >= created_at),
	CHECK (last_peer_event_at IS NULL OR last_peer_event_at >= created_at),
	CHECK (bytes_in >= 0 AND bytes_out >= 0),
	CHECK (revision > 0),
	CHECK (length(btrim(client_token_hash)) > 0),
	CHECK (length(btrim(node_token_hash)) > 0)
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
	created_at TIMESTAMPTZ NOT NULL,
	CHECK (bytes_in >= 0 AND bytes_out >= 0)
);

CREATE INDEX idx_principal_credentials_principal ON principal_credentials(principal_id, created_at DESC);
CREATE INDEX idx_principal_credentials_active ON principal_credentials(fingerprint) WHERE revoked_at IS NULL;
CREATE INDEX idx_role_bindings_principal ON role_bindings(principal_id, created_at DESC);
CREATE INDEX idx_role_bindings_namespace ON role_bindings(namespace, principal_id) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX idx_role_bindings_active_unique
	ON role_bindings(principal_id, scope_type, COALESCE(namespace, ''), role)
	WHERE revoked_at IS NULL;

CREATE INDEX idx_environments_namespace_created ON environments(namespace, created_at DESC, environment_id DESC);
CREATE INDEX idx_environments_labels ON environments USING GIN(labels jsonb_path_ops);
CREATE INDEX idx_secrets_namespace_created ON secrets(namespace, created_at DESC, secret_id DESC);
CREATE INDEX idx_secrets_labels ON secrets USING GIN(labels jsonb_path_ops);
CREATE INDEX idx_environment_secret_references_secret ON environment_secret_references(secret_id, namespace);
CREATE INDEX idx_run_secret_references_secret ON run_secret_references(secret_id, namespace);
CREATE INDEX idx_runs_namespace_created ON runs(namespace, created_at DESC, run_id DESC);
CREATE INDEX idx_runs_labels ON runs USING GIN(labels jsonb_path_ops);
CREATE INDEX idx_runs_namespace_id ON runs(namespace, run_id);
CREATE INDEX idx_allocations_node_lifecycle ON allocations(node_id, lifecycle_state);
CREATE INDEX idx_allocations_run_lifecycle_updated ON allocations(run_id, lifecycle_state, updated_at);
CREATE INDEX idx_admin_audit_events_created ON admin_audit_events(created_at DESC, event_id DESC);
CREATE INDEX idx_admin_audit_events_operation_created ON admin_audit_events(operation, created_at DESC, event_id DESC);
CREATE INDEX idx_admin_audit_events_target_created ON admin_audit_events(target_type, target_id, created_at DESC, event_id DESC);
CREATE INDEX idx_allocations_resource_charge_node
	ON allocations(node_id)
	INCLUDE (allocation_id, cpu_request_milli, sandbox_memory_request_bytes, ephemeral_storage_request_bytes)
	WHERE lifecycle_state <> 'ALLOCATION_LIFECYCLE_STATE_RELEASED';
CREATE INDEX idx_namespace_quota_events_namespace_created ON namespace_quota_events(namespace, created_at DESC, event_id DESC);
CREATE INDEX idx_namespace_quota_events_created ON namespace_quota_events(created_at DESC, event_id DESC);
CREATE INDEX idx_allocation_access_grants_node_revision ON allocation_access_grants(node_id, revision);
CREATE INDEX idx_allocation_access_grants_retention ON allocation_access_grants(created_at, expires_at, revoked);
CREATE INDEX idx_allocation_access_grants_active_created ON allocation_access_grants(created_at, allocation_id) WHERE revoked = FALSE;
CREATE INDEX idx_runs_status_updated ON runs(status, updated_at);
CREATE INDEX idx_allocation_reconcile_queue_claimable
	ON allocation_reconcile_queue(next_run_at, claim_expires_at, allocation_id);
CREATE UNIQUE INDEX idx_tunnel_sessions_active_remote_port
	ON tunnel_sessions(allocation_id, remote_port)
	WHERE status IN (
		'TUNNEL_SESSION_STATUS_PENDING',
		'TUNNEL_SESSION_STATUS_RUNNING',
		'TUNNEL_SESSION_STATUS_DEGRADED'
	);
CREATE INDEX idx_tunnel_sessions_expiry ON tunnel_sessions(expires_at, status);
CREATE INDEX idx_tunnel_sessions_active_created
	ON tunnel_sessions(created_at, allocation_id)
	WHERE status IN (
		'TUNNEL_SESSION_STATUS_PENDING',
		'TUNNEL_SESSION_STATUS_RUNNING',
		'TUNNEL_SESSION_STATUS_DEGRADED'
	);
CREATE INDEX idx_tunnel_session_events_session_created
	ON tunnel_session_events(session_id, created_at DESC, event_id DESC);

CREATE FUNCTION notify_allocation_access_grant_change()
RETURNS TRIGGER AS $$
BEGIN
	PERFORM pg_notify(
		'axern_allocation_access_grant_changes',
		CASE WHEN TG_OP = 'DELETE' THEN OLD.node_id ELSE NEW.node_id END
	);
	RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER allocation_access_grant_change_notify
AFTER INSERT OR UPDATE OR DELETE ON allocation_access_grants
FOR EACH ROW EXECUTE FUNCTION notify_allocation_access_grant_change();

CREATE FUNCTION notify_run_change()
RETURNS TRIGGER AS $$
BEGIN
	PERFORM pg_notify('axern_run_changes', NEW.run_id);
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER run_change_notify
AFTER INSERT OR UPDATE ON runs
FOR EACH ROW EXECUTE FUNCTION notify_run_change();

CREATE FUNCTION notify_tunnel_session_change()
RETURNS TRIGGER AS $$
DECLARE
	target_node_id TEXT;
BEGIN
	SELECT node_id INTO STRICT target_node_id
	FROM allocations
	WHERE allocation_id = NEW.allocation_id;
	PERFORM pg_notify('axern_tunnel_session_changes', target_node_id);
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER tunnel_session_change_notify
AFTER INSERT OR UPDATE OF revision, expires_at ON tunnel_sessions
FOR EACH ROW EXECUTE FUNCTION notify_tunnel_session_change();

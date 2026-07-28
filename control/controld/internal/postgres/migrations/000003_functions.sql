CREATE TABLE functions (
	function_id TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	name TEXT NOT NULL,
	active_revision_id TEXT NOT NULL DEFAULT '',
	spec JSONB NOT NULL DEFAULT '{}'::jsonb,
	status TEXT NOT NULL,
	deployment_status TEXT NOT NULL,
	labels JSONB NOT NULL DEFAULT '{}'::jsonb,
	version BIGINT NOT NULL DEFAULT 1,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	message TEXT NOT NULL DEFAULT '',
	diagnostic_code TEXT NOT NULL DEFAULT '',
	UNIQUE (namespace, name)
);

CREATE TABLE function_revisions (
	revision_id TEXT PRIMARY KEY,
	function_id TEXT NOT NULL REFERENCES functions(function_id) ON DELETE CASCADE,
	namespace TEXT NOT NULL,
	name TEXT NOT NULL,
	revision_number BIGINT NOT NULL,
	spec JSONB NOT NULL DEFAULT '{}'::jsonb,
	source JSONB NOT NULL DEFAULT '{}'::jsonb,
	source_digest TEXT NOT NULL DEFAULT '',
	manifest_digest TEXT NOT NULL DEFAULT '',
	labels JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL,
	created_by TEXT NOT NULL DEFAULT '',
	UNIQUE (function_id, revision_number)
);

CREATE TABLE function_deployments (
	function_id TEXT PRIMARY KEY REFERENCES functions(function_id) ON DELETE CASCADE,
	active_revision_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	scaling JSONB NOT NULL DEFAULT '{}'::jsonb,
	desired_replicas INTEGER NOT NULL DEFAULT 0,
	ready_replicas INTEGER NOT NULL DEFAULT 0,
	active_invocations INTEGER NOT NULL DEFAULT 0,
	updated_at TIMESTAMPTZ NOT NULL,
	message TEXT NOT NULL DEFAULT '',
	diagnostic_code TEXT NOT NULL DEFAULT '',
	worker_service_id TEXT NOT NULL DEFAULT ''
);

CREATE TABLE function_invocations (
	invocation_id TEXT PRIMARY KEY,
	function_id TEXT NOT NULL REFERENCES functions(function_id) ON DELETE CASCADE,
	function_name TEXT NOT NULL,
	namespace TEXT NOT NULL,
	revision_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	mode TEXT NOT NULL,
	payload JSONB NOT NULL DEFAULT '{}'::jsonb,
	result JSONB NOT NULL DEFAULT '{}'::jsonb,
	error JSONB NOT NULL DEFAULT '{}'::jsonb,
	timeout JSONB NOT NULL DEFAULT '{}'::jsonb,
	duration JSONB NOT NULL DEFAULT '{}'::jsonb,
	request_id TEXT NOT NULL DEFAULT '',
	labels JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL,
	started_at TIMESTAMPTZ,
	completed_at TIMESTAMPTZ,
	next_run_at TIMESTAMPTZ NOT NULL,
	deadline_at TIMESTAMPTZ NOT NULL,
	attempt INTEGER NOT NULL DEFAULT 0,
	execution_generation BIGINT NOT NULL DEFAULT 0,
	claim_owner TEXT NOT NULL DEFAULT '',
	lease_token_hash TEXT NOT NULL DEFAULT '',
	lease_expires_at TIMESTAMPTZ,
	message TEXT NOT NULL DEFAULT '',
	diagnostic_code TEXT NOT NULL DEFAULT '',
	CONSTRAINT function_invocations_attempt_nonnegative CHECK (attempt >= 0),
	CONSTRAINT function_invocations_generation_nonnegative CHECK (execution_generation >= 0),
	CONSTRAINT function_invocations_deadline_after_create CHECK (deadline_at > created_at),
	CONSTRAINT function_invocations_async_lease_state CHECK (
		mode <> 'FUNCTION_INVOCATION_MODE_ASYNC'
		OR (status = 'FUNCTION_INVOCATION_STATUS_QUEUED' AND claim_owner = '' AND lease_token_hash = '' AND lease_expires_at IS NULL)
		OR (status = 'FUNCTION_INVOCATION_STATUS_RUNNING' AND claim_owner <> '' AND lease_token_hash <> '' AND lease_expires_at IS NOT NULL)
		OR (status IN ('FUNCTION_INVOCATION_STATUS_SUCCEEDED','FUNCTION_INVOCATION_STATUS_FAILED','FUNCTION_INVOCATION_STATUS_CANCELLED','FUNCTION_INVOCATION_STATUS_TIMED_OUT') AND claim_owner = '' AND lease_token_hash = '' AND lease_expires_at IS NULL)
	)
);

CREATE TABLE function_events (
	event_id TEXT PRIMARY KEY,
	event_sequence BIGSERIAL NOT NULL,
	function_id TEXT NOT NULL REFERENCES functions(function_id) ON DELETE CASCADE,
	invocation_id TEXT NOT NULL DEFAULT '',
	revision_id TEXT NOT NULL DEFAULT '',
	event_type TEXT NOT NULL,
	message TEXT NOT NULL DEFAULT '',
	details JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE function_idempotency_records (
	namespace TEXT NOT NULL,
	function_id TEXT NOT NULL REFERENCES functions(function_id) ON DELETE CASCADE,
	revision_id TEXT NOT NULL DEFAULT '',
	request_id TEXT NOT NULL,
	invocation_id TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ,
	PRIMARY KEY (namespace, function_id, revision_id, request_id)
);

CREATE TABLE function_bundles (
	storage_uri TEXT PRIMARY KEY,
	namespace TEXT NOT NULL,
	name TEXT NOT NULL,
	digest TEXT NOT NULL,
	media_type TEXT NOT NULL,
	size_bytes BIGINT NOT NULL,
	payload BYTEA NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	UNIQUE (digest)
);

CREATE INDEX idx_functions_namespace_created ON functions(namespace, created_at DESC);
CREATE INDEX idx_function_revisions_function_created ON function_revisions(function_id, created_at DESC);
CREATE INDEX idx_function_invocations_function_created ON function_invocations(function_id, created_at DESC);
CREATE INDEX idx_function_invocations_namespace_created ON function_invocations(namespace, created_at DESC);
CREATE INDEX idx_function_invocations_request_id ON function_invocations(function_id, revision_id, request_id);
CREATE INDEX idx_function_invocations_async_claimable ON function_invocations(next_run_at, created_at, invocation_id)
	WHERE mode = 'FUNCTION_INVOCATION_MODE_ASYNC' AND status = 'FUNCTION_INVOCATION_STATUS_QUEUED';
CREATE INDEX idx_function_invocations_async_expired_lease ON function_invocations(lease_expires_at, invocation_id)
	WHERE mode = 'FUNCTION_INVOCATION_MODE_ASYNC' AND status = 'FUNCTION_INVOCATION_STATUS_RUNNING';
CREATE INDEX idx_function_invocations_async_deadline ON function_invocations(deadline_at, invocation_id)
	WHERE mode = 'FUNCTION_INVOCATION_MODE_ASYNC' AND status IN ('FUNCTION_INVOCATION_STATUS_QUEUED', 'FUNCTION_INVOCATION_STATUS_RUNNING');
CREATE INDEX idx_function_invocations_running_capacity ON function_invocations(function_id)
	WHERE status = 'FUNCTION_INVOCATION_STATUS_RUNNING';
CREATE INDEX idx_function_events_function_created ON function_events(function_id, created_at DESC);
CREATE INDEX idx_function_events_invocation_created ON function_events(invocation_id, created_at DESC);
CREATE UNIQUE INDEX idx_function_events_sequence ON function_events(event_sequence);
CREATE INDEX idx_function_bundles_namespace_name ON function_bundles(namespace, name);
CREATE INDEX idx_function_bundles_created ON function_bundles(created_at DESC);

CREATE TABLE agent_profiles (
    profile_id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL REFERENCES namespaces(namespace) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    spec JSONB NOT NULL,
    credential_secret_id TEXT NOT NULL REFERENCES secrets(secret_id) ON DELETE RESTRICT,
    credential_secret_version BIGINT NOT NULL,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (namespace, name)
);

CREATE TABLE agent_profile_operations (
    namespace TEXT NOT NULL,
    profile_name TEXT NOT NULL,
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    profile_id TEXT NOT NULL REFERENCES agent_profiles(profile_id) ON DELETE CASCADE,
    result JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (namespace, operation, idempotency_key)
);

CREATE TABLE rollouts (
    rollout_id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL REFERENCES namespaces(namespace) ON DELETE RESTRICT,
    status TEXT NOT NULL,
    failure_class TEXT NOT NULL DEFAULT 'FAILURE_CLASS_UNSPECIFIED',
    spec JSONB NOT NULL,
    spec_hash TEXT NOT NULL,
    idempotency_key TEXT,
    profile_id TEXT,
    source_digest TEXT NOT NULL DEFAULT '',
    descriptor_digest TEXT NOT NULL DEFAULT '',
    plan_artifact_id TEXT NOT NULL DEFAULT '',
    summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    labels JSONB NOT NULL DEFAULT '{}'::jsonb,
    message TEXT NOT NULL DEFAULT '',
    start_policy TEXT NOT NULL DEFAULT 'ROLLOUT_START_POLICY_AUTO',
    start_idempotency_key TEXT,
    frozen_profile JSONB,
    frozen_credential_secret_id TEXT REFERENCES secrets(secret_id) ON DELETE RESTRICT,
    frozen_credential_version BIGINT,
    preflight JSONB,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    deadline TIMESTAMPTZ,
    delete_requested_at TIMESTAMPTZ,
    UNIQUE (namespace, idempotency_key)
);

CREATE TABLE rollout_plans (
    rollout_id TEXT PRIMARY KEY REFERENCES rollouts(rollout_id) ON DELETE CASCADE,
    result_digest TEXT NOT NULL,
    plan JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE rollout_tasks (
    rollout_id TEXT NOT NULL REFERENCES rollouts(rollout_id) ON DELETE CASCADE,
    task_id TEXT NOT NULL,
    task_digest TEXT NOT NULL,
    task JSONB NOT NULL,
    ordinal INTEGER NOT NULL,
    PRIMARY KEY (rollout_id, task_id),
    UNIQUE (rollout_id, ordinal)
);

CREATE TABLE rollout_episodes (
    episode_id TEXT PRIMARY KEY,
    rollout_id TEXT NOT NULL REFERENCES rollouts(rollout_id) ON DELETE CASCADE,
    task_id TEXT NOT NULL,
    task_digest TEXT NOT NULL,
    attempt_index INTEGER NOT NULL,
    execution_generation BIGINT NOT NULL DEFAULT 1,
    status TEXT NOT NULL,
    failure_class TEXT NOT NULL DEFAULT 'FAILURE_CLASS_UNSPECIFIED',
    passed BOOLEAN NOT NULL DEFAULT FALSE,
    reward DOUBLE PRECISION NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    cached_input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cost_microusd BIGINT NOT NULL DEFAULT 0,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    execution_facts JSONB NOT NULL DEFAULT '{}'::jsonb,
    artifact_manifest_id TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    infrastructure_retries INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    UNIQUE (rollout_id, task_id, attempt_index)
);

CREATE TABLE agent_profile_doctor_jobs (
    job_id TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL REFERENCES agent_profiles(profile_id) ON DELETE CASCADE,
    frozen_profile JSONB NOT NULL,
    frozen_credential_secret_id TEXT NOT NULL REFERENCES secrets(secret_id) ON DELETE RESTRICT,
    model TEXT NOT NULL,
    status TEXT NOT NULL,
    checks JSONB NOT NULL DEFAULT '[]'::jsonb,
    healthy BOOLEAN NOT NULL DEFAULT FALSE,
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ
);

CREATE TABLE rollout_work_items (
    work_id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    rollout_id TEXT REFERENCES rollouts(rollout_id) ON DELETE CASCADE,
    episode_id TEXT REFERENCES rollout_episodes(episode_id) ON DELETE CASCADE,
    doctor_job_id TEXT REFERENCES agent_profile_doctor_jobs(job_id) ON DELETE CASCADE,
    execution_generation BIGINT NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'PENDING',
    required_agent TEXT NOT NULL DEFAULT '',
    required_wire_api TEXT NOT NULL DEFAULT '',
    required_profile_id TEXT NOT NULL DEFAULT '',
    required_profile_version BIGINT NOT NULL DEFAULT 0,
    required_profile_concurrency INTEGER NOT NULL DEFAULT 0,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    next_run_at TIMESTAMPTZ NOT NULL,
    claim_owner TEXT NOT NULL DEFAULT '',
    lease_token_hash TEXT NOT NULL DEFAULT '',
    lease_expires_at TIMESTAMPTZ,
    cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
    allocation_id TEXT NOT NULL DEFAULT '',
    result_digest TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT rollout_work_items_kind_owner_check CHECK (
        (kind = 'WORK_KIND_PLAN' AND rollout_id IS NOT NULL AND episode_id IS NULL AND doctor_job_id IS NULL) OR
        (kind = 'WORK_KIND_EPISODE' AND rollout_id IS NOT NULL AND episode_id IS NOT NULL AND doctor_job_id IS NULL) OR
        (kind = 'WORK_KIND_PROFILE_DOCTOR' AND rollout_id IS NULL AND episode_id IS NULL AND doctor_job_id IS NOT NULL)
    ),
    CONSTRAINT rollout_work_items_profile_contract_check CHECK (
        (required_profile_id = '' AND required_profile_version = 0 AND required_profile_concurrency = 0) OR
        (required_profile_id <> '' AND required_profile_version > 0 AND required_profile_concurrency > 0)
    )
);

CREATE TABLE rollout_events (
    sequence BIGSERIAL PRIMARY KEY,
    rollout_id TEXT NOT NULL REFERENCES rollouts(rollout_id) ON DELETE CASCADE,
    episode_id TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL,
    phase TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE rollout_artifacts (
    artifact_id TEXT PRIMARY KEY,
    rollout_id TEXT NOT NULL REFERENCES rollouts(rollout_id) ON DELETE CASCADE,
    episode_id TEXT NOT NULL DEFAULT '',
    execution_generation BIGINT NOT NULL DEFAULT 0,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    object_key TEXT NOT NULL UNIQUE,
    media_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    digest TEXT NOT NULL,
    status TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    committed_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    UNIQUE (rollout_id, episode_id, execution_generation, kind, name)
);

CREATE TABLE rollout_usage_reservations (
    reservation_id TEXT PRIMARY KEY,
    rollout_id TEXT NOT NULL REFERENCES rollouts(rollout_id) ON DELETE CASCADE,
    episode_id TEXT REFERENCES rollout_episodes(episode_id) ON DELETE CASCADE,
    execution_generation BIGINT NOT NULL,
    reserved_tokens BIGINT NOT NULL,
    reserved_cost_microusd BIGINT NOT NULL,
    actual_input_tokens BIGINT NOT NULL DEFAULT 0,
    actual_cached_input_tokens BIGINT NOT NULL DEFAULT 0,
    actual_output_tokens BIGINT NOT NULL DEFAULT 0,
    actual_cost_microusd BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ
);

CREATE TABLE rollout_worker_sessions (
    session_id TEXT PRIMARY KEY,
    worker_id TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    capabilities JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    last_heartbeat_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_secrets_public_namespace_created
    ON secrets(namespace, created_at DESC, secret_id DESC)
    WHERE visibility = 'PUBLIC';
CREATE INDEX idx_profile_credentials_owner
    ON secrets(owner_type, owner_id)
    WHERE visibility = 'INTERNAL';
CREATE INDEX idx_agent_profiles_namespace_created ON agent_profiles(namespace, created_at DESC, profile_id DESC);
CREATE INDEX idx_agent_profile_doctor_created ON agent_profile_doctor_jobs(created_at, job_id);
CREATE INDEX idx_rollouts_namespace_created ON rollouts(namespace, created_at DESC, rollout_id DESC);
CREATE INDEX idx_rollouts_status_created ON rollouts(status, created_at, rollout_id);
CREATE INDEX idx_rollout_episodes_rollout_status ON rollout_episodes(rollout_id, status, episode_id);
CREATE INDEX idx_rollout_episodes_task ON rollout_episodes(rollout_id, task_id, attempt_index);
CREATE INDEX idx_rollout_work_pending_claimable
    ON rollout_work_items(next_run_at, work_id)
    WHERE status = 'PENDING' AND cancel_requested = FALSE;
CREATE INDEX idx_rollout_work_expired_lease
    ON rollout_work_items(lease_expires_at, work_id)
    WHERE status = 'LEASED' AND cancel_requested = FALSE;
CREATE INDEX idx_rollout_work_owner_capacity
    ON rollout_work_items(claim_owner, lease_expires_at)
    WHERE status = 'LEASED';
CREATE INDEX idx_rollout_work_rollout_capacity
    ON rollout_work_items(rollout_id, lease_expires_at)
    WHERE status = 'LEASED';
CREATE INDEX idx_rollout_work_profile_capacity
    ON rollout_work_items(required_profile_id, required_profile_version, lease_expires_at)
    WHERE status = 'LEASED' AND required_profile_id <> '';
CREATE INDEX idx_rollout_work_rollout_status ON rollout_work_items(rollout_id, status, work_id);
CREATE INDEX idx_rollout_events_rollout_sequence ON rollout_events(rollout_id, sequence);
CREATE INDEX idx_rollout_artifacts_rollout_episode ON rollout_artifacts(rollout_id, episode_id, created_at);
CREATE INDEX idx_rollout_usage_active ON rollout_usage_reservations(rollout_id, status);
CREATE INDEX idx_rollout_worker_sessions_expiry ON rollout_worker_sessions(expires_at, session_id);

CREATE FUNCTION notify_rollout_work_change()
RETURNS TRIGGER AS $$
DECLARE
    work rollout_work_items%ROWTYPE;
    action TEXT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        work := OLD;
        IF OLD.status = 'LEASED' THEN
            action := 'capacity';
        END IF;
    ELSIF TG_OP = 'INSERT' THEN
        work := NEW;
        IF NEW.status = 'PENDING' THEN
            action := 'candidate';
        END IF;
    ELSE
        work := NEW;
        IF OLD.status = 'LEASED' AND NEW.status <> 'LEASED' THEN
            action := 'capacity';
        ELSIF NEW.status = 'PENDING' AND (
            OLD.status IS DISTINCT FROM NEW.status OR
            OLD.next_run_at IS DISTINCT FROM NEW.next_run_at OR
            OLD.kind IS DISTINCT FROM NEW.kind OR
            OLD.required_agent IS DISTINCT FROM NEW.required_agent OR
            OLD.required_wire_api IS DISTINCT FROM NEW.required_wire_api
        ) THEN
            action := 'candidate';
        END IF;
    END IF;

    IF action IS NOT NULL THEN
        PERFORM pg_notify(
            'axern_rollout_work_changes',
            json_build_object(
                'action', action,
                'work_id', work.work_id,
                'kind', work.kind,
                'required_agent', work.required_agent,
                'required_wire_api', work.required_wire_api
            )::TEXT
        );
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER rollout_work_change_notify
AFTER INSERT OR UPDATE OR DELETE ON rollout_work_items
FOR EACH ROW EXECUTE FUNCTION notify_rollout_work_change();

CREATE FUNCTION notify_rollout_event()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('axern_rollout_events', NEW.rollout_id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER rollout_event_notify
AFTER INSERT ON rollout_events
FOR EACH ROW EXECUTE FUNCTION notify_rollout_event();

CREATE TABLE IF NOT EXISTS storage_volume_classes (
	name text PRIMARY KEY,
	backend text NOT NULL,
	payload jsonb NOT NULL,
	created_at timestamptz NOT NULL,
	updated_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS storage_volume_claims (
	namespace text NOT NULL,
	name text NOT NULL,
	claim_id text PRIMARY KEY,
	class_name text NOT NULL REFERENCES storage_volume_classes(name),
	status text NOT NULL,
	labels jsonb NOT NULL DEFAULT '{}'::jsonb,
	payload jsonb NOT NULL,
	version bigint NOT NULL,
	reclaim_attempt bigint NOT NULL DEFAULT 0,
	next_reclaim_at timestamptz,
	reclaim_lease_owner text,
	reclaim_lease_token_hash bytea,
	reclaim_lease_until timestamptz,
	reclaim_generation bigint NOT NULL DEFAULT 0,
	created_at timestamptz NOT NULL,
	updated_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS storage_volume_claims_namespace_idx ON storage_volume_claims(namespace);
CREATE INDEX IF NOT EXISTS storage_volume_claims_status_idx ON storage_volume_claims(status);
CREATE INDEX IF NOT EXISTS storage_volume_claims_owner_idx
	ON storage_volume_claims(namespace, (payload->>'owner_type'), (payload->>'owner_id'), claim_id)
	WHERE status <> 'VOLUME_STATUS_DELETED';
CREATE UNIQUE INDEX IF NOT EXISTS storage_volume_claims_active_name_idx
	ON storage_volume_claims(namespace, name)
	WHERE status <> 'VOLUME_STATUS_DELETED';
CREATE INDEX IF NOT EXISTS storage_volume_claims_reclaim_due_idx
	ON storage_volume_claims(next_reclaim_at, updated_at, claim_id)
	WHERE status = 'VOLUME_STATUS_DELETING';

CREATE TABLE IF NOT EXISTS storage_volume_bindings (
	binding_id text PRIMARY KEY,
	claim_id text NOT NULL REFERENCES storage_volume_claims(claim_id),
	namespace text NOT NULL,
	claim_name text NOT NULL,
	workload_id text NOT NULL,
	workload_type text NOT NULL,
	allocation_id text NOT NULL,
	node_id text NOT NULL,
	status text NOT NULL,
	payload jsonb NOT NULL,
	message text NOT NULL DEFAULT '',
	published_at timestamptz,
	released_at timestamptz,
	created_at timestamptz NOT NULL,
	updated_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS storage_volume_bindings_claim_idx ON storage_volume_bindings(claim_id);
CREATE INDEX IF NOT EXISTS storage_volume_bindings_allocation_idx ON storage_volume_bindings(allocation_id);
CREATE INDEX IF NOT EXISTS storage_volume_bindings_status_idx ON storage_volume_bindings(status);

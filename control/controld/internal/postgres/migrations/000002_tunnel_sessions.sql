CREATE TABLE tunnel_sessions (
	session_id TEXT PRIMARY KEY,
	allocation_id TEXT NOT NULL,
	node_id TEXT NOT NULL,
	node_target TEXT NOT NULL DEFAULT '',
	attempt BIGINT NOT NULL,
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
	bytes_out BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX idx_tunnel_sessions_active_remote_port
ON tunnel_sessions(allocation_id, remote_port)
WHERE revoked = FALSE
  AND status IN (
	'TUNNEL_SESSION_STATUS_PENDING',
	'TUNNEL_SESSION_STATUS_RUNNING',
	'TUNNEL_SESSION_STATUS_DEGRADED'
  );

CREATE INDEX idx_tunnel_sessions_node_revision
ON tunnel_sessions(node_id, revision);

CREATE INDEX idx_tunnel_sessions_expiry
ON tunnel_sessions(expires_at, revoked);

CREATE INDEX idx_tunnel_sessions_active_created
ON tunnel_sessions(created_at, allocation_id)
WHERE revoked = FALSE
  AND status IN (
	'TUNNEL_SESSION_STATUS_PENDING',
	'TUNNEL_SESSION_STATUS_RUNNING',
	'TUNNEL_SESSION_STATUS_DEGRADED'
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

CREATE INDEX idx_tunnel_session_events_session_created
ON tunnel_session_events(session_id, created_at DESC, event_id DESC);

INSERT INTO control_revisions(name, revision)
VALUES ('tunnel_sessions', 0);

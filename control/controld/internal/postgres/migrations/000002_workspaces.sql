ALTER TABLE namespace_resource_quotas
  ADD COLUMN persistent_storage_bytes_limit BIGINT CHECK (persistent_storage_bytes_limit >= 0),
  ADD COLUMN persistent_storage_inodes_limit BIGINT CHECK (persistent_storage_inodes_limit >= 0);

CREATE TABLE workspaces (
  workspace_id TEXT PRIMARY KEY,
  namespace TEXT NOT NULL REFERENCES namespaces(namespace),
  node_id TEXT NOT NULL REFERENCES nodes(node_id),
  status TEXT NOT NULL CHECK (status IN ('WORKSPACE_STATUS_PROVISIONING', 'WORKSPACE_STATUS_READY', 'WORKSPACE_STATUS_UNAVAILABLE', 'WORKSPACE_STATUS_DELETING', 'WORKSPACE_STATUS_DELETED')),
  capacity_bytes BIGINT NOT NULL CHECK (capacity_bytes > 0),
  inode_limit BIGINT NOT NULL CHECK (inode_limit > 0),
  storage_root_identity TEXT NOT NULL CHECK (length(storage_root_identity) > 0),
  storage_identity TEXT NOT NULL DEFAULT '',
  generation BIGINT NOT NULL DEFAULT 0 CHECK (generation >= 0),
  attached_allocation_id TEXT REFERENCES allocations(allocation_id) ON DELETE RESTRICT,
  used_bytes BIGINT NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
  used_inodes BIGINT NOT NULL DEFAULT 0 CHECK (used_inodes >= 0),
  message TEXT NOT NULL DEFAULT '',
  labels JSONB NOT NULL CHECK (jsonb_typeof(labels) = 'object'),
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  next_reconcile_at TIMESTAMPTZ NOT NULL,
  claim_owner TEXT NOT NULL DEFAULT '',
  claim_expires_at TIMESTAMPTZ,
  CHECK (attached_allocation_id IS NULL OR (generation > 0 AND storage_identity <> '')),
  CHECK (status NOT IN ('WORKSPACE_STATUS_DELETING', 'WORKSPACE_STATUS_DELETED') OR attached_allocation_id IS NULL),
  CHECK (status NOT IN ('WORKSPACE_STATUS_READY', 'WORKSPACE_STATUS_UNAVAILABLE') OR storage_identity <> ''),
  CHECK ((claim_owner = '' AND claim_expires_at IS NULL) OR (claim_owner <> '' AND claim_expires_at IS NOT NULL))
);
CREATE INDEX idx_workspaces_namespace_page ON workspaces(namespace, created_at DESC, workspace_id DESC);
CREATE INDEX idx_workspaces_node_charge ON workspaces(node_id) WHERE status <> 'WORKSPACE_STATUS_DELETED';
CREATE INDEX idx_workspaces_reconcile ON workspaces(next_reconcile_at) WHERE status <> 'WORKSPACE_STATUS_DELETED';

ALTER TABLE runs ADD COLUMN workspace_binding JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(workspace_binding) = 'object');

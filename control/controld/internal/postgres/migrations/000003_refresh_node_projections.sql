-- Node summaries are derived telemetry. The OCI-only reporting generation
-- changes their private wire shape; require a new authenticated snapshot.
-- Durable Node admission, credentials, execution and Workspace authority stay
-- in their owning tables and are not reconstructed from this projection.
DELETE FROM node_summaries;

ALTER TABLE namespace_resource_quotas
    ADD COLUMN writable_layer_bytes_limit BIGINT,
    ADD CONSTRAINT namespace_resource_quotas_writable_layer_limit_nonnegative
        CHECK (writable_layer_bytes_limit IS NULL OR writable_layer_bytes_limit >= 0);

ALTER TABLE workload_reservations
    ADD COLUMN writable_layer_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN memory_overhead_bytes BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT workload_reservations_writable_layer_nonnegative
        CHECK (writable_layer_bytes >= 0),
    ADD CONSTRAINT workload_reservations_memory_overhead_nonnegative
        CHECK (memory_overhead_bytes >= 0);

ALTER TABLE namespace_quota_events
    ADD COLUMN requested_writable_layer_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN reserved_writable_layer_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN writable_layer_bytes_limit BIGINT,
    ADD COLUMN available_writable_layer_bytes BIGINT,
    ADD CONSTRAINT namespace_quota_events_writable_layer_limit_nonnegative
        CHECK (writable_layer_bytes_limit IS NULL OR writable_layer_bytes_limit >= 0),
    ADD CONSTRAINT namespace_quota_events_writable_layer_available_nonnegative
        CHECK (available_writable_layer_bytes IS NULL OR available_writable_layer_bytes >= 0);

-- +migrations Up
CREATE TABLE event_sourcing.snapshots (
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    aggregate_version bigint NOT NULL,
    snapshot_schema_version bigint NOT NULL,
    state bytea NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (aggregate_type, aggregate_id),
    CONSTRAINT snapshots_aggregate_type_length
        CHECK (octet_length(aggregate_type) BETWEEN 1 AND 255),
    CONSTRAINT snapshots_aggregate_id_length
        CHECK (octet_length(aggregate_id) BETWEEN 1 AND 512),
    CONSTRAINT snapshots_aggregate_version
        CHECK (aggregate_version > 0),
    CONSTRAINT snapshots_schema_version CHECK (
        snapshot_schema_version > 0
        AND snapshot_schema_version <= 4294967295
    ),
    CONSTRAINT snapshots_state_length
        CHECK (octet_length(state) BETWEEN 1 AND 8388608),
    CONSTRAINT snapshots_metadata_object
        CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT snapshots_metadata_length
        CHECK (octet_length(metadata::text) <= 65536),
    CONSTRAINT snapshots_created_at_finite
        CHECK (isfinite(created_at))
);

CREATE TABLE event_sourcing.projections (
    name text PRIMARY KEY,
    state smallint NOT NULL DEFAULT 1,
    checkpoint bigint,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT projections_name_length
        CHECK (octet_length(name) BETWEEN 1 AND 512),
    CONSTRAINT projections_state
        CHECK (state IN (1, 2)),
    CONSTRAINT projections_checkpoint
        CHECK (checkpoint IS NULL OR checkpoint > 0),
    CONSTRAINT projections_updated_at_finite
        CHECK (isfinite(updated_at))
);

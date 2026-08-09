-- +migrations Up
CREATE SCHEMA event_sourcing;

CREATE TABLE event_sourcing.positions (
    singleton boolean PRIMARY KEY DEFAULT true,
    last_position bigint NOT NULL DEFAULT 0,
    CONSTRAINT positions_singleton CHECK (singleton),
    CONSTRAINT positions_last_position CHECK (last_position >= 0)
);

INSERT INTO event_sourcing.positions (singleton, last_position)
VALUES (true, 0);

CREATE TABLE event_sourcing.streams (
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    current_version bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (aggregate_type, aggregate_id),
    CONSTRAINT streams_aggregate_type_length
        CHECK (octet_length(aggregate_type) BETWEEN 1 AND 255),
    CONSTRAINT streams_aggregate_id_length
        CHECK (octet_length(aggregate_id) BETWEEN 1 AND 512),
    CONSTRAINT streams_current_version
        CHECK (current_version >= 0),
    CONSTRAINT streams_timestamps_finite
        CHECK (isfinite(created_at) AND isfinite(updated_at))
);

CREATE TABLE event_sourcing.messages (
    global_position bigint PRIMARY KEY,
    message_id text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    stream_version bigint NOT NULL,
    event_name text NOT NULL,
    event_schema_version bigint NOT NULL,
    content_type text NOT NULL,
    payload bytea NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    recorded_at timestamptz NOT NULL,
    correlation_id text,
    causation_id text,
    tenant text,
    partition_key text,
    CONSTRAINT messages_message_id_unique UNIQUE (message_id),
    CONSTRAINT messages_stream_foreign_key
        FOREIGN KEY (aggregate_type, aggregate_id)
        REFERENCES event_sourcing.streams (aggregate_type, aggregate_id),
    CONSTRAINT messages_message_id_length
        CHECK (octet_length(message_id) BETWEEN 1 AND 128),
    CONSTRAINT messages_stream_version
        CHECK (stream_version > 0),
    CONSTRAINT messages_event_name_length
        CHECK (octet_length(event_name) BETWEEN 1 AND 255),
    CONSTRAINT messages_event_schema_version
        CHECK (
            event_schema_version > 0
            AND event_schema_version <= 4294967295
        ),
    CONSTRAINT messages_content_type_length
        CHECK (octet_length(content_type) BETWEEN 1 AND 128),
    CONSTRAINT messages_payload_length
        CHECK (octet_length(payload) BETWEEN 1 AND 1048576),
    CONSTRAINT messages_metadata_object
        CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT messages_metadata_length
        CHECK (octet_length(metadata::text) <= 65536),
    CONSTRAINT messages_recorded_at_finite
        CHECK (isfinite(recorded_at)),
    CONSTRAINT messages_optional_id_lengths CHECK (
        (correlation_id IS NULL OR octet_length(correlation_id)
            BETWEEN 1 AND 128)
        AND (causation_id IS NULL OR octet_length(causation_id)
            BETWEEN 1 AND 128)
    ),
    CONSTRAINT messages_tenant_length
        CHECK (tenant IS NULL OR octet_length(tenant) BETWEEN 1 AND 255),
    CONSTRAINT messages_partition_length
        CHECK (
            partition_key IS NULL
            OR octet_length(partition_key) BETWEEN 1 AND 255
        )
);

CREATE UNIQUE INDEX messages_stream_version_idx
    ON event_sourcing.messages (
        aggregate_type,
        aggregate_id,
        stream_version
    );

CREATE INDEX messages_recorded_at_idx
    ON event_sourcing.messages (recorded_at, global_position);

COMMENT ON TABLE event_sourcing.messages IS
    'Immutable event history; application repair must append or rebuild derived data';

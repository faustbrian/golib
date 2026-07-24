-- +migrations Up
CREATE TABLE outbox_messages (
    id text PRIMARY KEY,
    topic text NOT NULL,
    payload bytea NOT NULL,
    payload_version integer NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    ordering_key text NOT NULL DEFAULT '',
    idempotency_key text NOT NULL DEFAULT '',
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    state text NOT NULL DEFAULT 'pending',
    lease_owner text,
    lease_token text,
    leased_until timestamptz,
    delivered_at timestamptz,
    dead_lettered_at timestamptz,
    last_error text,
    CONSTRAINT outbox_messages_id_length CHECK (octet_length(id) BETWEEN 1 AND 255),
    CONSTRAINT outbox_messages_topic_length CHECK (octet_length(topic) BETWEEN 1 AND 255),
    CONSTRAINT outbox_messages_payload_version
        CHECK (payload_version BETWEEN 1 AND 65535),
    CONSTRAINT outbox_messages_metadata_object
        CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT outbox_messages_metadata_string_values
        CHECK (NOT jsonb_path_exists(
            metadata,
            'strict $.* ? (@.type() != "string")'
        )),
    CONSTRAINT outbox_messages_payload_length
        CHECK (octet_length(payload) <= 1048576),
    CONSTRAINT outbox_messages_metadata_length
        CHECK (octet_length(metadata::text) <= 131072),
    CONSTRAINT outbox_messages_attempts CHECK (attempts BETWEEN 0 AND 10000),
    CONSTRAINT outbox_messages_ordering_key_length CHECK (octet_length(ordering_key) <= 255),
    CONSTRAINT outbox_messages_idempotency_key_length CHECK (octet_length(idempotency_key) <= 255),
    CONSTRAINT outbox_messages_last_error_length
        CHECK (last_error IS NULL OR octet_length(last_error) <= 4096),
    CONSTRAINT outbox_messages_lease_owner_length
        CHECK (lease_owner IS NULL OR octet_length(lease_owner) <= 255),
    CONSTRAINT outbox_messages_lease_token_length
        CHECK (lease_token IS NULL OR octet_length(lease_token) <= 255),
    CONSTRAINT outbox_messages_timestamps_finite CHECK (
        isfinite(available_at)
        AND isfinite(created_at)
        AND isfinite(updated_at)
        AND (leased_until IS NULL OR isfinite(leased_until))
        AND (delivered_at IS NULL OR isfinite(delivered_at))
        AND (dead_lettered_at IS NULL OR isfinite(dead_lettered_at))
    ),
    CONSTRAINT outbox_messages_timestamps_envelope_range CHECK (
        available_at >= TIMESTAMPTZ '0001-01-01 00:00:00+00 BC'
        AND available_at < TIMESTAMPTZ '10000-01-01 00:00:00+00'
        AND created_at >= TIMESTAMPTZ '0001-01-01 00:00:00+00 BC'
        AND created_at < TIMESTAMPTZ '10000-01-01 00:00:00+00'
        AND updated_at >= TIMESTAMPTZ '0001-01-01 00:00:00+00 BC'
        AND updated_at < TIMESTAMPTZ '10000-01-01 00:00:00+00'
        AND (leased_until IS NULL OR (
            leased_until >= TIMESTAMPTZ '0001-01-01 00:00:00+00 BC'
            AND leased_until < TIMESTAMPTZ '10000-01-01 00:00:00+00'
        ))
        AND (delivered_at IS NULL OR (
            delivered_at >= TIMESTAMPTZ '0001-01-01 00:00:00+00 BC'
            AND delivered_at < TIMESTAMPTZ '10000-01-01 00:00:00+00'
        ))
        AND (dead_lettered_at IS NULL OR (
            dead_lettered_at >= TIMESTAMPTZ '0001-01-01 00:00:00+00 BC'
            AND dead_lettered_at < TIMESTAMPTZ '10000-01-01 00:00:00+00'
        ))
    ),
    CONSTRAINT outbox_messages_state CHECK (state IN ('pending', 'leased', 'delivered', 'dead')),
    CONSTRAINT outbox_messages_state_fields CHECK (
        (state = 'pending'
            AND lease_owner IS NULL
            AND lease_token IS NULL
            AND leased_until IS NULL
            AND delivered_at IS NULL
            AND dead_lettered_at IS NULL)
        OR (state = 'leased'
            AND lease_owner IS NOT NULL
            AND lease_token IS NOT NULL
            AND leased_until IS NOT NULL
            AND delivered_at IS NULL
            AND dead_lettered_at IS NULL)
        OR (state = 'delivered'
            AND lease_owner IS NULL
            AND lease_token IS NULL
            AND leased_until IS NULL
            AND delivered_at IS NOT NULL
            AND dead_lettered_at IS NULL)
        OR (state = 'dead'
            AND lease_owner IS NULL
            AND lease_token IS NULL
            AND leased_until IS NULL
            AND delivered_at IS NULL
            AND dead_lettered_at IS NOT NULL)
    )
);

CREATE INDEX outbox_messages_claim_idx
    ON outbox_messages (available_at, created_at, id)
    WHERE state IN ('pending', 'leased');

CREATE INDEX outbox_messages_lease_expiry_idx
    ON outbox_messages (leased_until, id)
    WHERE state = 'leased';

CREATE INDEX outbox_messages_ordering_idx
    ON outbox_messages (ordering_key, created_at, id)
    WHERE state IN ('pending', 'leased') AND ordering_key <> '';

CREATE INDEX outbox_messages_delivered_retention_idx
    ON outbox_messages (delivered_at, id)
    WHERE state = 'delivered';

CREATE INDEX outbox_messages_dead_retention_idx
    ON outbox_messages (dead_lettered_at, id)
    WHERE state = 'dead';

CREATE UNIQUE INDEX outbox_messages_idempotency_idx
    ON outbox_messages (idempotency_key)
    WHERE idempotency_key <> '';

COMMENT ON TABLE outbox_messages IS
    'At-least-once transactional outbox; publisher acceptance before delivery marking can produce duplicates';
COMMENT ON COLUMN outbox_messages.lease_token IS
    'Opaque claim generation token required for lease-safe updates';

CREATE TABLE outbox_replay_audit (
    replay_id text NOT NULL,
    message_id text NOT NULL,
    previous_state text NOT NULL,
    requested_by text NOT NULL,
    reason text NOT NULL,
    requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    available_at timestamptz NOT NULL,
    PRIMARY KEY (replay_id, message_id),
    CONSTRAINT outbox_replay_audit_replay_id_length
        CHECK (octet_length(replay_id) BETWEEN 1 AND 255),
    CONSTRAINT outbox_replay_audit_message_id_length
        CHECK (octet_length(message_id) BETWEEN 1 AND 255),
    CONSTRAINT outbox_replay_audit_previous_state
        CHECK (previous_state IN ('delivered', 'dead')),
    CONSTRAINT outbox_replay_audit_requested_by
        CHECK (octet_length(requested_by) BETWEEN 1 AND 255),
    CONSTRAINT outbox_replay_audit_reason
        CHECK (octet_length(reason) BETWEEN 1 AND 4096),
    CONSTRAINT outbox_replay_audit_timestamps_finite
        CHECK (isfinite(requested_at) AND isfinite(available_at)),
    CONSTRAINT outbox_replay_audit_timestamps_envelope_range CHECK (
        requested_at >= TIMESTAMPTZ '0001-01-01 00:00:00+00 BC'
        AND requested_at < TIMESTAMPTZ '10000-01-01 00:00:00+00'
        AND available_at >= TIMESTAMPTZ '0001-01-01 00:00:00+00 BC'
        AND available_at < TIMESTAMPTZ '10000-01-01 00:00:00+00'
    )
);

CREATE INDEX outbox_replay_audit_requested_at_idx
    ON outbox_replay_audit (requested_at, replay_id);

COMMENT ON TABLE outbox_replay_audit IS
    'Immutable operator audit for duplicate-producing replay actions';

-- +migrations Down
DROP TABLE outbox_replay_audit;
DROP TABLE outbox_messages;

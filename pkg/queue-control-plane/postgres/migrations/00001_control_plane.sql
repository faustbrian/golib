-- +migrations Up
CREATE TABLE queue_control_commands (
    tenant_id varchar(256) NOT NULL CHECK (tenant_id <> ''),
    idempotency_key varchar(256) NOT NULL CHECK (idempotency_key <> ''),
    actor varchar(256) NOT NULL CHECK (actor <> ''),
    reason varchar(1024) NOT NULL CHECK (reason <> ''),
    action varchar(32) NOT NULL CHECK (action IN (
        'pause',
        'resume',
        'drain',
        'terminate',
        'retry',
        'bulk_retry',
        'delete',
        'purge',
        'replay',
        'scale'
    )),
    target_kind varchar(32) NOT NULL CHECK (target_kind IN (
        'queue',
        'worker',
        'worker_group',
        'failure',
        'dead_letter',
        'workload'
    )),
    target_name varchar(256) NOT NULL CHECK (target_name <> ''),
    requested_at timestamptz NOT NULL,
    confirmed boolean NOT NULL DEFAULT false,
    selection_limit integer NULL CHECK (
        selection_limit IS NULL OR selection_limit BETWEEN 1 AND 1000
    ),
    replay_destination varchar(256) NULL,
    replay_policy varchar(32) NULL CHECK (
        replay_policy IS NULL OR replay_policy IN (
            'reject_duplicate',
            'replace_duplicate'
        )
    ),
    scale_replicas integer NULL CHECK (
        scale_replicas IS NULL OR scale_replicas BETWEEN 0 AND 10000
    ),
    status varchar(32) NOT NULL CHECK (status IN (
        'accepted',
        'succeeded',
        'failed',
        'unsupported',
        'timed_out',
        'partial',
        'unknown'
    )),
    failure_code varchar(256) NULL,
    completed_at timestamptz NULL,
    created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, idempotency_key),
    CHECK (
        (status = 'accepted' AND completed_at IS NULL) OR
        (status <> 'accepted' AND completed_at IS NOT NULL)
    ),
    CHECK (
        (status IN (
            'failed', 'unsupported', 'timed_out', 'partial', 'unknown'
        ) AND failure_code IS NOT NULL) OR
        (status IN ('accepted', 'succeeded') AND failure_code IS NULL)
    ),
    CHECK (action <> 'purge' OR confirmed),
    CHECK (
        action <> 'bulk_retry' OR
        (confirmed AND selection_limit IS NOT NULL)
    ),
    CHECK (
        action <> 'replay' OR
        (
            confirmed AND
            replay_destination IS NOT NULL AND
            replay_destination <> '' AND
            replay_policy IS NOT NULL
        )
    ),
    CHECK (
        action <> 'scale' OR
        (
            target_kind = 'workload' AND
            scale_replicas IS NOT NULL AND
            (scale_replicas <> 0 OR confirmed)
        )
    )
);

CREATE INDEX queue_control_commands_history_idx
    ON queue_control_commands (tenant_id, requested_at DESC, idempotency_key);

CREATE TABLE queue_control_desired_states (
    tenant_id varchar(256) NOT NULL,
    target_kind varchar(32) NOT NULL,
    target_name varchar(256) NOT NULL,
    state varchar(32) NOT NULL CHECK (state IN (
        'active',
        'paused',
        'draining',
        'terminating'
    )),
    revision bigint NOT NULL CHECK (revision > 0),
    command_key varchar(256) NOT NULL,
    changed_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, target_kind, target_name),
    FOREIGN KEY (tenant_id, command_key)
        REFERENCES queue_control_commands (tenant_id, idempotency_key)
        ON DELETE RESTRICT
);

CREATE TABLE queue_control_audit_events (
    tenant_id varchar(256) NOT NULL CHECK (tenant_id <> ''),
    sequence bigint GENERATED ALWAYS AS IDENTITY,
    idempotency_key varchar(256) NOT NULL,
    occurred_at timestamptz NOT NULL,
    actor varchar(256) NOT NULL CHECK (actor <> ''),
    action varchar(32) NOT NULL CHECK (action <> ''),
    target varchar(545) NOT NULL CHECK (target <> ''),
    result varchar(32) NOT NULL CHECK (result <> ''),
    previous_hash bytea NOT NULL CHECK (octet_length(previous_hash) = 32),
    hash bytea NOT NULL CHECK (octet_length(hash) = 32),
    PRIMARY KEY (tenant_id, sequence),
    UNIQUE (tenant_id, hash),
    FOREIGN KEY (tenant_id, idempotency_key)
        REFERENCES queue_control_commands (tenant_id, idempotency_key)
        ON DELETE RESTRICT
);

CREATE INDEX queue_control_audit_history_idx
    ON queue_control_audit_events (tenant_id, occurred_at DESC, sequence DESC);

CREATE TABLE queue_control_audit_anchors (
    tenant_id varchar(256) PRIMARY KEY CHECK (tenant_id <> ''),
    sequence bigint NOT NULL CHECK (sequence >= 0),
    hash bytea NOT NULL CHECK (octet_length(hash) = 32),
    retained_through timestamptz NOT NULL
);

-- +migrations Down
DROP TABLE queue_control_audit_anchors;
DROP TABLE queue_control_audit_events;
DROP TABLE queue_control_desired_states;
DROP TABLE queue_control_commands;

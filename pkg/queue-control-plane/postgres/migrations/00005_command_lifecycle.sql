-- +migrations Up
ALTER TABLE queue_control_commands
    ADD COLUMN dispatched_at timestamptz NULL,
    ADD COLUMN acknowledged_at timestamptz NULL,
    DROP CONSTRAINT queue_control_commands_status_check,
    DROP CONSTRAINT queue_control_commands_check,
    DROP CONSTRAINT queue_control_commands_check1;

ALTER TABLE queue_control_commands
    ADD CONSTRAINT queue_control_commands_status_check CHECK (status IN (
        'pending',
        'accepted',
        'dispatched',
        'acknowledged',
        'succeeded',
        'failed',
        'unsupported',
        'timed_out',
        'partial',
        'unknown',
        'canceled'
    )),
    ADD CONSTRAINT queue_control_commands_check1 CHECK (
        (status IN (
            'failed', 'unsupported', 'timed_out', 'partial', 'unknown',
            'canceled'
        ) AND failure_code IS NOT NULL) OR
        (status IN (
            'pending', 'accepted', 'dispatched', 'acknowledged', 'succeeded'
        ) AND failure_code IS NULL)
    ),
    ADD CONSTRAINT queue_control_commands_lifecycle_check CHECK (
        (status IN ('pending', 'accepted') AND
            dispatched_at IS NULL AND acknowledged_at IS NULL AND
            completed_at IS NULL) OR
        (status = 'dispatched' AND dispatched_at IS NOT NULL AND
            acknowledged_at IS NULL AND completed_at IS NULL) OR
        (status = 'acknowledged' AND dispatched_at IS NOT NULL AND
            acknowledged_at IS NOT NULL AND completed_at IS NULL) OR
        (status IN (
            'succeeded', 'failed', 'unsupported', 'timed_out', 'partial',
            'unknown', 'canceled'
        ) AND completed_at IS NOT NULL)
    ),
    ADD CONSTRAINT queue_control_commands_lifecycle_order_check CHECK (
        (acknowledged_at IS NULL OR dispatched_at IS NULL OR
            acknowledged_at >= dispatched_at) AND
        (completed_at IS NULL OR acknowledged_at IS NULL OR
            completed_at >= acknowledged_at)
    );

-- +migrations Down
UPDATE queue_control_commands
SET status = CASE
        WHEN status = 'pending' THEN 'accepted'
        ELSE 'unknown'
    END,
    failure_code = CASE
        WHEN status = 'pending' THEN NULL
        ELSE 'outcome_unknown'
    END,
    completed_at = CASE
        WHEN status = 'pending' THEN NULL
        ELSE COALESCE(completed_at, acknowledged_at, dispatched_at, updated_at)
    END
WHERE status IN ('pending', 'dispatched', 'acknowledged', 'canceled');

ALTER TABLE queue_control_commands
    DROP CONSTRAINT queue_control_commands_lifecycle_order_check,
    DROP CONSTRAINT queue_control_commands_lifecycle_check,
    DROP CONSTRAINT queue_control_commands_status_check,
    DROP CONSTRAINT queue_control_commands_check1,
    DROP COLUMN acknowledged_at,
    DROP COLUMN dispatched_at;

ALTER TABLE queue_control_commands
    ADD CONSTRAINT queue_control_commands_status_check CHECK (status IN (
        'accepted',
        'succeeded',
        'failed',
        'unsupported',
        'timed_out',
        'partial',
        'unknown'
    )),
    ADD CONSTRAINT queue_control_commands_check CHECK (
        (status = 'accepted' AND completed_at IS NULL) OR
        (status <> 'accepted' AND completed_at IS NOT NULL)
    ),
    ADD CONSTRAINT queue_control_commands_check1 CHECK (
        (status IN (
            'failed', 'unsupported', 'timed_out', 'partial', 'unknown'
        ) AND failure_code IS NOT NULL) OR
        (status IN ('accepted', 'succeeded') AND failure_code IS NULL)
    );

-- +migrations Up
ALTER TABLE queue_control_commands
    DROP CONSTRAINT queue_control_commands_status_check,
    DROP CONSTRAINT queue_control_commands_check1;

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
    ADD CONSTRAINT queue_control_commands_check1 CHECK (
        (status IN (
            'failed', 'unsupported', 'timed_out', 'partial', 'unknown'
        ) AND failure_code IS NOT NULL) OR
        (status IN ('accepted', 'succeeded') AND failure_code IS NULL)
    );

-- +migrations Down
ALTER TABLE queue_control_commands
    DROP CONSTRAINT queue_control_commands_status_check,
    DROP CONSTRAINT queue_control_commands_check1;

ALTER TABLE queue_control_commands
    ADD CONSTRAINT queue_control_commands_status_check CHECK (status IN (
        'accepted',
        'succeeded',
        'failed',
        'unknown'
    )),
    ADD CONSTRAINT queue_control_commands_check1 CHECK (
        (status IN ('failed', 'unknown') AND failure_code IS NOT NULL) OR
        (status IN ('accepted', 'succeeded') AND failure_code IS NULL)
    );

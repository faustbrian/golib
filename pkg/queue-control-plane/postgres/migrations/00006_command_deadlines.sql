-- +migrations Up
ALTER TABLE queue_control_commands
    ADD COLUMN deadline timestamptz NULL;

UPDATE queue_control_commands
SET deadline = requested_at + INTERVAL '30 seconds'
WHERE deadline IS NULL;

ALTER TABLE queue_control_commands
    ALTER COLUMN deadline SET NOT NULL,
    ADD CONSTRAINT queue_control_commands_deadline_check CHECK (
        deadline > requested_at AND
        deadline <= requested_at + INTERVAL '5 minutes'
    );

-- +migrations Down
ALTER TABLE queue_control_commands
    DROP CONSTRAINT queue_control_commands_deadline_check,
    DROP COLUMN deadline;

-- +migrations Up
ALTER TABLE queue_control_commands
    ADD COLUMN required_capability varchar(256) NULL,
    ADD COLUMN capability_available boolean NULL,
    ADD COLUMN acknowledged_by varchar(256) NULL,
    ADD COLUMN acknowledgement_protocol_major integer NULL,
    ADD COLUMN acknowledgement_protocol_minor integer NULL;

UPDATE queue_control_commands
SET required_capability = action
WHERE required_capability IS NULL;

ALTER TABLE queue_control_commands
    ALTER COLUMN required_capability SET NOT NULL,
    ADD CONSTRAINT queue_control_commands_required_capability_check CHECK (
        required_capability <> ''
    ),
    ADD CONSTRAINT queue_control_commands_acknowledgement_check CHECK (
        (acknowledged_by IS NULL AND
            acknowledgement_protocol_major IS NULL AND
            acknowledgement_protocol_minor IS NULL) OR
        (acknowledged_by IS NOT NULL AND acknowledged_by <> '' AND
            acknowledgement_protocol_major BETWEEN 1 AND 65535 AND
            acknowledgement_protocol_minor BETWEEN 0 AND 65535)
    );

-- +migrations Down
ALTER TABLE queue_control_commands
    DROP CONSTRAINT queue_control_commands_acknowledgement_check,
    DROP CONSTRAINT queue_control_commands_required_capability_check,
    DROP COLUMN acknowledgement_protocol_minor,
    DROP COLUMN acknowledgement_protocol_major,
    DROP COLUMN acknowledged_by,
    DROP COLUMN capability_available,
    DROP COLUMN required_capability;

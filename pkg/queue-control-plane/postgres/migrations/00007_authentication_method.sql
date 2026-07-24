-- +migrations Up
ALTER TABLE queue_control_commands
    ADD COLUMN authentication_method varchar(256) NULL;

UPDATE queue_control_commands
SET authentication_method = 'legacy'
WHERE authentication_method IS NULL;

ALTER TABLE queue_control_commands
    ALTER COLUMN authentication_method SET NOT NULL,
    ADD CONSTRAINT queue_control_commands_authentication_method_check CHECK (
        authentication_method <> ''
    );

-- +migrations Down
ALTER TABLE queue_control_commands
    DROP CONSTRAINT queue_control_commands_authentication_method_check,
    DROP COLUMN authentication_method;

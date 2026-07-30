-- +migrations Up
ALTER TABLE queue_control_desired_states
    DROP CONSTRAINT queue_control_desired_states_tenant_id_command_id_fkey;

ALTER TABLE queue_control_commands
    ALTER COLUMN command_id TYPE text USING command_id::text,
    ADD CONSTRAINT queue_control_commands_command_id_shape_check CHECK (
        (
            char_length(command_id) = 26 AND
            command_id = lower(command_id) AND
            command_id ~ '^[0-7][0-9a-hjkmnp-tv-z]{25}$'
        ) OR
        command_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    );

ALTER TABLE queue_control_desired_states
    ALTER COLUMN command_id TYPE text USING command_id::text,
    ADD CONSTRAINT queue_control_desired_states_command_id_shape_check CHECK (
        (
            char_length(command_id) = 26 AND
            command_id = lower(command_id) AND
            command_id ~ '^[0-7][0-9a-hjkmnp-tv-z]{25}$'
        ) OR
        command_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    ),
    ADD CONSTRAINT queue_control_desired_states_tenant_id_command_id_fkey
        FOREIGN KEY (tenant_id, command_id)
        REFERENCES queue_control_commands (tenant_id, command_id)
        ON DELETE RESTRICT;

ALTER TABLE queue_control_audit_events
    ALTER COLUMN command_id TYPE text USING command_id::text,
    ADD CONSTRAINT queue_control_audit_events_command_id_shape_check CHECK (
        (
            char_length(command_id) = 26 AND
            command_id = lower(command_id) AND
            command_id ~ '^[0-7][0-9a-hjkmnp-tv-z]{25}$'
        ) OR
        command_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    );

-- +migrations Down
ALTER TABLE queue_control_desired_states
    DROP CONSTRAINT queue_control_desired_states_tenant_id_command_id_fkey,
    DROP CONSTRAINT queue_control_desired_states_command_id_shape_check;

ALTER TABLE queue_control_audit_events
    DROP CONSTRAINT queue_control_audit_events_command_id_shape_check,
    ALTER COLUMN command_id TYPE uuid USING command_id::uuid;

ALTER TABLE queue_control_desired_states
    ALTER COLUMN command_id TYPE uuid USING command_id::uuid;

ALTER TABLE queue_control_commands
    DROP CONSTRAINT queue_control_commands_command_id_shape_check,
    ALTER COLUMN command_id TYPE uuid USING command_id::uuid;

ALTER TABLE queue_control_desired_states
    ADD CONSTRAINT queue_control_desired_states_tenant_id_command_id_fkey
        FOREIGN KEY (tenant_id, command_id)
        REFERENCES queue_control_commands (tenant_id, command_id)
        ON DELETE RESTRICT;

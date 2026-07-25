-- +migrations Up
ALTER TABLE queue_control_commands
    ADD COLUMN command_id uuid;

UPDATE queue_control_commands
SET command_id = gen_random_uuid();

ALTER TABLE queue_control_commands
    ALTER COLUMN command_id SET NOT NULL,
    ADD CONSTRAINT queue_control_commands_tenant_command_id_key
        UNIQUE (tenant_id, command_id);

ALTER TABLE queue_control_desired_states
    ADD COLUMN command_id uuid;

UPDATE queue_control_desired_states AS desired
SET command_id = command.command_id
FROM queue_control_commands AS command
WHERE command.tenant_id = desired.tenant_id
  AND command.idempotency_key = desired.command_key;

ALTER TABLE queue_control_desired_states
    DROP CONSTRAINT queue_control_desired_states_tenant_id_command_key_fkey,
    DROP COLUMN command_key,
    ALTER COLUMN command_id SET NOT NULL,
    ADD CONSTRAINT queue_control_desired_states_tenant_id_command_id_fkey
        FOREIGN KEY (tenant_id, command_id)
        REFERENCES queue_control_commands (tenant_id, command_id)
        ON DELETE RESTRICT;

-- +migrations Down
ALTER TABLE queue_control_desired_states
    ADD COLUMN command_key varchar(256);

UPDATE queue_control_desired_states AS desired
SET command_key = command.idempotency_key
FROM queue_control_commands AS command
WHERE command.tenant_id = desired.tenant_id
  AND command.command_id = desired.command_id;

ALTER TABLE queue_control_desired_states
    DROP CONSTRAINT queue_control_desired_states_tenant_id_command_id_fkey,
    DROP COLUMN command_id,
    ALTER COLUMN command_key SET NOT NULL,
    ADD CONSTRAINT queue_control_desired_states_tenant_id_command_key_fkey
        FOREIGN KEY (tenant_id, command_key)
        REFERENCES queue_control_commands (tenant_id, idempotency_key)
        ON DELETE RESTRICT;

ALTER TABLE queue_control_commands
    DROP CONSTRAINT queue_control_commands_tenant_command_id_key,
    DROP COLUMN command_id;

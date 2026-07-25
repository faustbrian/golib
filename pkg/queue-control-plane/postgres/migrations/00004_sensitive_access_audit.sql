-- +migrations Up
ALTER TABLE queue_control_audit_events
    ADD COLUMN command_id uuid,
    ADD COLUMN hash_version smallint NOT NULL DEFAULT 1
        CHECK (hash_version IN (1, 2));

UPDATE queue_control_audit_events AS audit
SET command_id = command.command_id
FROM queue_control_commands AS command
WHERE command.tenant_id = audit.tenant_id
  AND command.idempotency_key = audit.idempotency_key;

ALTER TABLE queue_control_audit_events
    DROP CONSTRAINT queue_control_audit_events_tenant_id_idempotency_key_fkey,
    ALTER COLUMN command_id SET NOT NULL,
    ALTER COLUMN idempotency_key DROP NOT NULL,
    ALTER COLUMN hash_version SET DEFAULT 2;

-- +migrations Down
DELETE FROM queue_control_audit_events
WHERE idempotency_key IS NULL;

ALTER TABLE queue_control_audit_events
    ALTER COLUMN idempotency_key SET NOT NULL,
    DROP COLUMN command_id,
    DROP COLUMN hash_version,
    ADD CONSTRAINT queue_control_audit_events_tenant_id_idempotency_key_fkey
        FOREIGN KEY (tenant_id, idempotency_key)
        REFERENCES queue_control_commands (tenant_id, idempotency_key)
        ON DELETE RESTRICT;

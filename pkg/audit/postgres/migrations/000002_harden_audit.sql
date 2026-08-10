-- +migrations Up
ALTER TABLE audit.records
    ADD COLUMN accepted_order bigint GENERATED ALWAYS AS IDENTITY;

CREATE UNIQUE INDEX records_accepted_order_idx
    ON audit.records (accepted_order);

ALTER TABLE audit.retention_events
    ADD COLUMN accepted_order bigint;

WITH ordered AS (
    SELECT event_id,
           row_number() OVER (
               PARTITION BY record_id
               ORDER BY occurred_at, event_id
           ) AS accepted_order
    FROM audit.retention_events
)
UPDATE audit.retention_events AS event
SET accepted_order = ordered.accepted_order
FROM ordered
WHERE event.event_id = ordered.event_id;

ALTER TABLE audit.retention_events
    ALTER COLUMN accepted_order SET NOT NULL;

DROP INDEX audit.retention_events_record_time_idx;
CREATE INDEX retention_events_record_order_idx
    ON audit.retention_events (record_id, accepted_order);

CREATE OR REPLACE FUNCTION audit.lock_retention_event() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.record_id, 0));
    SELECT COALESCE(MAX(accepted_order), 0) + 1 INTO NEW.accepted_order
    FROM audit.retention_events
    WHERE record_id = NEW.record_id;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION audit.prune_record(
    target_record_id text,
    expected_digest bytea
) RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, audit
AS $$
DECLARE
    removed_count integer;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(target_record_id, 0));
    DELETE FROM audit.records AS candidate
    WHERE candidate.record_id = target_record_id
      AND candidate.canonical_sha256 = expected_digest
      AND COALESCE((
          SELECT event_kind
          FROM audit.retention_events
          WHERE record_id = target_record_id
          ORDER BY accepted_order DESC
          LIMIT 1
      ), 'release') <> 'hold';
    GET DIAGNOSTICS removed_count = ROW_COUNT;
    RETURN removed_count = 1;
END;
$$;

CREATE OR REPLACE FUNCTION audit.append_record(
    supplied_record_id text,
    supplied_occurred_at timestamptz,
    supplied_recorded_at timestamptz,
    supplied_tenant_id text,
    supplied_actor_kind smallint,
    supplied_actor_id text,
    supplied_subject_type text,
    supplied_subject_id text,
    supplied_action text,
    supplied_outcome smallint,
    supplied_correlation_id text,
    supplied_canonical_record bytea,
    supplied_canonical_sha256 bytea
) RETURNS smallint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, audit
AS $$
DECLARE
    inserted_record_id text;
    existing_canonical bytea;
    existing_digest bytea;
    document jsonb;
BEGIN
    IF sha256(supplied_canonical_record) <> supplied_canonical_sha256 THEN
        RAISE EXCEPTION 'invalid audit record' USING ERRCODE = '22023';
    END IF;

    BEGIN
        document := convert_from(supplied_canonical_record, 'UTF8')::jsonb;
        IF (document ->> 'schema_version')::integer IS DISTINCT FROM 1
           OR document ->> 'id' IS DISTINCT FROM supplied_record_id
           OR (document ->> 'occurred_at')::timestamptz IS DISTINCT FROM supplied_occurred_at
           OR (document ->> 'recorded_at')::timestamptz IS DISTINCT FROM supplied_recorded_at
           OR NULLIF(document #>> '{context,tenant_id}', '') IS DISTINCT FROM supplied_tenant_id
           OR (document #>> '{actor,kind}')::smallint IS DISTINCT FROM supplied_actor_kind
           OR NULLIF(document #>> '{actor,id}', '') IS DISTINCT FROM supplied_actor_id
           OR document #>> '{subject,type}' IS DISTINCT FROM supplied_subject_type
           OR document #>> '{subject,id}' IS DISTINCT FROM supplied_subject_id
           OR document ->> 'action' IS DISTINCT FROM supplied_action
           OR (document ->> 'outcome')::smallint IS DISTINCT FROM supplied_outcome
           OR NULLIF(document #>> '{context,correlation_id}', '') IS DISTINCT FROM supplied_correlation_id THEN
            RAISE EXCEPTION 'invalid audit record' USING ERRCODE = '22023';
        END IF;
    EXCEPTION WHEN OTHERS THEN
        RAISE EXCEPTION 'invalid audit record' USING ERRCODE = '22023';
    END;

    INSERT INTO audit.records (
        record_id, occurred_at, recorded_at, tenant_id, actor_kind, actor_id,
        subject_type, subject_id, action, outcome, correlation_id,
        canonical_record, canonical_sha256
    ) VALUES (
        supplied_record_id, supplied_occurred_at, supplied_recorded_at,
        supplied_tenant_id, supplied_actor_kind, supplied_actor_id,
        supplied_subject_type, supplied_subject_id, supplied_action,
        supplied_outcome, supplied_correlation_id, supplied_canonical_record,
        supplied_canonical_sha256
    )
    ON CONFLICT (record_id) DO NOTHING
    RETURNING record_id INTO inserted_record_id;

    IF inserted_record_id IS NOT NULL THEN
        RETURN 1;
    END IF;

    SELECT records.canonical_record, records.canonical_sha256
    INTO STRICT existing_canonical, existing_digest
    FROM audit.records AS records
    WHERE records.record_id = supplied_record_id;

    IF existing_canonical = supplied_canonical_record
       AND existing_digest = supplied_canonical_sha256 THEN
        RETURN 2;
    END IF;
    RETURN 3;
END;
$$;

REVOKE USAGE ON SCHEMA audit FROM audit_writer, audit_reader, audit_retention;
REVOKE EXECUTE ON FUNCTION audit.append_record(
    text, timestamptz, timestamptz, text, smallint, text, text, text, text,
    smallint, text, bytea, bytea
) FROM audit_writer;
REVOKE SELECT ON audit.records, audit.retention_events FROM audit_reader;
REVOKE SELECT ON audit.records FROM audit_retention;
REVOKE INSERT, SELECT ON audit.retention_events FROM audit_retention;
REVOKE EXECUTE ON FUNCTION audit.prune_record(text, bytea) FROM audit_retention;

REVOKE ALL ON SCHEMA audit FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA audit FROM PUBLIC;
REVOKE ALL ON FUNCTION audit.append_record(
    text, timestamptz, timestamptz, text, smallint, text, text, text, text,
    smallint, text, bytea, bytea
) FROM PUBLIC;
REVOKE ALL ON FUNCTION audit.prune_record(text, bytea) FROM PUBLIC;

-- +migrations Down
DO $audit_down$
BEGIN
    RAISE EXCEPTION 'audit hardening cannot be reversed because that could expose or erase accepted history'
        USING ERRCODE = '55000';
END
$audit_down$;

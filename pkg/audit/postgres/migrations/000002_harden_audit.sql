-- +migrations Up
ALTER TABLE audit.records
    ADD COLUMN accepted_order bigint GENERATED ALWAYS AS IDENTITY;

CREATE TABLE audit.record_identities (
    record_id text PRIMARY KEY,
    canonical_sha256 bytea NOT NULL,
    CONSTRAINT record_identities_record_id_length CHECK (
        octet_length(record_id) BETWEEN 1 AND 1024
    ),
    CONSTRAINT record_identities_digest_length CHECK (
        octet_length(canonical_sha256) = 32
    )
);

INSERT INTO audit.record_identities (record_id, canonical_sha256)
SELECT record_id, canonical_sha256
FROM audit.records;

CREATE FUNCTION audit.reject_record_identity_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit record identities are immutable' USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER record_identities_reject_change
BEFORE UPDATE OR DELETE ON audit.record_identities
FOR EACH ROW EXECUTE FUNCTION audit.reject_record_identity_change();

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

CREATE FUNCTION audit.canonical_json_string(value text) RETURNS text
LANGUAGE sql
IMMUTABLE
STRICT
SET search_path = pg_catalog
AS $$
    SELECT replace(replace(replace(replace(replace(
        to_json(value)::text,
        '<', '\u003c'), '>', '\u003e'), '&', '\u0026'), chr(8232), '\u2028'), chr(8233), '\u2029')
$$;

CREATE FUNCTION audit.canonical_timestamp(value timestamptz) RETURNS text
LANGUAGE sql
IMMUTABLE
STRICT
SET search_path = pg_catalog
AS $$
    SELECT CASE
            WHEN extract(year FROM value AT TIME ZONE 'UTC') = -1 THEN '0000'
            ELSE to_char(value AT TIME ZONE 'UTC', 'YYYY')
        END || to_char(value AT TIME ZONE 'UTC', '-MM-DD"T"HH24:MI:SS') ||
        CASE
            WHEN to_char(value AT TIME ZONE 'UTC', 'US') = '000000' THEN ''
            ELSE '.' || rtrim(to_char(value AT TIME ZONE 'UTC', 'US'), '0')
        END || 'Z'
$$;

CREATE FUNCTION audit.canonical_string_map(
    value jsonb,
    attributes boolean,
    maximum_entries integer,
    maximum_bytes integer
) RETURNS text
LANGUAGE plpgsql
IMMUTABLE
STRICT
SET search_path = pg_catalog, audit
AS $$
DECLARE
    encoded text;
    entries integer;
    bytes integer;
BEGIN
    IF jsonb_typeof(value) <> 'object' OR EXISTS (
        SELECT 1 FROM jsonb_each(value) AS entry
        WHERE jsonb_typeof(entry.value) <> 'string'
           OR entry.key = ''
           OR regexp_replace(lower(entry.key), '[_\-./ ]', '', 'g') ~
              '(authorization|cookie|password|secret|token|credential|apikey|requestbody|responsebody|rawbody|httprequestbody|httpresponsebody)'
           OR (attributes AND lower(entry.key) ~ '^(audit|integrity)\.')
    ) THEN
        RETURN NULL;
    END IF;
    SELECT count(*), COALESCE(sum(octet_length(entry.key) +
        octet_length(entry.value #>> '{}')), 0)
    INTO entries, bytes FROM jsonb_each(value) AS entry;
    IF entries > maximum_entries OR bytes > maximum_bytes THEN
        RETURN NULL;
    END IF;
    SELECT '{' || COALESCE(string_agg(
        audit.canonical_json_string(entry.key) || ':' ||
        audit.canonical_json_string(entry.value #>> '{}'),
        ',' ORDER BY entry.key COLLATE "C"
    ), '') || '}' INTO encoded
    FROM jsonb_each(value) AS entry;
    RETURN encoded;
END;
$$;

CREATE FUNCTION audit.canonical_actor(value jsonb, delegated boolean) RETURNS text
LANGUAGE plpgsql
IMMUTABLE
STRICT
SET search_path = pg_catalog, audit
AS $$
DECLARE
    encoded text;
    nested text;
BEGIN
    IF jsonb_typeof(value) <> 'object'
       OR value ->> 'kind' !~ '^[0-9]+$'
       OR (value ->> 'kind')::smallint NOT BETWEEN 1 AND 5
       OR octet_length(COALESCE(value ->> 'id', '')) > 1024
       OR octet_length(COALESCE(value ->> 'authentication_method', '')) > 128
       OR COALESCE(value ->> 'authentication_method', '') !~ '^[A-Za-z0-9._-]*$'
       OR ((value ->> 'kind')::smallint BETWEEN 1 AND 3 AND COALESCE(value ->> 'id', '') = '')
       OR ((value ->> 'kind')::smallint BETWEEN 4 AND 5 AND COALESCE(value ->> 'id', '') <> '')
       OR (delegated AND value ? 'delegated_by') THEN
        RETURN NULL;
    END IF;
    encoded := '{"kind":' || (value ->> 'kind')::smallint::text;
    IF COALESCE(value ->> 'id', '') <> '' THEN
        encoded := encoded || ',"id":' || audit.canonical_json_string(value ->> 'id');
    END IF;
    IF COALESCE(value ->> 'authentication_method', '') <> '' THEN
        encoded := encoded || ',"authentication_method":' ||
            audit.canonical_json_string(value ->> 'authentication_method');
    END IF;
    IF value ? 'delegated_by' THEN
        nested := audit.canonical_actor(value -> 'delegated_by', true);
        IF nested IS NULL THEN
            RETURN NULL;
        END IF;
        encoded := encoded || ',"delegated_by":' || nested;
    END IF;
    RETURN encoded || '}';
EXCEPTION WHEN OTHERS THEN
    RETURN NULL;
END;
$$;

CREATE FUNCTION audit.canonical_record(value jsonb) RETURNS bytea
LANGUAGE plpgsql
IMMUTABLE
STRICT
SET search_path = pg_catalog, audit
AS $$
DECLARE
    encoded text;
    mapped text;
    actor text;
    number_text text;
    entries integer;
    bytes integer;
    states integer;
    algorithm integer;
    sequence_number numeric;
    sequence_text text;
BEGIN
    IF jsonb_typeof(value) <> 'object'
       OR value ->> 'schema_version' !~ '^[0-9]+$'
       OR value ->> 'outcome' !~ '^[0-9]+$'
       OR (value ->> 'outcome')::smallint NOT BETWEEN 1 AND 4
       OR octet_length(COALESCE(value ->> 'id', '')) NOT BETWEEN 1 AND 1024
       OR octet_length(COALESCE(value ->> 'action', '')) NOT BETWEEN 1 AND 1024
       OR octet_length(COALESCE(value ->> 'reason_code', '')) > 1024
       OR octet_length(COALESCE(value ->> 'description', '')) > 4096
       OR octet_length(COALESCE(value #>> '{subject,type}', '')) NOT BETWEEN 1 AND 1024
       OR octet_length(COALESCE(value #>> '{subject,id}', '')) NOT BETWEEN 1 AND 1024
       OR jsonb_typeof(value -> 'subject' -> 'deleted') <> 'boolean'
       OR jsonb_typeof(value -> 'changes' -> 'no_change') <> 'boolean' THEN
        RETURN NULL;
    END IF;
    actor := audit.canonical_actor(value -> 'actor', false);
    IF actor IS NULL THEN
        RETURN NULL;
    END IF;
    IF EXISTS (
        SELECT 1 FROM (VALUES
            (value #>> '{context,tenant_id}'), (value #>> '{context,correlation_id}'),
            (value #>> '{context,causation_id}'), (value #>> '{context,request_id}'),
            (value #>> '{context,trace_id}'), (value #>> '{context,idempotency_id}'),
            (value #>> '{context,source_service}'), (value #>> '{context,source_version}'),
            (value #>> '{context,environment}'), (value #>> '{context,network_origin}'),
            (value #>> '{context,user_agent}'), (value #>> '{policy,id}'),
            (value #>> '{policy,version}'), (value #>> '{integrity,partition}'),
            (value #>> '{integrity,key_id}')
        ) AS fields(content)
        WHERE octet_length(COALESCE(fields.content, '')) > 1024
    ) THEN
        RETURN NULL;
    END IF;
    states := CASE WHEN (value #>> '{changes,no_change}')::boolean THEN 1 ELSE 0 END +
        CASE WHEN COALESCE((value #>> '{changes,redacted}')::boolean, false) THEN 1 ELSE 0 END +
        CASE WHEN COALESCE(value #> '{changes,before}', '{}'::jsonb) <> '{}'::jsonb OR
                  COALESCE(value #> '{changes,after}', '{}'::jsonb) <> '{}'::jsonb THEN 1 ELSE 0 END;
    IF states <> 1 THEN
        RETURN NULL;
    END IF;
    SELECT count(*), COALESCE(sum(octet_length(entry.key) + octet_length(entry.value #>> '{}')), 0)
    INTO entries, bytes
    FROM (
        SELECT * FROM jsonb_each(COALESCE(value #> '{changes,before}', '{}'::jsonb))
        UNION ALL
        SELECT * FROM jsonb_each(COALESCE(value #> '{changes,after}', '{}'::jsonb))
    ) AS entry;
    IF entries > 256 OR bytes > 262144 THEN
        RETURN NULL;
    END IF;
    number_text := COALESCE(value #>> '{integrity,algorithm}', '0');
    sequence_text := COALESCE(value #>> '{integrity,sequence}', '0');
    IF number_text !~ '^[0-9]+$' OR sequence_text !~ '^[0-9]+$' THEN
        RETURN NULL;
    END IF;
    algorithm := number_text::integer;
    sequence_number := sequence_text::numeric;
    IF algorithm <> 0 OR sequence_number <> 0 OR COALESCE(value #>> '{integrity,partition}', '') <> '' OR
       COALESCE(value #>> '{integrity,key_id}', '') <> '' OR
       COALESCE(value #>> '{integrity,previous_digest}', '') <> '' OR
       COALESCE(value #>> '{integrity,digest}', '') <> '' THEN
        IF algorithm NOT IN (1, 2) OR sequence_number < 1 OR
           sequence_number > 18446744073709551615 OR
           COALESCE(value #>> '{integrity,partition}', '') = '' OR
           COALESCE(value #>> '{integrity,digest}', '') !~ '^[0-9a-f]{64}$' OR
           (sequence_number = 1 AND COALESCE(value #>> '{integrity,previous_digest}', '') <> '') OR
           (sequence_number > 1 AND COALESCE(value #>> '{integrity,previous_digest}', '') !~ '^[0-9a-f]{64}$') OR
           (algorithm = 1 AND COALESCE(value #>> '{integrity,key_id}', '') <> '') OR
           (algorithm = 2 AND COALESCE(value #>> '{integrity,key_id}', '') = '') THEN
            RETURN NULL;
        END IF;
    END IF;
    encoded := '{"schema_version":' || (value ->> 'schema_version')::integer::text ||
        ',"id":' || audit.canonical_json_string(value ->> 'id') ||
        ',"occurred_at":' || audit.canonical_json_string(value ->> 'occurred_at') ||
        ',"recorded_at":' || audit.canonical_json_string(value ->> 'recorded_at') ||
        ',"action":' || audit.canonical_json_string(value ->> 'action') ||
        ',"outcome":' || (value ->> 'outcome')::smallint::text;
    FOR mapped, number_text IN SELECT * FROM (VALUES
        ('reason_code', value ->> 'reason_code'),
        ('description', value ->> 'description')
    ) AS optional(name, content) LOOP
        IF COALESCE(number_text, '') <> '' THEN
            encoded := encoded || ',"' || mapped || '":' || audit.canonical_json_string(number_text);
        END IF;
    END LOOP;
    encoded := encoded || ',"actor":' || actor ||
        ',"subject":{"type":' || audit.canonical_json_string(value #>> '{subject,type}') ||
        ',"id":' || audit.canonical_json_string(value #>> '{subject,id}') ||
        ',"deleted":' || (value #>> '{subject,deleted}') || '}' ||
        ',"context":{';
    mapped := '';
    FOR number_text, actor IN SELECT * FROM (VALUES
        ('tenant_id', value #>> '{context,tenant_id}'),
        ('correlation_id', value #>> '{context,correlation_id}'),
        ('causation_id', value #>> '{context,causation_id}'),
        ('request_id', value #>> '{context,request_id}'),
        ('trace_id', value #>> '{context,trace_id}'),
        ('idempotency_id', value #>> '{context,idempotency_id}'),
        ('source_service', value #>> '{context,source_service}'),
        ('source_version', value #>> '{context,source_version}'),
        ('environment', value #>> '{context,environment}'),
        ('network_origin', value #>> '{context,network_origin}'),
        ('user_agent', value #>> '{context,user_agent}')
    ) AS optional(name, content) LOOP
        IF COALESCE(actor, '') <> '' THEN
            encoded := encoded || mapped || audit.canonical_json_string(number_text) || ':' ||
                audit.canonical_json_string(actor);
            mapped := ',';
        END IF;
    END LOOP;
    encoded := encoded || '},"changes":{"no_change":' || (value #>> '{changes,no_change}');
    IF COALESCE((value #>> '{changes,redacted}')::boolean, false) THEN
        encoded := encoded || ',"redacted":true';
    END IF;
    IF value #> '{changes,before}' IS NOT NULL AND value #> '{changes,before}' <> '{}'::jsonb THEN
        mapped := audit.canonical_string_map(value #> '{changes,before}', false, 256, 262144);
        IF mapped IS NULL THEN RETURN NULL; END IF;
        encoded := encoded || ',"before":' || mapped;
    END IF;
    IF value #> '{changes,after}' IS NOT NULL AND value #> '{changes,after}' <> '{}'::jsonb THEN
        mapped := audit.canonical_string_map(value #> '{changes,after}', false, 256, 262144);
        IF mapped IS NULL THEN RETURN NULL; END IF;
        encoded := encoded || ',"after":' || mapped;
    END IF;
    encoded := encoded || '},"policy":{';
    mapped := '';
    IF COALESCE(value #>> '{policy,id}', '') <> '' THEN
        encoded := encoded || '"id":' || audit.canonical_json_string(value #>> '{policy,id}');
        mapped := ',';
    END IF;
    IF COALESCE(value #>> '{policy,version}', '') <> '' THEN
        encoded := encoded || mapped || '"version":' || audit.canonical_json_string(value #>> '{policy,version}');
    END IF;
    encoded := encoded || '}';
    IF value -> 'attributes' IS NOT NULL AND value -> 'attributes' <> '{}'::jsonb THEN
        mapped := audit.canonical_string_map(value -> 'attributes', true, 64, 32768);
        IF mapped IS NULL THEN RETURN NULL; END IF;
        encoded := encoded || ',"attributes":' || mapped;
    END IF;
    encoded := encoded || ',"integrity":{';
    mapped := '';
    FOR number_text, actor IN SELECT * FROM (VALUES
        ('algorithm', value #>> '{integrity,algorithm}'),
        ('sequence', value #>> '{integrity,sequence}')
    ) AS optional(name, content) LOOP
        IF COALESCE(actor, '0') <> '0' THEN
            IF actor !~ '^[0-9]+$' THEN RETURN NULL; END IF;
            encoded := encoded || mapped || '"' || number_text || '":' || actor;
            mapped := ',';
        END IF;
        IF number_text = 'algorithm' THEN
            FOR number_text, actor IN SELECT * FROM (VALUES
                ('partition', value #>> '{integrity,partition}'),
                ('key_id', value #>> '{integrity,key_id}')
            ) AS strings(name, content) LOOP
                IF COALESCE(actor, '') <> '' THEN
                    encoded := encoded || mapped || '"' || number_text || '":' || audit.canonical_json_string(actor);
                    mapped := ',';
                END IF;
            END LOOP;
        END IF;
    END LOOP;
    FOR number_text, actor IN SELECT * FROM (VALUES
        ('previous_digest', value #>> '{integrity,previous_digest}'),
        ('digest', value #>> '{integrity,digest}')
    ) AS strings(name, content) LOOP
        IF COALESCE(actor, '') <> '' THEN
            encoded := encoded || mapped || '"' || number_text || '":' || audit.canonical_json_string(actor);
            mapped := ',';
        END IF;
    END LOOP;
    encoded := encoded || '}}';
    RETURN convert_to(encoded, 'UTF8');
END;
$$;

CREATE FUNCTION audit.validate_record(
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
) RETURNS void
LANGUAGE plpgsql
SET search_path = pg_catalog, audit
AS $$
DECLARE
    document jsonb;
BEGIN
    IF sha256(supplied_canonical_record) <> supplied_canonical_sha256 THEN
        RAISE EXCEPTION 'invalid audit record' USING ERRCODE = '22023';
    END IF;
    BEGIN
        document := convert_from(supplied_canonical_record, 'UTF8')::jsonb;
        IF audit.canonical_record(document) IS DISTINCT FROM supplied_canonical_record
           OR (document ->> 'schema_version')::integer IS DISTINCT FROM 1
           OR document ->> 'id' IS DISTINCT FROM supplied_record_id
           OR extract(year FROM supplied_occurred_at AT TIME ZONE 'UTC') NOT BETWEEN -1 AND 9999
           OR extract(year FROM supplied_recorded_at AT TIME ZONE 'UTC') NOT BETWEEN -1 AND 9999
           OR audit.canonical_timestamp(supplied_occurred_at) = '0001-01-01T00:00:00Z'
           OR audit.canonical_timestamp(supplied_recorded_at) = '0001-01-01T00:00:00Z'
           OR document ->> 'occurred_at' IS DISTINCT FROM audit.canonical_timestamp(supplied_occurred_at)
           OR document ->> 'recorded_at' IS DISTINCT FROM audit.canonical_timestamp(supplied_recorded_at)
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
END;
$$;

DO $validate_legacy_records$
DECLARE
    candidate audit.records%ROWTYPE;
BEGIN
    FOR candidate IN SELECT * FROM audit.records LOOP
        PERFORM audit.validate_record(
            candidate.record_id, candidate.occurred_at, candidate.recorded_at,
            candidate.tenant_id, candidate.actor_kind, candidate.actor_id,
            candidate.subject_type, candidate.subject_id, candidate.action,
            candidate.outcome, candidate.correlation_id,
            candidate.canonical_record, candidate.canonical_sha256
        );
    END LOOP;
END
$validate_legacy_records$;

CREATE FUNCTION audit.capture_record_identity() RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, audit
AS $$
BEGIN
    PERFORM audit.validate_record(
        NEW.record_id, NEW.occurred_at, NEW.recorded_at, NEW.tenant_id,
        NEW.actor_kind, NEW.actor_id, NEW.subject_type, NEW.subject_id,
        NEW.action, NEW.outcome, NEW.correlation_id,
        NEW.canonical_record, NEW.canonical_sha256
    );
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.record_id, 0));
    PERFORM 1 FROM audit.record_identities AS identity
    WHERE identity.record_id = NEW.record_id;
    IF FOUND THEN
        IF EXISTS (
            SELECT 1 FROM audit.records AS record
            WHERE record.record_id = NEW.record_id
        ) THEN
            RETURN NEW;
        END IF;
        RAISE EXCEPTION 'audit record identity is already retained' USING ERRCODE = '23505';
    END IF;
    INSERT INTO audit.record_identities (record_id, canonical_sha256)
    VALUES (NEW.record_id, NEW.canonical_sha256);
    RETURN NEW;
END;
$$;

CREATE TRIGGER records_capture_identity
BEFORE INSERT ON audit.records
FOR EACH ROW EXECUTE FUNCTION audit.capture_record_identity();

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
    existing_digest bytea;
BEGIN
    PERFORM audit.validate_record(
        supplied_record_id, supplied_occurred_at, supplied_recorded_at,
        supplied_tenant_id, supplied_actor_kind, supplied_actor_id,
        supplied_subject_type, supplied_subject_id, supplied_action,
        supplied_outcome, supplied_correlation_id, supplied_canonical_record,
        supplied_canonical_sha256
    );

    PERFORM pg_advisory_xact_lock(hashtextextended(supplied_record_id, 0));

    SELECT identities.canonical_sha256 INTO existing_digest
    FROM audit.record_identities AS identities
    WHERE identities.record_id = supplied_record_id;
    IF FOUND THEN
        IF existing_digest = supplied_canonical_sha256 THEN
            RETURN 2;
        END IF;
        RETURN 3;
    END IF;

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
    );

    RETURN 1;
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
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA audit FROM PUBLIC;
REVOKE ALL ON audit.record_identities FROM audit_writer, audit_reader, audit_retention;
REVOKE ALL ON FUNCTION audit.canonical_json_string(text) FROM PUBLIC, audit_writer, audit_reader, audit_retention;
REVOKE ALL ON FUNCTION audit.canonical_timestamp(timestamptz) FROM PUBLIC, audit_writer, audit_reader, audit_retention;
REVOKE ALL ON FUNCTION audit.canonical_string_map(jsonb, boolean, integer, integer) FROM PUBLIC, audit_writer, audit_reader, audit_retention;
REVOKE ALL ON FUNCTION audit.canonical_actor(jsonb, boolean) FROM PUBLIC, audit_writer, audit_reader, audit_retention;
REVOKE ALL ON FUNCTION audit.canonical_record(jsonb) FROM PUBLIC, audit_writer, audit_reader, audit_retention;
REVOKE ALL ON FUNCTION audit.validate_record(
    text, timestamptz, timestamptz, text, smallint, text, text, text, text,
    smallint, text, bytea, bytea
) FROM PUBLIC, audit_writer, audit_reader, audit_retention;
REVOKE ALL ON FUNCTION audit.capture_record_identity() FROM PUBLIC, audit_writer, audit_reader, audit_retention;
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

-- +migrations Up
CREATE SCHEMA audit;

CREATE TABLE audit.records (
    record_id text NOT NULL,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    tenant_id text,
    actor_kind smallint NOT NULL,
    actor_id text,
    subject_type text NOT NULL,
    subject_id text NOT NULL,
    action text NOT NULL,
    outcome smallint NOT NULL,
    correlation_id text,
    canonical_record bytea NOT NULL,
    canonical_sha256 bytea NOT NULL,
    PRIMARY KEY (record_id),
    CONSTRAINT records_times_finite CHECK (
        isfinite(occurred_at) AND isfinite(recorded_at)
    ),
    CONSTRAINT records_record_id_length CHECK (
        octet_length(record_id) BETWEEN 1 AND 1024
    ),
    CONSTRAINT records_canonical_length CHECK (
        octet_length(canonical_record) BETWEEN 1 AND 1048576
    ),
    CONSTRAINT records_canonical_digest_length CHECK (
        octet_length(canonical_sha256) = 32
    )
);

CREATE INDEX records_tenant_time_idx
    ON audit.records (tenant_id, recorded_at, record_id);
CREATE INDEX records_actor_time_idx
    ON audit.records (actor_id, recorded_at, record_id);
CREATE INDEX records_subject_time_idx
    ON audit.records (subject_type, subject_id, recorded_at, record_id);
CREATE INDEX records_action_time_idx
    ON audit.records (action, recorded_at, record_id);
CREATE INDEX records_correlation_time_idx
    ON audit.records (correlation_id, recorded_at, record_id);
CREATE INDEX records_time_idx
    ON audit.records (recorded_at, record_id);

CREATE TABLE audit.retention_events (
    event_id text PRIMARY KEY,
    record_id text NOT NULL,
    event_kind text NOT NULL CHECK (event_kind IN ('hold', 'release')),
    reason_code text NOT NULL,
    occurred_at timestamptz NOT NULL CHECK (isfinite(occurred_at)),
    CONSTRAINT retention_event_id_length CHECK (
        octet_length(event_id) BETWEEN 1 AND 1024
    )
);

CREATE INDEX retention_events_record_time_idx
    ON audit.retention_events (record_id, occurred_at, event_id);

CREATE FUNCTION audit.lock_retention_event() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended(NEW.record_id, 0));
    RETURN NEW;
END;
$$;

CREATE TRIGGER retention_events_lock_record
BEFORE INSERT ON audit.retention_events
FOR EACH ROW EXECUTE FUNCTION audit.lock_retention_event();

CREATE FUNCTION audit.prune_record(
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
          ORDER BY occurred_at DESC, event_id DESC
          LIMIT 1
      ), 'release') <> 'hold';
    GET DIAGNOSTICS removed_count = ROW_COUNT;
    RETURN removed_count = 1;
END;
$$;

CREATE FUNCTION audit.reject_record_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit records are immutable' USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER records_reject_update
BEFORE UPDATE ON audit.records
FOR EACH ROW EXECUTE FUNCTION audit.reject_record_update();

CREATE FUNCTION audit.append_record(
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
BEGIN
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

    SELECT records.canonical_record INTO STRICT existing_canonical
    FROM audit.records AS records
    WHERE records.record_id = supplied_record_id;

    IF existing_canonical = supplied_canonical_record THEN
        RETURN 2;
    END IF;
    RETURN 3;
END;
$$;

REVOKE ALL ON SCHEMA audit FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA audit FROM PUBLIC;

DO $roles$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'audit_writer') THEN
        CREATE ROLE audit_writer NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'audit_reader') THEN
        CREATE ROLE audit_reader NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'audit_retention') THEN
        CREATE ROLE audit_retention NOLOGIN;
    END IF;
END
$roles$;

DO $role_safety$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_roles
        WHERE rolname IN ('audit_writer', 'audit_reader', 'audit_retention')
          AND (rolcanlogin OR rolsuper OR rolcreatedb OR rolcreaterole OR
               rolreplication OR rolbypassrls)
    ) OR EXISTS (
        SELECT 1
        FROM pg_auth_members AS membership
        JOIN pg_roles AS fixed_role ON fixed_role.oid = membership.roleid
        WHERE fixed_role.rolname IN ('audit_writer', 'audit_reader', 'audit_retention')
    ) OR EXISTS (
        SELECT 1
        FROM pg_auth_members AS membership
        JOIN pg_roles AS fixed_role ON fixed_role.oid = membership.member
        WHERE fixed_role.rolname IN ('audit_writer', 'audit_reader', 'audit_retention')
    ) THEN
        RAISE EXCEPTION 'audit fixed roles must be inert and have no memberships before privileges are granted'
            USING ERRCODE = '42501';
    END IF;
END
$role_safety$;

GRANT USAGE ON SCHEMA audit TO audit_writer, audit_reader, audit_retention;
REVOKE ALL ON FUNCTION audit.append_record(
    text, timestamptz, timestamptz, text, smallint, text, text, text, text,
    smallint, text, bytea, bytea
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION audit.append_record(
    text, timestamptz, timestamptz, text, smallint, text, text, text, text,
    smallint, text, bytea, bytea
) TO audit_writer;
GRANT SELECT ON audit.records, audit.retention_events TO audit_reader;
GRANT SELECT ON audit.records TO audit_retention;
GRANT INSERT, SELECT ON audit.retention_events TO audit_retention;
REVOKE ALL ON FUNCTION audit.prune_record(text, bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION audit.prune_record(text, bytea) TO audit_retention;

COMMENT ON TABLE audit.records IS
    'Immutable audit records ordered for export by recorded_at, record_id';

-- +migrations Down
DROP SCHEMA audit CASCADE;

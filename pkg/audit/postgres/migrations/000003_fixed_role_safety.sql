-- +migrations Up
DO $role_safety$
DECLARE
    fixed_role text;
    membership record;
BEGIN
    FOR membership IN
        SELECT granted_role.rolname AS granted_role,
               member_role.rolname AS member_role
        FROM pg_catalog.pg_auth_members AS relation
        JOIN pg_catalog.pg_roles AS granted_role ON granted_role.oid = relation.roleid
        JOIN pg_catalog.pg_roles AS member_role ON member_role.oid = relation.member
        WHERE granted_role.rolname IN ('audit_writer', 'audit_reader', 'audit_retention')
           OR member_role.rolname IN ('audit_writer', 'audit_reader', 'audit_retention')
    LOOP
        EXECUTE pg_catalog.format(
            'REVOKE %I FROM %I',
            membership.granted_role,
            membership.member_role
        );
    END LOOP;
    FOREACH fixed_role IN ARRAY ARRAY[
        'audit_writer', 'audit_reader', 'audit_retention'
    ] LOOP
        EXECUTE pg_catalog.format(
            'ALTER ROLE %I NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT PASSWORD NULL',
            fixed_role
        );
    END LOOP;
END
$role_safety$;

-- +migrations Down
DO $audit_down$
BEGIN
    RAISE EXCEPTION 'audit fixed-role safety cannot be reversed because that could restore privileged access'
        USING ERRCODE = '55000';
END
$audit_down$;

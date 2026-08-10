package postgres

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/jackc/pgx/v5"
)

// RoleNames identifies deployment-owned PostgreSQL roles. Names must be
// unique to the deployment or database; the adapter never creates cluster-wide
// roles or chooses memberships.
type RoleNames struct {
	Writer    string
	Reader    string
	Retention string
}

// PrivilegeSQL returns least-privilege grants for deployment-owned roles.
// Apply it with the migration owner after creating and auditing the roles.
func PrivilegeSQL(roles RoleNames) (string, error) {
	values := []string{roles.Writer, roles.Reader, roles.Retention}
	reserved := map[string]struct{}{"audit_writer": {}, "audit_reader": {}, "audit_retention": {}}
	for _, value := range values {
		if value == "" || len(value) > 63 || !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
			return "", fmt.Errorf("%w: PostgreSQL role names must be bounded", audit.ErrInvalidArgument)
		}
		if _, exists := reserved[value]; exists {
			return "", fmt.Errorf("%w: PostgreSQL roles must be deployment-specific", audit.ErrInvalidArgument)
		}
	}
	if roles.Writer == roles.Reader || roles.Writer == roles.Retention || roles.Reader == roles.Retention {
		return "", fmt.Errorf("%w: PostgreSQL roles must be distinct", audit.ErrInvalidArgument)
	}
	writer := pgx.Identifier{roles.Writer}.Sanitize()
	reader := pgx.Identifier{roles.Reader}.Sanitize()
	retention := pgx.Identifier{roles.Retention}.Sanitize()
	return fmt.Sprintf(`GRANT USAGE ON SCHEMA audit TO %s, %s, %s;
GRANT EXECUTE ON FUNCTION audit.append_record(
    text, timestamptz, timestamptz, text, smallint, text, text, text, text,
    smallint, text, bytea, bytea
) TO %s;
GRANT SELECT ON audit.records, audit.retention_events TO %s;
GRANT SELECT ON audit.records TO %s;
GRANT INSERT, SELECT ON audit.retention_events TO %s;
GRANT EXECUTE ON FUNCTION audit.prune_record(text, bytea) TO %s;
`, writer, reader, retention, writer, reader, retention, retention, retention), nil
}

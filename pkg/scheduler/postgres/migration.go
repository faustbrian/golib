// Package postgres provides persistent server-time fenced leases.
package postgres

// Migration contains the versioned lease-table schema transition.
type Migration struct {
	Version uint
	Name    string
	Up      string
	Down    string
}

// SchemaMigration returns the PostgreSQL lease-table migration.
func SchemaMigration() Migration {
	return Migration{
		Version: 1,
		Name:    "create_scheduler_leases",
		Up: `CREATE TABLE scheduler_leases (
    lease_key text PRIMARY KEY,
    owner text NOT NULL,
    fencing_token bigint NOT NULL CHECK (fencing_token > 0),
    acquired_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    active boolean NOT NULL DEFAULT true
);
CREATE INDEX scheduler_leases_active_expiry_idx
    ON scheduler_leases (expires_at) WHERE active;`,
		Down: `DROP TABLE scheduler_leases;`,
	}
}

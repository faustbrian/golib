package postgres

import migrations "github.com/faustbrian/golib/pkg/migrations"

type Migration struct {
	Version uint
	Name    string
	Up      string
	Down    string
}

func SchemaMigration() Migration {
	return Migration{
		Version: 1,
		Name:    "create_authorization_policy_manifests",
		Up: `CREATE TABLE authorization_policy_manifests (
    singleton smallint PRIMARY KEY CHECK (singleton = 1),
    revision bigint NOT NULL CHECK (revision > 0),
    manifest jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);`,
		Down: `DROP TABLE authorization_policy_manifests;`,
	}
}

func GoMigration() (migrations.Migration, error) {
	migration := SchemaMigration()
	return migrations.NewMigration(
		migrations.Version(migration.Version),
		migration.Name,
		migrations.TransactionModeDefault,
		migration.Up,
		migration.Down,
	)
}

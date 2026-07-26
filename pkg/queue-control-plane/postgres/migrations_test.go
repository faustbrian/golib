package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	migrations "github.com/faustbrian/golib/pkg/migrations"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestMigrationSourceLoadsTenantScopedControlSchema(t *testing.T) {
	t.Parallel()

	source, err := MigrationSource()
	if err != nil {
		t.Fatalf("MigrationSource() error = %v", err)
	}

	migrations, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(migrations) != 8 {
		t.Fatalf("len(Load()) = %d, want 8", len(migrations))
	}

	migration := migrations[0]
	if migration.Version() != 1 || migration.Name() != "control_plane" {
		t.Fatalf("migration identity = %d/%q, want 1/control_plane", migration.Version(), migration.Name())
	}

	up := migration.UpSQL()
	for _, required := range []string{
		"CREATE TABLE queue_control_commands",
		"PRIMARY KEY (tenant_id, idempotency_key)",
		"CREATE TABLE queue_control_desired_states",
		"FOREIGN KEY (tenant_id, command_key)",
		"CREATE TABLE queue_control_audit_events",
		"CHECK (octet_length(previous_hash) = 32)",
		"CHECK (octet_length(hash) = 32)",
		"UNIQUE (tenant_id, hash)",
		"CREATE TABLE queue_control_audit_anchors",
		"CHECK (revision > 0)",
		"CREATE INDEX queue_control_commands_history_idx",
		"CREATE INDEX queue_control_audit_history_idx",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("UpSQL() missing %q", required)
		}
	}
	for _, forbidden := range []string{"redis", "valkey", "payload bytea"} {
		if strings.Contains(strings.ToLower(up), forbidden) {
			t.Fatalf("UpSQL() unexpectedly contains %q", forbidden)
		}
	}

	down := migration.DownSQL()
	for _, required := range []string{
		"DROP TABLE queue_control_audit_anchors",
		"DROP TABLE queue_control_audit_events",
		"DROP TABLE queue_control_desired_states",
		"DROP TABLE queue_control_commands",
	} {
		if !strings.Contains(down, required) {
			t.Fatalf("DownSQL() missing %q", required)
		}
	}

	statusMigration := migrations[1]
	if statusMigration.Version() != 2 || statusMigration.Name() != "command_outcomes" {
		t.Fatalf(
			"status migration identity = %d/%q, want 2/command_outcomes",
			statusMigration.Version(), statusMigration.Name(),
		)
	}
	for _, required := range []string{
		"DROP CONSTRAINT queue_control_commands_status_check",
		"DROP CONSTRAINT queue_control_commands_check1",
		"'unsupported'",
		"'timed_out'",
		"'partial'",
	} {
		if !strings.Contains(statusMigration.UpSQL(), required) {
			t.Fatalf("status UpSQL() missing %q", required)
		}
	}

	identifierMigration := migrations[2]
	if identifierMigration.Version() != 3 || identifierMigration.Name() != "command_identifiers" {
		t.Fatalf(
			"identifier migration = %d/%q, want 3/command_identifiers",
			identifierMigration.Version(), identifierMigration.Name(),
		)
	}
	for _, required := range []string{
		"ADD COLUMN command_id uuid",
		"SET command_id = gen_random_uuid()",
		"UNIQUE (tenant_id, command_id)",
		"FOREIGN KEY (tenant_id, command_id)",
	} {
		if !strings.Contains(identifierMigration.UpSQL(), required) {
			t.Fatalf("identifier UpSQL() missing %q", required)
		}
	}

	auditMigration := migrations[3]
	if auditMigration.Version() != 4 || auditMigration.Name() != "sensitive_access_audit" {
		t.Fatalf(
			"audit migration = %d/%q, want 4/sensitive_access_audit",
			auditMigration.Version(), auditMigration.Name(),
		)
	}
	for _, required := range []string{
		"ADD COLUMN command_id uuid",
		"ALTER COLUMN idempotency_key DROP NOT NULL",
		"DROP CONSTRAINT queue_control_audit_events_tenant_id_idempotency_key_fkey",
	} {
		if !strings.Contains(auditMigration.UpSQL(), required) {
			t.Fatalf("audit UpSQL() missing %q", required)
		}
	}

	lifecycleMigration := migrations[4]
	if lifecycleMigration.Version() != 5 || lifecycleMigration.Name() != "command_lifecycle" {
		t.Fatalf(
			"lifecycle migration = %d/%q, want 5/command_lifecycle",
			lifecycleMigration.Version(), lifecycleMigration.Name(),
		)
	}
	for _, required := range []string{
		"ADD COLUMN dispatched_at timestamptz",
		"ADD COLUMN acknowledged_at timestamptz",
		"'pending'",
		"'dispatched'",
		"'acknowledged'",
		"queue_control_commands_lifecycle_check",
	} {
		if !strings.Contains(lifecycleMigration.UpSQL(), required) {
			t.Fatalf("lifecycle UpSQL() missing %q", required)
		}
	}

	deadlineMigration := migrations[5]
	if deadlineMigration.Version() != 6 || deadlineMigration.Name() != "command_deadlines" {
		t.Fatalf(
			"deadline migration = %d/%q, want 6/command_deadlines",
			deadlineMigration.Version(), deadlineMigration.Name(),
		)
	}
	for _, required := range []string{
		"ADD COLUMN deadline timestamptz",
		"INTERVAL '30 seconds'",
		"queue_control_commands_deadline_check",
		"INTERVAL '5 minutes'",
	} {
		if !strings.Contains(deadlineMigration.UpSQL(), required) {
			t.Fatalf("deadline UpSQL() missing %q", required)
		}
	}

	authenticationMigration := migrations[6]
	if authenticationMigration.Version() != 7 ||
		authenticationMigration.Name() != "authentication_method" {
		t.Fatalf(
			"authentication migration = %d/%q, want 7/authentication_method",
			authenticationMigration.Version(), authenticationMigration.Name(),
		)
	}
	for _, required := range []string{
		"ADD COLUMN authentication_method varchar(256)",
		"SET authentication_method = 'legacy'",
		"queue_control_commands_authentication_method_check",
	} {
		if !strings.Contains(authenticationMigration.UpSQL(), required) {
			t.Fatalf("authentication UpSQL() missing %q", required)
		}
	}

	capabilityMigration := migrations[7]
	if capabilityMigration.Version() != 8 ||
		capabilityMigration.Name() != "capability_snapshots" {
		t.Fatalf(
			"capability migration = %d/%q, want 8/capability_snapshots",
			capabilityMigration.Version(), capabilityMigration.Name(),
		)
	}
	for _, required := range []string{
		"ADD COLUMN required_capability varchar(256)",
		"ADD COLUMN capability_available boolean",
		"ADD COLUMN acknowledged_by varchar(256)",
		"queue_control_commands_acknowledgement_check",
	} {
		if !strings.Contains(capabilityMigration.UpSQL(), required) {
			t.Fatalf("capability UpSQL() missing %q", required)
		}
	}
}

func TestNewMigrationRunnerRequiresDatabase(t *testing.T) {
	t.Parallel()

	runner, err := NewMigrationRunner(nil)
	if !errors.Is(err, ErrInvalidMigrationDatabase) || runner != nil {
		t.Fatalf(
			"NewMigrationRunner(nil) = (%v, %v), want (nil, ErrInvalidMigrationDatabase)",
			runner,
			err,
		)
	}
}

func TestNewMigrationRunnerBuildsEmbeddedPostgresRunner(t *testing.T) {
	t.Parallel()

	database, err := sql.Open("pgx", "postgres://localhost/control_plane")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	runner, err := NewMigrationRunner(database)
	if err != nil || runner == nil {
		t.Fatalf("NewMigrationRunner() = (%v, %v), want runner and nil", runner, err)
	}
}

func TestNewMigrationRunnerPropagatesCompositionFailures(t *testing.T) {
	t.Parallel()

	database, err := sql.Open("pgx", "postgres://localhost/control_plane")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	sourceErr := errors.New("source failed")
	backendErr := errors.New("backend failed")
	runnerErr := errors.New("runner failed")
	tests := map[string]struct {
		factories migrationFactories
		want      error
	}{
		"source": {
			factories: migrationFactories{
				source: func() (migrations.Source, error) { return nil, sourceErr },
			},
			want: sourceErr,
		},
		"backend": {
			factories: migrationFactories{
				source: MigrationSource,
				backend: func(*sql.DB) (migrations.Backend, error) {
					return nil, backendErr
				},
			},
			want: backendErr,
		},
		"runner": {
			factories: migrationFactories{
				source:  MigrationSource,
				backend: func(*sql.DB) (migrations.Backend, error) { return nil, nil },
				runner: func(migrations.Source, migrations.Backend) (*migrations.Runner, error) {
					return nil, runnerErr
				},
			},
			want: runnerErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runner, err := newMigrationRunner(database, tt.factories)
			if runner != nil || !errors.Is(err, tt.want) {
				t.Fatalf("newMigrationRunner() = (%v, %v), want nil and %v", runner, err, tt.want)
			}
		})
	}
}

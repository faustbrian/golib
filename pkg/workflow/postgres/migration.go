// Package postgres provides durable PostgreSQL workflow persistence.
package postgres

import (
	"fmt"
	"regexp"

	workflow "github.com/faustbrian/golib/pkg/workflow"
	"github.com/jackc/pgx/v5"
)

var schemaPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// Migration is one immutable versioned PostgreSQL schema transition.
type Migration struct {
	Version uint
	Name    string
	Up      string
	Down    string
}

// SchemaMigration returns the default workflow-schema migration.
func SchemaMigration() Migration {
	migration, _ := SchemaMigrationFor("workflow")
	return migration
}

// SchemaMigrations returns every immutable default-schema migration in order.
func SchemaMigrations() []Migration {
	migrations, _ := SchemaMigrationsFor("workflow")
	return migrations
}

// SchemaMigrationsFor returns every immutable migration for a caller-owned
// schema in application order. Callers roll back in reverse order.
func SchemaMigrationsFor(schema string) ([]Migration, error) {
	initial, err := SchemaMigrationFor(schema)
	if err != nil {
		return nil, err
	}
	resolutions := deadLetterResolutionMigrationFor(schema)
	return []Migration{initial, resolutions}, nil
}

// SchemaMigrationFor returns the first durable workflow-store migration for a
// caller-owned schema. The schema must already exist.
func SchemaMigrationFor(schema string) (Migration, error) {
	if !schemaPattern.MatchString(schema) {
		return Migration{}, workflow.ErrInvalidStoreRequest
	}
	instances := pgx.Identifier{schema, "workflow_instances"}.Sanitize()
	transitions := pgx.Identifier{schema, "workflow_transitions"}.Sanitize()
	history := pgx.Identifier{schema, "workflow_history"}.Sanitize()
	work := pgx.Identifier{schema, "workflow_work"}.Sanitize()
	dueIndex := pgx.Identifier{"workflow_work_due_idx"}.Sanitize()
	archiveIndex := pgx.Identifier{"workflow_instances_archive_idx"}.Sanitize()

	return Migration{
		Version: 1,
		Name:    "create_workflow_store",
		Up: fmt.Sprintf(`CREATE TABLE %s (
    instance_id text PRIMARY KEY,
    definition_name text NOT NULL,
    definition_version text NOT NULL,
    definition_fingerprint text NOT NULL CHECK (length(definition_fingerprint) = 64),
    sequence bigint NOT NULL CHECK (sequence >= 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz
);
CREATE INDEX %s ON %s (archived_at, instance_id)
    WHERE archived_at IS NOT NULL;

CREATE TABLE %s (
    transition_id text PRIMARY KEY,
    instance_id text NOT NULL REFERENCES %s(instance_id),
    fingerprint text NOT NULL CHECK (length(fingerprint) = 64),
    expected_sequence bigint NOT NULL CHECK (expected_sequence >= 0),
    committed_sequence bigint NOT NULL CHECK (committed_sequence > expected_sequence),
    committed_at timestamptz NOT NULL
);

CREATE TABLE %s (
    instance_id text NOT NULL REFERENCES %s(instance_id),
    sequence bigint NOT NULL CHECK (sequence > 0),
    kind smallint NOT NULL CHECK (kind > 0),
    occurred_at timestamptz NOT NULL,
    definition_name text NOT NULL DEFAULT '',
    definition_version text NOT NULL DEFAULT '',
    definition_fingerprint text NOT NULL DEFAULT '',
    successor_id text NOT NULL DEFAULT '',
    step_name text NOT NULL DEFAULT '',
    attempt bigint NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    idempotency_key text NOT NULL DEFAULT '',
    due_at timestamptz,
    code text NOT NULL DEFAULT '',
    retryable boolean NOT NULL DEFAULT false,
    data bytea,
    PRIMARY KEY (instance_id, sequence)
);

CREATE TABLE %s (
    work_id text PRIMARY KEY,
    kind smallint NOT NULL CHECK (kind > 0),
    instance_id text NOT NULL,
    sequence bigint NOT NULL,
    available_at timestamptz NOT NULL,
    deadline timestamptz NOT NULL,
    payload bytea,
    tenant_id text NOT NULL DEFAULT '',
    correlation_id text NOT NULL DEFAULT '',
    state smallint NOT NULL DEFAULT 1 CHECK (state BETWEEN 1 AND 4),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    lease_owner text,
    lease_token bigint NOT NULL DEFAULT 0 CHECK (lease_token >= 0),
    lease_expires_at timestamptz,
    failure_code text NOT NULL DEFAULT '',
    completed_at timestamptz,
    FOREIGN KEY (instance_id, sequence) REFERENCES %s(instance_id, sequence)
);
CREATE INDEX %s ON %s (available_at, work_id) WHERE state = 1;`,
			instances, archiveIndex, instances,
			transitions, instances,
			history, instances,
			work, history,
			dueIndex, work,
		),
		Down: fmt.Sprintf("DROP TABLE %s;\nDROP TABLE %s;\nDROP TABLE %s;\nDROP TABLE %s;",
			work, history, transitions, instances),
	}, nil
}

func deadLetterResolutionMigrationFor(schema string) Migration {
	work := pgx.Identifier{schema, "workflow_work"}.Sanitize()
	resolutions := pgx.Identifier{schema, "workflow_work_resolutions"}.Sanitize()
	deadLettersIndex := pgx.Identifier{"workflow_work_dead_letter_idx"}.Sanitize()
	qualifiedDeadLettersIndex := pgx.Identifier{schema, "workflow_work_dead_letter_idx"}.Sanitize()
	return Migration{
		Version: 2,
		Name:    "add_workflow_dead_letter_resolutions",
		Up: fmt.Sprintf(`CREATE TABLE %s (
    command_id text PRIMARY KEY,
    fingerprint text NOT NULL CHECK (length(fingerprint) = 64),
    work_id text NOT NULL REFERENCES %s(work_id),
    lease_token bigint NOT NULL CHECK (lease_token > 0),
    action smallint NOT NULL CHECK (action BETWEEN 1 AND 2),
    actor text NOT NULL,
    reason text NOT NULL,
    occurred_at timestamptz NOT NULL,
    retry_at timestamptz,
    deadline timestamptz,
    UNIQUE (work_id, lease_token),
    CHECK (
        (action = 1 AND retry_at IS NOT NULL AND deadline IS NOT NULL
            AND retry_at >= occurred_at AND deadline > retry_at)
        OR (action = 2 AND retry_at IS NULL AND deadline IS NULL)
    )
);
CREATE INDEX %s ON %s (completed_at, work_id, lease_token)
    WHERE state = 4;`, resolutions, work, deadLettersIndex, work),
		Down: fmt.Sprintf("DROP INDEX %s;\nDROP TABLE %s;", qualifiedDeadLettersIndex, resolutions),
	}
}

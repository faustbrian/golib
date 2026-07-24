package postgres_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/outbox/postgres"
)

func TestMigrationsExposeReversibleSchema(t *testing.T) {
	t.Parallel()

	migrations := postgres.Migrations()
	contents, err := fs.ReadFile(migrations, "000001_create_outbox.sql")
	if err != nil {
		t.Fatalf("read canonical migration: %v", err)
	}

	for _, fragment := range []string{
		"-- +migrations Up",
		"CREATE TABLE outbox_messages",
		"CREATE TABLE outbox_replay_audit",
		"CHECK (state IN ('pending', 'leased', 'delivered', 'dead'))",
		"CONSTRAINT outbox_messages_metadata_string_values",
		"CHECK (attempts BETWEEN 0 AND 10000)",
		"CONSTRAINT outbox_messages_timestamps_finite",
		"CONSTRAINT outbox_messages_timestamps_envelope_range",
		"CONSTRAINT outbox_replay_audit_timestamps_finite",
		"CONSTRAINT outbox_replay_audit_timestamps_envelope_range",
		"CREATE INDEX outbox_messages_claim_idx",
		"CREATE UNIQUE INDEX outbox_messages_idempotency_idx",
		"-- +migrations Down",
	} {
		if !strings.Contains(string(contents), fragment) {
			t.Fatalf("migration does not contain %q", fragment)
		}
	}
	for _, fragment := range []string{"DROP TABLE outbox_replay_audit", "DROP TABLE outbox_messages"} {
		if !strings.Contains(string(contents), fragment) {
			t.Fatalf("down migration does not contain %q: %s", fragment, contents)
		}
	}
}

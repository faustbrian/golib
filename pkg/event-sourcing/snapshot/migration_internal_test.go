package snapshot

import (
	"errors"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestStateMigrationConstructionRejectsInvalidDefinitions(t *testing.T) {
	t.Parallel()

	transform := unchangedMigration
	definitions := []struct {
		from      eventsourcing.SchemaVersion
		to        eventsourcing.SchemaVersion
		transform StateMigrationFunc
	}{
		{0, 2, transform},
		{1, 1, transform},
		{2, 1, transform},
		{1, 2, nil},
	}
	for _, definition := range definitions {
		migration, err := NewStateMigration(
			definition.from,
			definition.to,
			definition.transform,
		)
		if migration.from != 0 ||
			!errors.Is(err, eventsourcing.ErrInvalidArgument) {
			t.Fatalf("NewStateMigration() = %#v, %v", migration, err)
		}
	}

	valid, err := NewStateMigration(1, 2, transform)
	if err != nil {
		t.Fatal(err)
	}
	if chain, err := NewMigrationChain(); chain != nil ||
		!errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewMigrationChain(empty) = %#v, %v", chain, err)
	}
	if chain, err := NewMigrationChain(StateMigration{}); chain != nil ||
		!errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewMigrationChain(zero) = %#v, %v", chain, err)
	}
	if chain, err := NewMigrationChain(valid, valid); chain != nil ||
		!errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewMigrationChain(duplicate) = %#v, %v", chain, err)
	}
}

func TestMigrationChainValidatesInputAndPath(t *testing.T) {
	t.Parallel()

	migration, err := NewStateMigration(1, 3, unchangedMigration)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := NewMigrationChain(migration)
	if err != nil {
		t.Fatal(err)
	}
	stored := migrationSnapshot(t, 1, "state")
	var nilChain *MigrationChain
	checks := []struct {
		name   string
		chain  *MigrationChain
		stored eventsourcing.Snapshot
		target eventsourcing.SchemaVersion
		want   error
	}{
		{"nil chain", nilChain, stored, 2, eventsourcing.ErrInvalidArgument},
		{"zero chain", &MigrationChain{}, stored, 2, eventsourcing.ErrInvalidArgument},
		{"zero snapshot", chain, eventsourcing.Snapshot{}, 2, eventsourcing.ErrInvalidArgument},
		{"zero target", chain, stored, 0, eventsourcing.ErrInvalidArgument},
		{"downgrade", chain, migrationSnapshot(t, 2, "state"), 1, eventsourcing.ErrSnapshotIncompatible},
		{"overshoot", chain, stored, 2, eventsourcing.ErrSnapshotIncompatible},
	}
	for _, check := range checks {
		migrated, migrateErr := check.chain.Migrate(check.stored, check.target)
		if !migrated.IsZero() || !errors.Is(migrateErr, check.want) {
			t.Fatalf("Migrate(%s) = %#v, %v", check.name, migrated, migrateErr)
		}
	}

	current, err := chain.Migrate(stored, 1)
	if err != nil || !current.Equal(stored) {
		t.Fatalf("Migrate(current) = %#v, %v", current, err)
	}
	missing, err := NewMigrationChain(mustStateMigration(t, 2, 3, unchangedMigration))
	if err != nil {
		t.Fatal(err)
	}
	if migrated, err := missing.Migrate(stored, 3); !migrated.IsZero() ||
		!errors.Is(err, eventsourcing.ErrSnapshotIncompatible) {
		t.Fatalf("Migrate(missing) = %#v, %v", migrated, err)
	}
}

func TestMigrationChainBoundsLongPaths(t *testing.T) {
	t.Parallel()

	migrations := make([]StateMigration, maxMigrationSteps+1)
	for index := range migrations {
		from := eventsourcing.SchemaVersion(index + 1)
		migrations[index] = mustStateMigration(
			t,
			from,
			from+1,
			unchangedMigration,
		)
	}
	chain, err := NewMigrationChain(migrations...)
	if err != nil {
		t.Fatal(err)
	}
	if migrated, err := chain.Migrate(
		migrationSnapshot(t, 1, "state"),
		eventsourcing.SchemaVersion(maxMigrationSteps+2),
	); !migrated.IsZero() || !errors.Is(err, ErrStateMigrationLimit) {
		t.Fatalf("Migrate(long path) = %#v, %v", migrated, err)
	}
}

func TestMigrationChainContainsInvalidCallbacks(t *testing.T) {
	t.Parallel()

	secretErr := errors.New("secret migration detail")
	tests := map[string]struct {
		transform StateMigrationFunc
		want      error
	}{
		"error": {
			transform: func([]byte, map[string]string) ([]byte, map[string]string, error) {
				return nil, nil, secretErr
			},
			want: secretErr,
		},
		"panic": {
			transform: func([]byte, map[string]string) ([]byte, map[string]string, error) {
				panic("secret migration panic")
			},
			want: ErrStateMigrationPanic,
		},
		"invalid output": {
			transform: func([]byte, map[string]string) ([]byte, map[string]string, error) {
				return nil, nil, nil
			},
			want: eventsourcing.ErrInvalidArgument,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			chain, err := NewMigrationChain(
				mustStateMigration(t, 1, 2, testCase.transform),
			)
			if err != nil {
				t.Fatal(err)
			}
			migrated, err := chain.Migrate(
				migrationSnapshot(t, 1, "state"),
				2,
			)
			var migrationErr *MigrationError
			if !migrated.IsZero() ||
				!errors.Is(err, testCase.want) ||
				!errors.As(err, &migrationErr) ||
				migrationErr.From != 1 ||
				migrationErr.To != 2 ||
				strings.Contains(err.Error(), "secret") {
				t.Fatalf("Migrate() = %#v, %#v", migrated, err)
			}
		})
	}
}

func TestMigrationChainRequiresDeterministicOutput(t *testing.T) {
	t.Parallel()

	tests := map[string]StateMigrationFunc{
		"state": func() StateMigrationFunc {
			call := 0

			return func([]byte, map[string]string) ([]byte, map[string]string, error) {
				call++

				return []byte{byte(call)}, nil, nil
			}
		}(),
		"second failure": func() StateMigrationFunc {
			call := 0

			return func(state []byte, metadata map[string]string) ([]byte, map[string]string, error) {
				call++
				if call == 2 {
					return nil, nil, errManagerTest
				}

				return state, metadata, nil
			}
		}(),
	}
	for name, transform := range tests {
		transform := transform
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			chain, err := NewMigrationChain(
				mustStateMigration(t, 1, 2, transform),
			)
			if err != nil {
				t.Fatal(err)
			}
			if migrated, err := chain.Migrate(
				migrationSnapshot(t, 1, "state"),
				2,
			); !migrated.IsZero() ||
				!errors.Is(err, ErrNonDeterministicStateMigration) {
				t.Fatalf("Migrate() = %#v, %v", migrated, err)
			}
		})
	}
}

func unchangedMigration(
	state []byte,
	metadata map[string]string,
) ([]byte, map[string]string, error) {
	return state, metadata, nil
}

func mustStateMigration(
	t *testing.T,
	from eventsourcing.SchemaVersion,
	to eventsourcing.SchemaVersion,
	transform StateMigrationFunc,
) StateMigration {
	t.Helper()

	migration, err := NewStateMigration(from, to, transform)
	if err != nil {
		t.Fatal(err)
	}

	return migration
}

func migrationSnapshot(
	t *testing.T,
	schema eventsourcing.SchemaVersion,
	state string,
) eventsourcing.Snapshot {
	t.Helper()

	input := internalSnapshot(t)
	snapshot, err := eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
		Stream:           input.Stream(),
		AggregateVersion: input.AggregateVersion(),
		SchemaVersion:    schema,
		State:            []byte(state),
		Metadata:         input.Metadata(),
		CreatedAt:        input.CreatedAt(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return snapshot
}

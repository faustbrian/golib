package snapshot

import (
	"errors"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

const maxMigrationSteps = 32

var (
	// ErrStateMigrationPanic reports a contained migration callback panic.
	ErrStateMigrationPanic = errors.New("snapshot state migration panicked")
	// ErrNonDeterministicStateMigration reports different results from the same
	// migration input.
	ErrNonDeterministicStateMigration = errors.New(
		"snapshot state migration is non-deterministic",
	)
	// ErrStateMigrationLimit reports a migration path beyond the bounded step
	// count.
	ErrStateMigrationLimit = errors.New("snapshot state migration limit exceeded")
)

// StateMigrationFunc transforms owned snapshot state and metadata. It must be
// deterministic and must not perform external side effects.
type StateMigrationFunc func(
	[]byte,
	map[string]string,
) ([]byte, map[string]string, error)

// StateMigration advances one exact snapshot schema version.
type StateMigration struct {
	from      eventsourcing.SchemaVersion
	to        eventsourcing.SchemaVersion
	transform StateMigrationFunc
}

// NewStateMigration validates one monotonic snapshot schema transformation.
func NewStateMigration(
	from eventsourcing.SchemaVersion,
	to eventsourcing.SchemaVersion,
	transform StateMigrationFunc,
) (StateMigration, error) {
	if from == 0 || to <= from || transform == nil {
		return StateMigration{}, invalid(
			"snapshot migration must advance between assigned schema versions",
		)
	}

	return StateMigration{from: from, to: to, transform: transform}, nil
}

// MigrationChain applies a unique ordered migration path without modifying
// the stored snapshot.
type MigrationChain struct {
	steps      map[eventsourcing.SchemaVersion]StateMigration
	configured bool
}

// NewMigrationChain validates a non-empty unambiguous migration path.
func NewMigrationChain(
	migrations ...StateMigration,
) (*MigrationChain, error) {
	if len(migrations) == 0 {
		return nil, invalid("snapshot migration chain must not be empty")
	}
	steps := make(
		map[eventsourcing.SchemaVersion]StateMigration,
		len(migrations),
	)
	for _, migration := range migrations {
		if migration.from == 0 || migration.to <= migration.from ||
			migration.transform == nil {
			return nil, invalid("snapshot migration must be assigned")
		}
		if _, exists := steps[migration.from]; exists {
			return nil, invalid(
				"snapshot migration source version must be unique",
			)
		}
		steps[migration.from] = migration
	}

	return &MigrationChain{steps: steps, configured: true}, nil
}

// Migrate returns target-schema state while preserving aggregate identity,
// aggregate version, and creation time. Stored history is never rewritten.
func (chain *MigrationChain) Migrate(
	stored eventsourcing.Snapshot,
	target eventsourcing.SchemaVersion,
) (eventsourcing.Snapshot, error) {
	if chain == nil || !chain.configured || stored.IsZero() || target == 0 {
		return eventsourcing.Snapshot{}, eventsourcing.ErrInvalidArgument
	}
	if stored.SchemaVersion() > target {
		return eventsourcing.Snapshot{}, eventsourcing.ErrSnapshotIncompatible
	}
	current := stored
	for count := 0; current.SchemaVersion() != target; count++ {
		if count >= maxMigrationSteps {
			return eventsourcing.Snapshot{}, ErrStateMigrationLimit
		}
		migration, exists := chain.steps[current.SchemaVersion()]
		if !exists || migration.to > target {
			return eventsourcing.Snapshot{}, eventsourcing.ErrSnapshotIncompatible
		}
		next, err := deterministicMigration(migration, current)
		if err != nil {
			return eventsourcing.Snapshot{}, err
		}
		current = next
	}

	return current, nil
}

func deterministicMigration(
	migration StateMigration,
	stored eventsourcing.Snapshot,
) (eventsourcing.Snapshot, error) {
	first, err := callMigration(migration, stored)
	if err != nil {
		return eventsourcing.Snapshot{}, err
	}
	second, err := callMigration(migration, stored)
	if err != nil || !first.Equal(second) {
		return eventsourcing.Snapshot{}, &MigrationError{
			From:  migration.from,
			To:    migration.to,
			Cause: ErrNonDeterministicStateMigration,
		}
	}

	return first, nil
}

func callMigration(
	migration StateMigration,
	stored eventsourcing.Snapshot,
) (migrated eventsourcing.Snapshot, err error) {
	defer func() {
		if recover() != nil {
			migrated = eventsourcing.Snapshot{}
			err = &MigrationError{
				From:  migration.from,
				To:    migration.to,
				Cause: ErrStateMigrationPanic,
			}
		}
	}()

	state, metadata, err := migration.transform(
		stored.State(),
		stored.Metadata(),
	)
	if err != nil {
		return eventsourcing.Snapshot{}, &MigrationError{
			From: migration.from, To: migration.to, Cause: err,
		}
	}
	migrated, err = eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
		Stream:           stored.Stream(),
		AggregateVersion: stored.AggregateVersion(),
		SchemaVersion:    migration.to,
		State:            state,
		Metadata:         metadata,
		CreatedAt:        stored.CreatedAt(),
	})
	if err != nil {
		return eventsourcing.Snapshot{}, &MigrationError{
			From: migration.from, To: migration.to, Cause: err,
		}
	}

	return migrated, nil
}

// MigrationError identifies the schema boundary without disclosing snapshot
// state, metadata, aggregate identity, or callback diagnostics.
type MigrationError struct {
	From  eventsourcing.SchemaVersion
	To    eventsourcing.SchemaVersion
	Cause error
}

// Error implements error with a payload-safe diagnostic.
func (*MigrationError) Error() string {
	return "snapshot state migration failed"
}

// Unwrap preserves the cause for errors.Is and errors.As.
func (err *MigrationError) Unwrap() error {
	return err.Cause
}

var _ error = (*MigrationError)(nil)

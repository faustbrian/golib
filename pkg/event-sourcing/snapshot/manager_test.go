package snapshot_test

import (
	"context"
	"errors"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
	snapshotpkg "github.com/faustbrian/golib/pkg/event-sourcing/snapshot"
)

type managerAccount struct {
	id        string
	owner     string
	lifecycle eventsourcing.Lifecycle
}

func TestManagerLoadsCompatibleSnapshotThroughRepository(t *testing.T) {
	t.Parallel()

	store := memory.NewSnapshotStore()
	stored := managerSnapshot(t, 3, 2, `Ada`)
	if err := store.Save(context.Background(), stored); err != nil {
		t.Fatal(err)
	}
	repository := &managerRepository{
		restore: func(
			_ context.Context,
			id string,
			version uint64,
			restore eventsourcing.AggregateRestorer[*managerAccount],
		) (*managerAccount, error) {
			if id != "account-42" || version != 3 {
				t.Fatalf("Restore() = id %q version %d", id, version)
			}
			account, err := restore()
			if err != nil {
				return nil, err
			}
			if err := account.lifecycle.RestoreSnapshotVersion(version); err != nil {
				t.Fatal(err)
			}

			return account, nil
		},
	}
	manager := newManager(t, repository, store, managerCodec{schema: 2})

	account, info, err := manager.Load(context.Background(), "account-42")
	if err != nil {
		t.Fatal(err)
	}
	if account.owner != "Ada" ||
		account.lifecycle.CommittedVersion() != 3 ||
		info.Source() != snapshotpkg.LoadSnapshot ||
		info.SnapshotVersion() != 3 ||
		info.FallbackReason() != nil {
		t.Fatalf("Load() = %#v, %#v", account, info)
	}
}

func TestManagerFallsBackAccordingToExplicitPolicy(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		store  eventsourcing.SnapshotStore
		codec  managerCodec
		reason error
	}{
		"missing": {
			store:  memory.NewSnapshotStore(),
			codec:  managerCodec{schema: 2},
			reason: eventsourcing.ErrSnapshotNotFound,
		},
		"incompatible": {
			store:  snapshotStoreWith(t, managerSnapshot(t, 3, 1, `Ada`)),
			codec:  managerCodec{schema: 2},
			reason: eventsourcing.ErrSnapshotIncompatible,
		},
		"corrupt": {
			store: snapshotStoreWith(t, managerSnapshot(t, 3, 2, `corrupt`)),
			codec: managerCodec{
				schema: 2,
				decode: func([]byte, map[string]string) (*managerAccount, error) {
					return nil, eventsourcing.ErrSnapshotCorrupt
				},
			},
			reason: eventsourcing.ErrSnapshotCorrupt,
		},
	}
	for name, testCase := range cases {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repository := &managerRepository{
				load: func(context.Context, string) (*managerAccount, error) {
					return &managerAccount{
						id:    "account-42",
						owner: "Full History",
					}, nil
				},
				restore: managerRestore,
			}
			policy, err := snapshotpkg.NewFallbackPolicy(
				snapshotpkg.FallbackMissing,
				snapshotpkg.FallbackCorrupt,
				snapshotpkg.FallbackIncompatible,
			)
			if err != nil {
				t.Fatal(err)
			}
			manager := managerFromConfig(t, managerConfig(
				repository,
				testCase.store,
				testCase.codec,
				policy,
			))

			account, info, err := manager.Load(
				context.Background(),
				"account-42",
			)
			if err != nil {
				t.Fatal(err)
			}
			if account.owner != "Full History" ||
				info.Source() != snapshotpkg.LoadFullHistory ||
				!errors.Is(info.FallbackReason(), testCase.reason) {
				t.Fatalf("Load() = %#v, %#v", account, info)
			}
		})
	}
}

func TestManagerFailsClosedWhenFallbackIsNotAllowed(t *testing.T) {
	t.Parallel()

	policy := snapshotpkg.FailClosed()
	repository := &managerRepository{
		load: func(context.Context, string) (*managerAccount, error) {
			t.Fatal("full history loaded despite fail-closed policy")

			return nil, nil
		},
	}
	manager := managerFromConfig(t, managerConfig(
		repository,
		snapshotStoreWith(t, managerSnapshot(t, 3, 1, `Ada`)),
		managerCodec{schema: 2},
		policy,
	))

	account, info, err := manager.Load(context.Background(), "account-42")
	if !errors.Is(err, eventsourcing.ErrSnapshotIncompatible) ||
		account != nil ||
		info.Source() != snapshotpkg.LoadUnknown {
		t.Fatalf("Load() = %#v, %#v, %v", account, info, err)
	}
}

func TestManagerRefreshesCompleteAggregateExplicitly(t *testing.T) {
	t.Parallel()

	store := memory.NewSnapshotStore()
	repository := &managerRepository{}
	manager := newManager(t, repository, store, managerCodec{schema: 2})
	account := &managerAccount{id: "account-42", owner: "Ada"}
	if err := account.lifecycle.RestoreSnapshotVersion(3); err != nil {
		t.Fatal(err)
	}

	created, err := manager.Refresh(
		context.Background(),
		"account-42",
		account,
	)
	if err != nil {
		t.Fatal(err)
	}
	if created.AggregateVersion() != 3 ||
		created.SchemaVersion() != 2 ||
		string(created.State()) != "Ada" ||
		created.Metadata()["codec"] != "plain" ||
		created.CreatedAt() != managerTime() {
		t.Fatalf("Refresh() = %#v", created)
	}
	loaded, err := store.Load(context.Background(), created.Stream())
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Equal(created) {
		t.Fatalf("stored snapshot = %#v", loaded)
	}
}

func newManager(
	t *testing.T,
	repository snapshotpkg.AggregateRepository[string, *managerAccount],
	store eventsourcing.SnapshotStore,
	codec snapshotpkg.StateCodec[*managerAccount],
) *snapshotpkg.Manager[string, *managerAccount] {
	t.Helper()

	policy, err := snapshotpkg.NewFallbackPolicy(
		snapshotpkg.FallbackMissing,
		snapshotpkg.FallbackCorrupt,
		snapshotpkg.FallbackIncompatible,
	)
	if err != nil {
		t.Fatal(err)
	}

	return managerFromConfig(
		t,
		managerConfig(repository, store, codec, policy),
	)
}

func managerConfig(
	repository snapshotpkg.AggregateRepository[string, *managerAccount],
	store eventsourcing.SnapshotStore,
	codec snapshotpkg.StateCodec[*managerAccount],
	policy snapshotpkg.FallbackPolicy,
) snapshotpkg.ManagerConfig[string, *managerAccount] {
	clock, _ := eventsourcing.NewFixedClock(managerTime())

	return snapshotpkg.ManagerConfig[string, *managerAccount]{
		AggregateType: "account",
		EncodeID: func(id string) (string, error) {
			if id == "" {
				return "", eventsourcing.ErrInvalidArgument
			}

			return id, nil
		},
		Identify: func(account *managerAccount) string {
			return account.id
		},
		Lifecycle: func(account *managerAccount) *eventsourcing.Lifecycle {
			return &account.lifecycle
		},
		Repository: repository,
		Store:      store,
		Codec:      codec,
		Clock:      clock,
		Metadata: func(*managerAccount) (map[string]string, error) {
			return map[string]string{"codec": "plain"}, nil
		},
		Fallback: policy,
	}
}

func managerFromConfig(
	t *testing.T,
	config snapshotpkg.ManagerConfig[string, *managerAccount],
) *snapshotpkg.Manager[string, *managerAccount] {
	t.Helper()

	manager, err := snapshotpkg.NewManager(config)
	if err != nil {
		t.Fatal(err)
	}

	return manager
}

func managerRestore(
	_ context.Context,
	_ string,
	version uint64,
	restore eventsourcing.AggregateRestorer[*managerAccount],
) (*managerAccount, error) {
	account, err := restore()
	if err != nil {
		return nil, err
	}
	if err := account.lifecycle.RestoreSnapshotVersion(version); err != nil {
		return nil, err
	}

	return account, nil
}

type managerRepository struct {
	load    func(context.Context, string) (*managerAccount, error)
	restore func(
		context.Context,
		string,
		uint64,
		eventsourcing.AggregateRestorer[*managerAccount],
	) (*managerAccount, error)
}

func (repository *managerRepository) Load(
	ctx context.Context,
	id string,
) (*managerAccount, error) {
	return repository.load(ctx, id)
}

func (repository *managerRepository) Restore(
	ctx context.Context,
	id string,
	version uint64,
	restore eventsourcing.AggregateRestorer[*managerAccount],
) (*managerAccount, error) {
	return repository.restore(ctx, id, version, restore)
}

type managerCodec struct {
	schema eventsourcing.SchemaVersion
	encode func(*managerAccount) ([]byte, error)
	decode func([]byte, map[string]string) (*managerAccount, error)
}

func (codec managerCodec) SchemaVersion() eventsourcing.SchemaVersion {
	return codec.schema
}

func (codec managerCodec) Encode(account *managerAccount) ([]byte, error) {
	if codec.encode != nil {
		return codec.encode(account)
	}

	return []byte(account.owner), nil
}

func (codec managerCodec) Decode(
	state []byte,
	metadata map[string]string,
) (*managerAccount, error) {
	if codec.decode != nil {
		return codec.decode(state, metadata)
	}

	return &managerAccount{id: "account-42", owner: string(state)}, nil
}

func managerSnapshot(
	t *testing.T,
	aggregateVersion uint64,
	schemaVersion eventsourcing.SchemaVersion,
	state string,
) eventsourcing.Snapshot {
	t.Helper()

	snapshot, err := eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
		Stream:           managerStream(t),
		AggregateVersion: aggregateVersion,
		SchemaVersion:    schemaVersion,
		State:            []byte(state),
		Metadata:         map[string]string{"codec": "plain"},
		CreatedAt:        managerTime(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return snapshot
}

func snapshotStoreWith(
	t *testing.T,
	snapshot eventsourcing.Snapshot,
) eventsourcing.SnapshotStore {
	t.Helper()

	store := memory.NewSnapshotStore()
	if err := store.Save(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}

	return store
}

func managerStream(t *testing.T) eventsourcing.StreamID {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}

	return stream
}

func managerTime() time.Time {
	return time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
}

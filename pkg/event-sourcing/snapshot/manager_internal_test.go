package snapshot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

var errManagerTest = errors.New("manager test failure")

type internalAggregate struct {
	id        string
	state     string
	lifecycle eventsourcing.Lifecycle
}

type internalRepository struct {
	load    func(context.Context, string) (*internalAggregate, error)
	restore func(
		context.Context,
		string,
		uint64,
		eventsourcing.AggregateRestorer[*internalAggregate],
	) (*internalAggregate, error)
}

func (repository internalRepository) Load(
	ctx context.Context,
	id string,
) (*internalAggregate, error) {
	return repository.load(ctx, id)
}

func (repository internalRepository) Restore(
	ctx context.Context,
	id string,
	version uint64,
	restore eventsourcing.AggregateRestorer[*internalAggregate],
) (*internalAggregate, error) {
	return repository.restore(ctx, id, version, restore)
}

type internalStore struct {
	load func(context.Context, eventsourcing.StreamID) (
		eventsourcing.Snapshot,
		error,
	)
	save func(context.Context, eventsourcing.Snapshot) error
}

func (store internalStore) Load(
	ctx context.Context,
	stream eventsourcing.StreamID,
) (eventsourcing.Snapshot, error) {
	return store.load(ctx, stream)
}

func (store internalStore) Save(
	ctx context.Context,
	snapshot eventsourcing.Snapshot,
) error {
	return store.save(ctx, snapshot)
}

func (internalStore) Delete(
	context.Context,
	eventsourcing.StreamID,
) error {
	return nil
}

type internalCodec struct {
	schema eventsourcing.SchemaVersion
	encode func(*internalAggregate) ([]byte, error)
	decode func([]byte, map[string]string) (*internalAggregate, error)
}

func (codec internalCodec) SchemaVersion() eventsourcing.SchemaVersion {
	return codec.schema
}

func (codec internalCodec) Encode(
	aggregate *internalAggregate,
) ([]byte, error) {
	return codec.encode(aggregate)
}

func (codec internalCodec) Decode(
	state []byte,
	metadata map[string]string,
) (*internalAggregate, error) {
	return codec.decode(state, metadata)
}

func TestFallbackPolicyRejectsAmbiguousConfiguration(t *testing.T) {
	t.Parallel()

	tests := map[string][]FallbackKind{
		"empty":     nil,
		"unknown":   {99},
		"duplicate": {FallbackMissing, FallbackMissing},
	}
	for name, kinds := range tests {
		kinds := kinds
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			policy, err := NewFallbackPolicy(kinds...)
			if !errors.Is(err, eventsourcing.ErrInvalidArgument) ||
				policy.configured {
				t.Fatalf("NewFallbackPolicy() = %#v, %v", policy, err)
			}
		})
	}
}

func TestNewManagerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	valid := internalConfig()
	tests := map[string]func(*ManagerConfig[string, *internalAggregate]){
		"aggregate type": func(config *ManagerConfig[string, *internalAggregate]) {
			config.AggregateType = ""
		},
		"callback": func(config *ManagerConfig[string, *internalAggregate]) {
			config.EncodeID = nil
		},
		"dependency": func(config *ManagerConfig[string, *internalAggregate]) {
			config.Store = nil
		},
		"fallback": func(config *ManagerConfig[string, *internalAggregate]) {
			config.Fallback = FallbackPolicy{}
		},
		"schema": func(config *ManagerConfig[string, *internalAggregate]) {
			config.Codec = internalCodec{
				schema: 0,
				encode: valid.Codec.Encode,
				decode: valid.Codec.Decode,
			}
		},
	}
	for name, mutate := range tests {
		mutate := mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := internalConfig()
			mutate(&config)
			manager, err := NewManager(config)
			if !errors.Is(err, eventsourcing.ErrInvalidArgument) ||
				manager != nil {
				t.Fatalf("NewManager() = %#v, %v", manager, err)
			}
		})
	}
}

func TestManagerLoadRejectsInvalidInputsAndUnexpectedFailures(t *testing.T) {
	t.Parallel()

	manager := internalManager(t)
	var nilManager *Manager[string, *internalAggregate]
	var nilContext context.Context
	if aggregate, info, err := nilManager.Load(
		context.Background(),
		"id",
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) ||
		aggregate != nil ||
		info.Source() != LoadUnknown {
		t.Fatalf("nil Load() = %#v, %#v, %v", aggregate, info, err)
	}
	if aggregate, info, err := manager.Load(
		nilContext,
		"id",
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) ||
		aggregate != nil ||
		info.Source() != LoadUnknown {
		t.Fatalf("Load(nil) = %#v, %#v, %v", aggregate, info, err)
	}

	config := internalConfig()
	config.EncodeID = func(string) (string, error) {
		return "", errManagerTest
	}
	encodeManager := managerFromInternalConfig(t, config)
	if _, _, err := encodeManager.Load(
		context.Background(),
		"id",
	); !errors.Is(err, errManagerTest) {
		t.Fatalf("Load(encode failure) = %v", err)
	}

	config = internalConfig()
	config.Store = internalStore{
		load: func(
			context.Context,
			eventsourcing.StreamID,
		) (eventsourcing.Snapshot, error) {
			return eventsourcing.Snapshot{}, errManagerTest
		},
		save: func(context.Context, eventsourcing.Snapshot) error {
			return nil
		},
	}
	storeManager := managerFromInternalConfig(t, config)
	if _, _, err := storeManager.Load(
		context.Background(),
		"id",
	); !errors.Is(err, errManagerTest) {
		t.Fatalf("Load(store failure) = %v", err)
	}

	config.Store = storeReturning(
		eventsourcing.Snapshot{},
		eventsourcing.ErrSnapshotIncompatible,
	)
	config.Fallback = FailClosed()
	incompatibleManager := managerFromInternalConfig(t, config)
	if _, _, err := incompatibleManager.Load(
		context.Background(),
		"id",
	); !errors.Is(err, eventsourcing.ErrSnapshotIncompatible) {
		t.Fatalf("Load(incompatible store failure) = %v", err)
	}
}

func TestManagerLoadPreservesRepositoryFailures(t *testing.T) {
	t.Parallel()

	stored := internalSnapshot(t)
	config := internalConfig()
	config.Store = storeReturning(stored, nil)
	config.Repository = internalRepository{
		load: func(context.Context, string) (*internalAggregate, error) {
			return nil, errManagerTest
		},
		restore: func(
			context.Context,
			string,
			uint64,
			eventsourcing.AggregateRestorer[*internalAggregate],
		) (*internalAggregate, error) {
			return nil, errManagerTest
		},
	}
	manager := managerFromInternalConfig(t, config)
	if _, _, err := manager.Load(
		context.Background(),
		"id",
	); !errors.Is(err, errManagerTest) {
		t.Fatalf("Load(restore failure) = %v", err)
	}

	config.Store = storeReturning(
		eventsourcing.Snapshot{},
		eventsourcing.ErrSnapshotNotFound,
	)
	manager = managerFromInternalConfig(t, config)
	if _, _, err := manager.Load(
		context.Background(),
		"id",
	); !errors.Is(err, errManagerTest) {
		t.Fatalf("Load(fallback failure) = %v", err)
	}
}

func TestManagerLoadRejectsForeignSnapshotsAndContainsDecoderPanics(
	t *testing.T,
) {
	t.Parallel()

	foreignStream, err := eventsourcing.NewStreamID("account", "other")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
		Stream:           foreignStream,
		AggregateVersion: 3,
		SchemaVersion:    1,
		State:            []byte("foreign"),
		CreatedAt: time.Date(
			2026,
			time.July,
			25,
			13,
			0,
			0,
			0,
			time.UTC,
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	config := internalConfig()
	config.Store = storeReturning(foreign, nil)
	manager := managerFromInternalConfig(t, config)
	aggregate, info, err := manager.Load(context.Background(), "id")
	if err != nil ||
		aggregate.state != "history" ||
		!errors.Is(info.FallbackReason(), eventsourcing.ErrSnapshotCorrupt) {
		t.Fatalf("Load(foreign snapshot) = %#v, %#v, %v", aggregate, info, err)
	}

	config = internalConfig()
	config.Store = storeReturning(internalSnapshot(t), nil)
	codec := config.Codec.(internalCodec)
	codec.decode = func(
		[]byte,
		map[string]string,
	) (*internalAggregate, error) {
		panic("secret snapshot state")
	}
	config.Codec = codec
	manager = managerFromInternalConfig(t, config)
	aggregate, info, err = manager.Load(context.Background(), "id")
	if aggregate != nil ||
		info.Source() != LoadUnknown ||
		!errors.Is(err, ErrStateCodecPanic) ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("Load(decoder panic) = %#v, %#v, %v", aggregate, info, err)
	}
}

func TestManagerRefreshRejectsInvalidAggregateStates(t *testing.T) {
	t.Parallel()

	manager := internalManager(t)
	var nilManager *Manager[string, *internalAggregate]
	var nilContext context.Context
	if _, err := nilManager.Refresh(
		context.Background(),
		"id",
		&internalAggregate{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil Refresh() = %v", err)
	}
	if _, err := manager.Refresh(
		nilContext,
		"id",
		&internalAggregate{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Refresh(nil) = %v", err)
	}

	config := internalConfig()
	config.EncodeID = func(string) (string, error) {
		return "", errManagerTest
	}
	encodeManager := managerFromInternalConfig(t, config)
	if _, err := encodeManager.Refresh(
		context.Background(),
		"id",
		&internalAggregate{},
	); !errors.Is(err, errManagerTest) {
		t.Fatalf("Refresh(encode failure) = %v", err)
	}
	if _, err := manager.Refresh(
		context.Background(),
		"id",
		&internalAggregate{id: "other"},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Refresh(mismatched ID) = %v", err)
	}

	config = internalConfig()
	config.Lifecycle = func(*internalAggregate) *eventsourcing.Lifecycle {
		return nil
	}
	nilLifecycle := managerFromInternalConfig(t, config)
	if _, err := nilLifecycle.Refresh(
		context.Background(),
		"id",
		&internalAggregate{id: "id"},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Refresh(nil lifecycle) = %v", err)
	}

	if _, err := manager.Refresh(
		context.Background(),
		"id",
		&internalAggregate{id: "id"},
	); !errors.Is(err, eventsourcing.ErrInvalidLifecycleState) {
		t.Fatalf("Refresh(new aggregate) = %v", err)
	}

	pending := &internalAggregate{id: "id"}
	event, err := eventsourcing.NewDecodedEvent(
		eventsourcing.DecodedEventInput{
			Name:    "changed",
			Version: 1,
			Value:   "value",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := pending.lifecycle.Record(
		event,
		func(eventsourcing.DecodedEvent) error { return nil },
	); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(
		context.Background(),
		"id",
		pending,
	); !errors.Is(err, eventsourcing.ErrInvalidLifecycleState) {
		t.Fatalf("Refresh(pending aggregate) = %v", err)
	}

	poisoned := &internalAggregate{id: "id"}
	if err := poisoned.lifecycle.Record(
		event,
		func(eventsourcing.DecodedEvent) error { return errManagerTest },
	); !errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(
		context.Background(),
		"id",
		poisoned,
	); !errors.Is(err, eventsourcing.ErrLifecyclePoisoned) {
		t.Fatalf("Refresh(poisoned aggregate) = %v", err)
	}
}

func TestManagerRefreshRedactsApplicationFailures(t *testing.T) {
	t.Parallel()

	secretErr := errors.New("secret aggregate state")
	tests := map[string]func(*ManagerConfig[string, *internalAggregate]){
		"codec": func(config *ManagerConfig[string, *internalAggregate]) {
			codec := config.Codec.(internalCodec)
			codec.encode = func(*internalAggregate) ([]byte, error) {
				return nil, secretErr
			}
			config.Codec = codec
		},
		"metadata": func(config *ManagerConfig[string, *internalAggregate]) {
			config.Metadata = func(*internalAggregate) (map[string]string, error) {
				return nil, secretErr
			}
		},
	}
	for name, mutate := range tests {
		mutate := mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := internalConfig()
			mutate(&config)
			manager := managerFromInternalConfig(t, config)
			aggregate := persistedInternalAggregate(t)

			_, err := manager.Refresh(context.Background(), "id", aggregate)
			var creationErr *CreationError
			if !errors.Is(err, secretErr) ||
				!errors.As(err, &creationErr) ||
				strings.Contains(err.Error(), "secret") {
				t.Fatalf("Refresh() = %v", err)
			}
		})
	}
}

func TestManagerRefreshContainsApplicationPanics(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		mutate func(*ManagerConfig[string, *internalAggregate])
		cause  error
	}{
		"codec": {
			mutate: func(config *ManagerConfig[string, *internalAggregate]) {
				codec := config.Codec.(internalCodec)
				codec.encode = func(*internalAggregate) ([]byte, error) {
					panic("secret aggregate state")
				}
				config.Codec = codec
			},
			cause: ErrStateCodecPanic,
		},
		"metadata": {
			mutate: func(config *ManagerConfig[string, *internalAggregate]) {
				config.Metadata = func(
					*internalAggregate,
				) (map[string]string, error) {
					panic("secret aggregate metadata")
				}
			},
			cause: ErrMetadataProviderPanic,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			config := internalConfig()
			testCase.mutate(&config)
			manager := managerFromInternalConfig(t, config)
			_, err := manager.Refresh(
				context.Background(),
				"id",
				persistedInternalAggregate(t),
			)
			if !errors.Is(err, testCase.cause) ||
				strings.Contains(err.Error(), "secret") {
				t.Fatalf("Refresh() = %v", err)
			}
		})
	}
}

func TestManagerRefreshPreservesSnapshotAndStoreFailures(t *testing.T) {
	t.Parallel()

	config := internalConfig()
	codec := config.Codec.(internalCodec)
	codec.encode = func(*internalAggregate) ([]byte, error) {
		return nil, nil
	}
	config.Codec = codec
	manager := managerFromInternalConfig(t, config)
	if _, err := manager.Refresh(
		context.Background(),
		"id",
		persistedInternalAggregate(t),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Refresh(invalid snapshot) = %v", err)
	}

	config = internalConfig()
	config.Store = internalStore{
		load: func(
			context.Context,
			eventsourcing.StreamID,
		) (eventsourcing.Snapshot, error) {
			return eventsourcing.Snapshot{}, eventsourcing.ErrSnapshotNotFound
		},
		save: func(context.Context, eventsourcing.Snapshot) error {
			return errManagerTest
		},
	}
	manager = managerFromInternalConfig(t, config)
	if _, err := manager.Refresh(
		context.Background(),
		"id",
		persistedInternalAggregate(t),
	); !errors.Is(err, errManagerTest) {
		t.Fatalf("Refresh(store failure) = %v", err)
	}
}

func TestManagerRefreshAllowsEmptyMetadataProvider(t *testing.T) {
	t.Parallel()

	config := internalConfig()
	config.Metadata = nil
	manager := managerFromInternalConfig(t, config)
	created, err := manager.Refresh(
		context.Background(),
		"id",
		persistedInternalAggregate(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Metadata()) != 0 {
		t.Fatalf("Metadata() = %#v", created.Metadata())
	}
}

func TestFallbackHelpersRejectUnclassifiedErrors(t *testing.T) {
	t.Parallel()

	policy, err := NewFallbackPolicy(FallbackMissing)
	if err != nil {
		t.Fatal(err)
	}
	if policy.allows(errManagerTest) {
		t.Fatal("unclassified error was allowed")
	}
	if bit, ok := fallbackBit(99); bit != 0 || ok {
		t.Fatalf("fallbackBit(99) = %d, %t", bit, ok)
	}
	if reason := snapshotReason(errManagerTest); reason != nil {
		t.Fatalf("snapshotReason() = %v", reason)
	}
}

func internalConfig() ManagerConfig[string, *internalAggregate] {
	policy, _ := NewFallbackPolicy(
		FallbackMissing,
		FallbackCorrupt,
		FallbackIncompatible,
	)
	clock, _ := eventsourcing.NewFixedClock(
		time.Date(2026, time.July, 25, 13, 0, 0, 0, time.UTC),
	)

	return ManagerConfig[string, *internalAggregate]{
		AggregateType: "account",
		EncodeID: func(id string) (string, error) {
			return id, nil
		},
		Identify: func(aggregate *internalAggregate) string {
			return aggregate.id
		},
		Lifecycle: func(aggregate *internalAggregate) *eventsourcing.Lifecycle {
			return &aggregate.lifecycle
		},
		Repository: internalRepository{
			load: func(
				context.Context,
				string,
			) (*internalAggregate, error) {
				return &internalAggregate{id: "id", state: "history"}, nil
			},
			restore: func(
				_ context.Context,
				_ string,
				version uint64,
				restore eventsourcing.AggregateRestorer[*internalAggregate],
			) (*internalAggregate, error) {
				aggregate, err := restore()
				if err != nil {
					return nil, err
				}
				if err := aggregate.lifecycle.RestoreSnapshotVersion(
					version,
				); err != nil {
					return nil, err
				}

				return aggregate, nil
			},
		},
		Store: storeReturning(
			eventsourcing.Snapshot{},
			eventsourcing.ErrSnapshotNotFound,
		),
		Codec: internalCodec{
			schema: 1,
			encode: func(aggregate *internalAggregate) ([]byte, error) {
				return []byte(aggregate.state), nil
			},
			decode: func(
				state []byte,
				_ map[string]string,
			) (*internalAggregate, error) {
				return &internalAggregate{
					id:    "id",
					state: string(state),
				}, nil
			},
		},
		Clock: clock,
		Metadata: func(*internalAggregate) (map[string]string, error) {
			return map[string]string{"codec": "internal"}, nil
		},
		Fallback: policy,
	}
}

func internalManager(t *testing.T) *Manager[string, *internalAggregate] {
	t.Helper()

	return managerFromInternalConfig(t, internalConfig())
}

func managerFromInternalConfig(
	t *testing.T,
	config ManagerConfig[string, *internalAggregate],
) *Manager[string, *internalAggregate] {
	t.Helper()

	manager, err := NewManager(config)
	if err != nil {
		t.Fatal(err)
	}

	return manager
}

func persistedInternalAggregate(t *testing.T) *internalAggregate {
	t.Helper()

	aggregate := &internalAggregate{id: "id", state: "saved"}
	if err := aggregate.lifecycle.RestoreSnapshotVersion(3); err != nil {
		t.Fatal(err)
	}

	return aggregate
}

func internalSnapshot(t *testing.T) eventsourcing.Snapshot {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "id")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
		Stream:           stream,
		AggregateVersion: 3,
		SchemaVersion:    1,
		State:            []byte("saved"),
		CreatedAt: time.Date(
			2026,
			time.July,
			25,
			13,
			0,
			0,
			0,
			time.UTC,
		),
	})
	if err != nil {
		t.Fatal(err)
	}

	return snapshot
}

func storeReturning(
	snapshot eventsourcing.Snapshot,
	err error,
) internalStore {
	return internalStore{
		load: func(
			context.Context,
			eventsourcing.StreamID,
		) (eventsourcing.Snapshot, error) {
			return snapshot, err
		},
		save: func(context.Context, eventsourcing.Snapshot) error {
			return nil
		},
	}
}

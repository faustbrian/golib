package verkletree

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
)

func TestStorageFacadeValuesAndErrorsFailClosed(t *testing.T) {
	t.Parallel()

	id := NodeID{1, 2, 3}
	if id.Bytes() != [NodeIDSize]byte(id) {
		t.Fatalf("NodeID.Bytes() = %x", id.Bytes())
	}
	capabilityErr := &StoreCapabilityError{
		Required:  RequiredWriteStoreCapabilities,
		Available: StoreCapabilityAtomicCommit,
		Missing:   StoreCapabilityImmutableNodes,
	}
	if capabilityErr.Error() == "" ||
		!errors.Is(capabilityErr, ErrStoreCapability) {
		t.Fatalf("capability error = %v", capabilityErr)
	}

	var zero StoreCommit
	if _, _, err := zero.PreviousRoot(); !errors.Is(err, ErrStorageCommit) {
		t.Fatalf("zero PreviousRoot() error = %v", err)
	}
	if _, err := zero.Root(); !errors.Is(err, ErrStorageCommit) {
		t.Fatalf("zero Root() error = %v", err)
	}
	if _, err := zero.RootNode(); !errors.Is(err, ErrStorageCommit) {
		t.Fatalf("zero RootNode() error = %v", err)
	}
	if _, err := zero.Nodes(context.Background()); !errors.Is(err, ErrStorageCommit) {
		t.Fatalf("zero Nodes() error = %v", err)
	}

	snapshot := testStorageFacadeSnapshot(t)
	store := &internalCaptureStore{
		capabilities: RequiredWriteStoreCapabilities,
	}
	if err := snapshot.Commit(
		context.Background(),
		store,
		nil,
		testStorageFacadeLimits(),
	); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	var nilContext context.Context
	if _, err := store.commit.Nodes(nilContext); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("nil-context Nodes() error = %v", err)
	}
	if _, err := store.commit.Nodes(&cancellingContext{remaining: 1}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("mid-copy Nodes() error = %v", err)
	}

	invalidRoot := store.commit
	invalidRoot.root = Root{}
	if _, err := invalidRoot.Root(); !errors.Is(err, ErrStorageCommit) {
		t.Fatalf("invalid-root Root() error = %v", err)
	}
	invalidPrevious := store.commit
	invalidPrevious.hasPrevious = true
	invalidPrevious.previous = Root{}
	if _, _, err := invalidPrevious.PreviousRoot(); !errors.Is(err, ErrStorageCommit) {
		t.Fatalf("invalid-previous PreviousRoot() error = %v", err)
	}
	invalidFlag := store.commit
	invalidFlag.valid = false
	if _, err := invalidFlag.Root(); !errors.Is(err, ErrStorageCommit) {
		t.Fatalf("invalid-flag Root() error = %v", err)
	}
	missingNodes := store.commit
	missingNodes.nodes = nil
	if _, err := missingNodes.Root(); !errors.Is(err, ErrStorageCommit) {
		t.Fatalf("missing-nodes Root() error = %v", err)
	}
}

func TestStorageLimitsRejectEachInvalidFieldAndAcceptBoundaries(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*StorageLimits){
		"nodes zero": func(limits *StorageLimits) {
			limits.MaxNodes = 0
		},
		"nodes excessive": func(limits *StorageLimits) {
			limits.MaxNodes = maxPublicCount + 1
		},
		"node bytes zero": func(limits *StorageLimits) {
			limits.MaxNodeBytes = 0
		},
		"encoded bytes zero": func(limits *StorageLimits) {
			limits.MaxEncodedBytes = 0
		},
		"hashes zero": func(limits *StorageLimits) {
			limits.MaxHashes = 0
		},
		"hashes excessive": func(limits *StorageLimits) {
			limits.MaxHashes = maxPublicCount + 1
		},
		"temporary bytes zero": func(limits *StorageLimits) {
			limits.MaxTemporaryBytes = 0
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			limits := testStorageFacadeLimits()
			mutate(&limits)
			if err := limits.validate(); !errors.Is(err, ErrInvalidLimits) {
				t.Fatalf("validate() error = %v, want ErrInvalidLimits", err)
			}
		})
	}

	boundary := StorageLimits{
		MaxNodes:          maxPublicCount,
		MaxNodeBytes:      1,
		MaxEncodedBytes:   1,
		MaxHashes:         maxPublicCount,
		MaxTemporaryBytes: 1,
	}
	if err := boundary.validate(); err != nil {
		t.Fatalf("boundary validate() error = %v", err)
	}
}

func TestSnapshotCommitTranslatesEveryStorageResource(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	store := &internalCaptureStore{
		capabilities: RequiredWriteStoreCapabilities,
	}
	tests := map[string]struct {
		resource Resource
		mutate   func(*StorageLimits)
	}{
		"nodes": {
			resource: ResourceNodes,
			mutate: func(limits *StorageLimits) {
				limits.MaxNodes = 1
			},
		},
		"node bytes": {
			resource: ResourceNodeBytes,
			mutate: func(limits *StorageLimits) {
				limits.MaxNodeBytes = 44
			},
		},
		"encoded bytes": {
			resource: ResourceEncodedNodeBytes,
			mutate: func(limits *StorageLimits) {
				limits.MaxEncodedBytes = 44
			},
		},
		"hashes": {
			resource: ResourceNodeHashes,
			mutate: func(limits *StorageLimits) {
				limits.MaxHashes = 1
			},
		},
		"temporary bytes": {
			resource: ResourceTemporaryBytes,
			mutate: func(limits *StorageLimits) {
				limits.MaxTemporaryBytes = 44
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			limits := testStorageFacadeLimits()
			test.mutate(&limits)
			err := snapshot.Commit(context.Background(), store, nil, limits)
			var resourceErr *ResourceError
			if !errors.As(err, &resourceErr) ||
				resourceErr.Resource != test.resource {
				t.Fatalf("Commit() error = %v, want resource %d", err, test.resource)
			}
		})
	}

	cancelled := translateStorageEncodingError(context.Canceled)
	if !errors.Is(cancelled, ErrCancelled) ||
		!errors.Is(cancelled, context.Canceled) {
		t.Fatalf("cancelled translation = %v", cancelled)
	}
	if err := translateStorageEncodingError(errors.New("corrupt")); !errors.Is(err, ErrCryptographic) {
		t.Fatalf("fallback translation = %v", err)
	}
}

func TestSnapshotCommitRejectsInvalidPreviousAndAcceptsValueStore(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	store := internalValueStore{}
	zeroRoot := Root{}
	if err := snapshot.Commit(
		context.Background(),
		store,
		&zeroRoot,
		testStorageFacadeLimits(),
	); !errors.Is(err, ErrInvalidRoot) {
		t.Fatalf("invalid previous root error = %v", err)
	}
	if !validNodeStore(store) {
		t.Fatal("value store rejected")
	}
	if validNodeStore(nil) {
		t.Fatal("nil store accepted")
	}

	corrupt := Snapshot{valid: true}
	if err := corrupt.Commit(
		context.Background(),
		store,
		nil,
		testStorageFacadeLimits(),
	); !errors.Is(err, ErrCryptographic) {
		t.Fatalf("corrupt snapshot error = %v", err)
	}
}

func TestSnapshotCommitRemainsCancellableAcrossEncodingAndCopying(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	for remaining := 0; remaining <= 160; remaining++ {
		store := &internalCaptureStore{
			capabilities: RequiredWriteStoreCapabilities,
		}
		err := snapshot.Commit(
			&cancellingContext{remaining: remaining},
			store,
			nil,
			testStorageFacadeLimits(),
		)
		if err != nil && !errors.Is(err, ErrCancelled) {
			t.Fatalf("remaining %d error = %v", remaining, err)
		}
	}
}

type internalCaptureStore struct {
	capabilities StoreCapabilities
	commit       StoreCommit
}

func (store *internalCaptureStore) Capabilities() StoreCapabilities {
	return store.capabilities
}

func (store *internalCaptureStore) CommitSnapshot(
	_ context.Context,
	commit StoreCommit,
) error {
	store.commit = commit

	return nil
}

type internalValueStore struct{}

func (internalValueStore) Capabilities() StoreCapabilities {
	return RequiredWriteStoreCapabilities
}

func (internalValueStore) CommitSnapshot(
	context.Context,
	StoreCommit,
) error {
	return nil
}

func testStorageFacadeSnapshot(t testing.TB) Snapshot {
	t.Helper()

	var key Key
	key[0] = 1
	key[31] = 1
	snapshot, err := NewSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		[]Entry{{Key: key, Value: Value{1}}},
		testFacadeSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	return snapshot
}

func testStorageFacadeLimits() StorageLimits {
	return StorageLimits{
		MaxNodes:          64,
		MaxNodeBytes:      1 << 20,
		MaxEncodedBytes:   1 << 20,
		MaxHashes:         64,
		MaxTemporaryBytes: 2 << 20,
	}
}

func TestTranslateStorageEncodingErrorDefaultResource(t *testing.T) {
	t.Parallel()

	err := translateStorageEncodingError(
		&committedtree.StorageEncodingResourceError{
			Resource: 0xff,
			Limit:    2,
			Actual:   3,
		},
	)
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != ResourceTemporaryBytes {
		t.Fatalf("translation = %v", err)
	}
}

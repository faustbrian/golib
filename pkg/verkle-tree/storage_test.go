package verkletree_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"testing"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

func TestSnapshotCommitPublishesCompleteOwnedCanonicalBatch(t *testing.T) {
	t.Parallel()

	snapshot := mustPublicSnapshot(t, []verkletree.Entry{
		{Key: publicKey(2, 129), Value: publicValue(3)},
		{Key: publicKey(1, 0), Value: publicValue(1)},
		{Key: publicKey(1, 128), Value: publicValue(2)},
	})
	previousSnapshot := mustPublicSnapshot(t, nil)
	previous, err := previousSnapshot.Root()
	if err != nil {
		t.Fatalf("previous Root() error = %v", err)
	}
	store := newCaptureNodeStore()

	if err := snapshot.Commit(
		context.Background(),
		store,
		&previous,
		testPublicStorageLimits(),
	); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", store.calls)
	}
	published, err := store.commit.Root()
	if err != nil {
		t.Fatalf("commit Root() error = %v", err)
	}
	wantRoot, _ := snapshot.Root()
	if !equalPublicRoots(t, published, wantRoot) {
		t.Fatal("published root differs from snapshot root")
	}
	gotPrevious, present, err := store.commit.PreviousRoot()
	if err != nil || !present || !equalPublicRoots(t, gotPrevious, previous) {
		t.Fatalf("PreviousRoot() = (%v, %t, %v)", gotPrevious, present, err)
	}
	rootNode, err := store.commit.RootNode()
	if err != nil {
		t.Fatalf("RootNode() error = %v", err)
	}
	nodes, err := store.commit.Nodes(context.Background())
	if err != nil {
		t.Fatalf("Nodes() error = %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("stored node count = %d, want 3", len(nodes))
	}
	rootPresent := false
	for index := range nodes {
		encoded := nodes[index].Encoded()
		if got, want := nodes[index].ID(), verkletree.NodeID(sha256.Sum256(encoded)); got != want {
			t.Fatalf("node %d ID = %x, want %x", index, got, want)
		}
		if nodes[index].ID() == rootNode {
			rootPresent = true
		}
		encoded[0] ^= 0xff
		if slices.Equal(encoded, nodes[index].Encoded()) {
			t.Fatalf("node %d encoding aliases commit storage", index)
		}
	}
	if !rootPresent {
		t.Fatalf("root node %x absent from commit", rootNode)
	}
}

func TestSnapshotCommitWithoutPreviousRootPreservesExpectation(t *testing.T) {
	t.Parallel()

	snapshot := mustPublicSnapshot(t, nil)
	store := newCaptureNodeStore()
	if err := snapshot.Commit(
		context.Background(),
		store,
		nil,
		testPublicStorageLimits(),
	); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if _, present, err := store.commit.PreviousRoot(); err != nil || present {
		t.Fatalf("PreviousRoot() present = %t, error = %v", present, err)
	}
}

func TestSnapshotCommitRejectsInvalidInputsAndCapabilitiesBeforeStoreCall(t *testing.T) {
	t.Parallel()

	snapshot := mustPublicSnapshot(t, nil)
	store := newCaptureNodeStore()
	var nilStore *captureNodeStore
	var nilContext context.Context

	tests := map[string]struct {
		snapshot verkletree.Snapshot
		ctx      context.Context
		store    verkletree.NodeStore
		previous *verkletree.Root
		limits   verkletree.StorageLimits
		want     error
	}{
		"zero snapshot": {
			ctx:    context.Background(),
			store:  store,
			limits: testPublicStorageLimits(),
			want:   verkletree.ErrInvalidSnapshot,
		},
		"nil context": {
			snapshot: snapshot,
			ctx:      nilContext,
			store:    store,
			limits:   testPublicStorageLimits(),
			want:     verkletree.ErrInvalidContext,
		},
		"typed nil store": {
			snapshot: snapshot,
			ctx:      context.Background(),
			store:    nilStore,
			limits:   testPublicStorageLimits(),
			want:     verkletree.ErrInvalidStore,
		},
		"invalid previous root": {
			snapshot: snapshot,
			ctx:      context.Background(),
			store:    store,
			previous: &verkletree.Root{},
			limits:   testPublicStorageLimits(),
			want:     verkletree.ErrInvalidRoot,
		},
		"invalid limits": {
			snapshot: snapshot,
			ctx:      context.Background(),
			store:    store,
			want:     verkletree.ErrInvalidLimits,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			before := store.calls
			beforeCapabilities := store.capabilityCalls
			err := test.snapshot.Commit(
				test.ctx,
				test.store,
				test.previous,
				test.limits,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("Commit() error = %v, want %v", err, test.want)
			}
			if store.calls != before {
				t.Fatalf("store calls changed from %d to %d", before, store.calls)
			}
			if store.capabilityCalls != beforeCapabilities {
				t.Fatalf(
					"capability calls changed from %d to %d",
					beforeCapabilities,
					store.capabilityCalls,
				)
			}
		})
	}

	required := verkletree.RequiredWriteStoreCapabilities
	for _, missing := range []verkletree.StoreCapabilities{
		verkletree.StoreCapabilityImmutableNodes,
		verkletree.StoreCapabilityAtomicCommit,
		verkletree.StoreCapabilityDurablePublication,
		verkletree.StoreCapabilityCompareAndSwap,
	} {
		t.Run("missing capability", func(t *testing.T) {
			store := newCaptureNodeStore()
			store.capabilities = required &^ missing
			err := snapshot.Commit(
				context.Background(),
				store,
				nil,
				testPublicStorageLimits(),
			)
			var capabilityErr *verkletree.StoreCapabilityError
			if !errors.Is(err, verkletree.ErrStoreCapability) ||
				!errors.As(err, &capabilityErr) ||
				capabilityErr.Missing != missing {
				t.Fatalf("Commit() error = %v, want missing %d", err, missing)
			}
			if store.calls != 0 {
				t.Fatalf("store calls = %d, want 0", store.calls)
			}
		})
	}
}

func TestSnapshotCommitWrapsStoreFailureAndPreservesSnapshot(t *testing.T) {
	t.Parallel()

	snapshot := mustPublicSnapshot(t, []verkletree.Entry{{
		Key: publicKey(1, 1), Value: publicValue(1),
	}})
	store := newCaptureNodeStore()
	store.err = verkletree.ErrStaleRoot
	before, _ := snapshot.Root()

	err := snapshot.Commit(
		context.Background(),
		store,
		nil,
		testPublicStorageLimits(),
	)
	if !errors.Is(err, verkletree.ErrStorageCommit) ||
		!errors.Is(err, verkletree.ErrStaleRoot) {
		t.Fatalf("Commit() error = %v", err)
	}
	after, rootErr := snapshot.Root()
	if rootErr != nil || !equalPublicRoots(t, before, after) {
		t.Fatalf("snapshot root changed after failed commit: %v", rootErr)
	}
}

type captureNodeStore struct {
	capabilities    verkletree.StoreCapabilities
	commit          verkletree.StoreCommit
	err             error
	calls           int
	capabilityCalls int
}

func newCaptureNodeStore() *captureNodeStore {
	return &captureNodeStore{
		capabilities: verkletree.RequiredWriteStoreCapabilities,
	}
}

func (store *captureNodeStore) Capabilities() verkletree.StoreCapabilities {
	store.capabilityCalls++
	return store.capabilities
}

func (store *captureNodeStore) CommitSnapshot(
	_ context.Context,
	commit verkletree.StoreCommit,
) error {
	store.calls++
	if store.err != nil {
		return store.err
	}
	store.commit = commit

	return nil
}

func testPublicStorageLimits() verkletree.StorageLimits {
	return verkletree.StorageLimits{
		MaxNodes:          64,
		MaxNodeBytes:      1 << 20,
		MaxEncodedBytes:   1 << 20,
		MaxHashes:         64,
		MaxTemporaryBytes: 2 << 20,
	}
}

func mustPublicSnapshot(
	t testing.TB,
	entries []verkletree.Entry,
) verkletree.Snapshot {
	t.Helper()

	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		entries,
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	return snapshot
}

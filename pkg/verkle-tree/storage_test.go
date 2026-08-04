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

func TestLoadSnapshotReconstructsPublishedCanonicalState(t *testing.T) {
	t.Parallel()

	want := mustPublicSnapshot(t, []verkletree.Entry{
		{Key: publicKey(2, 129), Value: publicValue(3)},
		{Key: publicKey(1, 0), Value: verkletree.Value{}},
		{Key: publicKey(1, 128), Value: publicValue(2)},
	})
	store := newCaptureNodeStore()
	store.capabilities |= verkletree.RequiredReadStoreCapabilities
	if err := want.Commit(
		context.Background(),
		store,
		nil,
		testPublicStorageLimits(),
	); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	got, err := verkletree.LoadSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		store,
		testPublicStorageReadLimits(),
	)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	wantRoot, _ := want.Root()
	gotRoot, rootErr := got.Root()
	if rootErr != nil || !equalPublicRoots(t, gotRoot, wantRoot) {
		t.Fatalf("loaded root differs: %v", rootErr)
	}
	zero, present, getErr := got.Get(context.Background(), publicKey(1, 0))
	if getErr != nil || !present || zero != (verkletree.Value{}) {
		t.Fatalf("zero value = (%x, %t, %v)", zero, present, getErr)
	}
	if store.openCalls != 1 || store.readCalls == 0 || store.closeCalls != 1 {
		t.Fatalf(
			"store calls open=%d read=%d close=%d",
			store.openCalls,
			store.readCalls,
			store.closeCalls,
		)
	}
	if store.maxReadBytes != testPublicStorageReadLimits().MaxNodeBytes {
		t.Fatalf("read byte bound = %d", store.maxReadBytes)
	}
}

func TestLoadSnapshotReconstructsEmptyRootWithoutPointDecoding(t *testing.T) {
	t.Parallel()

	want := mustPublicSnapshot(t, nil)
	store := newCaptureNodeStore()
	store.capabilities |= verkletree.RequiredReadStoreCapabilities
	if err := want.Commit(
		context.Background(),
		store,
		nil,
		testPublicStorageLimits(),
	); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	limits := testPublicStorageReadLimits()
	limits.MaxPointDecodes = 0
	got, err := verkletree.LoadSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		store,
		limits,
	)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	root, err := got.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	empty, err := root.IsEmpty()
	if err != nil || !empty {
		t.Fatalf("IsEmpty() = (%t, %v)", empty, err)
	}
}

func TestLoadSnapshotDistinguishesMissingCorruptAndStoreFailures(t *testing.T) {
	t.Parallel()

	want := mustPublicSnapshot(t, []verkletree.Entry{{
		Key: publicKey(1, 1), Value: publicValue(1),
	}})
	makeStore := func(t testing.TB) *captureNodeStore {
		t.Helper()
		store := newCaptureNodeStore()
		store.capabilities |= verkletree.RequiredReadStoreCapabilities
		if err := want.Commit(
			context.Background(),
			store,
			nil,
			testPublicStorageLimits(),
		); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}
		return store
	}

	t.Run("missing publication", func(t *testing.T) {
		store := makeStore(t)
		store.openErr = verkletree.ErrStorageSnapshotMissing
		_, err := verkletree.LoadSnapshot(
			context.Background(),
			verkletree.BandersnatchIPA256V0(),
			store,
			testPublicStorageReadLimits(),
		)
		if !errors.Is(err, verkletree.ErrStorageSnapshotMissing) ||
			!errors.Is(err, verkletree.ErrStorageRead) {
			t.Fatalf("LoadSnapshot() error = %v", err)
		}
	})

	t.Run("missing node", func(t *testing.T) {
		store := makeStore(t)
		store.missing = true
		_, err := verkletree.LoadSnapshot(
			context.Background(),
			verkletree.BandersnatchIPA256V0(),
			store,
			testPublicStorageReadLimits(),
		)
		if !errors.Is(err, verkletree.ErrStorageNodeMissing) ||
			!errors.Is(err, verkletree.ErrStorageRead) {
			t.Fatalf("LoadSnapshot() error = %v", err)
		}
	})

	t.Run("corrupt node", func(t *testing.T) {
		store := makeStore(t)
		store.corrupt = true
		_, err := verkletree.LoadSnapshot(
			context.Background(),
			verkletree.BandersnatchIPA256V0(),
			store,
			testPublicStorageReadLimits(),
		)
		if !errors.Is(err, verkletree.ErrStorageNodeCorrupt) {
			t.Fatalf("LoadSnapshot() error = %v", err)
		}
	})

	t.Run("reader failure", func(t *testing.T) {
		store := makeStore(t)
		store.readErr = errors.New("reader unavailable")
		_, err := verkletree.LoadSnapshot(
			context.Background(),
			verkletree.BandersnatchIPA256V0(),
			store,
			testPublicStorageReadLimits(),
		)
		if !errors.Is(err, verkletree.ErrStorageRead) ||
			errors.Is(err, verkletree.ErrStorageNodeMissing) {
			t.Fatalf("LoadSnapshot() error = %v", err)
		}
	})

	t.Run("close failure is atomic", func(t *testing.T) {
		store := makeStore(t)
		store.closeErr = errors.New("close failed")
		loaded, err := verkletree.LoadSnapshot(
			context.Background(),
			verkletree.BandersnatchIPA256V0(),
			store,
			testPublicStorageReadLimits(),
		)
		_, rootErr := loaded.Root()
		if !errors.Is(err, verkletree.ErrStorageRead) ||
			!errors.Is(rootErr, verkletree.ErrInvalidSnapshot) {
			t.Fatalf("LoadSnapshot() errors = (%v, %v)", err, rootErr)
		}
	})
}

type captureNodeStore struct {
	capabilities    verkletree.StoreCapabilities
	commit          verkletree.StoreCommit
	publication     verkletree.StorePublication
	nodes           map[verkletree.NodeID][]byte
	err             error
	openErr         error
	readErr         error
	closeErr        error
	missing         bool
	corrupt         bool
	calls           int
	capabilityCalls int
	openCalls       int
	readCalls       int
	closeCalls      int
	maxReadBytes    uint64
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
	publication, err := commit.Publication()
	if err != nil {
		return err
	}
	nodes, err := commit.Nodes(context.Background())
	if err != nil {
		return err
	}
	store.publication = publication
	store.nodes = make(map[verkletree.NodeID][]byte, len(nodes))
	for _, node := range nodes {
		store.nodes[node.ID()] = node.Encoded()
	}

	return nil
}

func (store *captureNodeStore) OpenSnapshot(
	_ context.Context,
) (verkletree.NodeReadSnapshot, error) {
	store.openCalls++
	if store.openErr != nil {
		return nil, store.openErr
	}

	return &captureNodeReadSnapshot{store: store}, nil
}

type captureNodeReadSnapshot struct {
	store *captureNodeStore
}

func (snapshot *captureNodeReadSnapshot) Publication(
	ctx context.Context,
) (verkletree.StorePublication, error) {
	if err := ctx.Err(); err != nil {
		return verkletree.StorePublication{}, err
	}

	return snapshot.store.publication, nil
}

func (snapshot *captureNodeReadSnapshot) ReadNode(
	_ context.Context,
	id verkletree.NodeID,
	maxBytes uint64,
) ([]byte, error) {
	snapshot.store.readCalls++
	snapshot.store.maxReadBytes = maxBytes
	if snapshot.store.readErr != nil {
		return nil, snapshot.store.readErr
	}
	if snapshot.store.missing {
		return nil, verkletree.ErrStorageNodeMissing
	}
	encoded, present := snapshot.store.nodes[id]
	if !present {
		return nil, verkletree.ErrStorageNodeMissing
	}
	owned := append([]byte(nil), encoded...)
	if snapshot.store.corrupt {
		owned[len(owned)-1] ^= 0xff
	}

	return owned, nil
}

func (snapshot *captureNodeReadSnapshot) Close(context.Context) error {
	snapshot.store.closeCalls++
	return snapshot.store.closeErr
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

func testPublicStorageReadLimits() verkletree.StorageReadLimits {
	return verkletree.StorageReadLimits{
		MaxEntries:        64,
		MaxNodes:          64,
		MaxEdges:          64,
		MaxNodeReads:      64,
		MaxNodeBytes:      1 << 20,
		MaxEncodedBytes:   2 << 20,
		MaxHashes:         128,
		MaxPointDecodes:   192,
		MaxTemporaryBytes: 4 << 20,
		Snapshot:          publicSnapshotLimits(),
	}
}

func mustPublicSnapshot(
	t testing.TB,
	entries []verkletree.Entry,
) verkletree.Snapshot {
	t.Helper()

	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		entries,
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	return snapshot
}

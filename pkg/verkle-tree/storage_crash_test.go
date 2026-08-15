package verkletree_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

var errSimulatedStorageCrash = errors.New("simulated storage crash")

type storageCrashPoint uint8

const (
	storageCrashNone storageCrashPoint = iota
	storageCrashBeforeNodes
	storageCrashDuringNodes
	storageCrashAfterNodes
	storageCrashDuringPublicationOld
	storageCrashDuringPublicationNew
	storageCrashAfterPublication
	storageCrashMaintenanceOld
	storageCrashMaintenanceNew
)

func TestStorageCommitCrashMatrixAndRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		point   storageCrashPoint
		newRoot bool
		debris  bool
	}{
		{name: "before any node", point: storageCrashBeforeNodes},
		{name: "during node writes", point: storageCrashDuringNodes, debris: true},
		{name: "after nodes before publication", point: storageCrashAfterNodes, debris: true},
		{name: "publication keeps old state", point: storageCrashDuringPublicationOld, debris: true},
		{name: "publication commits new state", point: storageCrashDuringPublicationNew, newRoot: true},
		{name: "after publication", point: storageCrashAfterPublication, newRoot: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := newCrashReferenceStore()
			oldSnapshot := mustPublicSnapshot(t, []verkletree.Entry{{
				Key: publicKey(0x31, 0x01), Value: publicValue(0x11),
			}})
			newSnapshot := mustPublicSnapshot(t, []verkletree.Entry{
				{Key: publicKey(0x41, 0x01), Value: publicValue(0x21)},
				{Key: publicKey(0x42, 0x80), Value: publicValue(0x22)},
				{Key: publicKey(0x43, 0xff), Value: publicValue(0x23)},
			})
			if err := oldSnapshot.Commit(
				context.Background(), store, nil, testPublicStorageLimits(),
			); err != nil {
				t.Fatalf("seed Commit() error = %v", err)
			}
			oldRoot, _ := oldSnapshot.Root()
			newRoot, _ := newSnapshot.Root()

			store.setCrashPoint(test.point)
			err := newSnapshot.Commit(
				context.Background(), store, &oldRoot, testPublicStorageLimits(),
			)
			if !errors.Is(err, verkletree.ErrStorageCommit) ||
				!errors.Is(err, errSimulatedStorageCrash) {
				t.Fatalf("crashing Commit() error = %v", err)
			}
			store.setCrashPoint(storageCrashNone)

			loaded := mustLoadCrashStore(t, store)
			loadedRoot, _ := loaded.Root()
			wantRoot := oldRoot
			if test.newRoot {
				wantRoot = newRoot
			}
			if !equalPublicRoots(t, loadedRoot, wantRoot) {
				t.Fatal("restart exposed a mixed or unexpected publication")
			}

			if !test.newRoot {
				recovery, err := verkletree.RecoverStorage(
					context.Background(),
					verkletree.BandersnatchIPA256V0(),
					store,
					testPublicStorageAuditLimits(),
				)
				if err != nil {
					t.Fatalf("RecoverStorage() error = %v", err)
				}
				if got := recovery.DeletedNodeCount(); (got > 0) != test.debris {
					t.Fatalf("recovered debris count = %d, want debris = %t", got, test.debris)
				}
				if err := newSnapshot.Commit(
					context.Background(), store, &oldRoot,
					testPublicStorageLimits(),
				); err != nil {
					t.Fatalf("retry Commit() error = %v", err)
				}
			} else {
				err := newSnapshot.Commit(
					context.Background(), store, &oldRoot,
					testPublicStorageLimits(),
				)
				if !errors.Is(err, verkletree.ErrStorageCommit) ||
					!errors.Is(err, verkletree.ErrStaleRoot) {
					t.Fatalf("committed-crash retry error = %v", err)
				}
			}

			finalRoot, _ := mustLoadCrashStore(t, store).Root()
			if !equalPublicRoots(t, finalRoot, newRoot) {
				t.Fatal("retry did not leave the complete new publication")
			}
		})
	}
}

func TestStorageMaintenanceCrashRetryAndPinnedAuditView(t *testing.T) {
	t.Parallel()

	for _, point := range []storageCrashPoint{
		storageCrashMaintenanceOld,
		storageCrashMaintenanceNew,
	} {
		point := point
		t.Run(crashPointName(point), func(t *testing.T) {
			t.Parallel()

			store := newCrashReferenceStore()
			oldSnapshot := mustPublicSnapshot(t, []verkletree.Entry{{
				Key: publicKey(0x51, 0x01), Value: publicValue(0x31),
			}})
			newSnapshot := mustPublicSnapshot(t, []verkletree.Entry{{
				Key: publicKey(0x61, 0x01), Value: publicValue(0x41),
			}})
			if err := oldSnapshot.Commit(
				context.Background(), store, nil, testPublicStorageLimits(),
			); err != nil {
				t.Fatalf("old Commit() error = %v", err)
			}
			oldRoot, _ := oldSnapshot.Root()
			oldPublication := store.retainCurrent()
			if err := newSnapshot.Commit(
				context.Background(), store, &oldRoot, testPublicStorageLimits(),
			); err != nil {
				t.Fatalf("new Commit() error = %v", err)
			}
			orphan := verkletree.NodeID{0xff, byte(point)}
			store.injectNode(orphan, []byte("interrupted unpublished write"))

			oldNode := exclusiveSnapshotNode(t, oldSnapshot, newSnapshot)
			pinned, err := store.OpenAudit(context.Background())
			if err != nil {
				t.Fatalf("OpenAudit() error = %v", err)
			}
			if _, err := pinned.ReadNode(context.Background(), oldNode, 1<<20); err != nil {
				t.Fatalf("pinned pre-maintenance ReadNode() error = %v", err)
			}

			maintenanceStarted := make(chan struct{})
			continueMaintenance := make(chan struct{})
			store.pauseNextMaintenance(maintenanceStarted, continueMaintenance)
			type maintenanceOutcome struct {
				result verkletree.StorageMaintenanceResult
				err    error
			}
			maintenanceDone := make(chan maintenanceOutcome, 1)
			maintenanceContext, cancelMaintenance := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancelMaintenance()
			store.setCrashPoint(point)
			go func() {
				result, maintainErr := verkletree.MaintainStorage(
					maintenanceContext,
					verkletree.BandersnatchIPA256V0(),
					store,
					nil,
					testPublicStorageAuditLimits(),
				)
				maintenanceDone <- maintenanceOutcome{result: result, err: maintainErr}
			}()
			select {
			case <-maintenanceStarted:
			case outcome := <-maintenanceDone:
				t.Fatalf(
					"MaintainStorage() ended before atomic handoff: (%+v, %v)",
					outcome.result, outcome.err,
				)
			case <-maintenanceContext.Done():
				t.Fatalf("MaintainStorage() did not reach atomic handoff: %v", maintenanceContext.Err())
			}
			resumed := false
			defer func() {
				if !resumed {
					close(continueMaintenance)
				}
			}()
			for range 32 {
				if _, readErr := pinned.ReadNode(
					context.Background(), oldNode, 1<<20,
				); readErr != nil {
					t.Fatalf("concurrent pinned ReadNode() error = %v", readErr)
				}
			}
			close(continueMaintenance)
			resumed = true
			var outcome maintenanceOutcome
			select {
			case outcome = <-maintenanceDone:
			case <-maintenanceContext.Done():
				t.Fatalf("MaintainStorage() did not finish after atomic handoff: %v", maintenanceContext.Err())
			}
			result, err := outcome.result, outcome.err
			if !errors.Is(err, verkletree.ErrStorageMaintenance) ||
				!errors.Is(err, errSimulatedStorageCrash) ||
				result.DeletedNodeCount() != 0 {
				t.Fatalf("crashing MaintainStorage() = (%+v, %v)", result, err)
			}

			committed := point == storageCrashMaintenanceNew
			if got := store.retainedCount(); got != boolCount(!committed) {
				t.Fatalf("retained count = %d, committed = %t", got, committed)
			}
			if got := store.hasNode(orphan); got == committed {
				t.Fatalf("orphan present = %t, committed = %t", got, committed)
			}
			if got := store.hasNode(oldNode); got == committed {
				t.Fatalf("old node present = %t, committed = %t", got, committed)
			}

			store.setCrashPoint(storageCrashNone)
			if _, err := verkletree.MaintainStorage(
				context.Background(),
				verkletree.BandersnatchIPA256V0(),
				store,
				nil,
				testPublicStorageAuditLimits(),
			); err != nil {
				t.Fatalf("retry MaintainStorage() error = %v", err)
			}
			if store.retainedCount() != 0 || store.hasNode(orphan) ||
				store.hasNode(oldNode) {
				t.Fatal("maintenance retry left dropped or unreachable state")
			}
			if _, err := pinned.ReadNode(
				context.Background(), oldNode, 1<<20,
			); err != nil {
				t.Fatalf("pinned post-maintenance ReadNode() error = %v", err)
			}
			retained, err := pinned.RetainedPublications(context.Background(), 1)
			if err != nil || len(retained) != 1 ||
				!crashPublicationEqual(retained[0], oldPublication) {
				t.Fatalf("pinned retained publications = (%+v, %v)", retained, err)
			}
			if err := pinned.Close(context.Background()); err != nil {
				t.Fatalf("pinned Close() error = %v", err)
			}
		})
	}
}

func TestStorageRecoveryCrashIsAtomicAndRetryable(t *testing.T) {
	t.Parallel()

	for _, point := range []storageCrashPoint{
		storageCrashMaintenanceOld,
		storageCrashMaintenanceNew,
	} {
		point := point
		t.Run(crashPointName(point), func(t *testing.T) {
			t.Parallel()

			store := newCrashReferenceStore()
			snapshot := mustPublicSnapshot(t, []verkletree.Entry{{
				Key: publicKey(0x71, 0x01), Value: publicValue(0x51),
			}})
			if err := snapshot.Commit(
				context.Background(), store, nil, testPublicStorageLimits(),
			); err != nil {
				t.Fatalf("Commit() error = %v", err)
			}
			wantRoot, _ := snapshot.Root()
			orphan := verkletree.NodeID{0xfe, byte(point)}
			store.injectNode(orphan, []byte("recovery debris"))

			store.setCrashPoint(point)
			result, err := verkletree.RecoverStorage(
				context.Background(),
				verkletree.BandersnatchIPA256V0(),
				store,
				testPublicStorageAuditLimits(),
			)
			if !errors.Is(err, verkletree.ErrStorageMaintenance) ||
				!errors.Is(err, errSimulatedStorageCrash) ||
				result.DeletedNodeCount() != 0 {
				t.Fatalf("crashing RecoverStorage() = (%+v, %v)", result, err)
			}
			gotRoot, _ := mustLoadCrashStore(t, store).Root()
			if !equalPublicRoots(t, gotRoot, wantRoot) {
				t.Fatal("recovery crash changed the current publication")
			}
			committed := point == storageCrashMaintenanceNew
			if got := store.hasNode(orphan); got == committed {
				t.Fatalf("recovery orphan present = %t, committed = %t", got, committed)
			}

			store.setCrashPoint(storageCrashNone)
			if _, err := verkletree.RecoverStorage(
				context.Background(),
				verkletree.BandersnatchIPA256V0(),
				store,
				testPublicStorageAuditLimits(),
			); err != nil {
				t.Fatalf("retry RecoverStorage() error = %v", err)
			}
			if store.hasNode(orphan) {
				t.Fatal("recovery retry left debris")
			}
		})
	}
}

type crashReferenceStore struct {
	mu                  sync.Mutex
	point               storageCrashPoint
	current             verkletree.StorePublication
	currentOK           bool
	retained            []verkletree.StorePublication
	nodes               map[verkletree.NodeID][]byte
	maintenanceStarted  chan struct{}
	continueMaintenance chan struct{}
}

func newCrashReferenceStore() *crashReferenceStore {
	return &crashReferenceStore{nodes: make(map[verkletree.NodeID][]byte)}
}

func (store *crashReferenceStore) Capabilities() verkletree.StoreCapabilities {
	return verkletree.RequiredWriteStoreCapabilities |
		verkletree.RequiredMaintenanceStoreCapabilities
}

func (store *crashReferenceStore) MaintenanceProfile() verkletree.Profile {
	return verkletree.BandersnatchIPA256V0()
}

func (store *crashReferenceStore) CommitSnapshot(
	ctx context.Context,
	commit verkletree.StoreCommit,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	previous, hasPrevious, err := commit.PreviousRoot()
	if err != nil {
		return err
	}
	publication, err := commit.Publication()
	if err != nil {
		return err
	}
	nodes, err := commit.Nodes(ctx)
	if err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.matchesPrevious(previous, hasPrevious) {
		return verkletree.ErrStaleRoot
	}
	if store.point == storageCrashBeforeNodes {
		return errSimulatedStorageCrash
	}
	for index := range nodes {
		store.nodes[nodes[index].ID()] = nodes[index].Encoded()
		if store.point == storageCrashDuringNodes && index == 0 {
			return errSimulatedStorageCrash
		}
	}
	if store.point == storageCrashAfterNodes ||
		store.point == storageCrashDuringPublicationOld {
		return errSimulatedStorageCrash
	}
	store.current = publication
	store.currentOK = true
	if store.point == storageCrashDuringPublicationNew ||
		store.point == storageCrashAfterPublication {
		return errSimulatedStorageCrash
	}

	return nil
}

func (store *crashReferenceStore) OpenSnapshot(
	ctx context.Context,
) (verkletree.NodeReadSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.currentOK {
		return nil, verkletree.ErrStorageSnapshotMissing
	}

	return &crashReadView{
		publication: store.current,
		nodes:       cloneCrashNodes(store.nodes),
	}, nil
}

func (store *crashReferenceStore) OpenAudit(
	ctx context.Context,
) (verkletree.NodeAuditSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()

	return &crashAuditView{
		current:   store.current,
		currentOK: store.currentOK,
		retained:  slices.Clone(store.retained),
		nodes:     cloneCrashNodes(store.nodes),
	}, nil
}

func (store *crashReferenceStore) ApplyMaintenance(
	ctx context.Context,
	maintenance verkletree.StoreMaintenance,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, currentOK, err := maintenance.CurrentPublication()
	if err != nil {
		return err
	}
	previousRetained, err := maintenance.PreviousRetainedPublications(ctx)
	if err != nil {
		return err
	}
	retained, err := maintenance.RetainedPublications(ctx)
	if err != nil {
		return err
	}
	deleted, err := maintenance.DeletedNodes(ctx)
	if err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.currentOK != currentOK ||
		(currentOK && !crashPublicationEqual(store.current, current)) ||
		!crashPublicationsEqual(store.retained, previousRetained) {
		return verkletree.ErrStaleRoot
	}
	if store.maintenanceStarted != nil {
		close(store.maintenanceStarted)
		<-store.continueMaintenance
		store.maintenanceStarted = nil
		store.continueMaintenance = nil
	}
	if store.point == storageCrashMaintenanceOld {
		return errSimulatedStorageCrash
	}

	nextNodes := cloneCrashNodes(store.nodes)
	for _, id := range deleted {
		delete(nextNodes, id)
	}
	store.retained = slices.Clone(retained)
	store.nodes = nextNodes
	if store.point == storageCrashMaintenanceNew {
		return errSimulatedStorageCrash
	}

	return nil
}

func (store *crashReferenceStore) matchesPrevious(
	previous verkletree.Root,
	hasPrevious bool,
) bool {
	if !hasPrevious {
		return !store.currentOK
	}
	if !store.currentOK {
		return false
	}
	current, err := store.current.Root()
	if err != nil {
		return false
	}

	return crashRootEqual(current, previous)
}

func (store *crashReferenceStore) setCrashPoint(point storageCrashPoint) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.point = point
}

func (store *crashReferenceStore) pauseNextMaintenance(
	started chan struct{},
	resume chan struct{},
) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.maintenanceStarted = started
	store.continueMaintenance = resume
}

func (store *crashReferenceStore) retainCurrent() verkletree.StorePublication {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.retained = []verkletree.StorePublication{store.current}

	return store.current
}

func (store *crashReferenceStore) injectNode(
	id verkletree.NodeID,
	encoded []byte,
) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.nodes[id] = slices.Clone(encoded)
}

func (store *crashReferenceStore) retainedCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()

	return len(store.retained)
}

func (store *crashReferenceStore) hasNode(id verkletree.NodeID) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	_, present := store.nodes[id]

	return present
}

type crashReadView struct {
	publication verkletree.StorePublication
	nodes       map[verkletree.NodeID][]byte
}

func (view *crashReadView) Publication(
	ctx context.Context,
) (verkletree.StorePublication, error) {
	if err := ctx.Err(); err != nil {
		return verkletree.StorePublication{}, err
	}

	return view.publication, nil
}

func (view *crashReadView) ReadNode(
	ctx context.Context,
	id verkletree.NodeID,
	maxBytes uint64,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	encoded, present := view.nodes[id]
	if !present {
		return nil, verkletree.ErrStorageNodeMissing
	}
	if uint64(len(encoded)) > maxBytes {
		return nil, verkletree.ErrResourceExhausted
	}

	return slices.Clone(encoded), nil
}

func (*crashReadView) Close(context.Context) error { return nil }

type crashAuditView struct {
	current   verkletree.StorePublication
	currentOK bool
	retained  []verkletree.StorePublication
	nodes     map[verkletree.NodeID][]byte
	closed    bool
}

func (view *crashAuditView) CurrentPublication(
	ctx context.Context,
) (verkletree.StorePublication, bool, error) {
	if err := ctx.Err(); err != nil {
		return verkletree.StorePublication{}, false, err
	}

	return view.current, view.currentOK, nil
}

func (view *crashAuditView) RetainedPublications(
	ctx context.Context,
	maxPublications uint32,
) ([]verkletree.StorePublication, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uint32(len(view.retained)) > maxPublications {
		return nil, verkletree.ErrResourceExhausted
	}

	return slices.Clip(slices.Clone(view.retained)), nil
}

func (view *crashAuditView) ReadNode(
	ctx context.Context,
	id verkletree.NodeID,
	maxBytes uint64,
) ([]byte, error) {
	return (&crashReadView{nodes: view.nodes}).ReadNode(ctx, id, maxBytes)
}

func (view *crashAuditView) NodeIDs(
	ctx context.Context,
	after *verkletree.NodeID,
	maxIDs uint32,
) ([]verkletree.NodeID, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if maxIDs == 0 {
		return nil, false, verkletree.ErrResourceExhausted
	}
	ids := make([]verkletree.NodeID, 0, len(view.nodes))
	for id := range view.nodes {
		if after == nil || bytes.Compare(id[:], after[:]) > 0 {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(left, right verkletree.NodeID) int {
		return bytes.Compare(left[:], right[:])
	})
	more := uint32(len(ids)) > maxIDs
	if more {
		ids = ids[:maxIDs]
	}

	return slices.Clip(ids), more, nil
}

func (view *crashAuditView) Close(ctx context.Context) error {
	view.closed = true
	return ctx.Err()
}

func testPublicStorageAuditLimits() verkletree.StorageAuditLimits {
	return verkletree.StorageAuditLimits{
		MaxPublications:     8,
		MaxInventoryNodes:   256,
		MaxNodeIDsPerPage:   3,
		MaxInventoryPages:   128,
		MaxUnreachableNodes: 64,
		MaxTemporaryBytes:   8 << 20,
		Read:                testPublicStorageReadLimits(),
	}
}

func mustLoadCrashStore(
	t testing.TB,
	store *crashReferenceStore,
) verkletree.Snapshot {
	t.Helper()

	snapshot, err := verkletree.LoadSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		store,
		testPublicStorageReadLimits(),
	)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	return snapshot
}

func exclusiveSnapshotNode(
	t testing.TB,
	left verkletree.Snapshot,
	right verkletree.Snapshot,
) verkletree.NodeID {
	t.Helper()

	leftIDs := snapshotNodeIDs(t, left)
	rightIDs := snapshotNodeIDs(t, right)
	for _, id := range leftIDs {
		if !slices.Contains(rightIDs, id) {
			return id
		}
	}
	t.Fatal("snapshots have no exclusive node")

	return verkletree.NodeID{}
}

func snapshotNodeIDs(
	t testing.TB,
	snapshot verkletree.Snapshot,
) []verkletree.NodeID {
	t.Helper()

	store := newCaptureNodeStore()
	if err := snapshot.Commit(
		context.Background(), store, nil, testPublicStorageLimits(),
	); err != nil {
		t.Fatalf("capture Commit() error = %v", err)
	}
	nodes, err := store.commit.Nodes(context.Background())
	if err != nil {
		t.Fatalf("capture Nodes() error = %v", err)
	}
	ids := make([]verkletree.NodeID, len(nodes))
	for index := range nodes {
		ids[index] = nodes[index].ID()
	}

	return ids
}

func cloneCrashNodes(
	nodes map[verkletree.NodeID][]byte,
) map[verkletree.NodeID][]byte {
	cloned := make(map[verkletree.NodeID][]byte, len(nodes))
	for id, encoded := range nodes {
		cloned[id] = slices.Clone(encoded)
	}

	return cloned
}

func crashRootEqual(left, right verkletree.Root) bool {
	leftBytes, leftErr := left.Bytes()
	rightBytes, rightErr := right.Bytes()

	return leftErr == nil && rightErr == nil && leftBytes == rightBytes
}

func crashPublicationEqual(
	left verkletree.StorePublication,
	right verkletree.StorePublication,
) bool {
	leftRoot, leftErr := left.Root()
	rightRoot, rightErr := right.Root()
	leftNode, leftNodeErr := left.RootNode()
	rightNode, rightNodeErr := right.RootNode()

	return leftErr == nil && rightErr == nil &&
		leftNodeErr == nil && rightNodeErr == nil &&
		crashRootEqual(leftRoot, rightRoot) && leftNode == rightNode
}

func crashPublicationsEqual(
	left []verkletree.StorePublication,
	right []verkletree.StorePublication,
) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !crashPublicationEqual(left[index], right[index]) {
			return false
		}
	}

	return true
}

func boolCount(value bool) int {
	if value {
		return 1
	}

	return 0
}

func crashPointName(point storageCrashPoint) string {
	if point == storageCrashMaintenanceOld {
		return "old state survives"
	}

	return "new state commits"
}

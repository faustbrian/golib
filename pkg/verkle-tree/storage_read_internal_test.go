package verkletree

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
)

type storageCountingContext struct {
	context.Context
	calls int
}

func (ctx *storageCountingContext) Err() error {
	ctx.calls++

	return ctx.Context.Err()
}

func TestStoragePublicationAndReadLimitsFailClosed(t *testing.T) {
	t.Parallel()

	var zero StorePublication
	if _, err := zero.Root(); !errors.Is(err, ErrStorageRead) {
		t.Fatalf("zero Root() error = %v", err)
	}
	if _, err := zero.RootNode(); !errors.Is(err, ErrStorageRead) {
		t.Fatalf("zero RootNode() error = %v", err)
	}
	if _, _, err := zero.values(); !errors.Is(err, ErrStorageRead) {
		t.Fatalf("zero values() error = %v", err)
	}
	if _, err := (StoreCommit{}).Publication(); !errors.Is(err, ErrStorageCommit) {
		t.Fatalf("zero commit Publication() error = %v", err)
	}
	if _, err := NewStorePublication(Root{}, NodeID{}); !errors.Is(err, ErrInvalidRoot) {
		t.Fatalf("invalid NewStorePublication() error = %v", err)
	}
	corrupt := StorePublication{root: Root{}, valid: true}
	if _, err := corrupt.Root(); !errors.Is(err, ErrStorageRead) {
		t.Fatalf("corrupt Root() error = %v", err)
	}

	snapshot := testStorageFacadeSnapshot(t)
	reader := internalReaderFromSnapshot(t, snapshot)
	publication, err := reader.view.publication.Root()
	if err != nil {
		t.Fatalf("publication Root() error = %v", err)
	}
	want, _ := snapshot.Root()
	if !rootsEqualForStorageTest(t, publication, want) {
		t.Fatal("publication root differs")
	}
	reconstructed, err := NewStorePublication(want, NodeID{1})
	if err != nil {
		t.Fatalf("NewStorePublication() error = %v", err)
	}
	if id, idErr := reconstructed.RootNode(); idErr != nil || id != (NodeID{1}) {
		t.Fatalf("reconstructed RootNode() = (%x, %v)", id, idErr)
	}

	invalid := map[string]func(*StorageReadLimits){
		"entries zero":       func(value *StorageReadLimits) { value.MaxEntries = 0 },
		"entries excessive":  func(value *StorageReadLimits) { value.MaxEntries = maxPublicCount + 1 },
		"nodes zero":         func(value *StorageReadLimits) { value.MaxNodes = 0 },
		"nodes excessive":    func(value *StorageReadLimits) { value.MaxNodes = maxPublicCount + 1 },
		"edges zero":         func(value *StorageReadLimits) { value.MaxEdges = 0 },
		"edges excessive":    func(value *StorageReadLimits) { value.MaxEdges = maxPublicCount + 1 },
		"reads zero":         func(value *StorageReadLimits) { value.MaxNodeReads = 0 },
		"reads excessive":    func(value *StorageReadLimits) { value.MaxNodeReads = maxPublicCount + 1 },
		"node bytes zero":    func(value *StorageReadLimits) { value.MaxNodeBytes = 0 },
		"encoded bytes zero": func(value *StorageReadLimits) { value.MaxEncodedBytes = 0 },
		"hashes zero":        func(value *StorageReadLimits) { value.MaxHashes = 0 },
		"temporary zero":     func(value *StorageReadLimits) { value.MaxTemporaryBytes = 0 },
		"snapshot invalid":   func(value *StorageReadLimits) { value.Snapshot = SnapshotLimits{} },
	}
	for name, mutate := range invalid {
		t.Run(name, func(t *testing.T) {
			limits := testInternalStorageReadLimits()
			mutate(&limits)
			if err := limits.validate(); !errors.Is(err, ErrInvalidLimits) {
				t.Fatalf("validate() error = %v", err)
			}
		})
	}
	boundary := testInternalStorageReadLimits()
	boundary.MaxEntries = maxPublicCount
	boundary.MaxNodes = maxPublicCount
	boundary.MaxEdges = maxPublicCount
	boundary.MaxNodeReads = maxPublicCount
	boundary.MaxNodeBytes = 1
	boundary.MaxEncodedBytes = 1
	boundary.MaxHashes = 1
	boundary.MaxTemporaryBytes = 1
	if err := boundary.validate(); err != nil {
		t.Fatalf("boundary validate() error = %v", err)
	}
}

func TestLoadSnapshotRejectsFacadeAndReaderLifecycleFailures(t *testing.T) {
	t.Parallel()

	snapshot := testStorageFacadeSnapshot(t)
	valid := internalReaderFromSnapshot(t, snapshot)
	var nilContext context.Context
	var nilReader *internalStorageReader

	tests := map[string]struct {
		ctx     context.Context
		profile Profile
		reader  NodeReader
		limits  StorageReadLimits
		want    error
	}{
		"nil context": {
			ctx: nilContext, profile: BandersnatchIPA256V0(),
			reader: valid, limits: testInternalStorageReadLimits(), want: ErrInvalidContext,
		},
		"invalid profile": {
			ctx: context.Background(), reader: valid,
			limits: testInternalStorageReadLimits(), want: ErrUnsupportedProfile,
		},
		"nil reader": {
			ctx: context.Background(), profile: BandersnatchIPA256V0(),
			limits: testInternalStorageReadLimits(), want: ErrInvalidStore,
		},
		"typed nil reader": {
			ctx: context.Background(), profile: BandersnatchIPA256V0(),
			reader: nilReader, limits: testInternalStorageReadLimits(), want: ErrInvalidStore,
		},
		"invalid limits": {
			ctx: context.Background(), profile: BandersnatchIPA256V0(),
			reader: valid, want: ErrInvalidLimits,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			before := valid.openCalls
			_, err := LoadSnapshot(test.ctx, test.profile, test.reader, test.limits)
			if !errors.Is(err, test.want) {
				t.Fatalf("LoadSnapshot() error = %v, want %v", err, test.want)
			}
			if valid.openCalls != before {
				t.Fatalf("open calls changed from %d to %d", before, valid.openCalls)
			}
		})
	}

	missingCapability := internalReaderFromSnapshot(t, snapshot)
	missingCapability.capabilities = StoreCapabilityImmutableNodes
	_, err := LoadSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		missingCapability,
		testInternalStorageReadLimits(),
	)
	var capabilityErr *StoreCapabilityError
	if !errors.As(err, &capabilityErr) ||
		capabilityErr.Missing != StoreCapabilitySnapshotReads ||
		missingCapability.openCalls != 0 {
		t.Fatalf("capability error = %v", err)
	}

	openCancelled := internalReaderFromSnapshot(t, snapshot)
	openCancelled.openErr = context.Canceled
	_, err = LoadSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		openCancelled,
		testInternalStorageReadLimits(),
	)
	if !errors.Is(err, ErrStorageRead) || !errors.Is(err, ErrCancelled) {
		t.Fatalf("cancelled open error = %v", err)
	}

	typedNilView := internalReaderFromSnapshot(t, snapshot)
	typedNilView.returnNilView = true
	_, err = LoadSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		typedNilView,
		testInternalStorageReadLimits(),
	)
	if !errors.Is(err, ErrStorageRead) {
		t.Fatalf("typed nil view error = %v", err)
	}

	publicationFailure := internalReaderFromSnapshot(t, snapshot)
	publicationSentinel := errors.New("publication unavailable")
	closeSentinel := errors.New("close unavailable")
	publicationFailure.view.publicationErr = publicationSentinel
	publicationFailure.view.closeErr = closeSentinel
	_, err = LoadSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		publicationFailure,
		testInternalStorageReadLimits(),
	)
	if !errors.Is(err, ErrStorageRead) ||
		!errors.Is(err, publicationSentinel) ||
		!errors.Is(err, closeSentinel) ||
		publicationFailure.view.closeCalls != 1 {
		t.Fatalf("publication/close error = %v", err)
	}

	invalidPublication := internalReaderFromSnapshot(t, snapshot)
	invalidPublication.view.publication = StorePublication{}
	_, err = LoadSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		invalidPublication,
		testInternalStorageReadLimits(),
	)
	if !errors.Is(err, ErrStorageRead) {
		t.Fatalf("invalid publication error = %v", err)
	}

	publicationContext, cancelPublication := context.WithCancel(context.Background())
	cancelledPublication := internalReaderFromSnapshot(t, snapshot)
	cancelledPublication.cancelOnOpen = cancelPublication
	_, err = LoadSnapshot(
		publicationContext,
		BandersnatchIPA256V0(),
		cancelledPublication,
		testInternalStorageReadLimits(),
	)
	if !errors.Is(err, ErrStorageRead) ||
		!errors.Is(err, ErrCancelled) ||
		cancelledPublication.view.readCalls != 0 ||
		cancelledPublication.view.closeCalls != 1 {
		t.Fatalf("cancelled publication read error = %v", err)
	}
}

func TestLoadSnapshotEnforcesEveryReadResource(t *testing.T) {
	t.Parallel()

	snapshot := testStorageReadSnapshot(t)
	base := internalReaderFromSnapshot(t, snapshot)
	totalBytes := uint64(0)
	maxNodeBytes := uint64(0)
	for _, encoded := range base.view.nodes {
		totalBytes += uint64(len(encoded))
		maxNodeBytes = max(maxNodeBytes, uint64(len(encoded)))
	}
	nodeCount := uint64(len(base.view.nodes))

	tests := map[string]struct {
		resource Resource
		mutate   func(*StorageReadLimits)
	}{
		"entries":       {ResourceEntries, func(value *StorageReadLimits) { value.MaxEntries = 1 }},
		"nodes":         {ResourceNodes, func(value *StorageReadLimits) { value.MaxNodes = 1 }},
		"edges":         {ResourceEdges, func(value *StorageReadLimits) { value.MaxEdges = 1 }},
		"node reads":    {ResourceNodeReads, func(value *StorageReadLimits) { value.MaxNodeReads = 1 }},
		"node bytes":    {ResourceNodeBytes, func(value *StorageReadLimits) { value.MaxNodeBytes = maxNodeBytes - 1 }},
		"encoded bytes": {ResourceEncodedNodeBytes, func(value *StorageReadLimits) { value.MaxEncodedBytes = 1 }},
		"encoded bytes before next read": {ResourceEncodedNodeBytes, func(value *StorageReadLimits) {
			rootID, _ := base.view.publication.RootNode()
			value.MaxEncodedBytes = uint64(len(base.view.nodes[rootID]))
		}},
		"hashes":            {ResourceNodeHashes, func(value *StorageReadLimits) { value.MaxHashes = 1 }},
		"point decodes":     {ResourcePointDecodes, func(value *StorageReadLimits) { value.MaxPointDecodes = 0 }},
		"temporary":         {ResourceTemporaryBytes, func(value *StorageReadLimits) { value.MaxTemporaryBytes = 1 }},
		"re-encoded bytes":  {ResourceEncodedNodeBytes, func(value *StorageReadLimits) { value.MaxEncodedBytes = totalBytes }},
		"recomputed hashes": {ResourceNodeHashes, func(value *StorageReadLimits) { value.MaxHashes = nodeCount }},
		"snapshot reconstruction": {ResourceEntries, func(value *StorageReadLimits) {
			value.Snapshot.State.MaxEntries = 1
		}},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := cloneInternalStorageReader(base)
			limits := testInternalStorageReadLimits()
			test.mutate(&limits)
			_, err := LoadSnapshot(
				context.Background(),
				BandersnatchIPA256V0(),
				reader,
				limits,
			)
			var resourceErr *ResourceError
			if !errors.As(err, &resourceErr) || resourceErr.Resource != test.resource {
				t.Fatalf("LoadSnapshot() error = %v, want resource %d", err, test.resource)
			}
			if reader.view.closeCalls != 1 {
				t.Fatalf("close calls = %d", reader.view.closeCalls)
			}
			if (name == "node reads" || name == "hashes" ||
				name == "encoded bytes before next read") && reader.view.readCalls != 1 {
				t.Fatalf("read calls = %d, want fail-fast after 1", reader.view.readCalls)
			}
			if name == "encoded bytes before next read" {
				var resourceErr *ResourceError
				if !errors.As(err, &resourceErr) || resourceErr.Actual != resourceErr.Limit+1 {
					t.Fatalf("encoded boundary error = %#v", resourceErr)
				}
			}
		})
	}
}

func TestLoadSnapshotPassesExactRemainingReadBounds(t *testing.T) {
	t.Parallel()

	base := internalReaderFromSnapshot(t, testStorageReadSnapshot(t))
	rootID, _ := base.view.publication.RootNode()
	rootBytes := uint64(len(base.view.nodes[rootID]))

	encodedReader := cloneInternalStorageReader(base)
	encodedLimits := testInternalStorageReadLimits()
	encodedLimits.MaxEncodedBytes = rootBytes + 17
	_, err := LoadSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		encodedReader,
		encodedLimits,
	)
	if len(encodedReader.view.readBounds) != 2 ||
		encodedReader.view.readBounds[0] != rootBytes+17 ||
		encodedReader.view.readBounds[1] != 17 ||
		!errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("encoded read bounds = %v, error = %v", encodedReader.view.readBounds, err)
	}

	temporaryReader := cloneInternalStorageReader(base)
	temporaryLimits := testInternalStorageReadLimits()
	temporaryLimits.MaxTemporaryBytes = storageReadRetainedBytes(1, 0) + 23
	_, err = LoadSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		temporaryReader,
		temporaryLimits,
	)
	if len(temporaryReader.view.readBounds) != 1 ||
		temporaryReader.view.readBounds[0] != 23 ||
		!errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("temporary read bounds = %v, error = %v", temporaryReader.view.readBounds, err)
	}
}

func TestLoadSnapshotAccountsPointDecodesAcrossNodes(t *testing.T) {
	t.Parallel()

	reader := internalReaderFromSnapshot(t, testStorageReadSnapshot(t))
	total := uint64(0)
	for _, encoded := range reader.view.nodes {
		total += uint64(decodeInternalStorageNode(t, encoded).PointDecodes())
	}
	limits := testInternalStorageReadLimits()
	limits.MaxPointDecodes = total - 1
	_, err := LoadSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		reader,
		limits,
	)
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != ResourcePointDecodes ||
		reader.view.readCalls != len(reader.view.nodes) {
		t.Fatalf("point accounting error = %v after %d reads", err, reader.view.readCalls)
	}
}

func TestLoadSnapshotRemainsCancellableAcrossReadsAndReconstruction(t *testing.T) {
	t.Parallel()

	base := internalReaderFromSnapshot(t, testStorageReadSnapshot(t))
	for cancelAt := 1; cancelAt <= 160; cancelAt++ {
		reader := cloneInternalStorageReader(base)
		_, err := LoadSnapshot(
			&cancellingContext{remaining: cancelAt},
			BandersnatchIPA256V0(),
			reader,
			testInternalStorageReadLimits(),
		)
		if err != nil && !errors.Is(err, ErrCancelled) {
			t.Fatalf("cancelAt %d error = %v", cancelAt, err)
		}
		if reader.openCalls > 0 && reader.view.closeCalls != 1 {
			t.Fatalf("cancelAt %d close calls = %d", cancelAt, reader.view.closeCalls)
		}
	}

	counting := &storageCountingContext{Context: context.Background()}
	reader := cloneInternalStorageReader(base)
	if _, err := LoadSnapshot(
		counting,
		BandersnatchIPA256V0(),
		reader,
		testInternalStorageReadLimits(),
	); err != nil {
		t.Fatalf("counting LoadSnapshot() error = %v", err)
	}
	reader = cloneInternalStorageReader(base)
	_, err := LoadSnapshot(
		&cancellingContext{remaining: counting.calls - 1},
		BandersnatchIPA256V0(),
		reader,
		testInternalStorageReadLimits(),
	)
	if !errors.Is(err, ErrCancelled) || reader.view.readCalls != len(reader.view.nodes) {
		t.Fatalf(
			"final reconstruction cancellation = %v after %d/%d reads",
			err,
			reader.view.readCalls,
			len(reader.view.nodes),
		)
	}
}

func TestLoadSnapshotRejectsAdapterThatExceedsReadBound(t *testing.T) {
	t.Parallel()

	reader := internalReaderFromSnapshot(t, testStorageReadSnapshot(t))
	limits := testInternalStorageReadLimits()
	limits.MaxTemporaryBytes = storageReadRetainedBytes(1, 0) + 1
	_, err := LoadSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		reader,
		limits,
	)
	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != ResourceTemporaryBytes ||
		resourceErr.Actual != storageReadRetainedBytes(1, 0)+
			uint64(len(reader.view.nodes[mustInternalRootNode(t, reader)]))+1 ||
		len(reader.view.readBounds) != 1 ||
		reader.view.readBounds[0] != 1 ||
		reader.view.readCalls != 1 {
		t.Fatalf("oversized adapter read error = %v after %d reads", err, reader.view.readCalls)
	}
}

func TestLoadSnapshotTemporaryBudgetStopsEachAllocationPhase(t *testing.T) {
	t.Parallel()

	base := internalReaderFromSnapshot(t, testStorageReadSnapshot(t))
	for _, budget := range []uint64{
		288, 657, 945, 1_200, 1_300, 1_400, 1_500, 1_600, 1_680, 1_800,
		2_100, 2_200, 2_300, 2_400,
	} {
		reader := cloneInternalStorageReader(base)
		limits := testInternalStorageReadLimits()
		limits.MaxTemporaryBytes = budget
		_, err := LoadSnapshot(
			context.Background(),
			BandersnatchIPA256V0(),
			reader,
			limits,
		)
		if err != nil {
			var resourceErr *ResourceError
			if !errors.As(err, &resourceErr) || resourceErr.Resource != ResourceTemporaryBytes {
				t.Fatalf("budget %d error = %v", budget, err)
			}
		}
	}
}

func TestLoadSnapshotIsDeterministicAcrossConcurrentReadViews(t *testing.T) {
	t.Parallel()

	base := internalReaderFromSnapshot(t, testStorageReadSnapshot(t))
	reader := concurrentInternalStorageReader{
		publication: base.view.publication,
		nodes:       base.view.nodes,
	}
	want, _ := base.view.publication.Root()
	wantBytes, _ := want.Bytes()
	errorsSeen := make(chan error, 16)
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			loaded, err := LoadSnapshot(
				context.Background(),
				BandersnatchIPA256V0(),
				reader,
				testInternalStorageReadLimits(),
			)
			if err != nil {
				errorsSeen <- err
				return
			}
			root, err := loaded.Root()
			rootBytes, bytesErr := root.Bytes()
			if err != nil || bytesErr != nil || rootBytes != wantBytes {
				errorsSeen <- errors.New("loaded root differs")
			}
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatalf("concurrent load error = %v", err)
	}
}

func TestLoadSnapshotRejectsCanonicalTopologyAndCommitmentSubstitution(t *testing.T) {
	t.Parallel()

	snapshot := testStorageReadSnapshot(t)
	base := internalReaderFromSnapshot(t, snapshot)

	tests := map[string]func(*internalStorageReader){
		"root is stem": func(reader *internalStorageReader) {
			rootID, _ := reader.view.publication.RootNode()
			root := decodeInternalStorageNode(t, reader.view.nodes[rootID])
			children, _ := root.Children(context.Background())
			reader.view.publication.rootNode = NodeID(children[0].ID)
		},
		"root depth": func(reader *internalStorageReader) {
			mutateInternalRoot(t, reader, func(encoded []byte) { encoded[10] = 1 })
		},
		"duplicate child": func(reader *internalStorageReader) {
			mutateInternalRoot(t, reader, func(encoded []byte) {
				copy(encoded[80:112], encoded[47:79])
			})
		},
		"missing child": func(reader *internalStorageReader) {
			rootID, _ := reader.view.publication.RootNode()
			root := decodeInternalStorageNode(t, reader.view.nodes[rootID])
			children, _ := root.Children(context.Background())
			delete(reader.view.nodes, NodeID(children[0].ID))
		},
		"stem path": func(reader *internalStorageReader) {
			mutateFirstChild(t, reader, func(encoded []byte) { encoded[44] ^= 0x7f })
		},
		"value substitution": func(reader *internalStorageReader) {
			mutateFirstChild(t, reader, func(encoded []byte) { encoded[len(encoded)-1] ^= 1 })
		},
		"subcommitment substitution": func(reader *internalStorageReader) {
			mutateFirstChild(t, reader, func(encoded []byte) { copy(encoded[108:141], encoded[75:108]) })
		},
		"malformed root": func(reader *internalStorageReader) {
			mutateInternalRoot(t, reader, func(encoded []byte) { encoded[9] = 0xff })
		},
		"published root": func(reader *internalStorageReader) {
			empty := testStorageEmptySnapshot(t)
			other := internalReaderFromSnapshot(t, empty)
			root, _ := other.view.publication.Root()
			reader.view.publication.root = root
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := cloneInternalStorageReader(base)
			mutate(reader)
			_, err := LoadSnapshot(
				context.Background(),
				BandersnatchIPA256V0(),
				reader,
				testInternalStorageReadLimits(),
			)
			if !errors.Is(err, ErrStorageNodeCorrupt) &&
				!errors.Is(err, ErrStorageNodeMissing) {
				t.Fatalf("LoadSnapshot() error = %v", err)
			}
		})
	}

	wrongProfile := cloneInternalStorageReader(base)
	mutateInternalRoot(t, wrongProfile, func(encoded []byte) { encoded[4]++ })
	_, err := LoadSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		wrongProfile,
		testInternalStorageReadLimits(),
	)
	if !errors.Is(err, ErrUnsupportedProfile) {
		t.Fatalf("wrong-profile LoadSnapshot() error = %v", err)
	}

	wrongChildDepth := cloneInternalStorageReader(base)
	mutateFirstChild(t, wrongChildDepth, func(encoded []byte) { encoded[10]++ })
	_, err = LoadSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		wrongChildDepth,
		testInternalStorageReadLimits(),
	)
	if !errors.Is(err, ErrStorageNodeCorrupt) || wrongChildDepth.view.readCalls != 2 {
		t.Fatalf("wrong child depth error = %v after %d reads", err, wrongChildDepth.view.readCalls)
	}
}

func TestStorageReadHelpersCoverOverflowCancellationAndTranslations(t *testing.T) {
	t.Parallel()

	current := uint64(math.MaxUint64)
	if err := addStorageReadResource(ResourceNodes, math.MaxUint64, &current, 1); err == nil {
		t.Fatal("overflowing resource accepted")
	}
	current = 1
	if err := addStorageReadResource(ResourceNodes, 1, &current, 1); err == nil || current != 1 {
		t.Fatalf("limited resource = (%d, %v)", current, err)
	}
	current = math.MaxUint64 - 1
	if err := addStorageReadResource(
		ResourceNodes,
		math.MaxUint64,
		&current,
		1,
	); err != nil || current != math.MaxUint64 {
		t.Fatalf("maximum resource boundary = (%d, %v)", current, err)
	}
	if got := storageReadNextActual(7); got != 8 {
		t.Fatalf("next actual = %d", got)
	}
	if got := storageReadNextActual(math.MaxUint64); got != math.MaxUint64 {
		t.Fatalf("saturated actual = %d", got)
	}
	if got := storageReadRemaining(101, 37); got != 64 {
		t.Fatalf("remaining resource = %d", got)
	}
	if got := storageReadSum(3, 5, 7, 11); got != 26 {
		t.Fatalf("resource sum = %d", got)
	}
	if got := storageReadRecordBytes(3); got != 192 {
		t.Fatalf("record bytes = %d", got)
	}
	if got := storageReadRetainedBytes(3, 4); got != 1_376 {
		t.Fatalf("retained bytes = %d", got)
	}
	if err := checkStorageReadTemporary(
		testInternalStorageReadLimits(),
		math.MaxUint64,
		math.MaxUint64,
		math.MaxUint64,
	); err == nil {
		t.Fatal("overflowing temporary resource accepted")
	}

	for internalResource, publicResource := range map[committedtree.StorageDecodingResource]Resource{
		committedtree.StorageDecodingResourceNodeBytes:      ResourceNodeBytes,
		committedtree.StorageDecodingResourcePointDecodes:   ResourcePointDecodes,
		committedtree.StorageDecodingResourceTemporaryBytes: ResourceTemporaryBytes,
		0xff: ResourceTemporaryBytes,
	} {
		err := translateStorageDecodingError(&committedtree.StorageDecodingResourceError{
			Resource: internalResource, Limit: 1, Actual: 2,
		})
		var resourceErr *ResourceError
		if !errors.As(err, &resourceErr) || resourceErr.Resource != publicResource {
			t.Fatalf("translation %d = %v", internalResource, err)
		}
	}
	if err := translateStorageDecodingError(context.Canceled); !errors.Is(err, ErrCancelled) {
		t.Fatalf("cancel translation = %v", err)
	}
	if err := translateStorageDecodingError(errors.New("bad")); !errors.Is(err, ErrStorageNodeCorrupt) {
		t.Fatalf("corrupt translation = %v", err)
	}
	if err := translateStorageDecodingError(committedtree.ErrStorageNodeProfile); !errors.Is(err, ErrUnsupportedProfile) {
		t.Fatalf("profile translation = %v", err)
	}
	if err := translateStorageReadEncodingError(context.Canceled); !errors.Is(err, ErrCancelled) {
		t.Fatalf("encoding cancellation = %v", err)
	}
	if err := translateStorageReadEncodingError(errors.New("bad")); !errors.Is(err, ErrStorageNodeCorrupt) {
		t.Fatalf("encoding corruption = %v", err)
	}
	if err := translateStorageReconstructionError(&ResourceError{Resource: ResourceEntries}); !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("reconstruction resource = %v", err)
	}
	if err := translateStorageReconstructionError(context.Canceled); !errors.Is(err, ErrStorageNodeCorrupt) {
		t.Fatalf("untranslated cancellation = %v", err)
	}
	if err := translateStorageReconstructionError(errors.New("bad")); !errors.Is(err, ErrStorageNodeCorrupt) {
		t.Fatalf("reconstruction corruption = %v", err)
	}
	if err := wrapStorageReadError("read", context.DeadlineExceeded); !errors.Is(err, ErrCancelled) {
		t.Fatalf("deadline wrapping = %v", err)
	}
	if validStorageValue(nil) || !validStorageValue(internalValueReader{}) {
		t.Fatal("storage value validity mismatch")
	}
}

type internalStorageReader struct {
	capabilities  StoreCapabilities
	view          *internalStorageReadSnapshot
	openErr       error
	openCalls     int
	returnNilView bool
	cancelOnOpen  context.CancelFunc
}

func (reader *internalStorageReader) Capabilities() StoreCapabilities {
	return reader.capabilities
}

func (reader *internalStorageReader) OpenSnapshot(
	context.Context,
) (NodeReadSnapshot, error) {
	reader.openCalls++
	if reader.openErr != nil {
		return nil, reader.openErr
	}
	if reader.returnNilView {
		var view *internalStorageReadSnapshot
		return view, nil
	}
	if reader.cancelOnOpen != nil {
		reader.cancelOnOpen()
	}

	return reader.view, nil
}

type internalStorageReadSnapshot struct {
	publication    StorePublication
	nodes          map[NodeID][]byte
	publicationErr error
	readErr        error
	closeErr       error
	closeCalls     int
	readCalls      int
	readBounds     []uint64
	aliasReads     bool
}

func (snapshot *internalStorageReadSnapshot) Publication(
	ctx context.Context,
) (StorePublication, error) {
	if err := ctx.Err(); err != nil {
		return StorePublication{}, err
	}
	if snapshot.publicationErr != nil {
		return StorePublication{}, snapshot.publicationErr
	}

	return snapshot.publication, nil
}

func (snapshot *internalStorageReadSnapshot) ReadNode(
	ctx context.Context,
	id NodeID,
	maxBytes uint64,
) ([]byte, error) {
	snapshot.readCalls++
	snapshot.readBounds = append(snapshot.readBounds, maxBytes)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if snapshot.readErr != nil {
		return nil, snapshot.readErr
	}
	encoded, present := snapshot.nodes[id]
	if !present {
		return nil, ErrStorageNodeMissing
	}
	if snapshot.aliasReads {
		return encoded, nil
	}

	return append([]byte(nil), encoded...), nil
}

func mustInternalRootNode(t testing.TB, reader *internalStorageReader) NodeID {
	t.Helper()
	id, err := reader.view.publication.RootNode()
	if err != nil {
		t.Fatalf("RootNode() error = %v", err)
	}

	return id
}

func (snapshot *internalStorageReadSnapshot) Close(context.Context) error {
	snapshot.closeCalls++
	return snapshot.closeErr
}

type internalValueReader struct{}

func (internalValueReader) Capabilities() StoreCapabilities {
	return RequiredReadStoreCapabilities
}

func (internalValueReader) OpenSnapshot(context.Context) (NodeReadSnapshot, error) {
	return nil, ErrStorageSnapshotMissing
}

type concurrentInternalStorageReader struct {
	publication StorePublication
	nodes       map[NodeID][]byte
}

func (concurrentInternalStorageReader) Capabilities() StoreCapabilities {
	return RequiredReadStoreCapabilities
}

func (reader concurrentInternalStorageReader) OpenSnapshot(
	context.Context,
) (NodeReadSnapshot, error) {
	return concurrentInternalStorageView(reader), nil
}

type concurrentInternalStorageView concurrentInternalStorageReader

func (view concurrentInternalStorageView) Publication(
	ctx context.Context,
) (StorePublication, error) {
	if err := ctx.Err(); err != nil {
		return StorePublication{}, err
	}

	return view.publication, nil
}

func (view concurrentInternalStorageView) ReadNode(
	_ context.Context,
	id NodeID,
	_ uint64,
) ([]byte, error) {
	encoded, present := view.nodes[id]
	if !present {
		return nil, ErrStorageNodeMissing
	}

	return append([]byte(nil), encoded...), nil
}

func (concurrentInternalStorageView) Close(context.Context) error {
	return nil
}

func internalReaderFromSnapshot(t testing.TB, snapshot Snapshot) *internalStorageReader {
	t.Helper()
	writer := &internalCaptureStore{capabilities: RequiredWriteStoreCapabilities}
	if err := snapshot.Commit(
		context.Background(),
		writer,
		nil,
		testStorageFacadeLimits(),
	); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	publication, err := writer.commit.Publication()
	if err != nil {
		t.Fatalf("Publication() error = %v", err)
	}
	nodes, err := writer.commit.Nodes(context.Background())
	if err != nil {
		t.Fatalf("Nodes() error = %v", err)
	}
	stored := make(map[NodeID][]byte, len(nodes))
	for _, node := range nodes {
		stored[node.ID()] = node.Encoded()
	}

	return &internalStorageReader{
		capabilities: RequiredReadStoreCapabilities,
		view: &internalStorageReadSnapshot{
			publication: publication,
			nodes:       stored,
		},
	}
}

func cloneInternalStorageReader(source *internalStorageReader) *internalStorageReader {
	nodes := make(map[NodeID][]byte, len(source.view.nodes))
	for id, encoded := range source.view.nodes {
		nodes[id] = append([]byte(nil), encoded...)
	}

	return &internalStorageReader{
		capabilities: source.capabilities,
		view: &internalStorageReadSnapshot{
			publication: source.view.publication,
			nodes:       nodes,
		},
	}
}

func mutateInternalRoot(
	t testing.TB,
	reader *internalStorageReader,
	mutate func([]byte),
) {
	t.Helper()
	rootID, _ := reader.view.publication.RootNode()
	encoded := append([]byte(nil), reader.view.nodes[rootID]...)
	mutate(encoded)
	newID := NodeID(sha256.Sum256(encoded))
	delete(reader.view.nodes, rootID)
	reader.view.nodes[newID] = encoded
	reader.view.publication.rootNode = newID
}

func mutateFirstChild(
	t testing.TB,
	reader *internalStorageReader,
	mutate func([]byte),
) {
	t.Helper()
	rootID, _ := reader.view.publication.RootNode()
	rootEncoded := append([]byte(nil), reader.view.nodes[rootID]...)
	root := decodeInternalStorageNode(t, rootEncoded)
	children, _ := root.Children(context.Background())
	oldChild := NodeID(children[0].ID)
	childEncoded := append([]byte(nil), reader.view.nodes[oldChild]...)
	mutate(childEncoded)
	newChild := NodeID(sha256.Sum256(childEncoded))
	copy(rootEncoded[47:79], newChild[:])
	newRoot := NodeID(sha256.Sum256(rootEncoded))
	delete(reader.view.nodes, oldChild)
	delete(reader.view.nodes, rootID)
	reader.view.nodes[newChild] = childEncoded
	reader.view.nodes[newRoot] = rootEncoded
	reader.view.publication.rootNode = newRoot
}

func decodeInternalStorageNode(t testing.TB, encoded []byte) committedtree.DecodedStorageNode {
	t.Helper()
	decoded, err := committedtree.DecodeStorageNode(
		context.Background(),
		encoded,
		committedtree.StorageDecodingLimits{
			MaxNodeBytes:      1 << 20,
			MaxPointDecodes:   3,
			MaxTemporaryBytes: 1 << 20,
		},
	)
	if err != nil {
		t.Fatalf("DecodeStorageNode() error = %v", err)
	}

	return decoded
}

func testStorageReadSnapshot(t testing.TB) Snapshot {
	t.Helper()
	var first Key
	first[0] = 1
	first[31] = 1
	var second Key
	second[0] = 2
	second[31] = 2
	snapshot, err := NewSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		[]Entry{
			{Key: first, Value: Value{1}},
			{Key: second, Value: Value{2}},
		},
		testFacadeSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	return snapshot
}

func testStorageEmptySnapshot(t testing.TB) Snapshot {
	t.Helper()
	snapshot, err := NewSnapshot(
		context.Background(),
		BandersnatchIPA256V0(),
		nil,
		testFacadeSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}

	return snapshot
}

func testInternalStorageReadLimits() StorageReadLimits {
	return StorageReadLimits{
		MaxEntries:        64,
		MaxNodes:          64,
		MaxEdges:          64,
		MaxNodeReads:      64,
		MaxNodeBytes:      1 << 20,
		MaxEncodedBytes:   2 << 20,
		MaxHashes:         128,
		MaxPointDecodes:   192,
		MaxTemporaryBytes: 4 << 20,
		Snapshot:          testFacadeSnapshotLimits(),
	}
}

func rootsEqualForStorageTest(t testing.TB, left Root, right Root) bool {
	t.Helper()
	leftBytes, leftErr := left.Bytes()
	rightBytes, rightErr := right.Bytes()
	if leftErr != nil || rightErr != nil {
		t.Fatalf("root bytes errors = %v / %v", leftErr, rightErr)
	}

	return leftBytes == rightBytes
}

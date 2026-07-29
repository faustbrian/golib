package merkletree

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"math/bits"
	"testing"
)

func TestSnapshotPersistenceInputAndResourceLimits(t *testing.T) {
	t.Parallel()

	snapshot, data := persistedSnapshotFixture(t)
	if _, err := ParseSnapshot(
		nil,
		data,
		DefaultSnapshotPersistenceLimits(),
	); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("ParseSnapshot(nil context) error = %v", err)
	}
	if _, err := ParseSnapshot(
		context.Background(),
		data,
		SnapshotPersistenceLimits{},
	); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("ParseSnapshot(invalid limits) error = %v", err)
	}
	for name, edit := range map[string]func(*SnapshotPersistenceLimits){
		"encoded bytes": func(limits *SnapshotPersistenceLimits) {
			limits.MaxEncodedBytes = 0
		},
		"leaves": func(limits *SnapshotPersistenceLimits) {
			limits.MaxLeaves = 0
		},
		"total bytes": func(limits *SnapshotPersistenceLimits) {
			limits.MaxTotalLeafBytes = 0
		},
		"nodes": func(limits *SnapshotPersistenceLimits) {
			limits.MaxRetainedNodes = 0
		},
		"depth": func(limits *SnapshotPersistenceLimits) {
			limits.MaxTraversalDepth = 0
		},
		"node reads": func(limits *SnapshotPersistenceLimits) {
			limits.MaxNodeReads = 0
		},
		"temporary bytes": func(limits *SnapshotPersistenceLimits) {
			limits.MaxTemporaryBytes = 0
		},
	} {
		t.Run("invalid "+name, func(t *testing.T) {
			limits := DefaultSnapshotPersistenceLimits()
			edit(&limits)
			if _, err := ParseSnapshot(
				context.Background(),
				data,
				limits,
			); !errors.Is(err, ErrInvalidLimits) {
				t.Fatalf("ParseSnapshot() error = %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseSnapshot(
		ctx,
		data,
		DefaultSnapshotPersistenceLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("ParseSnapshot(cancelled) error = %v", err)
	}

	exact := DefaultSnapshotPersistenceLimits()
	exact.MaxEncodedBytes = uint64(len(data))
	exact.MaxLeaves = snapshot.root.treeSize
	exact.MaxTotalLeafBytes = snapshot.totalBytes
	exact.MaxRetainedNodes = uint64(len(snapshot.nodes))
	exact.MaxTraversalDepth = uint64(bits.Len64(snapshot.root.treeSize))
	exact.MaxNodeReads = uint64(len(snapshot.nodes))
	exact.MaxTemporaryBytes =
		uint64(len(snapshot.nodes)) * encodedSnapshotNodeSize
	if _, err := ParseSnapshot(
		context.Background(),
		data,
		exact,
	); err != nil {
		t.Fatalf("ParseSnapshot(exact limits) error = %v", err)
	}

	tests := []struct {
		name string
		kind ResourceKind
		edit func(*SnapshotPersistenceLimits)
	}{
		{
			name: "leaves",
			kind: ResourceLeaves,
			edit: func(limits *SnapshotPersistenceLimits) {
				limits.MaxLeaves = snapshot.root.treeSize - 1
			},
		},
		{
			name: "total bytes",
			kind: ResourceTotalBytes,
			edit: func(limits *SnapshotPersistenceLimits) {
				limits.MaxTotalLeafBytes = snapshot.totalBytes - 1
			},
		},
		{
			name: "retained nodes",
			kind: ResourceRetainedNodes,
			edit: func(limits *SnapshotPersistenceLimits) {
				limits.MaxRetainedNodes = uint64(len(snapshot.nodes) - 1)
			},
		},
		{
			name: "node reads",
			kind: ResourceNodeReads,
			edit: func(limits *SnapshotPersistenceLimits) {
				limits.MaxNodeReads = uint64(len(snapshot.nodes) - 1)
			},
		},
		{
			name: "depth",
			kind: ResourceTraversalDepth,
			edit: func(limits *SnapshotPersistenceLimits) {
				limits.MaxTraversalDepth =
					uint64(bits.Len64(snapshot.root.treeSize) - 1)
			},
		},
		{
			name: "temporary bytes",
			kind: ResourceTemporaryBytes,
			edit: func(limits *SnapshotPersistenceLimits) {
				limits.MaxTemporaryBytes =
					uint64(len(snapshot.nodes))*encodedSnapshotNodeSize - 1
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultSnapshotPersistenceLimits()
			test.edit(&limits)
			_, err := ParseSnapshot(context.Background(), data, limits)
			assertResourceKind(t, err, test.kind)
		})
	}
}

func TestSnapshotPersistenceRejectsCorruptMetadataAndNodes(t *testing.T) {
	t.Parallel()

	_, data := persistedSnapshotFixture(t)
	mutations := []struct {
		name   string
		mutate func([]byte)
	}{
		{
			name: "tree size",
			mutate: func(value []byte) {
				binary.BigEndian.PutUint64(value[10:18], 4)
			},
		},
		{
			name: "node count",
			mutate: func(value []byte) {
				binary.BigEndian.PutUint64(value[58:66], 4)
			},
		},
		{
			name: "root node",
			mutate: func(value []byte) {
				binary.BigEndian.PutUint64(value[66:74], 3)
			},
		},
		{
			name: "leaf size",
			mutate: func(value []byte) {
				binary.BigEndian.PutUint64(value[106:114], 0)
			},
		},
		{
			name: "leaf child",
			mutate: func(value []byte) {
				binary.BigEndian.PutUint64(value[114:122], 0)
			},
		},
		{
			name: "branch digest",
			mutate: func(value []byte) {
				value[186] ^= 1
			},
		},
		{
			name: "branch size",
			mutate: func(value []byte) {
				binary.BigEndian.PutUint64(value[218:226], 3)
			},
		},
		{
			name: "branch left",
			mutate: func(value []byte) {
				binary.BigEndian.PutUint64(value[226:234], 1)
			},
		},
		{
			name: "branch right",
			mutate: func(value []byte) {
				binary.BigEndian.PutUint64(value[234:242], 0)
			},
		},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			mutated := append([]byte(nil), data...)
			test.mutate(mutated)
			if _, err := ParseSnapshot(
				context.Background(),
				mutated,
				DefaultSnapshotPersistenceLimits(),
			); !errors.Is(err, ErrMalformedEncoding) {
				t.Fatalf("ParseSnapshot() error = %v", err)
			}
		})
	}

	overflow := append([]byte(nil), data...)
	binary.BigEndian.PutUint64(overflow[58:66], math.MaxUint64)
	unbounded := SnapshotPersistenceLimits{
		MaxEncodedBytes:   math.MaxUint64,
		MaxLeaves:         math.MaxUint64,
		MaxTotalLeafBytes: math.MaxUint64,
		MaxRetainedNodes:  math.MaxUint64,
		MaxTraversalDepth: math.MaxUint64,
		MaxNodeReads:      math.MaxUint64,
		MaxTemporaryBytes: math.MaxUint64,
	}
	if _, err := ParseSnapshot(
		context.Background(),
		overflow,
		unbounded,
	); !errors.Is(err, ErrMalformedEncoding) {
		t.Fatalf("ParseSnapshot(overflow) error = %v", err)
	}
}

func TestSnapshotPersistenceCancellationDuringNodesAndValidation(t *testing.T) {
	t.Parallel()

	snapshot, data := persistedSnapshotFixture(t)
	contexts := []*checkpointContext{
		{remaining: 1, done: make(chan struct{})},
		{
			remaining: len(snapshot.nodes) + 1,
			done:      make(chan struct{}),
		},
	}
	for index, ctx := range contexts {
		if _, err := ParseSnapshot(
			ctx,
			data,
			DefaultSnapshotPersistenceLimits(),
		); !errors.Is(err, context.Canceled) {
			t.Fatalf("ParseSnapshot(cancel %d) error = %v", index, err)
		}
	}
}

func TestSnapshotPersistenceEmptyMetadataAndInternalCorruption(t *testing.T) {
	t.Parallel()

	empty, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		nil,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	data := mustMarshalProof(t, empty.MarshalBinary)
	for name, mutate := range map[string]func([]byte){
		"total bytes": func(value []byte) {
			binary.BigEndian.PutUint64(value[18:26], 1)
		},
		"root digest": func(value []byte) {
			value[26] ^= 1
		},
		"root node": func(value []byte) {
			binary.BigEndian.PutUint64(value[66:74], 0)
		},
	} {
		t.Run(name, func(t *testing.T) {
			mutated := append([]byte(nil), data...)
			mutate(mutated)
			if _, err := ParseSnapshot(
				context.Background(),
				mutated,
				DefaultSnapshotPersistenceLimits(),
			); !errors.Is(err, ErrMalformedEncoding) {
				t.Fatalf("ParseSnapshot() error = %v", err)
			}
		})
	}

	snapshot, _ := persistedSnapshotFixture(t)
	corruptions := []func(*Snapshot){
		func(value *Snapshot) { value.nodes[0].left = 0 },
		func(value *Snapshot) { value.nodes[len(value.nodes)-1].digest[0] ^= 1 },
		func(value *Snapshot) { value.rootNode-- },
	}
	for index, corrupt := range corruptions {
		value := snapshot
		value.nodes = append([]snapshotNode(nil), snapshot.nodes...)
		corrupt(&value)
		if _, err := value.MarshalBinary(); !errors.Is(
			err,
			ErrInvalidSnapshot,
		) {
			t.Fatalf("MarshalBinary(corrupt %d) error = %v", index, err)
		}
	}

	var zero Snapshot
	if _, err := zero.TotalLeafBytes(); !errors.Is(
		err,
		ErrInvalidSnapshot,
	) {
		t.Fatalf("TotalLeafBytes(zero) error = %v", err)
	}
}

func TestResumeBuilderValidationCancellationAndLimits(t *testing.T) {
	t.Parallel()

	snapshot, _ := persistedSnapshotFixture(t)
	if _, err := ResumeBuilder(
		nil,
		snapshot,
		snapshot.totalBytes,
		DefaultSnapshotLimits(),
	); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("ResumeBuilder(nil context) error = %v", err)
	}
	if _, err := ResumeBuilder(
		context.Background(),
		snapshot,
		snapshot.totalBytes,
		SnapshotLimits{},
	); !errors.Is(err, ErrInvalidLimits) {
		t.Fatalf("ResumeBuilder(invalid limits) error = %v", err)
	}
	var zero Snapshot
	if _, err := ResumeBuilder(
		context.Background(),
		zero,
		0,
		DefaultSnapshotLimits(),
	); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("ResumeBuilder(zero) error = %v", err)
	}
	corrupt := snapshot
	corrupt.nodes = append([]snapshotNode(nil), snapshot.nodes...)
	corrupt.nodes[0].left = 0
	if _, err := ResumeBuilder(
		context.Background(),
		corrupt,
		corrupt.totalBytes,
		DefaultSnapshotLimits(),
	); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("ResumeBuilder(corrupt) error = %v", err)
	}
	ctx := &checkpointContext{remaining: 0, done: make(chan struct{})}
	if _, err := ResumeBuilder(
		ctx,
		snapshot,
		snapshot.totalBytes,
		DefaultSnapshotLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("ResumeBuilder(cancelled) error = %v", err)
	}

	leafLimits := DefaultSnapshotLimits()
	leafLimits.Construction.MaxLeaves = snapshot.root.treeSize - 1
	_, err := ResumeBuilder(
		context.Background(),
		snapshot,
		snapshot.totalBytes,
		leafLimits,
	)
	assertResourceKind(t, err, ResourceLeaves)

	byteLimits := DefaultSnapshotLimits()
	byteLimits.Construction.MaxTotalBytes = snapshot.totalBytes - 1
	_, err = ResumeBuilder(
		context.Background(),
		snapshot,
		snapshot.totalBytes,
		byteLimits,
	)
	assertResourceKind(t, err, ResourceTotalBytes)

	nodeLimits := DefaultSnapshotLimits()
	nodeLimits.MaxRetainedNodes = uint64(len(snapshot.nodes) - 1)
	_, err = ResumeBuilder(
		context.Background(),
		snapshot,
		snapshot.totalBytes,
		nodeLimits,
	)
	assertResourceKind(t, err, ResourceRetainedNodes)

	leafCancel := &checkpointContext{
		remaining: len(snapshot.nodes) + 1,
		done:      make(chan struct{}),
	}
	if _, err := ResumeBuilder(
		leafCancel,
		snapshot,
		snapshot.totalBytes,
		DefaultSnapshotLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("ResumeBuilder(leaf cancelled) error = %v", err)
	}

	if _, err := ResumeBuilder(
		context.Background(),
		snapshot,
		snapshot.totalBytes+1,
		DefaultSnapshotLimits(),
	); !errors.Is(err, ErrSnapshotAccountingMismatch) {
		t.Fatalf("ResumeBuilder(accounting mismatch) error = %v", err)
	}

	exact := DefaultSnapshotLimits()
	exact.Construction.MaxLeaves = snapshot.root.treeSize
	exact.Construction.MaxTotalBytes = snapshot.totalBytes
	exact.MaxRetainedNodes = uint64(len(snapshot.nodes))
	resumed, err := ResumeBuilder(
		context.Background(),
		snapshot,
		snapshot.totalBytes,
		exact,
	)
	if err != nil {
		t.Fatalf("ResumeBuilder(exact limits) error = %v", err)
	}
	if err := resumed.validate(); err != nil {
		t.Fatalf("resumed.validate() error = %v", err)
	}
	resumedSnapshot, err := resumed.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("resumed.Snapshot() error = %v", err)
	}
	resumedRoot, _ := resumedSnapshot.Root()
	if resumedRoot.digest != snapshot.root.digest {
		t.Fatal("resumed builder changed the current root")
	}
}

func TestSnapshotStructureLimitsAndCounts(t *testing.T) {
	t.Parallel()

	snapshot, _ := persistedSnapshotFixture(t)
	if err := validateSnapshotStructure(
		context.Background(),
		snapshot,
		1,
		math.MaxUint64,
	); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("validateSnapshotStructure(depth) error = %v", err)
	}
	err := validateSnapshotStructure(
		context.Background(),
		snapshot,
		math.MaxUint64,
		1,
	)
	assertResourceKind(t, err, ResourceNodeReads)

	if validSnapshotNodeCount(math.MaxUint64, math.MaxUint64) {
		t.Fatal("validSnapshotNodeCount() accepted overflowed tree size")
	}
	if validSnapshotNodeCount(0, 1) {
		t.Fatal("validSnapshotNodeCount() accepted empty tree nodes")
	}
	if !validSnapshotNodeCount(math.MaxUint64/2+1, math.MaxUint64) {
		t.Fatal("validSnapshotNodeCount() rejected exact uint64 boundary")
	}
	missing := snapshot
	missing.nodes = missing.nodes[:len(missing.nodes)-1]
	if err := validateSnapshotStructure(
		context.Background(),
		missing,
		math.MaxUint64,
		math.MaxUint64,
	); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("validateSnapshotStructure(missing node) error = %v", err)
	}

	badSize := snapshot
	badSize.nodes = append([]snapshotNode(nil), snapshot.nodes...)
	badSize.nodes[len(badSize.nodes)-1].size = 0
	if err := validateSnapshotStructure(
		context.Background(),
		badSize,
		math.MaxUint64,
		math.MaxUint64,
	); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("validateSnapshotStructure(zero branch) error = %v", err)
	}

	rightSize := snapshot
	rightSize.nodes = append([]snapshotNode(nil), snapshot.nodes...)
	rightSize.nodes[3].size = 2
	if err := validateSnapshotStructure(
		context.Background(),
		rightSize,
		math.MaxUint64,
		math.MaxUint64,
	); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("validateSnapshotStructure(right size) error = %v", err)
	}

	one, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		[]RawLeaf{NewRawLeaf([]byte("one"))},
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(one) error = %v", err)
	}
	one.root.digest.value[0] ^= 1
	if err := validateSnapshotStructure(
		context.Background(),
		one,
		math.MaxUint64,
		math.MaxUint64,
	); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("validateSnapshotStructure(one root) error = %v", err)
	}
}

func persistedSnapshotFixture(t *testing.T) (Snapshot, []byte) {
	t.Helper()

	leaves := []RawLeaf{
		NewRawLeaf([]byte("a")),
		NewRawLeaf([]byte("bb")),
		NewRawLeaf([]byte("ccc")),
	}
	snapshot, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves,
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	data, err := snapshot.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}

	return snapshot, data
}

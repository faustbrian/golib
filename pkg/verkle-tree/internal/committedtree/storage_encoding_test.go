package committedtree

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
)

func TestStorageImageOwnsCanonicalContentAddressedNodes(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(2, 129), Value: testValue(3)},
		{Key: testKey(1, 0), Value: testValue(1)},
		{Key: testKey(1, 128), Value: testValue(2)},
	}
	tree, err := Build(
		context.Background(),
		entries,
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	image, err := tree.StorageImage(
		context.Background(),
		testStorageEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("StorageImage() error = %v", err)
	}
	rootID, err := image.RootID()
	if err != nil {
		t.Fatalf("RootID() error = %v", err)
	}
	nodes, err := image.Nodes(context.Background())
	if err != nil {
		t.Fatalf("Nodes() error = %v", err)
	}
	if got, want := len(nodes), 3; got != want {
		t.Fatalf("node count = %d, want %d", got, want)
	}

	rootPresent := false
	for index := range nodes {
		encoded := nodes[index].Encoded()
		id := nodes[index].ID()
		if got, want := id, StorageNodeID(sha256.Sum256(encoded)); got != want {
			t.Fatalf("node %d ID = %x, want %x", index, got, want)
		}
		if index > 0 {
			previous := nodes[index-1].ID()
			if bytes.Compare(previous[:], id[:]) >= 0 {
				t.Fatalf("node IDs are not strictly ordered at %d", index)
			}
		}
		if id == rootID {
			rootPresent = true
		}
		if len(encoded) < storageNodeHeaderBytes ||
			!bytes.Equal(encoded[:len(storageNodeMagic)], storageNodeMagic[:]) {
			t.Fatalf("node %d has invalid canonical header", index)
		}
		encoded[0] ^= 0xff
		if slices.Equal(encoded, nodes[index].Encoded()) {
			t.Fatalf("node %d encoding aliases retained storage", index)
		}
	}
	if !rootPresent {
		t.Fatalf("root ID %x is absent from image", rootID)
	}

	nodes[0].encoded[0] ^= 0xff
	again, err := image.Nodes(context.Background())
	if err != nil {
		t.Fatalf("second Nodes() error = %v", err)
	}
	if !bytes.Equal(again[0].encoded[:len(storageNodeMagic)], storageNodeMagic[:]) {
		t.Fatal("Nodes() returned aliases to retained image storage")
	}
}

func TestStorageImageIsDeterministicAcrossInputOrder(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(1, 0), Value: testValue(1)},
		{Key: testKey(2, 1), Value: testValue(2)},
	}
	reversed := slices.Clone(entries)
	slices.Reverse(reversed)

	left := mustStorageImage(t, entries)
	right := mustStorageImage(t, reversed)
	leftRoot, _ := left.RootID()
	rightRoot, _ := right.RootID()
	if leftRoot != rightRoot {
		t.Fatalf("root IDs differ: %x != %x", leftRoot, rightRoot)
	}
	leftNodes, _ := left.Nodes(context.Background())
	rightNodes, _ := right.Nodes(context.Background())
	if len(leftNodes) != len(rightNodes) {
		t.Fatalf("node counts differ: %d != %d", len(leftNodes), len(rightNodes))
	}
	for index := range leftNodes {
		if leftNodes[index].ID() != rightNodes[index].ID() ||
			!slices.Equal(leftNodes[index].Encoded(), rightNodes[index].Encoded()) {
			t.Fatalf("node %d differs across input order", index)
		}
	}
}

func TestStorageImageEncodesEmptyTreeAsOneAddressableNode(t *testing.T) {
	t.Parallel()

	image := mustStorageImage(t, nil)
	nodes, err := image.Nodes(context.Background())
	if err != nil {
		t.Fatalf("Nodes() error = %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("empty tree node count = %d, want 1", len(nodes))
	}
	rootID, _ := image.RootID()
	if rootID != nodes[0].ID() {
		t.Fatalf("empty root ID = %x, node ID = %x", rootID, nodes[0].ID())
	}
	wantEncoding, err := hex.DecodeString(
		"564b4e44010000000101000000000000000000000000000000000000000000000000000000000000000000000000",
	)
	if err != nil {
		t.Fatalf("decode expected encoding: %v", err)
	}
	if !slices.Equal(nodes[0].Encoded(), wantEncoding) {
		t.Fatalf("empty node encoding = %x, want %x", nodes[0].Encoded(), wantEncoding)
	}
	wantID, err := hex.DecodeString(
		"0a267fedc4c869edcabbddf34a5cc64ece3dbf22c85700e925d7cc8190de34f0",
	)
	if err != nil {
		t.Fatalf("decode expected ID: %v", err)
	}
	if !slices.Equal(rootID[:], wantID) {
		t.Fatalf("empty root ID = %x, want %x", rootID, wantID)
	}
}

func TestStorageImageAcceptsExactEmptyTreeResourceBounds(t *testing.T) {
	t.Parallel()

	tree := mustStorageTree(t, nil)
	exact := StorageEncodingLimits{
		MaxNodes:          1,
		MaxNodeBytes:      46,
		MaxEncodedBytes:   46,
		MaxHashes:         1,
		MaxTemporaryBytes: 348,
	}
	if _, err := tree.StorageImage(context.Background(), exact); err != nil {
		t.Fatalf("exact StorageImage() error = %v", err)
	}

	tests := map[string]struct {
		resource StorageEncodingResource
		actual   uint64
		mutate   func(*StorageEncodingLimits)
	}{
		"node bytes": {
			resource: StorageEncodingResourceNodeBytes,
			actual:   46,
			mutate: func(limits *StorageEncodingLimits) {
				limits.MaxNodeBytes = 45
			},
		},
		"encoded bytes": {
			resource: StorageEncodingResourceEncodedBytes,
			actual:   46,
			mutate: func(limits *StorageEncodingLimits) {
				limits.MaxEncodedBytes = 45
			},
		},
		"temporary bytes": {
			resource: StorageEncodingResourceTemporaryBytes,
			actual:   348,
			mutate: func(limits *StorageEncodingLimits) {
				limits.MaxTemporaryBytes = 347
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			limits := exact
			test.mutate(&limits)
			_, err := tree.StorageImage(context.Background(), limits)
			var resourceErr *StorageEncodingResourceError
			if !errors.As(err, &resourceErr) ||
				resourceErr.Resource != test.resource ||
				resourceErr.Actual != test.actual {
				t.Fatalf("StorageImage() error = %v, want resource %d actual %d", err, test.resource, test.actual)
			}
		})
	}
}

func TestStorageImageRejectsInvalidStateLimitsAndCancellation(t *testing.T) {
	t.Parallel()

	tree, err := Build(
		context.Background(),
		[]Entry{{Key: testKey(1, 0), Value: testValue(1)}},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if _, err := (Tree{}).StorageImage(
		context.Background(),
		testStorageEncodingLimits(),
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("zero tree error = %v, want errInvalidTree", err)
	}
	var nilContext context.Context
	if _, err := tree.StorageImage(nilContext, testStorageEncodingLimits()); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil context error = %v, want errInvalidContext", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tree.StorageImage(cancelled, testStorageEncodingLimits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled error = %v, want context.Canceled", err)
	}

	invalid := testStorageEncodingLimits()
	invalid.MaxNodes = 0
	if _, err := tree.StorageImage(context.Background(), invalid); !errors.Is(err, errInvalidStorageEncodingLimits) {
		t.Fatalf("invalid limits error = %v, want errInvalidStorageEncodingLimits", err)
	}

	tests := map[string]struct {
		resource StorageEncodingResource
		mutate   func(*StorageEncodingLimits)
	}{
		"nodes": {
			resource: StorageEncodingResourceNodes,
			mutate: func(limits *StorageEncodingLimits) {
				limits.MaxNodes = 1
			},
		},
		"node bytes": {
			resource: StorageEncodingResourceNodeBytes,
			mutate: func(limits *StorageEncodingLimits) {
				limits.MaxNodeBytes = 1
			},
		},
		"encoded bytes": {
			resource: StorageEncodingResourceEncodedBytes,
			mutate: func(limits *StorageEncodingLimits) {
				limits.MaxEncodedBytes = 1
			},
		},
		"hashes": {
			resource: StorageEncodingResourceHashes,
			mutate: func(limits *StorageEncodingLimits) {
				limits.MaxHashes = 1
			},
		},
		"temporary bytes": {
			resource: StorageEncodingResourceTemporaryBytes,
			mutate: func(limits *StorageEncodingLimits) {
				limits.MaxTemporaryBytes = 1
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			limits := testStorageEncodingLimits()
			test.mutate(&limits)
			_, imageErr := tree.StorageImage(context.Background(), limits)
			var resourceErr *StorageEncodingResourceError
			if !errors.As(imageErr, &resourceErr) || resourceErr.Resource != test.resource {
				t.Fatalf("StorageImage() error = %v, want resource %d", imageErr, test.resource)
			}
		})
	}
}

func TestStorageEncodingErrorsAndZeroValuesFailClosed(t *testing.T) {
	t.Parallel()

	resourceErr := &StorageEncodingResourceError{
		Resource: StorageEncodingResourceNodes,
		Limit:    1,
		Actual:   2,
	}
	if !errors.Is(resourceErr, errStorageEncodingResource) ||
		resourceErr.Error() == "" {
		t.Fatalf("resource error = %v", resourceErr)
	}

	var image StorageImage
	if _, err := image.RootID(); !errors.Is(err, errInvalidStorageImage) {
		t.Fatalf("zero RootID() error = %v", err)
	}
	if _, err := image.Nodes(context.Background()); !errors.Is(err, errInvalidStorageImage) {
		t.Fatalf("zero Nodes() error = %v", err)
	}
	invalidFlag := StorageImage{
		nodes: []StorageNode{{encoded: []byte{1}}},
	}
	if _, err := invalidFlag.RootID(); !errors.Is(err, errInvalidStorageImage) {
		t.Fatalf("invalid-flag RootID() error = %v", err)
	}
	if _, err := invalidFlag.Nodes(context.Background()); !errors.Is(err, errInvalidStorageImage) {
		t.Fatalf("invalid-flag Nodes() error = %v", err)
	}
	missingNodes := StorageImage{valid: true}
	if _, err := missingNodes.RootID(); !errors.Is(err, errInvalidStorageImage) {
		t.Fatalf("missing-nodes RootID() error = %v", err)
	}
	if _, err := missingNodes.Nodes(context.Background()); !errors.Is(err, errInvalidStorageImage) {
		t.Fatalf("missing-nodes Nodes() error = %v", err)
	}

	image = mustStorageImage(t, []Entry{
		{Key: testKey(1, 0), Value: testValue(1)},
		{Key: testKey(2, 0), Value: testValue(2)},
	})
	var nilContext context.Context
	if _, err := image.Nodes(nilContext); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil-context Nodes() error = %v", err)
	}
	if _, err := image.Nodes(&cancelContext{cancelAt: 2}); !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-copy Nodes() error = %v", err)
	}
}

func TestStorageEncodingLimitsRejectCountsBeyondSupportedBoundary(t *testing.T) {
	t.Parallel()

	boundary := StorageEncodingLimits{
		MaxNodes:          maxSupportedCount,
		MaxNodeBytes:      1,
		MaxEncodedBytes:   1,
		MaxHashes:         maxSupportedCount,
		MaxTemporaryBytes: 1,
	}
	if err := boundary.validate(); err != nil {
		t.Fatalf("boundary validate() error = %v", err)
	}

	excessiveNodes := boundary
	excessiveNodes.MaxNodes++
	if err := excessiveNodes.validate(); !errors.Is(err, errInvalidStorageEncodingLimits) {
		t.Fatalf("excessive-node validate() error = %v", err)
	}
	excessiveHashes := boundary
	excessiveHashes.MaxHashes++
	if err := excessiveHashes.validate(); !errors.Is(err, errInvalidStorageEncodingLimits) {
		t.Fatalf("excessive-hash validate() error = %v", err)
	}
}

func TestStorageImageAccountsExactMultiNodeBytes(t *testing.T) {
	t.Parallel()

	tree := mustStorageTree(t, []Entry{
		{Key: testKey(1, 0), Value: testValue(1)},
	})
	exact := StorageEncodingLimits{
		MaxNodes:          2,
		MaxNodeBytes:      176,
		MaxEncodedBytes:   255,
		MaxHashes:         2,
		MaxTemporaryBytes: 1022,
	}
	if _, err := tree.StorageImage(context.Background(), exact); err != nil {
		t.Fatalf("exact StorageImage() error = %v", err)
	}

	encodedShort := exact
	encodedShort.MaxEncodedBytes--
	_, err := tree.StorageImage(context.Background(), encodedShort)
	assertStorageResourceError(
		t,
		err,
		StorageEncodingResourceEncodedBytes,
		255,
	)

	temporaryShort := exact
	temporaryShort.MaxTemporaryBytes--
	_, err = tree.StorageImage(context.Background(), temporaryShort)
	assertStorageResourceError(
		t,
		err,
		StorageEncodingResourceTemporaryBytes,
		1022,
	)
}

func TestStorageNodeSizeRejectsEveryCorruptArenaInvariant(t *testing.T) {
	t.Parallel()

	base := mustStorageTree(t, []Entry{
		{Key: testKey(1, 0), Value: testValue(1)},
		{Key: testKey(2, 0), Value: testValue(2)},
	})
	if _, err := base.storageNodeSize(uint32(len(base.nodes))); !errors.Is(err, errInvalidTree) {
		t.Fatalf("out-of-range node error = %v", err)
	}

	tests := map[string]func(*Tree){
		"unknown kind": func(tree *Tree) {
			tree.nodes[0].kind = 0
		},
		"internal edge range": func(tree *Tree) {
			tree.nodes[tree.root].firstEdge = uint32(len(tree.edges))
		},
		"internal invalid commitment": func(tree *Tree) {
			tree.nodes[tree.root].commitment = backend.VectorCommitment{}
		},
		"internal identity with edges": func(tree *Tree) {
			tree.nodes[tree.root].commitment = backend.EmptyVectorCommitment()
		},
		"internal edge order": func(tree *Tree) {
			root := tree.nodes[tree.root]
			tree.edges[root.firstEdge+1].index = tree.edges[root.firstEdge].index
		},
		"internal child not preceding parent": func(tree *Tree) {
			root := tree.nodes[tree.root]
			tree.edges[root.firstEdge].child = tree.root
		},
		"stem zero depth": func(tree *Tree) {
			tree.nodes[0].depth = 0
		},
		"stem excessive depth": func(tree *Tree) {
			tree.nodes[0].depth = 32
		},
		"stem entry range": func(tree *Tree) {
			tree.nodes[0].entryStart = uint32(len(tree.entries))
		},
		"stem empty entries": func(tree *Tree) {
			tree.nodes[0].entryCount = 0
		},
		"stem edges": func(tree *Tree) {
			tree.nodes[0].edgeCount = 1
		},
		"stem identity commitment": func(tree *Tree) {
			tree.nodes[0].commitment = backend.EmptyVectorCommitment()
		},
		"stem invalid commitment": func(tree *Tree) {
			tree.nodes[0].commitment = backend.VectorCommitment{}
		},
		"stem invalid c1": func(tree *Tree) {
			tree.nodes[0].c1 = backend.VectorCommitment{}
		},
		"stem invalid c2": func(tree *Tree) {
			tree.nodes[0].c2 = backend.VectorCommitment{}
		},
		"stem mismatch": func(tree *Tree) {
			tree.entries[0].Key[1] ^= 1
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			tree := cloneStorageTree(base)
			mutate(&tree)
			index := uint32(0)
			if name[:8] == "internal" {
				index = tree.root
			}
			if _, err := tree.storageNodeSize(index); !errors.Is(err, errInvalidTree) {
				t.Fatalf("storageNodeSize() error = %v, want errInvalidTree", err)
			}
		})
	}

	duplicate := mustStorageTree(t, []Entry{
		{Key: testKey(1, 0), Value: testValue(1)},
		{Key: testKey(1, 1), Value: testValue(2)},
	})
	duplicate.entries[1].Key[31] = duplicate.entries[0].Key[31]
	if _, err := duplicate.storageNodeSize(0); !errors.Is(err, errInvalidTree) {
		t.Fatalf("duplicate suffix error = %v", err)
	}
}

func TestStorageImageRejectsEveryCorruptTopologyInvariant(t *testing.T) {
	t.Parallel()

	twoStems := mustStorageTree(t, []Entry{
		{Key: testKey(1, 0), Value: testValue(1)},
		{Key: testKey(2, 0), Value: testValue(2)},
	})
	oneStem := mustStorageTree(t, []Entry{
		{Key: testKey(1, 0), Value: testValue(1)},
	})
	tests := map[string]struct {
		base   Tree
		mutate func(*Tree)
	}{
		"root is not canonical last node": {
			base: twoStems,
			mutate: func(tree *Tree) {
				tree.root = 0
			},
		},
		"root depth": {
			base: twoStems,
			mutate: func(tree *Tree) {
				tree.nodes[tree.root].depth = 1
			},
		},
		"child depth": {
			base: oneStem,
			mutate: func(tree *Tree) {
				tree.nodes[0].depth = 2
			},
		},
		"edge path": {
			base: oneStem,
			mutate: func(tree *Tree) {
				tree.edges[0].index = 2
			},
		},
		"duplicate child": {
			base: twoStems,
			mutate: func(tree *Tree) {
				tree.edges[1].child = tree.edges[0].child
			},
		},
		"surplus edge": {
			base: oneStem,
			mutate: func(tree *Tree) {
				tree.edges = append(tree.edges, tree.edges[0])
			},
		},
		"surplus entry": {
			base: oneStem,
			mutate: func(tree *Tree) {
				tree.entries = append(tree.entries, tree.entries[0])
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			tree := cloneStorageTree(test.base)
			test.mutate(&tree)
			if _, err := tree.StorageImage(
				context.Background(),
				testStorageEncodingLimits(),
			); !errors.Is(err, errInvalidTree) {
				t.Fatalf("StorageImage() error = %v, want errInvalidTree", err)
			}
		})
	}

	t.Run("internal excessive depth", func(t *testing.T) {
		tree := cloneStorageTree(oneStem)
		tree.nodes[0] = node{
			kind:      nodeInternal,
			depth:     31,
			edgeCount: 1,
		}
		nodeCursor := uint64(0)
		edgeCursor := uint64(0)
		entryCursor := uint64(0)
		if err := tree.validateStorageSubtree(
			context.Background(),
			0,
			31,
			[31]byte{},
			&nodeCursor,
			&edgeCursor,
			&entryCursor,
		); !errors.Is(err, errInvalidTree) {
			t.Fatalf("validateStorageSubtree() error = %v, want errInvalidTree", err)
		}
	})
	t.Run("empty internal below root", func(t *testing.T) {
		tree := cloneStorageTree(oneStem)
		tree.nodes[0] = node{
			kind:  nodeInternal,
			depth: 1,
		}
		nodeCursor := uint64(0)
		edgeCursor := uint64(0)
		entryCursor := uint64(0)
		if err := tree.validateStorageSubtree(
			context.Background(),
			0,
			1,
			[31]byte{},
			&nodeCursor,
			&edgeCursor,
			&entryCursor,
		); !errors.Is(err, errInvalidTree) {
			t.Fatalf("validateStorageSubtree() error = %v, want errInvalidTree", err)
		}
	})
}

func TestStorageTreeValidationRejectsEachInvalidStateIndependently(t *testing.T) {
	t.Parallel()

	validCommitment := backend.EmptyVectorCommitment()
	tests := map[string]Tree{
		"invalid flag": {
			nodes: []node{{kind: nodeInternal, commitment: validCommitment}},
			root:  0,
		},
		"missing nodes": {
			root:  0,
			valid: true,
		},
		"root range": {
			nodes: []node{{kind: nodeInternal, commitment: validCommitment}},
			root:  1,
			valid: true,
		},
	}
	for name, tree := range tests {
		t.Run(name, func(t *testing.T) {
			if err := tree.validateStorageTree(); !errors.Is(err, errInvalidTree) {
				t.Fatalf("validateStorageTree() error = %v, want errInvalidTree", err)
			}
		})
	}
}

func TestStorageTopologyRejectsEachRootInvariantIndependently(t *testing.T) {
	t.Parallel()

	twoStems := mustStorageTree(t, []Entry{
		{Key: testKey(1, 0), Value: testValue(1)},
		{Key: testKey(2, 0), Value: testValue(2)},
	})
	left := testKey(1, 0)
	right := testKey(1, 0)
	right[1] = 2
	nested := mustStorageTree(t, []Entry{
		{Key: left, Value: testValue(1)},
		{Key: right, Value: testValue(2)},
	})
	tests := map[string]struct {
		base   Tree
		mutate func(*Tree)
	}{
		"root position": {
			base: nested,
			mutate: func(tree *Tree) {
				tree.root--
				tree.nodes[tree.root].depth = 0
			},
		},
		"edge count": {
			base: twoStems,
			mutate: func(tree *Tree) {
				tree.edges = append(tree.edges, tree.edges[0])
			},
		},
		"root kind": {
			base: twoStems,
			mutate: func(tree *Tree) {
				tree.nodes[tree.root].kind = nodeStem
			},
		},
		"root depth": {
			base: twoStems,
			mutate: func(tree *Tree) {
				tree.nodes[tree.root].depth = 1
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			tree := cloneStorageTree(test.base)
			test.mutate(&tree)
			if err := tree.validateStorageTopology(
				context.Background(),
			); !errors.Is(err, errInvalidTree) {
				t.Fatalf(
					"validateStorageTopology() error = %v, want errInvalidTree",
					err,
				)
			}
		})
	}
}

func TestStorageImageAcceptsNestedCanonicalTopology(t *testing.T) {
	t.Parallel()

	left := testKey(1, 0)
	right := testKey(1, 0)
	right[1] = 2
	tree := mustStorageTree(t, []Entry{
		{Key: left, Value: testValue(1)},
		{Key: right, Value: testValue(2)},
	})
	if _, err := tree.StorageImage(
		context.Background(),
		testStorageEncodingLimits(),
	); err != nil {
		t.Fatalf("StorageImage() error = %v", err)
	}
}

func TestStorageImageAcceptsMaximumCanonicalDepth(t *testing.T) {
	t.Parallel()

	var left Key
	right := left
	right[30] = 1
	tree := mustStorageTree(t, []Entry{
		{Key: left, Value: testValue(1)},
		{Key: right, Value: testValue(2)},
	})
	if _, err := tree.StorageImage(
		context.Background(),
		testStorageEncodingLimits(),
	); err != nil {
		t.Fatalf("StorageImage() error = %v", err)
	}
}

func TestValidateStorageSubtreeRejectsDefensiveInvalidState(t *testing.T) {
	t.Parallel()

	base := mustStorageTree(t, []Entry{
		{Key: testKey(1, 0), Value: testValue(1)},
	})
	tests := map[string]func(*Tree, *uint32, *uint64, *uint64, *uint64){
		"node range": func(
			tree *Tree,
			index *uint32,
			_ *uint64,
			_ *uint64,
			_ *uint64,
		) {
			*index = uint32(len(tree.nodes))
		},
		"edge cursor": func(
			tree *Tree,
			index *uint32,
			_ *uint64,
			edgeCursor *uint64,
			_ *uint64,
		) {
			*index = tree.root
			*edgeCursor = 1
		},
		"edge range": func(
			tree *Tree,
			index *uint32,
			_ *uint64,
			_ *uint64,
			_ *uint64,
		) {
			*index = tree.root
			tree.nodes[tree.root].edgeCount = 2
		},
		"entry range": func(
			tree *Tree,
			_ *uint32,
			_ *uint64,
			_ *uint64,
			entryCursor *uint64,
		) {
			tree.nodes[0].entryStart = 1
			*entryCursor = 1
		},
		"unknown kind": func(
			tree *Tree,
			_ *uint32,
			_ *uint64,
			_ *uint64,
			_ *uint64,
		) {
			tree.nodes[0].kind = 0
		},
		"node cursor": func(
			_ *Tree,
			_ *uint32,
			nodeCursor *uint64,
			_ *uint64,
			_ *uint64,
		) {
			*nodeCursor = 1
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			tree := cloneStorageTree(base)
			index := uint32(0)
			nodeCursor := uint64(0)
			edgeCursor := uint64(0)
			entryCursor := uint64(0)
			mutate(
				&tree,
				&index,
				&nodeCursor,
				&edgeCursor,
				&entryCursor,
			)
			var prefix [31]byte
			prefix[0] = 1
			depth := uint8(1)
			if index == tree.root {
				prefix = [31]byte{}
				depth = 0
			}
			if err := tree.validateStorageSubtree(
				context.Background(),
				index,
				depth,
				prefix,
				&nodeCursor,
				&edgeCursor,
				&entryCursor,
			); !errors.Is(err, errInvalidTree) {
				t.Fatalf(
					"validateStorageSubtree() error = %v, want errInvalidTree",
					err,
				)
			}
		})
	}
}

func TestStorageEncodingHelpersFailClosedAndRemainCancellable(t *testing.T) {
	t.Parallel()

	tree := mustStorageTree(t, []Entry{
		{Key: testKey(1, 0), Value: testValue(1)},
		{Key: testKey(2, 0), Value: testValue(2)},
	})
	sizes, _, err := tree.storageNodeSizes(
		context.Background(),
		testStorageEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("storageNodeSizes() error = %v", err)
	}
	var nilContext context.Context
	if _, err := tree.encodeStorageNode(
		nilContext,
		0,
		sizes[0],
		make([]StorageNode, len(tree.nodes)),
	); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil-context encode error = %v", err)
	}

	corrupt := cloneStorageTree(tree)
	corrupt.nodes[0].commitment = backend.VectorCommitment{}
	if _, err := corrupt.encodeStorageNode(
		context.Background(),
		0,
		sizes[0],
		make([]StorageNode, len(tree.nodes)),
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("invalid commitment encode error = %v", err)
	}
	corrupt = cloneStorageTree(tree)
	corrupt.nodes[0].c1 = backend.VectorCommitment{}
	if _, err := corrupt.encodeStorageNode(
		context.Background(),
		0,
		sizes[0],
		make([]StorageNode, len(tree.nodes)),
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("invalid c1 encode error = %v", err)
	}
	corrupt = cloneStorageTree(tree)
	corrupt.nodes[0].c2 = backend.VectorCommitment{}
	if _, err := corrupt.encodeStorageNode(
		context.Background(),
		0,
		sizes[0],
		make([]StorageNode, len(tree.nodes)),
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("invalid c2 encode error = %v", err)
	}
	corrupt = cloneStorageTree(tree)
	corrupt.nodes[0].kind = 0
	if _, err := corrupt.encodeStorageNode(
		context.Background(),
		0,
		sizes[0],
		make([]StorageNode, len(tree.nodes)),
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("unknown kind encode error = %v", err)
	}
	if _, err := tree.encodeStorageNode(
		context.Background(),
		0,
		sizes[0]+1,
		make([]StorageNode, len(tree.nodes)),
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("wrong size encode error = %v", err)
	}

	target := make([]byte, storageCommitmentBytes)
	if _, err := encodeStorageCommitment(
		target,
		0,
		backend.VectorCommitment{},
	); err == nil {
		t.Fatal("invalid encodeStorageCommitment() error = nil")
	}
	if next, err := encodeStorageCommitment(
		target,
		0,
		backend.EmptyVectorCommitment(),
	); err != nil || next != storageCommitmentBytes {
		t.Fatalf("identity encode = (%d, %v)", next, err)
	}
	nonIdentityTarget := bytes.Repeat(
		[]byte{0xaa},
		storageCommitmentBytes+2,
	)
	wantCommitment, err := tree.nodes[0].commitment.DeduplicationKey()
	if err != nil {
		t.Fatalf("commitment key error = %v", err)
	}
	next, err := encodeStorageCommitment(
		nonIdentityTarget,
		1,
		tree.nodes[0].commitment,
	)
	if err != nil ||
		next != storageCommitmentBytes+1 ||
		nonIdentityTarget[0] != 0xaa ||
		nonIdentityTarget[next] != 0xaa ||
		nonIdentityTarget[1] != 1 ||
		!slices.Equal(
			nonIdentityTarget[2:next],
			wantCommitment[:],
		) {
		t.Fatalf(
			"non-identity encoding = (%x, %d, %v)",
			nonIdentityTarget,
			next,
			err,
		)
	}

	nodes := []StorageNode{{id: StorageNodeID{4}}, {id: StorageNodeID{3}}, {id: StorageNodeID{2}}, {id: StorageNodeID{1}}}
	for cancelAt := 1; cancelAt <= 40; cancelAt++ {
		candidate := slices.Clone(nodes)
		err := sortStorageNodes(
			&cancelContext{cancelAt: cancelAt},
			candidate,
			make([]StorageNode, len(candidate)),
			0,
			len(candidate),
		)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("sort cancellation %d error = %v", cancelAt, err)
		}
	}
}

func TestSortStorageNodesOrdersBoundariesAndPreservesEqualIDs(t *testing.T) {
	t.Parallel()

	tests := map[string][]StorageNode{
		"right exhausted": {
			{id: StorageNodeID{2}, encoded: []byte{2}},
			{id: StorageNodeID{1}, encoded: []byte{1}},
		},
		"left exhausted": {
			{id: StorageNodeID{1}, encoded: []byte{1}},
			{id: StorageNodeID{2}, encoded: []byte{2}},
			{id: StorageNodeID{3}, encoded: []byte{3}},
		},
	}
	for name, nodes := range tests {
		t.Run(name, func(t *testing.T) {
			if err := sortStorageNodes(
				context.Background(),
				nodes,
				make([]StorageNode, len(nodes)),
				0,
				len(nodes),
			); err != nil {
				t.Fatalf("sortStorageNodes() error = %v", err)
			}
			for index := range nodes {
				if got, want := nodes[index].id[0], byte(index+1); got != want {
					t.Fatalf("node %d ID = %d, want %d", index, got, want)
				}
			}
		})
	}

	equal := []StorageNode{
		{id: StorageNodeID{1}, encoded: []byte{1}},
		{id: StorageNodeID{1}, encoded: []byte{2}},
	}
	if err := sortStorageNodes(
		context.Background(),
		equal,
		make([]StorageNode, len(equal)),
		0,
		len(equal),
	); err != nil {
		t.Fatalf("equal sortStorageNodes() error = %v", err)
	}
	if equal[0].encoded[0] != 1 || equal[1].encoded[0] != 2 {
		t.Fatalf("equal-ID order = %v, want stable [1 2]", equal)
	}
}

func TestStorageImageRemainsCancellableAcrossEveryEncodingPhase(t *testing.T) {
	t.Parallel()

	tree := mustStorageTree(t, []Entry{
		{Key: testKey(1, 0), Value: testValue(1)},
		{Key: testKey(1, 1), Value: testValue(2)},
		{Key: testKey(2, 0), Value: testValue(3)},
	})
	for cancelAt := 1; cancelAt <= 160; cancelAt++ {
		_, err := tree.StorageImage(
			&cancelContext{cancelAt: cancelAt},
			testStorageEncodingLimits(),
		)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelAt %d error = %v", cancelAt, err)
		}
	}

	corrupt := cloneStorageTree(tree)
	corrupt.nodes[0].kind = 0
	if _, err := corrupt.StorageImage(
		context.Background(),
		testStorageEncodingLimits(),
	); !errors.Is(err, errInvalidTree) {
		t.Fatalf("corrupt StorageImage() error = %v", err)
	}
}

func mustStorageImage(t testing.TB, entries []Entry) StorageImage {
	t.Helper()

	tree, err := Build(
		context.Background(),
		entries,
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	image, err := tree.StorageImage(
		context.Background(),
		testStorageEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("StorageImage() error = %v", err)
	}

	return image
}

func mustStorageTree(t testing.TB, entries []Entry) Tree {
	t.Helper()

	tree, err := Build(
		context.Background(),
		entries,
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	return tree
}

func cloneStorageTree(tree Tree) Tree {
	tree.entries = slices.Clone(tree.entries)
	tree.nodes = slices.Clone(tree.nodes)
	tree.edges = slices.Clone(tree.edges)

	return tree
}

func testStorageEncodingLimits() StorageEncodingLimits {
	return StorageEncodingLimits{
		MaxNodes:          64,
		MaxNodeBytes:      1 << 20,
		MaxEncodedBytes:   1 << 20,
		MaxHashes:         64,
		MaxTemporaryBytes: 2 << 20,
	}
}

func assertStorageResourceError(
	t testing.TB,
	err error,
	resource StorageEncodingResource,
	actual uint64,
) {
	t.Helper()

	var resourceErr *StorageEncodingResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != resource ||
		resourceErr.Actual != actual {
		t.Fatalf(
			"error = %v, want resource %d actual %d",
			err,
			resource,
			actual,
		)
	}
}

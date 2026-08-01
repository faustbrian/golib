package committedtree

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

func TestDecodeStorageNodeRoundTripsCanonicalImage(t *testing.T) {
	t.Parallel()

	tree, err := Build(
		context.Background(),
		[]Entry{
			{Key: Key{0: 1, 30: 2, 31: 3}, Value: Value{0: 4}},
			{Key: Key{0: 1, 30: 2, 31: 5}, Value: Value{}},
			{Key: Key{0: 9, 31: 7}, Value: Value{31: 8}},
		},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	image, err := tree.StorageImage(context.Background(), testStorageEncodingLimits())
	if err != nil {
		t.Fatalf("StorageImage() error = %v", err)
	}

	seenInternal := false
	seenStem := false
	for _, encoded := range image.nodes {
		decoded, decodeErr := DecodeStorageNode(
			context.Background(),
			encoded.Encoded(),
			testStorageDecodingLimits(),
		)
		if decodeErr != nil {
			t.Fatalf("DecodeStorageNode() error = %v", decodeErr)
		}
		canonical, canonicalErr := decoded.Encoded(context.Background())
		if canonicalErr != nil {
			t.Fatalf("Encoded() error = %v", canonicalErr)
		}
		if !bytes.Equal(canonical, encoded.encoded) {
			t.Fatal("decoded node did not preserve canonical bytes")
		}
		if decoded.PointDecodes() > 3 {
			t.Fatalf("PointDecodes() = %d, want at most 3", decoded.PointDecodes())
		}
		if decoded.Depth() > 31 || decoded.RecordCount() == 0 && decoded.Kind() != StorageNodeKindInternal ||
			decoded.TemporaryBytes() == 0 {
			t.Fatalf(
				"decoded metadata depth=%d records=%d temporary=%d",
				decoded.Depth(),
				decoded.RecordCount(),
				decoded.TemporaryBytes(),
			)
		}
		switch decoded.Kind() {
		case StorageNodeKindInternal:
			seenInternal = true
			children, childErr := decoded.Children(context.Background())
			if childErr != nil {
				t.Fatalf("Children() error = %v", childErr)
			}
			if _, entryErr := decoded.Entries(context.Background()); !errors.Is(entryErr, ErrInvalidStorageNode) {
				t.Fatalf("Entries() error = %v, want ErrInvalidStorageNode", entryErr)
			}
			if len(children) > 0 {
				children[0].Index++
				again, againErr := decoded.Children(context.Background())
				if againErr != nil {
					t.Fatalf("Children() again error = %v", againErr)
				}
				if len(again) > 0 && again[0].Index == children[0].Index {
					t.Fatal("Children() returned aliased storage")
				}
			}
		case StorageNodeKindStem:
			seenStem = true
			entries, entryErr := decoded.Entries(context.Background())
			if entryErr != nil {
				t.Fatalf("Entries() error = %v", entryErr)
			}
			if _, childErr := decoded.Children(context.Background()); !errors.Is(childErr, ErrInvalidStorageNode) {
				t.Fatalf("Children() error = %v, want ErrInvalidStorageNode", childErr)
			}
			entries[0].Value[0]++
			again, againErr := decoded.Entries(context.Background())
			if againErr != nil {
				t.Fatalf("Entries() again error = %v", againErr)
			}
			if again[0].Value == entries[0].Value {
				t.Fatal("Entries() returned aliased storage")
			}
		default:
			t.Fatalf("Kind() = %d", decoded.Kind())
		}
	}
	if !seenInternal || !seenStem {
		t.Fatalf("decoded kinds internal=%t stem=%t", seenInternal, seenStem)
	}
}

func TestDecodeStorageNodeRejectsHostileEnvelopeBeforePointDecoding(t *testing.T) {
	t.Parallel()

	canonical := canonicalStorageNode(t)
	mutations := map[string]func([]byte){
		"magic":           func(value []byte) { value[0] ^= 0xff },
		"profile":         func(value []byte) { value[storageNodeMagicBytes]++ },
		"profile version": func(value []byte) { value[storageNodeMagicBytes+storageNodeProfileIDBytes+1]++ },
		"encoding version": func(value []byte) {
			value[storageNodeMagicBytes+storageNodeProfileIDBytes+storageNodeVersionBytes+1]++
		},
		"kind":          func(value []byte) { value[storageNodeHeaderBytes-storageCommitmentBytes-2] = 0xff },
		"trailing byte": func(value []byte) { value = append(value, 1) },
	}
	for name, mutate := range mutations {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value := append([]byte(nil), canonical...)
			if name == "trailing byte" {
				value = append(value, 1)
			} else {
				mutate(value)
			}
			limits := testStorageDecodingLimits()
			limits.MaxPointDecodes = 0
			_, err := DecodeStorageNode(context.Background(), value, limits)
			if !errors.Is(err, ErrInvalidStorageNode) {
				t.Fatalf("DecodeStorageNode() error = %v, want ErrInvalidStorageNode", err)
			}
		})
	}
}

func TestDecodeStorageNodeRejectsInvalidInputsAndBudgets(t *testing.T) {
	t.Parallel()

	canonical := canonicalStorageNode(t)
	wrongProfile := append([]byte(nil), canonical...)
	wrongProfile[storageNodeMagicBytes]++
	if _, err := DecodeStorageNode(
		context.Background(),
		wrongProfile,
		testStorageDecodingLimits(),
	); !errors.Is(err, ErrStorageNodeProfile) || !errors.Is(err, ErrInvalidStorageNode) {
		t.Fatalf("wrong-profile error = %v", err)
	}
	if _, err := DecodeStorageNode(nil, canonical, testStorageDecodingLimits()); !errors.Is(err, ErrInvalidStorageDecodingContext) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := DecodeStorageNode(context.Background(), canonical, StorageDecodingLimits{}); !errors.Is(err, ErrInvalidStorageDecodingLimits) {
		t.Fatalf("zero limits error = %v", err)
	}
	for name, limits := range map[string]StorageDecodingLimits{
		"node bytes only zero": {
			MaxTemporaryBytes: 1,
		},
		"temporary bytes only zero": {
			MaxNodeBytes: 1,
		},
	} {
		if _, err := DecodeStorageNode(
			context.Background(),
			canonical,
			limits,
		); !errors.Is(err, ErrInvalidStorageDecodingLimits) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DecodeStorageNode(cancelled, canonical, testStorageDecodingLimits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled error = %v", err)
	}

	for name, configure := range map[string]func(*StorageDecodingLimits){
		"node bytes":      func(limits *StorageDecodingLimits) { limits.MaxNodeBytes = uint64(len(canonical) - 1) },
		"point decodes":   func(limits *StorageDecodingLimits) { limits.MaxPointDecodes = 0 },
		"temporary bytes": func(limits *StorageDecodingLimits) { limits.MaxTemporaryBytes = 1 },
	} {
		name, configure := name, configure
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			limits := testStorageDecodingLimits()
			configure(&limits)
			_, err := DecodeStorageNode(context.Background(), canonical, limits)
			var resourceErr *StorageDecodingResourceError
			if !errors.As(err, &resourceErr) {
				t.Fatalf("DecodeStorageNode() error = %v, want resource error", err)
			}
		})
	}
}

func TestDecodeStorageNodeRejectsNonCanonicalNodeBodies(t *testing.T) {
	t.Parallel()

	internal, stem := canonicalStorageNodePair(t)
	kindOffset := storageNodeMagicBytes + storageNodeProfileIDBytes +
		storageNodeVersionBytes + storageNodeEncodingBytes
	depthOffset := kindOffset + storageNodeKindBytes
	commitmentOffset := depthOffset + storageNodeDepthBytes

	tests := map[string]struct {
		base   []byte
		mutate func([]byte) []byte
	}{
		"short header": {
			base: stem,
			mutate: func(value []byte) []byte {
				return value[:storageNodeHeaderBytes+storageNodeCountBytes-1]
			},
		},
		"short stem body": {
			base: stem,
			mutate: func(value []byte) []byte {
				return value[:storageNodeHeaderBytes+storageStemBytes]
			},
		},
		"internal excessive depth": {
			base: internal,
			mutate: func(value []byte) []byte {
				value[depthOffset] = 31
				return value
			},
		},
		"empty internal below root": {
			base: canonicalEmptyStorageRoot(t),
			mutate: func(value []byte) []byte {
				value[depthOffset] = 1
				return value
			},
		},
		"stem zero depth": {
			base: stem,
			mutate: func(value []byte) []byte {
				value[depthOffset] = 0
				return value
			},
		},
		"stem excessive depth": {
			base: stem,
			mutate: func(value []byte) []byte {
				value[depthOffset] = 32
				return value
			},
		},
		"unknown commitment marker": {
			base: stem,
			mutate: func(value []byte) []byte {
				value[commitmentOffset] = 2
				return value
			},
		},
		"identity with payload": {
			base: stem,
			mutate: func(value []byte) []byte {
				value[commitmentOffset] = 0
				return value
			},
		},
		"nonidentity without payload": {
			base: stem,
			mutate: func(value []byte) []byte {
				for index := commitmentOffset + 1; index < commitmentOffset+storageCommitmentBytes; index++ {
					value[index] = 0
				}
				return value
			},
		},
		"malformed nonidentity point": {
			base: stem,
			mutate: func(value []byte) []byte {
				for index := commitmentOffset + 1; index < commitmentOffset+storageCommitmentBytes; index++ {
					value[index] = 0xff
				}
				return value
			},
		},
		"malformed c1 point": {
			base: stem,
			mutate: func(value []byte) []byte {
				c1 := storageNodeHeaderBytes + storageStemBytes
				for index := c1 + 1; index < c1+storageCommitmentBytes; index++ {
					value[index] = 0xff
				}
				return value
			},
		},
		"malformed c2 point": {
			base: stem,
			mutate: func(value []byte) []byte {
				c2 := storageNodeHeaderBytes + storageStemBytes + storageCommitmentBytes
				value[c2] = 1
				for index := c2 + 1; index < c2+storageCommitmentBytes; index++ {
					value[index] = 0xff
				}
				return value
			},
		},
		"invalid c1 marker": {
			base: stem,
			mutate: func(value []byte) []byte {
				value[storageNodeHeaderBytes+storageStemBytes] = 2
				return value
			},
		},
		"stem identity main commitment": {
			base: stem,
			mutate: func(value []byte) []byte {
				for index := commitmentOffset; index < commitmentOffset+storageCommitmentBytes; index++ {
					value[index] = 0
				}
				return value
			},
		},
		"stem zero entries": {
			base: stem,
			mutate: func(value []byte) []byte {
				countOffset := storageNodeHeaderBytes + storageStemBytes + 2*storageCommitmentBytes
				binary.BigEndian.PutUint16(value[countOffset:countOffset+2], 0)
				return value[:countOffset+2]
			},
		},
		"stem duplicate suffix": {
			base: canonicalTwoEntryStem(t),
			mutate: func(value []byte) []byte {
				entriesOffset := storageNodeHeaderBytes + storageStemBytes + 2*storageCommitmentBytes + storageNodeCountBytes
				value[entriesOffset+storageStemEntryBytes] = value[entriesOffset]
				return value
			},
		},
		"stem excessive entries": {
			base: stem,
			mutate: func(value []byte) []byte {
				countOffset := storageNodeHeaderBytes + storageStemBytes + 2*storageCommitmentBytes
				prefix := append([]byte(nil), value[:countOffset+2]...)
				binary.BigEndian.PutUint16(prefix[countOffset:], 257)
				return append(prefix, make([]byte, 257*storageStemEntryBytes)...)
			},
		},
		"internal excessive edges": {
			base: internal,
			mutate: func(value []byte) []byte {
				prefix := append([]byte(nil), value[:storageNodeHeaderBytes+storageNodeCountBytes]...)
				binary.BigEndian.PutUint16(prefix[storageNodeHeaderBytes:], 257)
				return append(prefix, make([]byte, 257*storageInternalEdgeBytes)...)
			},
		},
		"internal duplicate edge": {
			base: internal,
			mutate: func(value []byte) []byte {
				first := storageNodeHeaderBytes + storageNodeCountBytes
				value[first+storageInternalEdgeBytes] = value[first]
				return value
			},
		},
		"internal identity with edges": {
			base: internal,
			mutate: func(value []byte) []byte {
				for index := commitmentOffset; index < commitmentOffset+storageCommitmentBytes; index++ {
					value[index] = 0
				}
				return value
			},
		},
	}

	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			value := test.mutate(append([]byte(nil), test.base...))
			_, err := DecodeStorageNode(
				context.Background(),
				value,
				testStorageDecodingLimits(),
			)
			if !errors.Is(err, ErrInvalidStorageNode) {
				t.Fatalf("DecodeStorageNode() error = %v, want ErrInvalidStorageNode", err)
			}
		})
	}
}

func TestDecodedStorageNodeAccessorsFailClosedAndRemainCancellable(t *testing.T) {
	t.Parallel()

	var zero DecodedStorageNode
	if zero.Kind() != 0 || zero.Depth() != 0 || zero.PointDecodes() != 0 ||
		zero.RecordCount() != 0 || zero.TemporaryBytes() != 0 {
		t.Fatal("zero decoded node reported valid metadata")
	}
	if _, err := zero.Encoded(context.Background()); !errors.Is(err, ErrInvalidStorageNode) {
		t.Fatalf("zero Encoded() error = %v", err)
	}
	if _, err := zero.Children(context.Background()); !errors.Is(err, ErrInvalidStorageNode) {
		t.Fatalf("zero Children() error = %v", err)
	}
	if _, err := zero.Entries(context.Background()); !errors.Is(err, ErrInvalidStorageNode) {
		t.Fatalf("zero Entries() error = %v", err)
	}
	invalidWithBytes := DecodedStorageNode{encoded: []byte{1}}
	if _, err := invalidWithBytes.Encoded(context.Background()); !errors.Is(err, ErrInvalidStorageNode) {
		t.Fatalf("invalid-with-bytes Encoded() error = %v", err)
	}
	validWithoutBytes := DecodedStorageNode{valid: true}
	if _, err := validWithoutBytes.Encoded(context.Background()); !errors.Is(err, ErrInvalidStorageNode) {
		t.Fatalf("valid-without-bytes Encoded() error = %v", err)
	}

	decoded, err := DecodeStorageNode(
		context.Background(),
		canonicalStorageNode(t),
		testStorageDecodingLimits(),
	)
	if err != nil {
		t.Fatalf("DecodeStorageNode() error = %v", err)
	}
	var nilContext context.Context
	if _, err := decoded.Encoded(nilContext); !errors.Is(err, ErrInvalidStorageDecodingContext) {
		t.Fatalf("Encoded() error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := decoded.Entries(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Entries() error = %v", err)
	}
	internalEncoded, _ := canonicalStorageNodePair(t)
	internal, err := DecodeStorageNode(
		context.Background(),
		internalEncoded,
		testStorageDecodingLimits(),
	)
	if err != nil {
		t.Fatalf("decode internal error = %v", err)
	}
	if _, err := internal.Children(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Children() error = %v", err)
	}
	if internal.RecordCount() == 0 || internal.TemporaryBytes() == 0 {
		t.Fatal("internal metadata rejected")
	}
	for cancelAt := 1; cancelAt <= 16; cancelAt++ {
		_, decodeErr := DecodeStorageNode(
			&cancelContext{cancelAt: cancelAt},
			internalEncoded,
			testStorageDecodingLimits(),
		)
		if decodeErr != nil && !errors.Is(decodeErr, context.Canceled) {
			t.Fatalf("internal cancelAt %d error = %v", cancelAt, decodeErr)
		}
	}

	for cancelAt := 1; cancelAt <= 12; cancelAt++ {
		_, decodeErr := DecodeStorageNode(
			&cancelContext{cancelAt: cancelAt},
			canonicalTwoEntryStem(t),
			testStorageDecodingLimits(),
		)
		if decodeErr != nil && !errors.Is(decodeErr, context.Canceled) {
			t.Fatalf("cancelAt %d error = %v", cancelAt, decodeErr)
		}
	}

	resourceErr := &StorageDecodingResourceError{
		Resource: StorageDecodingResourceNodeBytes,
		Limit:    1,
		Actual:   2,
	}
	if resourceErr.Error() == "" || !errors.Is(resourceErr, ErrStorageDecodingResource) {
		t.Fatalf("resource error = %v", resourceErr)
	}
	if _, err := inspectStorageCommitment(nil, -1); !errors.Is(err, ErrInvalidStorageNode) {
		t.Fatalf("negative commitment offset error = %v", err)
	}
	if _, _, err := decodeStorageCommitment(context.Background(), nil, 0); !errors.Is(err, ErrInvalidStorageNode) {
		t.Fatalf("short commitment error = %v", err)
	}
}

func TestStorageDecodingArithmeticAndBoundariesAreExact(t *testing.T) {
	t.Parallel()

	if got := storageDecodedTemporaryBytes(100, 3, StorageNodeKindStem); got !=
		100+storageDecodedNodeWorkingBytes+3*entryWorkingBytes {
		t.Fatalf("stem temporary bytes = %d", got)
	}
	if got := storageDecodedTemporaryBytes(100, 3, StorageNodeKindInternal); got !=
		100+storageDecodedNodeWorkingBytes+3*uint64(storageInternalEdgeBytes) {
		t.Fatalf("internal temporary bytes = %d", got)
	}
	if got := storageMinimumNodeBytes(); got != storageNodeHeaderBytes+storageNodeCountBytes {
		t.Fatalf("minimum node bytes = %d", got)
	}
	if got := storageStemFixedBytes(); got !=
		storageStemBytes+2*storageCommitmentBytes+storageNodeCountBytes {
		t.Fatalf("fixed stem bytes = %d", got)
	}
	if !storageRecordCountFits(uint16(256)) || storageRecordCountFits(uint16(257)) {
		t.Fatal("record-count boundary mismatch")
	}
	if !validStorageInternalDepth(30) || validStorageInternalDepth(31) {
		t.Fatal("internal-depth boundary mismatch")
	}
	if !validStorageStemDepth(1) || !validStorageStemDepth(31) ||
		validStorageStemDepth(0) || validStorageStemDepth(32) {
		t.Fatal("stem-depth boundary mismatch")
	}
	for name, test := range map[string]struct {
		length int
		offset int
		size   int
		want   bool
	}{
		"exact":         {33, 0, 33, true},
		"negative":      {33, -1, 33, false},
		"zero at end":   {33, 33, 0, true},
		"past end":      {33, 34, 0, false},
		"short payload": {32, 0, 33, false},
	} {
		if got := storageDecodingRangeFits(test.length, test.offset, test.size); got != test.want {
			t.Fatalf("%s range fit = %t", name, got)
		}
	}
	identity := make([]byte, storageCommitmentBytes)
	if point, err := inspectStorageCommitment(identity, 0); err != nil || point != 0 {
		t.Fatalf("exact identity commitment = (%d, %v)", point, err)
	}
	if storageDecodedRecordBytes(StorageNodeKindStem) != entryWorkingBytes ||
		storageDecodedRecordBytes(StorageNodeKindInternal) != uint64(storageInternalEdgeBytes) {
		t.Fatal("decoded record-byte classification mismatch")
	}
	if !allZero([]byte{0, 0, 0}) || allZero([]byte{0, 1, 0}) {
		t.Fatal("zero-byte classification mismatch")
	}
}

func TestDecodeStorageNodeReportsEveryDecodedPoint(t *testing.T) {
	t.Parallel()

	encoded := canonicalStorageNode(t)
	mainOffset := storageNodeHeaderBytes - storageCommitmentBytes
	c1Offset := storageNodeHeaderBytes + storageStemBytes
	c2Offset := c1Offset + storageCommitmentBytes
	want := uint32(0)
	for _, offset := range []int{mainOffset, c1Offset, c2Offset} {
		point, err := inspectStorageCommitment(encoded, offset)
		if err != nil {
			t.Fatalf("inspect commitment at %d error = %v", offset, err)
		}
		want += point
	}
	decoded, err := DecodeStorageNode(
		context.Background(),
		encoded,
		testStorageDecodingLimits(),
	)
	if err != nil || decoded.PointDecodes() != want {
		t.Fatalf("decoded points = %d, want %d, error = %v", decoded.PointDecodes(), want, err)
	}
}

func canonicalStorageNode(t testing.TB) []byte {
	t.Helper()
	tree, err := Build(
		context.Background(),
		[]Entry{{Key: Key{0: 1, 31: 2}, Value: Value{0: 3}}},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	image, err := tree.StorageImage(context.Background(), testStorageEncodingLimits())
	if err != nil {
		t.Fatalf("StorageImage() error = %v", err)
	}
	for _, node := range image.nodes {
		if len(node.encoded) > storageNodeHeaderBytes && node.encoded[storageNodeHeaderBytes-storageCommitmentBytes-2] == byte(nodeStem) {
			return node.Encoded()
		}
	}
	t.Fatal("canonical stem node not found")
	return nil
}

func canonicalStorageNodePair(t testing.TB) ([]byte, []byte) {
	t.Helper()
	tree, err := Build(
		context.Background(),
		[]Entry{
			{Key: Key{0: 1, 31: 1}, Value: Value{0: 1}},
			{Key: Key{0: 2, 31: 2}, Value: Value{0: 2}},
		},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	image, err := tree.StorageImage(context.Background(), testStorageEncodingLimits())
	if err != nil {
		t.Fatalf("StorageImage() error = %v", err)
	}
	var internal []byte
	var stem []byte
	kindOffset := storageNodeMagicBytes + storageNodeProfileIDBytes +
		storageNodeVersionBytes + storageNodeEncodingBytes
	for _, node := range image.nodes {
		switch node.encoded[kindOffset] {
		case byte(nodeInternal):
			if binary.BigEndian.Uint16(node.encoded[storageNodeHeaderBytes:]) >= 2 {
				internal = node.Encoded()
			}
		case byte(nodeStem):
			stem = node.Encoded()
		}
	}
	if internal == nil || stem == nil {
		t.Fatal("canonical node pair not found")
	}

	return internal, stem
}

func canonicalEmptyStorageRoot(t testing.TB) []byte {
	t.Helper()
	tree, err := Build(
		context.Background(),
		nil,
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	image, err := tree.StorageImage(context.Background(), testStorageEncodingLimits())
	if err != nil {
		t.Fatalf("StorageImage() error = %v", err)
	}
	return image.nodes[0].Encoded()
}

func canonicalTwoEntryStem(t testing.TB) []byte {
	t.Helper()
	tree, err := Build(
		context.Background(),
		[]Entry{
			{Key: Key{0: 1, 31: 1}, Value: Value{0: 1}},
			{Key: Key{0: 1, 31: 2}, Value: Value{0: 2}},
		},
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	image, err := tree.StorageImage(context.Background(), testStorageEncodingLimits())
	if err != nil {
		t.Fatalf("StorageImage() error = %v", err)
	}
	kindOffset := storageNodeMagicBytes + storageNodeProfileIDBytes +
		storageNodeVersionBytes + storageNodeEncodingBytes
	for _, node := range image.nodes {
		if node.encoded[kindOffset] == byte(nodeStem) {
			return node.Encoded()
		}
	}
	t.Fatal("canonical two-entry stem not found")
	return nil
}

func testStorageDecodingLimits() StorageDecodingLimits {
	return StorageDecodingLimits{
		MaxNodeBytes:      1 << 20,
		MaxPointDecodes:   3,
		MaxTemporaryBytes: 1 << 20,
	}
}

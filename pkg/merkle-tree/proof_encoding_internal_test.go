package merkletree

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"math/bits"
	"testing"
)

func TestProofEncodingInputLimits(t *testing.T) {
	t.Parallel()

	inclusion, consistency, multi := encodedProofObjects(t)
	inclusionData := mustMarshalProof(t, inclusion.MarshalBinary)
	consistencyData := mustMarshalProof(t, consistency.MarshalBinary)
	multiData := mustMarshalProof(t, multi.MarshalBinary)

	t.Run("inclusion", func(t *testing.T) {
		assertProofDecodeInputErrors(t, inclusionData, func(
			ctx context.Context,
			data []byte,
			limits EncodingLimits,
			invalid bool,
		) error {
			proofLimits := DefaultProofLimits()
			if invalid {
				proofLimits = ProofLimits{}
			}
			_, err := ParseInclusionProof(ctx, data, limits, proofLimits)

			return err
		})
	})
	t.Run("consistency", func(t *testing.T) {
		assertProofDecodeInputErrors(t, consistencyData, func(
			ctx context.Context,
			data []byte,
			limits EncodingLimits,
			invalid bool,
		) error {
			proofLimits := DefaultConsistencyProofLimits()
			if invalid {
				proofLimits = ConsistencyProofLimits{}
			}
			_, err := ParseConsistencyProof(ctx, data, limits, proofLimits)

			return err
		})
	})
	t.Run("multi", func(t *testing.T) {
		assertProofDecodeInputErrors(t, multiData, func(
			ctx context.Context,
			data []byte,
			limits EncodingLimits,
			invalid bool,
		) error {
			proofLimits := DefaultMultiProofLimits()
			if invalid {
				proofLimits = MultiProofLimits{}
			}
			_, err := ParseMultiInclusionProof(ctx, data, limits, proofLimits)

			return err
		})
	})
}

func TestProofEncodingRejectsResourceAndStructuralClaims(t *testing.T) {
	t.Parallel()

	inclusion, consistency, multi := encodedProofObjects(t)
	inclusionData := mustMarshalProof(t, inclusion.MarshalBinary)
	consistencyData := mustMarshalProof(t, consistency.MarshalBinary)
	multiData := mustMarshalProof(t, multi.MarshalBinary)

	inclusionLimits := DefaultProofLimits()
	inclusionLimits.MaxElements = uint64(len(inclusion.siblings) - 1)
	_, err := ParseInclusionProof(
		context.Background(),
		inclusionData,
		DefaultEncodingLimits(),
		inclusionLimits,
	)
	assertResourceKind(t, err, ResourceProofElements)

	inclusionLimits = DefaultProofLimits()
	inclusionLimits.MaxElements = uint64(len(inclusion.siblings))
	inclusionLimits.MaxTraversalDepth = uint64(bits.Len64(inclusion.treeSize))
	if _, err := ParseInclusionProof(
		context.Background(),
		inclusionData,
		EncodingLimits{MaxBytes: uint64(len(inclusionData))},
		inclusionLimits,
	); err != nil {
		t.Fatalf("ParseInclusionProof(exact limits) error = %v", err)
	}

	inclusionLimits = DefaultProofLimits()
	inclusionLimits.MaxTraversalDepth = 1
	_, err = ParseInclusionProof(
		context.Background(),
		inclusionData,
		DefaultEncodingLimits(),
		inclusionLimits,
	)
	assertResourceKind(t, err, ResourceTraversalDepth)

	consistencyLimits := DefaultConsistencyProofLimits()
	consistencyLimits.MaxElements = uint64(len(consistency.nodes))
	consistencyLimits.MaxTraversalDepth = uint64(bits.Len64(
		consistency.newerTreeSize,
	))
	if _, err := ParseConsistencyProof(
		context.Background(),
		consistencyData,
		EncodingLimits{MaxBytes: uint64(len(consistencyData))},
		consistencyLimits,
	); err != nil {
		t.Fatalf("ParseConsistencyProof(exact limits) error = %v", err)
	}

	consistencyLimits = DefaultConsistencyProofLimits()
	consistencyLimits.MaxElements = uint64(len(consistency.nodes) - 1)
	_, err = ParseConsistencyProof(
		context.Background(),
		consistencyData,
		DefaultEncodingLimits(),
		consistencyLimits,
	)
	assertResourceKind(t, err, ResourceProofElements)

	multiLimits := DefaultMultiProofLimits()
	multiLimits.MaxLeaves = uint64(len(multi.leafIndexes))
	multiLimits.MaxElements = uint64(len(multi.frontier))
	multiLimits.MaxTraversalDepth = uint64(bits.Len64(multi.treeSize))
	if _, err := ParseMultiInclusionProof(
		context.Background(),
		multiData,
		EncodingLimits{MaxBytes: uint64(len(multiData))},
		multiLimits,
	); err != nil {
		t.Fatalf("ParseMultiInclusionProof(exact limits) error = %v", err)
	}

	consistencyLimits = DefaultConsistencyProofLimits()
	consistencyLimits.MaxTraversalDepth = 1
	_, err = ParseConsistencyProof(
		context.Background(),
		consistencyData,
		DefaultEncodingLimits(),
		consistencyLimits,
	)
	assertResourceKind(t, err, ResourceTraversalDepth)

	multiLimits = DefaultMultiProofLimits()
	multiLimits.MaxLeaves = uint64(len(multi.leafIndexes) - 1)
	_, err = ParseMultiInclusionProof(
		context.Background(),
		multiData,
		DefaultEncodingLimits(),
		multiLimits,
	)
	assertResourceKind(t, err, ResourceLeaves)

	multiLimits = DefaultMultiProofLimits()
	multiLimits.MaxTraversalDepth = 1
	_, err = ParseMultiInclusionProof(
		context.Background(),
		multiData,
		DefaultEncodingLimits(),
		multiLimits,
	)
	assertResourceKind(t, err, ResourceTraversalDepth)

	multiLimits = DefaultMultiProofLimits()
	multiLimits.MaxElements = uint64(len(multi.frontier) - 1)
	_, err = ParseMultiInclusionProof(
		context.Background(),
		multiData,
		DefaultEncodingLimits(),
		multiLimits,
	)
	assertResourceKind(t, err, ResourceProofElements)

	for name, test := range map[string]func([]byte) error{
		"inclusion count": func(data []byte) error {
			binary.BigEndian.PutUint64(data[90:98], math.MaxUint64)
			_, parseErr := ParseInclusionProof(
				context.Background(),
				data,
				DefaultEncodingLimits(),
				DefaultProofLimits(),
			)

			return parseErr
		},
		"consistency count": func(data []byte) error {
			binary.BigEndian.PutUint64(data[90:98], math.MaxUint64)
			_, parseErr := ParseConsistencyProof(
				context.Background(),
				data,
				DefaultEncodingLimits(),
				DefaultConsistencyProofLimits(),
			)

			return parseErr
		},
		"multi leaf count": func(data []byte) error {
			binary.BigEndian.PutUint64(data[50:58], math.MaxUint64)
			_, parseErr := ParseMultiInclusionProof(
				context.Background(),
				data,
				DefaultEncodingLimits(),
				DefaultMultiProofLimits(),
			)

			return parseErr
		},
	} {
		t.Run(name, func(t *testing.T) {
			var source []byte
			switch name {
			case "inclusion count":
				source = inclusionData
			case "consistency count":
				source = consistencyData
			default:
				source = multiData
			}
			if err := test(append([]byte(nil), source...)); err == nil {
				t.Fatal("parser accepted impossible encoded count")
			}
		})
	}

	shortMulti := append([]byte(nil), multiData...)
	binary.BigEndian.PutUint64(shortMulti[50:58], 100)
	if _, err := ParseMultiInclusionProof(
		context.Background(),
		shortMulti,
		DefaultEncodingLimits(),
		DefaultMultiProofLimits(),
	); !errors.Is(err, ErrMalformedEncoding) {
		t.Fatalf("ParseMultiInclusionProof(short leaves) error = %v", err)
	}

	tooShortFrontier := make([]byte, encodedMultiPrefix+(8+sha256.Size)+2)
	appendEncodingHeader(
		tooShortFrontier,
		encodingObjectMulti,
		CanonicalProfile(),
	)
	binary.BigEndian.PutUint64(tooShortFrontier[10:18], 1)
	binary.BigEndian.PutUint64(tooShortFrontier[50:58], 1)
	if _, err := ParseMultiInclusionProof(
		context.Background(),
		tooShortFrontier,
		DefaultEncodingLimits(),
		DefaultMultiProofLimits(),
	); !errors.Is(err, ErrMalformedEncoding) {
		t.Fatalf("ParseMultiInclusionProof(short frontier count) error = %v", err)
	}

	headerOnlyMulti := make([]byte, encodedMultiPrefix)
	appendEncodingHeader(
		headerOnlyMulti,
		encodingObjectMulti,
		CanonicalProfile(),
	)
	binary.BigEndian.PutUint64(headerOnlyMulti[10:18], 1)
	if _, err := ParseMultiInclusionProof(
		context.Background(),
		headerOnlyMulti,
		DefaultEncodingLimits(),
		DefaultMultiProofLimits(),
	); !errors.Is(err, ErrMalformedEncoding) {
		t.Fatalf("ParseMultiInclusionProof(header only) error = %v", err)
	}

	minimumMulti := make([]byte, encodedMultiPrefix+encodedMultiSuffix)
	appendEncodingHeader(minimumMulti, encodingObjectMulti, CanonicalProfile())
	binary.BigEndian.PutUint64(minimumMulti[10:18], 1)
	if _, err := ParseMultiInclusionProof(
		context.Background(),
		minimumMulti,
		DefaultEncodingLimits(),
		DefaultMultiProofLimits(),
	); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("ParseMultiInclusionProof(minimum size) error = %v", err)
	}
}

func TestProofEncodingRejectsNonCanonicalMetadata(t *testing.T) {
	t.Parallel()

	inclusion, consistency, multi := encodedProofObjects(t)
	inclusionData := mustMarshalProof(t, inclusion.MarshalBinary)
	consistencyData := mustMarshalProof(t, consistency.MarshalBinary)
	multiData := mustMarshalProof(t, multi.MarshalBinary)

	cases := []struct {
		name   string
		data   []byte
		mutate func([]byte)
		parse  func([]byte) error
	}{
		{
			name:   "inclusion index out of range",
			data:   inclusionData,
			mutate: func(data []byte) { binary.BigEndian.PutUint64(data[50:58], 7) },
			parse: func(data []byte) error {
				_, err := ParseInclusionProof(
					context.Background(), data, DefaultEncodingLimits(), DefaultProofLimits(),
				)

				return err
			},
		},
		{
			name:   "consistency reversed sizes",
			data:   consistencyData,
			mutate: func(data []byte) { binary.BigEndian.PutUint64(data[10:18], 8) },
			parse: func(data []byte) error {
				_, err := ParseConsistencyProof(
					context.Background(), data, DefaultEncodingLimits(),
					DefaultConsistencyProofLimits(),
				)

				return err
			},
		},
		{
			name:   "multi duplicate indexes",
			data:   multiData,
			mutate: func(data []byte) { copy(data[98:106], data[58:66]) },
			parse: func(data []byte) error {
				_, err := ParseMultiInclusionProof(
					context.Background(), data, DefaultEncodingLimits(),
					DefaultMultiProofLimits(),
				)

				return err
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			data := append([]byte(nil), test.data...)
			test.mutate(data)
			if err := test.parse(data); err == nil {
				t.Fatal("parser accepted non-canonical metadata")
			}
		})
	}

	badInclusion := inclusion
	badInclusion.siblings = badInclusion.siblings[:len(badInclusion.siblings)-1]
	if _, err := badInclusion.MarshalBinary(); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("inclusion MarshalBinary() error = %v", err)
	}

	badConsistency := consistency
	badConsistency.nodes = badConsistency.nodes[:len(badConsistency.nodes)-1]
	if _, err := badConsistency.MarshalBinary(); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("consistency MarshalBinary() error = %v", err)
	}

	badMulti := multi
	badMulti.frontier = badMulti.frontier[:len(badMulti.frontier)-1]
	if _, err := badMulti.MarshalBinary(); !errors.Is(err, ErrMalformedProof) {
		t.Fatalf("multi MarshalBinary() error = %v", err)
	}

	equalConsistency := consistency
	equalConsistency.olderTreeSize = equalConsistency.newerTreeSize
	equalConsistency.olderRoot = equalConsistency.newerRoot
	equalConsistency.olderRoot.digest.value[0] ^= 1
	equalConsistency.nodes = nil
	if _, err := equalConsistency.MarshalBinary(); !errors.Is(
		err,
		ErrVerificationFailed,
	) {
		t.Fatalf("equal consistency MarshalBinary() error = %v", err)
	}
}

func TestProofDecodingCancellationAndOwnership(t *testing.T) {
	t.Parallel()

	inclusion, consistency, multi := encodedProofObjects(t)
	tests := []struct {
		name  string
		data  []byte
		parse func(context.Context, []byte) ([]byte, error)
	}{
		{
			name: "inclusion",
			data: mustMarshalProof(t, inclusion.MarshalBinary),
			parse: func(ctx context.Context, data []byte) ([]byte, error) {
				proof, err := ParseInclusionProof(
					ctx, data, DefaultEncodingLimits(), DefaultProofLimits(),
				)
				if err != nil {
					return nil, err
				}

				return proof.MarshalBinary()
			},
		},
		{
			name: "consistency",
			data: mustMarshalProof(t, consistency.MarshalBinary),
			parse: func(ctx context.Context, data []byte) ([]byte, error) {
				proof, err := ParseConsistencyProof(
					ctx, data, DefaultEncodingLimits(),
					DefaultConsistencyProofLimits(),
				)
				if err != nil {
					return nil, err
				}

				return proof.MarshalBinary()
			},
		},
		{
			name: "multi",
			data: mustMarshalProof(t, multi.MarshalBinary),
			parse: func(ctx context.Context, data []byte) ([]byte, error) {
				proof, err := ParseMultiInclusionProof(
					ctx, data, DefaultEncodingLimits(), DefaultMultiProofLimits(),
				)
				if err != nil {
					return nil, err
				}

				return proof.MarshalBinary()
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := &checkpointContext{remaining: 1, done: make(chan struct{})}
			if _, err := test.parse(ctx, test.data); !errors.Is(
				err,
				context.Canceled,
			) {
				t.Fatalf("parse(cancelled) error = %v", err)
			}

			input := append([]byte(nil), test.data...)
			decoded, err := test.parse(context.Background(), input)
			if err != nil {
				t.Fatalf("parse() error = %v", err)
			}
			input[len(input)-1] ^= 0xff
			if string(decoded) != string(test.data) {
				t.Fatal("decoded proof aliases input")
			}
		})
	}

	multiData := mustMarshalProof(t, multi.MarshalBinary)
	frontierContext := &checkpointContext{
		remaining: len(multi.leafIndexes) + 1,
		done:      make(chan struct{}),
	}
	if _, err := ParseMultiInclusionProof(
		frontierContext,
		multiData,
		DefaultEncodingLimits(),
		DefaultMultiProofLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("ParseMultiInclusionProof(frontier cancelled) error = %v", err)
	}
}

func TestProofEncodingSizeArithmetic(t *testing.T) {
	t.Parallel()

	if _, err := encodedVectorSize(1, math.MaxUint64, 2); !errors.Is(
		err,
		ErrMalformedEncoding,
	) {
		t.Fatalf("encodedVectorSize(product overflow) error = %v", err)
	}
	if _, err := encodedVectorSize(1, math.MaxUint64, 1); !errors.Is(
		err,
		ErrMalformedEncoding,
	) {
		t.Fatalf("encodedVectorSize(sum overflow) error = %v", err)
	}
	if _, err := encodedVectorSize(0, uint64(maxInt())+1, 1); !errors.Is(
		err,
		ErrMalformedEncoding,
	) {
		t.Fatalf("encodedVectorSize(int overflow) error = %v", err)
	}
	if _, err := encodedMultiSize(math.MaxUint64, 1); !errors.Is(
		err,
		ErrMalformedEncoding,
	) {
		t.Fatalf("encodedMultiSize() error = %v", err)
	}
	if size, err := encodedVectorSize(
		0,
		uint64(maxInt()),
		1,
	); err != nil || size != maxInt() {
		t.Fatalf("encodedVectorSize(exact max int) = %d, %v", size, err)
	}
	if got := multiFrontierCount(10, 6, []uint64{14}); got != 2 {
		t.Fatalf("multiFrontierCount(boundary) = %d, want 2", got)
	}
	if got := multiFrontierCount(10, 6, []uint64{14, 15}); got != 1 {
		t.Fatalf("multiFrontierCount(right start) = %d, want 1", got)
	}
}

type proofDecodeFunc func(
	context.Context,
	[]byte,
	EncodingLimits,
	bool,
) error

func assertProofDecodeInputErrors(
	t *testing.T,
	data []byte,
	parse proofDecodeFunc,
) {
	t.Helper()

	if err := parse(nil, data, DefaultEncodingLimits(), false); !errors.Is(
		err,
		ErrInvalidContext,
	) {
		t.Fatalf("nil context error = %v", err)
	}
	if err := parse(context.Background(), data, EncodingLimits{}, false); !errors.Is(
		err,
		ErrInvalidLimits,
	) {
		t.Fatalf("invalid encoding limits error = %v", err)
	}
	if err := parse(context.Background(), data, DefaultEncodingLimits(), true); !errors.Is(
		err,
		ErrInvalidLimits,
	) {
		t.Fatalf("invalid proof limits error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := parse(ctx, data, DefaultEncodingLimits(), false); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("cancelled context error = %v", err)
	}
	limits := EncodingLimits{MaxBytes: uint64(len(data) - 1)}
	err := parse(context.Background(), data, limits, false)
	assertResourceKind(t, err, ResourceEncodedBytes)
}

func encodedProofObjects(
	t *testing.T,
) (InclusionProof, ConsistencyProof, MultiInclusionProof) {
	t.Helper()

	leaves := make([]RawLeaf, 7)
	for index := range leaves {
		leaves[index] = NewRawLeaf([]byte{byte(index)})
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
	inclusion, err := snapshot.InclusionProof(
		context.Background(),
		3,
		DefaultProofLimits(),
	)
	if err != nil {
		t.Fatalf("InclusionProof() error = %v", err)
	}
	older, err := NewSnapshot(
		context.Background(),
		CanonicalProfile(),
		leaves[:3],
		DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(older) error = %v", err)
	}
	olderRoot, err := older.Root()
	if err != nil {
		t.Fatalf("older.Root() error = %v", err)
	}
	consistency, err := snapshot.ConsistencyProof(
		context.Background(),
		olderRoot,
		DefaultConsistencyProofLimits(),
	)
	if err != nil {
		t.Fatalf("ConsistencyProof() error = %v", err)
	}
	multi, err := snapshot.MultiInclusionProof(
		context.Background(),
		[]uint64{1, 5},
		DefaultMultiProofLimits(),
	)
	if err != nil {
		t.Fatalf("MultiInclusionProof() error = %v", err)
	}

	return inclusion, consistency, multi
}

func mustMarshalProof(
	t *testing.T,
	marshal func() ([]byte, error),
) []byte {
	t.Helper()

	data, err := marshal()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}

	return data
}

func assertResourceKind(
	t *testing.T,
	err error,
	want ResourceKind,
) {
	t.Helper()

	var resourceErr *ResourceError
	if !errors.As(err, &resourceErr) || resourceErr.Kind != want {
		t.Fatalf("error = %v, want ResourceError kind %v", err, want)
	}
}

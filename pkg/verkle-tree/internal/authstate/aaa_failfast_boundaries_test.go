package authstate

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestFailFastProofMaterialRemainingBoundary(t *testing.T) {
	t.Parallel()

	remaining, err := remainingProofMaterialResource(
		ProofMaterialResourceNodeReads,
		2,
		1,
	)
	if err != nil || remaining != 1 {
		t.Fatalf("remaining resource = %d, error = %v", remaining, err)
	}
	_, err = remainingProofMaterialResource(
		ProofMaterialResourceNodeReads,
		2,
		2,
	)
	var resourceErr *ProofMaterialResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != ProofMaterialResourceNodeReads ||
		resourceErr.Limit != 2 ||
		resourceErr.Actual != 3 {
		t.Fatalf("exhausted resource error = %v", err)
	}
}

func TestFailFastSnapshotLimitDisjunction(t *testing.T) {
	t.Parallel()

	limits := testLimits()
	limits.MaxEntries = maxSupportedCount + 1
	limits.MaxBatchUpdates = 1
	if err := limits.validate(); !errors.Is(err, errInvalidLimits) {
		t.Fatalf("validate() error = %v, want errInvalidLimits", err)
	}
}

func TestFailFastTreeProofMergeCopiesSortedValues(t *testing.T) {
	t.Parallel()

	values := []int{2, 1}
	if err := sortTreeProofValues(
		context.Background(),
		values,
		func(left int, right int) int {
			return left - right
		},
	); err != nil {
		t.Fatalf("sortTreeProofValues() error = %v", err)
	}
	if !slices.Equal(values, []int{1, 2}) {
		t.Fatalf("sorted values = %v, want [1 2]", values)
	}
}

package management

import (
	"errors"
	"fmt"
	"testing"
)

func TestManagementErrorsDistinguishStableOperationOutcomes(t *testing.T) {
	t.Parallel()

	stable := []error{
		ErrRecordNotFound,
		ErrUnsupportedCapability,
		ErrManagementUnavailable,
		ErrMalformedCursor,
		ErrInvalidFilter,
		ErrStaleRecord,
		ErrMutationConflict,
		ErrPartialMutation,
		ErrUnknownMutation,
	}
	for index, target := range stable {
		if target == nil {
			t.Fatalf("stable error %d is nil", index)
		}
		wrapped := fmt.Errorf("adapter operation: %w", target)
		if !errors.Is(wrapped, target) {
			t.Fatalf("errors.Is(wrapped, %v) = false", target)
		}
		for otherIndex, other := range stable {
			if index != otherIndex && errors.Is(target, other) {
				t.Fatalf("stable errors %d and %d are indistinguishable", index, otherIndex)
			}
		}
	}
}

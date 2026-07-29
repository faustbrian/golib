package merkletree

import (
	"errors"
	"math"
	"testing"
)

func TestSaturatedAdd(t *testing.T) {
	t.Parallel()

	if got := saturatedAdd(4, 5); got != 9 {
		t.Fatalf("non-overflowing sum = %d", got)
	}
	if got := saturatedAdd(math.MaxUint64, 1); got != math.MaxUint64 {
		t.Fatalf("overflowing sum = %d", got)
	}
}

func TestProfileValidationRejectsPartiallyMatchingRFCIdentity(t *testing.T) {
	t.Parallel()

	tests := []Profile{
		{
			id:        ProfileRFC9162,
			version:   1,
			algorithm: HashAlgorithm(255),
		},
		{
			id:        ProfileRFC9162,
			version:   2,
			algorithm: HashSHA256,
		},
	}
	for _, profile := range tests {
		if err := profile.validate(); !errors.Is(err, ErrUnsupportedProfile) {
			t.Fatalf("profile %#v error = %v", profile, err)
		}
	}
}

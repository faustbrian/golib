package merkletree

import (
	"context"
	"errors"
	"testing"
)

func TestRootEncodingRejectsInvalidInternalIdentityAndLimits(t *testing.T) {
	t.Parallel()

	if _, err := ParseRoot(nil, EncodingLimits{}); !errors.Is(
		err,
		ErrInvalidLimits,
	) {
		t.Fatalf("ParseRoot(invalid limits) error = %v", err)
	}

	root, err := ComputeRoot(
		context.Background(),
		CanonicalProfile(),
		[]RawLeaf{NewRawLeaf([]byte("leaf"))},
		DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("ComputeRoot() error = %v", err)
	}
	wrongDigestAlgorithm := root
	wrongDigestAlgorithm.digest.algorithm = HashAlgorithm(0xff)
	if _, err := wrongDigestAlgorithm.MarshalBinary(); !errors.Is(
		err,
		ErrMalformedEncoding,
	) {
		t.Fatalf("MarshalBinary(wrong digest algorithm) error = %v", err)
	}

	empty, err := ComputeRoot(
		context.Background(),
		CanonicalProfile(),
		nil,
		DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("ComputeRoot(empty) error = %v", err)
	}
	empty.digest.value[0] ^= 0xff
	if _, err := empty.MarshalBinary(); !errors.Is(
		err,
		ErrMalformedEncoding,
	) {
		t.Fatalf("MarshalBinary(changed empty digest) error = %v", err)
	}
}

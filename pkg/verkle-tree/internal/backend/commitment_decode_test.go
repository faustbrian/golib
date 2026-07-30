package backend

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"testing"
)

func TestDecodeVectorCommitmentReturnsStrictCanonicalValue(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	encoded, err := hex.DecodeString(
		"4a2c7486fd924882bf02c6908de395122843e3e05264d7991e18e7985dad51e9",
	)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	original := bytes.Clone(encoded)

	decoded, err := DecodeVectorCommitment(
		context.Background(),
		encoded,
		VectorCommitmentDecodingLimits{
			MaxCommitmentBytes: commitmentSize,
			MaxPointDecodes:    1,
		},
	)
	if err != nil {
		t.Fatalf("decode vector commitment: %v", err)
	}
	got, err := decoded.Bytes()
	if err != nil {
		t.Fatalf("commitment bytes: %v", err)
	}
	if !bytes.Equal(got[:], original) {
		t.Fatalf("commitment bytes = %x, want %x", got, original)
	}
	if !bytes.Equal(encoded, original) {
		t.Fatal("decoder mutated caller input")
	}

	for name, test := range map[string]struct {
		ctx    context.Context
		input  []byte
		limits VectorCommitmentDecodingLimits
	}{
		"nil context": {
			input:  encoded,
			limits: testVectorCommitmentDecodingLimits(),
		},
		"cancelled": {
			ctx:    cancelled,
			input:  encoded,
			limits: testVectorCommitmentDecodingLimits(),
		},
		"invalid limits": {
			ctx:   context.Background(),
			input: encoded,
		},
		"byte budget": {
			ctx:   context.Background(),
			input: encoded,
			limits: VectorCommitmentDecodingLimits{
				MaxCommitmentBytes: commitmentSize - 1,
				MaxPointDecodes:    1,
			},
		},
		"point budget": {
			ctx:   context.Background(),
			input: encoded,
			limits: VectorCommitmentDecodingLimits{
				MaxCommitmentBytes: commitmentSize,
			},
		},
		"malformed": {
			ctx:    context.Background(),
			input:  make([]byte, commitmentSize),
			limits: testVectorCommitmentDecodingLimits(),
		},
		"wrong length": {
			ctx:    context.Background(),
			input:  encoded[:commitmentSize-1],
			limits: testVectorCommitmentDecodingLimits(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, decodeErr := DecodeVectorCommitment(
				test.ctx,
				test.input,
				test.limits,
			)
			if decodeErr == nil {
				t.Fatal("decode unexpectedly succeeded")
			}
		})
	}

	_, err = DecodeVectorCommitment(
		&commitCancelContext{cancelAt: 2},
		encoded,
		testVectorCommitmentDecodingLimits(),
	)
	if !errors.Is(err, errVectorCommitmentDecodingCancelled) {
		t.Fatalf("post-decode cancellation error = %v", err)
	}

	for name, test := range map[string]struct {
		input    []byte
		limits   VectorCommitmentDecodingLimits
		resource VectorCommitmentDecodingResource
		limit    uint64
		actual   uint64
	}{
		"bytes": {
			input: encoded,
			limits: VectorCommitmentDecodingLimits{
				MaxCommitmentBytes: commitmentSize - 1,
				MaxPointDecodes:    1,
			},
			resource: VectorCommitmentDecodingResourceBytes,
			limit:    commitmentSize - 1,
			actual:   commitmentSize,
		},
		"points": {
			input: encoded,
			limits: VectorCommitmentDecodingLimits{
				MaxCommitmentBytes: commitmentSize,
			},
			resource: VectorCommitmentDecodingResourcePointDecodes,
			actual:   1,
		},
	} {
		t.Run("resource "+name, func(t *testing.T) {
			t.Parallel()

			_, resourceErr := DecodeVectorCommitment(
				context.Background(),
				test.input,
				test.limits,
			)
			var typed *VectorCommitmentDecodingResourceError
			if !errors.As(resourceErr, &typed) ||
				!errors.Is(
					resourceErr,
					errVectorCommitmentDecodingResource,
				) ||
				typed.Resource != test.resource ||
				typed.Limit != test.limit ||
				typed.Actual != test.actual ||
				typed.Error() == "" {
				t.Fatalf("resource error = %#v, error = %v", typed, resourceErr)
			}
		})
	}
}

func testVectorCommitmentDecodingLimits() VectorCommitmentDecodingLimits {
	return VectorCommitmentDecodingLimits{
		MaxCommitmentBytes: commitmentSize,
		MaxPointDecodes:    1,
	}
}

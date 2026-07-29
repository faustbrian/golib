package backend

import (
	"context"
	"errors"
	"testing"
)

func FuzzCommitmentEngine(f *testing.F) {
	valid := make([]byte, 33)
	valid[32] = 1
	f.Add(valid)
	modulus := make([]byte, 33)
	copy(
		modulus[1:],
		[]byte{
			0xe1, 0xe7, 0x76, 0x28, 0xb5, 0x06, 0xfd, 0x74,
			0x71, 0x04, 0x19, 0x74, 0x00, 0x87, 0x8f, 0xff,
			0x00, 0x76, 0x68, 0x02, 0x02, 0x76, 0xce, 0x0c,
			0x52, 0x5f, 0x67, 0xca, 0xd4, 0x69, 0xfb, 0x1c,
		},
	)
	f.Add(modulus)

	engine, err := NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		f.Fatalf("new commitment engine: %v", err)
	}
	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 4*33 {
			return
		}

		var vector Vector
		for len(encoded) >= 33 {
			index := encoded[0]
			copy(vector[index][:], encoded[1:33])
			encoded = encoded[33:]
		}
		left, leftErr := engine.Commit(context.Background(), vector)
		right, rightErr := engine.Commit(context.Background(), vector)
		if leftErr != nil || rightErr != nil {
			if !errors.Is(leftErr, errInvalidScalar) ||
				!errors.Is(rightErr, errInvalidScalar) {
				t.Fatalf("commit errors differ: %v / %v", leftErr, rightErr)
			}
			return
		}

		leftIdentity, err := left.IsIdentity()
		if err != nil {
			t.Fatalf("classify first commitment: %v", err)
		}
		rightIdentity, err := right.IsIdentity()
		if err != nil {
			t.Fatalf("classify second commitment: %v", err)
		}
		if leftIdentity != rightIdentity {
			t.Fatalf("identity classification differs: %t / %t", leftIdentity, rightIdentity)
		}
		leftScalar, err := left.ScalarBytes()
		if err != nil {
			t.Fatalf("map first commitment: %v", err)
		}
		rightScalar, err := right.ScalarBytes()
		if err != nil {
			t.Fatalf("map second commitment: %v", err)
		}
		if leftScalar != rightScalar {
			t.Fatalf("mapped commitments differ: %x / %x", leftScalar, rightScalar)
		}
	})
}

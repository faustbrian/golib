package committedtree

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func FuzzBuildDeterministic(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, 64))
	seed := make([]byte, 128)
	seed[0] = 1
	seed[64] = 2
	f.Add(seed)

	builder, err := NewBuilder(
		context.Background(),
		testLimits(),
		testCommitmentLimits(),
	)
	if err != nil {
		f.Fatalf("new builder: %v", err)
	}
	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 4*64 {
			return
		}
		entries := make([]Entry, 0, len(encoded)/64)
		for len(encoded) >= 64 {
			var entry Entry
			copy(entry.Key[:], encoded[:32])
			copy(entry.Value[:], encoded[32:64])
			entries = append(entries, entry)
			encoded = encoded[64:]
		}
		reversed := slices.Clone(entries)
		slices.Reverse(reversed)

		left, leftErr := builder.Build(context.Background(), entries)
		right, rightErr := builder.Build(context.Background(), reversed)
		if leftErr != nil || rightErr != nil {
			if !errors.Is(leftErr, errDuplicateKey) || !errors.Is(rightErr, errDuplicateKey) {
				t.Fatalf("build errors differ: %v / %v", leftErr, rightErr)
			}
			return
		}
		leftRoot, err := left.Root()
		if err != nil {
			t.Fatalf("get left root: %v", err)
		}
		rightRoot, err := right.Root()
		if err != nil {
			t.Fatalf("get right root: %v", err)
		}
		leftScalar, err := leftRoot.ScalarBytes()
		if err != nil {
			t.Fatalf("map left root: %v", err)
		}
		rightScalar, err := rightRoot.ScalarBytes()
		if err != nil {
			t.Fatalf("map right root: %v", err)
		}
		if leftScalar != rightScalar {
			t.Fatalf("root fields differ: %x / %x", leftScalar, rightScalar)
		}
		leftCount, err := left.NodeCount()
		if err != nil {
			t.Fatalf("count left nodes: %v", err)
		}
		rightCount, err := right.NodeCount()
		if err != nil {
			t.Fatalf("count right nodes: %v", err)
		}
		if leftCount != rightCount {
			t.Fatalf("node counts differ: %d / %d", leftCount, rightCount)
		}

		var query Key
		if len(entries) > 0 {
			query = entries[0].Key
		}
		leftPath, err := left.ProofPath(
			context.Background(),
			query,
			testProofPathLimits(),
		)
		if err != nil {
			t.Fatalf("extract left proof path: %v", err)
		}
		rightPath, err := right.ProofPath(
			context.Background(),
			query,
			testProofPathLimits(),
		)
		if err != nil {
			t.Fatalf("extract right proof path: %v", err)
		}
		if leftPath.Kind != rightPath.Kind ||
			leftPath.Depth != rightPath.Depth ||
			leftPath.ExistingStem != rightPath.ExistingStem ||
			len(leftPath.Commitments) != len(rightPath.Commitments) {
			t.Fatalf("proof-path metadata differs: %#v / %#v", leftPath, rightPath)
		}
		for index := range leftPath.Commitments {
			leftCommitment := leftPath.Commitments[index]
			rightCommitment := rightPath.Commitments[index]
			leftScalar, leftErr := leftCommitment.Commitment.ScalarBytes()
			rightScalar, rightErr := rightCommitment.Commitment.ScalarBytes()
			if leftErr != nil ||
				rightErr != nil ||
				leftCommitment.Path != rightCommitment.Path ||
				leftCommitment.Length != rightCommitment.Length ||
				leftScalar != rightScalar {
				t.Fatalf(
					"proof-path commitment %d differs: %#v / %#v",
					index,
					leftCommitment,
					rightCommitment,
				)
			}
		}
	})
}

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

		leftImage, err := left.StorageImage(
			context.Background(),
			testStorageEncodingLimits(),
		)
		if err != nil {
			t.Fatalf("encode left storage image: %v", err)
		}
		rightImage, err := right.StorageImage(
			context.Background(),
			testStorageEncodingLimits(),
		)
		if err != nil {
			t.Fatalf("encode right storage image: %v", err)
		}
		leftID, err := leftImage.RootID()
		if err != nil {
			t.Fatalf("read left storage root ID: %v", err)
		}
		rightID, err := rightImage.RootID()
		if err != nil {
			t.Fatalf("read right storage root ID: %v", err)
		}
		if leftID != rightID {
			t.Fatalf("storage root IDs differ: %x / %x", leftID, rightID)
		}
		leftNodes, err := leftImage.Nodes(context.Background())
		if err != nil {
			t.Fatalf("read left storage nodes: %v", err)
		}
		rightNodes, err := rightImage.Nodes(context.Background())
		if err != nil {
			t.Fatalf("read right storage nodes: %v", err)
		}
		if len(leftNodes) != len(rightNodes) {
			t.Fatalf(
				"storage node counts differ: %d / %d",
				len(leftNodes),
				len(rightNodes),
			)
		}
		for index := range leftNodes {
			if leftNodes[index].ID() != rightNodes[index].ID() ||
				!slices.Equal(
					leftNodes[index].Encoded(),
					rightNodes[index].Encoded(),
				) {
				t.Fatalf("storage node %d differs", index)
			}
		}
	})
}

func FuzzBuilderUpdateMatchesFullRebuild(f *testing.F) {
	base := make([]byte, 3*64)
	base[0] = 0x10
	base[31] = 0x00
	base[32] = 0x11
	base[64] = 0x10
	base[64+31] = 0x01
	base[64+32] = 0x12
	base[128] = 0x20
	base[128+31] = 0x00
	base[128+32] = 0x21

	replaced := slices.Clone(base)
	replaced[32] = 0xf1
	f.Add(base, replaced)

	insertedSuffix := append(slices.Clone(base), make([]byte, 64)...)
	insertedSuffix[192] = 0x10
	insertedSuffix[192+31] = 0x02
	insertedSuffix[192+32] = 0x13
	f.Add(base, insertedSuffix)

	insertedStem := append(slices.Clone(base), make([]byte, 64)...)
	insertedStem[192] = 0x30
	insertedStem[192+32] = 0x31
	f.Add(base, insertedStem)
	f.Add(base, base[:2*64])
	f.Add([]byte(nil), []byte(nil))

	builder, err := NewBuilder(
		context.Background(), testLimits(), testCommitmentLimits(),
	)
	if err != nil {
		f.Fatalf("new builder: %v", err)
	}
	f.Fuzz(func(t *testing.T, baseEncoded []byte, nextEncoded []byte) {
		if len(baseEncoded) > 8*64 || len(nextEncoded) > 8*64 {
			return
		}
		baseEntries := decodeFuzzEntries(baseEncoded)
		nextEntries := decodeFuzzEntries(nextEncoded)
		baseTree, err := builder.Build(context.Background(), baseEntries)
		if err != nil {
			if !errors.Is(err, errDuplicateKey) {
				t.Fatalf("build base tree: %v", err)
			}
			return
		}

		updated, updateErr := builder.Update(
			context.Background(), baseTree, nextEntries,
		)
		rebuilt, rebuildErr := builder.Build(context.Background(), nextEntries)
		if (updateErr != nil) != (rebuildErr != nil) {
			t.Fatalf("update/rebuild errors differ: %v / %v", updateErr, rebuildErr)
		}
		if updateErr != nil {
			if !errors.Is(updateErr, errDuplicateKey) ||
				!errors.Is(rebuildErr, errDuplicateKey) {
				t.Fatalf("update/rebuild errors: %v / %v", updateErr, rebuildErr)
			}
			return
		}

		if len(nextEntries) == 0 {
			updatedRoot, err := updated.Root()
			if err != nil {
				t.Fatalf("read updated empty root: %v", err)
			}
			rebuiltRoot, err := rebuilt.Root()
			if err != nil {
				t.Fatalf("read rebuilt empty root: %v", err)
			}
			updatedEmpty, err := updatedRoot.IsIdentity()
			if err != nil {
				t.Fatalf("classify updated empty root: %v", err)
			}
			rebuiltEmpty, err := rebuiltRoot.IsIdentity()
			if err != nil {
				t.Fatalf("classify rebuilt empty root: %v", err)
			}
			if !updatedEmpty || !rebuiltEmpty {
				t.Fatal("empty update and rebuild roots differ")
			}
			return
		}
		assertSameRoot(t, updated, rebuilt)
		updatedImage, err := updated.StorageImage(
			context.Background(), testStorageEncodingLimits(),
		)
		if err != nil {
			t.Fatalf("encode updated storage image: %v", err)
		}
		rebuiltImage, err := rebuilt.StorageImage(
			context.Background(), testStorageEncodingLimits(),
		)
		if err != nil {
			t.Fatalf("encode rebuilt storage image: %v", err)
		}
		updatedNodes, err := updatedImage.Nodes(context.Background())
		if err != nil {
			t.Fatalf("read updated storage nodes: %v", err)
		}
		rebuiltNodes, err := rebuiltImage.Nodes(context.Background())
		if err != nil {
			t.Fatalf("read rebuilt storage nodes: %v", err)
		}
		if len(updatedNodes) != len(rebuiltNodes) {
			t.Fatalf(
				"storage node counts differ: %d / %d",
				len(updatedNodes), len(rebuiltNodes),
			)
		}
		for index := range updatedNodes {
			if updatedNodes[index].ID() != rebuiltNodes[index].ID() ||
				!slices.Equal(updatedNodes[index].Encoded(), rebuiltNodes[index].Encoded()) {
				t.Fatalf("storage node %d differs", index)
			}
		}
	})
}

func decodeFuzzEntries(encoded []byte) []Entry {
	entries := make([]Entry, 0, len(encoded)/64)
	for len(encoded) >= 64 {
		var entry Entry
		copy(entry.Key[:], encoded[:32])
		copy(entry.Value[:], encoded[32:64])
		entries = append(entries, entry)
		encoded = encoded[64:]
	}

	return entries
}

package merkletree_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func TestSnapshotPersistenceRoundTripAndResume(t *testing.T) {
	t.Parallel()

	leaves := consistencyLeaves(7)
	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	encoded, err := snapshot.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	decoded, err := merkletree.ParseSnapshot(
		context.Background(),
		encoded,
		merkletree.DefaultSnapshotPersistenceLimits(),
	)
	if err != nil {
		t.Fatalf("ParseSnapshot() error = %v", err)
	}
	wantRoot, err := snapshot.Root()
	if err != nil {
		t.Fatalf("snapshot.Root() error = %v", err)
	}
	gotRoot, err := decoded.Root()
	if err != nil {
		t.Fatalf("decoded.Root() error = %v", err)
	}
	if !sameRoot(gotRoot, wantRoot) {
		t.Fatal("ParseSnapshot() changed root identity")
	}
	totalBytes, err := decoded.TotalLeafBytes()
	if err != nil {
		t.Fatalf("TotalLeafBytes() error = %v", err)
	}
	var wantBytes uint64
	for _, leaf := range leaves {
		wantBytes += uint64(len(leaf.Bytes()))
	}
	if totalBytes != wantBytes {
		t.Fatalf("TotalLeafBytes() = %d, want %d", totalBytes, wantBytes)
	}

	proof, err := decoded.InclusionProof(
		context.Background(),
		4,
		merkletree.DefaultProofLimits(),
	)
	if err != nil {
		t.Fatalf("InclusionProof() error = %v", err)
	}
	if err := merkletree.VerifyInclusion(
		context.Background(),
		proof,
		leaves[4],
		merkletree.DefaultProofLimits(),
	); err != nil {
		t.Fatalf("VerifyInclusion() error = %v", err)
	}

	encodedCopy := append([]byte(nil), encoded...)
	encoded[len(encoded)-1] ^= 0xff
	again, err := decoded.MarshalBinary()
	if err != nil {
		t.Fatalf("decoded.MarshalBinary() error = %v", err)
	}
	if !bytes.Equal(again, encodedCopy) {
		t.Fatal("decoded snapshot aliases encoded input")
	}

	builder, err := merkletree.ResumeBuilder(
		context.Background(),
		decoded,
		wantBytes,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("ResumeBuilder() error = %v", err)
	}
	added := []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("next")),
		merkletree.NewRawLeaf([]byte("last")),
	}
	if err := builder.AppendBatch(context.Background(), added); err != nil {
		t.Fatalf("AppendBatch() error = %v", err)
	}
	resumed, err := builder.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	allLeaves := append(append([]merkletree.RawLeaf(nil), leaves...), added...)
	fresh, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		allLeaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot(all) error = %v", err)
	}
	resumedRoot, _ := resumed.Root()
	freshRoot, _ := fresh.Root()
	if !sameRoot(resumedRoot, freshRoot) {
		t.Fatal("resumed builder root differs from fresh construction")
	}
}

func TestSnapshotPersistenceEmptyAndRFCProfiles(t *testing.T) {
	t.Parallel()

	profiles := []merkletree.Profile{merkletree.CanonicalProfile()}
	rfc, err := merkletree.RFC9162Profile(merkletree.HashSHA256)
	if err != nil {
		t.Fatalf("RFC9162Profile() error = %v", err)
	}
	profiles = append(profiles, rfc)

	for _, profile := range profiles {
		snapshot, err := merkletree.NewSnapshot(
			context.Background(),
			profile,
			nil,
			merkletree.DefaultSnapshotLimits(),
		)
		if err != nil {
			t.Fatalf("NewSnapshot() error = %v", err)
		}
		encoded, err := snapshot.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary() error = %v", err)
		}
		decoded, err := merkletree.ParseSnapshot(
			context.Background(),
			encoded,
			merkletree.DefaultSnapshotPersistenceLimits(),
		)
		if err != nil {
			t.Fatalf("ParseSnapshot() error = %v", err)
		}
		root, err := decoded.Root()
		if err != nil {
			t.Fatalf("Root() error = %v", err)
		}
		if root.ProfileID() != profile.ID() || root.TreeSize() != 0 {
			t.Fatal("empty snapshot identity changed")
		}
		builder, err := merkletree.ResumeBuilder(
			context.Background(),
			decoded,
			0,
			merkletree.DefaultSnapshotLimits(),
		)
		if err != nil {
			t.Fatalf("ResumeBuilder() error = %v", err)
		}
		if err := builder.Append(
			context.Background(),
			merkletree.NewRawLeaf(nil),
		); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}
}

func TestSnapshotPersistenceRejectsMalformedAndBoundedInput(t *testing.T) {
	t.Parallel()

	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		consistencyLeaves(3),
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	valid, err := snapshot.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}

	for index, data := range [][]byte{
		nil,
		valid[:len(valid)-1],
		append(append([]byte(nil), valid...), 0),
		mutateEncodingByte(valid, 5, 0xff),
		mutateEncodingByte(valid, len(valid)-1, 0xff),
	} {
		if _, err := merkletree.ParseSnapshot(
			context.Background(),
			data,
			merkletree.DefaultSnapshotPersistenceLimits(),
		); err == nil {
			t.Fatalf("ParseSnapshot(malformed %d) succeeded", index)
		}
	}

	limits := merkletree.DefaultSnapshotPersistenceLimits()
	limits.MaxEncodedBytes = uint64(len(valid) - 1)
	var resourceErr *merkletree.ResourceError
	if _, err := merkletree.ParseSnapshot(
		context.Background(),
		valid,
		limits,
	); !errors.As(err, &resourceErr) ||
		resourceErr.Kind != merkletree.ResourceEncodedBytes {
		t.Fatalf("ParseSnapshot(encoded limit) error = %v", err)
	}

	limits = merkletree.DefaultSnapshotPersistenceLimits()
	limits.MaxRetainedNodes = 1
	if _, err := merkletree.ParseSnapshot(
		context.Background(),
		valid,
		limits,
	); !errors.As(err, &resourceErr) ||
		resourceErr.Kind != merkletree.ResourceRetainedNodes {
		t.Fatalf("ParseSnapshot(node limit) error = %v", err)
	}

	var zero merkletree.Snapshot
	if _, err := zero.MarshalBinary(); !errors.Is(
		err,
		merkletree.ErrInvalidSnapshot,
	) {
		t.Fatalf("zero MarshalBinary() error = %v", err)
	}
}

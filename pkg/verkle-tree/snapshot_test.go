package verkletree_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

func TestPublicSnapshotSupportsImmutableAuthenticatedTransitions(t *testing.T) {
	t.Parallel()

	keyA := publicKey(0, 0)
	keyB := publicKey(0, 1)
	zero := verkletree.Value{}
	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		[]verkletree.Entry{
			{Key: keyB, Value: publicValue(2)},
			{Key: keyA, Value: zero},
		},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	value, present, err := snapshot.Get(context.Background(), keyA)
	if err != nil || !present || value != zero {
		t.Fatalf("present zero = %x/%t, error = %v", value, present, err)
	}
	if _, present, err := snapshot.Get(
		context.Background(),
		publicKey(9, 9),
	); err != nil || present {
		t.Fatalf("absent lookup present = %t, error = %v", present, err)
	}
	preRoot, err := snapshot.Root()
	if err != nil {
		t.Fatalf("pre root: %v", err)
	}
	if profile, profileErr := preRoot.Profile(); profileErr != nil ||
		profile != verkletree.BandersnatchIPA256V0() {
		t.Fatalf("root profile = %#v, error = %v", profile, profileErr)
	}

	next, transition, err := snapshot.Apply(
		context.Background(),
		[]verkletree.Update{
			verkletree.Delete(keyB),
			verkletree.Set(keyA, publicValue(3)),
			verkletree.Set(publicKey(1, 0), publicValue(4)),
		},
	)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	oldValue, present, err := snapshot.Get(context.Background(), keyA)
	if err != nil || !present || oldValue != zero {
		t.Fatalf("old snapshot changed = %x/%t, error = %v", oldValue, present, err)
	}
	newValue, present, err := next.Get(context.Background(), keyA)
	if err != nil || !present || newValue != publicValue(3) {
		t.Fatalf("new value = %x/%t, error = %v", newValue, present, err)
	}
	if _, present, err := next.Get(context.Background(), keyB); err != nil || present {
		t.Fatalf("deleted value present = %t, error = %v", present, err)
	}
	postRoot, err := next.Root()
	if err != nil {
		t.Fatalf("post root: %v", err)
	}
	transitionPre, err := transition.PreRoot()
	if err != nil || !equalPublicRoots(t, transitionPre, preRoot) {
		t.Fatalf("transition pre-root mismatch: %v", err)
	}
	transitionPost, err := transition.PostRoot()
	if err != nil || !equalPublicRoots(t, transitionPost, postRoot) {
		t.Fatalf("transition post-root mismatch: %v", err)
	}

	encoded, err := postRoot.Bytes()
	if err != nil {
		t.Fatalf("encode root: %v", err)
	}
	decoded, err := verkletree.DecodeRoot(
		context.Background(),
		encoded[:],
		verkletree.RootDecodingLimits{
			MaxRootBytes:    verkletree.RootSize,
			MaxPointDecodes: 1,
		},
	)
	if err != nil || !equalPublicRoots(t, decoded, postRoot) {
		t.Fatalf("decode root mismatch: %v", err)
	}
}

func TestPublicSnapshotIsDeterministicAndFailsClosed(t *testing.T) {
	t.Parallel()

	entries := []verkletree.Entry{
		{Key: publicKey(2, 0), Value: publicValue(2)},
		{Key: publicKey(1, 0), Value: publicValue(1)},
	}
	forward, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		entries,
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("forward snapshot: %v", err)
	}
	reverse, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		[]verkletree.Entry{entries[1], entries[0]},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("reverse snapshot: %v", err)
	}
	forwardRoot, _ := forward.Root()
	reverseRoot, _ := reverse.Root()
	if !equalPublicRoots(t, forwardRoot, reverseRoot) {
		t.Fatal("root depends on entry order")
	}

	if _, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.Profile{},
		nil,
		publicSnapshotLimits(),
	); !errors.Is(err, verkletree.ErrUnsupportedProfile) {
		t.Fatalf("unsupported profile error = %v", err)
	}
	if _, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		nil,
		verkletree.SnapshotLimits{},
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("invalid limits error = %v", err)
	}
	if _, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		[]verkletree.Entry{entries[0], entries[0]},
		publicSnapshotLimits(),
	); !errors.Is(err, verkletree.ErrDuplicateKey) {
		t.Fatalf("duplicate error = %v", err)
	}
	limited := publicSnapshotLimits()
	limited.State.MaxEntries = 1
	_, err = verkletree.NewSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		entries,
		limited,
	)
	var resourceErr *verkletree.ResourceError
	if !errors.As(err, &resourceErr) ||
		!errors.Is(err, verkletree.ErrResourceExhausted) ||
		resourceErr.Resource != verkletree.ResourceEntries ||
		resourceErr.Limit != 1 ||
		resourceErr.Actual != 2 {
		t.Fatalf("resource error = %#v (%v)", resourceErr, err)
	}

	var zeroSnapshot verkletree.Snapshot
	if _, _, err := zeroSnapshot.Get(
		context.Background(),
		publicKey(0, 0),
	); !errors.Is(err, verkletree.ErrInvalidSnapshot) {
		t.Fatalf("zero snapshot error = %v", err)
	}
	var nilContext context.Context
	if _, err := verkletree.NewSnapshot(
		nilContext,
		verkletree.BandersnatchIPA256V0(),
		nil,
		publicSnapshotLimits(),
	); !errors.Is(err, verkletree.ErrInvalidContext) {
		t.Fatalf("nil context error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verkletree.NewSnapshot(
		cancelled,
		verkletree.BandersnatchIPA256V0(),
		nil,
		publicSnapshotLimits(),
	); !errors.Is(err, verkletree.ErrCancelled) ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
}

func publicSnapshotLimits() verkletree.SnapshotLimits {
	return verkletree.SnapshotLimits{
		State: verkletree.StateLimits{
			MaxEntries:        64,
			MaxBatchUpdates:   64,
			MaxTemporaryBytes: 16 << 20,
		},
		Tree: verkletree.TreeLimits{
			MaxEntries:         64,
			MaxStems:           64,
			MaxNodes:           128,
			MaxEdges:           128,
			MaxCommitments:     256,
			MaxFieldMappings:   256,
			MaxCommitmentTerms: 1 << 16,
			MaxTemporaryBytes:  16 << 20,
		},
		Commitment: verkletree.CommitmentLimits{
			MaxGeneratorDerivations: 256,
			MaxScalarDecodes:        256,
			MaxMSMTerms:             256,
			MaxTemporaryBytes:       1 << 20,
		},
	}
}

func publicKey(first, suffix byte) verkletree.Key {
	var key verkletree.Key
	key[0] = first
	key[31] = suffix

	return key
}

func publicValue(marker byte) verkletree.Value {
	var value verkletree.Value
	value[0] = marker

	return value
}

func equalPublicRoots(t testing.TB, left, right verkletree.Root) bool {
	t.Helper()

	leftBytes, leftErr := left.Bytes()
	rightBytes, rightErr := right.Bytes()
	if leftErr != nil || rightErr != nil {
		t.Fatalf("encode roots: %v / %v", leftErr, rightErr)
	}

	return bytes.Equal(leftBytes[:], rightBytes[:])
}

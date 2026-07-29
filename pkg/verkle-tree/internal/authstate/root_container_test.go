package authstate

import (
	"bytes"
	"context"
	"errors"
	"testing"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

func TestSnapshotRootContainerBindsProfileAndCommitment(t *testing.T) {
	t.Parallel()

	snapshot := newTestSnapshot(t, []Entry{{
		Key:   testKey(1, 2),
		Value: testValue(3),
	}})
	container, err := snapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("snapshot root container: %v", err)
	}
	profile, err := container.Profile()
	if err != nil {
		t.Fatalf("root profile: %v", err)
	}
	if profile != verkletree.ExperimentalBandersnatchIPA256V0() {
		t.Fatalf("root profile = %#v", profile)
	}
	encoded, err := container.Bytes()
	if err != nil {
		t.Fatalf("encode root container: %v", err)
	}
	wantPrefix := []byte{'V', 'K', 'R', 'T', byte(profile.ID()), 0, 0, 0, 1, 2}
	if !bytes.Equal(encoded[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("root header = %x, want %x", encoded[:len(wantPrefix)], wantPrefix)
	}
	root, err := snapshot.Root()
	if err != nil {
		t.Fatalf("snapshot root: %v", err)
	}
	rootBytes, err := root.Bytes()
	if err != nil {
		t.Fatalf("encode snapshot root: %v", err)
	}
	if !bytes.Equal(encoded[len(wantPrefix):], rootBytes[:]) {
		t.Fatalf("root payload = %x, want %x", encoded[len(wantPrefix):], rootBytes)
	}
}

func TestSnapshotRootContainerRepresentsEmptyRoot(t *testing.T) {
	t.Parallel()

	snapshot := newTestSnapshot(t, nil)
	container, err := snapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("empty root container: %v", err)
	}
	empty, err := container.IsEmpty()
	if err != nil || !empty {
		t.Fatalf("empty root = %t, error = %v", empty, err)
	}
	encoded, err := container.Bytes()
	if err != nil {
		t.Fatalf("encode empty root: %v", err)
	}
	if encoded[9] != 1 || !bytes.Equal(encoded[10:], make([]byte, len(encoded)-10)) {
		t.Fatalf("empty root encoding = %x", encoded)
	}
}

func TestTransitionRootContainersBindExactSnapshots(t *testing.T) {
	t.Parallel()

	before := newTestSnapshot(t, nil)
	after, transition, err := before.Apply(
		context.Background(),
		[]Update{Set(testKey(1, 1), testValue(1))},
	)
	if err != nil {
		t.Fatalf("apply transition: %v", err)
	}
	pre, err := transition.PreRootContainer(context.Background())
	if err != nil {
		t.Fatalf("pre-root container: %v", err)
	}
	post, err := transition.PostRootContainer(context.Background())
	if err != nil {
		t.Fatalf("post-root container: %v", err)
	}
	wantPre, err := before.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("snapshot pre-root container: %v", err)
	}
	wantPost, err := after.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("snapshot post-root container: %v", err)
	}
	preBytes, err := pre.Bytes()
	if err != nil {
		t.Fatalf("encode transition pre-root: %v", err)
	}
	wantPreBytes, err := wantPre.Bytes()
	if err != nil {
		t.Fatalf("encode snapshot pre-root: %v", err)
	}
	postBytes, err := post.Bytes()
	if err != nil {
		t.Fatalf("encode transition post-root: %v", err)
	}
	wantPostBytes, err := wantPost.Bytes()
	if err != nil {
		t.Fatalf("encode snapshot post-root: %v", err)
	}
	if preBytes != wantPreBytes || postBytes != wantPostBytes {
		t.Fatalf(
			"transition roots = %x/%x, want %x/%x",
			preBytes,
			postBytes,
			wantPreBytes,
			wantPostBytes,
		)
	}
}

func TestRootContainersRejectInvalidStateAndCancellation(t *testing.T) {
	t.Parallel()

	var snapshot Snapshot
	if _, err := snapshot.RootContainer(context.Background()); !errors.Is(
		err,
		errInvalidSnapshot,
	) {
		t.Fatalf("zero snapshot error = %v", err)
	}
	var transition Transition
	if _, err := transition.PreRootContainer(context.Background()); !errors.Is(
		err,
		errInvalidTransition,
	) {
		t.Fatalf("zero transition pre-root error = %v", err)
	}
	if _, err := transition.PostRootContainer(context.Background()); !errors.Is(
		err,
		errInvalidTransition,
	) {
		t.Fatalf("zero transition post-root error = %v", err)
	}

	snapshot = newTestSnapshot(t, nil)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := snapshot.RootContainer(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled snapshot root error = %v", err)
	}
	_, transition, err := snapshot.Apply(context.Background(), nil)
	if err != nil {
		t.Fatalf("empty transition: %v", err)
	}
	if _, err := transition.PreRootContainer(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled pre-root error = %v", err)
	}
	if _, err := transition.PostRootContainer(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled post-root error = %v", err)
	}
}

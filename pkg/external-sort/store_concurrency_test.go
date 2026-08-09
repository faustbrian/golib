package externalsort

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func TestStoreRejectsCloseWhileIterationOwnsTheStore(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	store := openTestStore(t, newTestFactory(t, parent, 1, 1, 2))
	addTestRecords(t, store, []byte{2}, []byte{1})
	workDirectory := store.directory
	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	iterationDone := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		iterationDone <- store.ForEachSorted(
			ctx,
			func([]byte) error {
				select {
				case callbackStarted <- struct{}{}:
					<-releaseCallback
				default:
				}

				return nil
			},
		)
	}()
	waitForLifecycleSignal(t, callbackStarted)

	closeErr := store.Close()
	addErr := store.Add(context.Background(), []byte{3})
	iterateErr := store.ForEachSorted(
		context.Background(),
		func([]byte) error { return nil },
	)
	_, statErr := os.Stat(workDirectory)
	cancel()
	close(releaseCallback)
	iterationErr := waitForLifecycleResult(t, iterationDone)

	if !errors.Is(closeErr, ErrConcurrentUse) {
		t.Fatalf("Close() error = %v, want concurrent use", closeErr)
	}
	if !errors.Is(addErr, ErrConcurrentUse) {
		t.Fatalf("Add() error = %v, want concurrent use", addErr)
	}
	if !errors.Is(iterateErr, ErrConcurrentUse) {
		t.Fatalf("ForEachSorted() error = %v, want concurrent use", iterateErr)
	}
	if statErr != nil {
		t.Fatalf("work directory changed during rejected Close: %v", statErr)
	}
	if !errors.Is(iterationErr, context.Canceled) {
		t.Fatalf("ForEachSorted() error = %v, want canceled", iterationErr)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() after iteration error = %v", err)
	}
	if _, err := os.Stat(workDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("work directory remains after Close: %v", err)
	}
}

func TestStoreRejectsCallbackReentry(t *testing.T) {
	t.Parallel()

	store := openTestStore(
		t,
		newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1),
	)
	addTestRecords(t, store, []byte{1})
	if err := store.ForEachSorted(
		context.Background(),
		func([]byte) error {
			if err := store.Add(context.Background(), []byte{2}); !errors.Is(
				err,
				ErrConcurrentUse,
			) {
				return errors.New("reentrant Add did not return concurrent use")
			}
			if err := store.Close(); !errors.Is(err, ErrConcurrentUse) {
				return errors.New("reentrant Close did not return concurrent use")
			}

			return nil
		},
	); err != nil {
		t.Fatalf("ForEachSorted() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStoreRejectsAConcurrentRepeatedClose(t *testing.T) {
	t.Parallel()

	store := openTestStore(
		t,
		newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1),
	)
	removeStarted := make(chan struct{})
	releaseRemove := make(chan struct{})
	removeAll := store.removeAll
	store.removeAll = func(path string) error {
		close(removeStarted)
		<-releaseRemove

		return removeAll(path)
	}
	firstCloseDone := make(chan error, 1)
	go func() { firstCloseDone <- store.Close() }()
	waitForLifecycleSignal(t, removeStarted)

	secondCloseErr := store.Close()
	close(releaseRemove)
	firstCloseErr := waitForLifecycleResult(t, firstCloseDone)

	if !errors.Is(secondCloseErr, ErrConcurrentUse) {
		t.Fatalf("concurrent Close() error = %v, want concurrent use", secondCloseErr)
	}
	if firstCloseErr != nil {
		t.Fatalf("first Close() error = %v", firstCloseErr)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("idempotent Close() error = %v", err)
	}
}

func TestStoreRejectsCloseDuringCorruptionDetection(t *testing.T) {
	t.Parallel()

	store := openTestStore(
		t,
		newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1),
	)
	addTestRecords(t, store, []byte{1})
	contents, err := os.ReadFile(store.chunks[0])
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	contents[len(contents)-1] ^= 0xff
	if err := os.WriteFile(store.chunks[0], contents, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	readStarted := make(chan struct{})
	releaseRead := make(chan struct{})
	openFile := store.openFile
	store.openFile = func(path string) (chunkFile, error) {
		file, err := openFile(path)
		if err != nil {
			return nil, err
		}

		return &blockingReadChunkFile{
			chunkFile: file,
			started:   readStarted,
			release:   releaseRead,
		}, nil
	}
	iterationDone := make(chan error, 1)
	go func() {
		iterationDone <- store.ForEachSorted(
			context.Background(),
			func([]byte) error { return nil },
		)
	}()
	waitForLifecycleSignal(t, readStarted)
	closeErr := store.Close()
	close(releaseRead)
	iterationErr := waitForLifecycleResult(t, iterationDone)

	if !errors.Is(closeErr, ErrConcurrentUse) {
		t.Fatalf("Close() error = %v, want concurrent use", closeErr)
	}
	if !errors.Is(iterationErr, ErrCorrupt) {
		t.Fatalf("ForEachSorted() error = %v, want corrupt", iterationErr)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() after corruption error = %v", err)
	}
}

type blockingReadChunkFile struct {
	chunkFile
	once    sync.Once
	started chan<- struct{}
	release <-chan struct{}
}

func (file *blockingReadChunkFile) Read(destination []byte) (int, error) {
	file.once.Do(func() {
		close(file.started)
		<-file.release
	})

	return file.chunkFile.Read(destination)
}

func waitForLifecycleSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycle operation did not reach the expected boundary")
	}
}

func waitForLifecycleResult(t *testing.T, result <-chan error) error {
	t.Helper()

	select {
	case err := <-result:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycle operation did not return")

		return nil
	}
}

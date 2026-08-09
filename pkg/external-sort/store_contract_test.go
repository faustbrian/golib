package externalsort

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFactoryRejectsUnsafeStorageAndInvalidResourceBounds(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	invalid := []Config{
		{},
		{ParentDirectory: "relative", RecordBytes: 1, ChunkRecords: 1, MaximumRecords: 1},
		{ParentDirectory: parent, RecordBytes: 0, ChunkRecords: 1, MaximumRecords: 1},
		{ParentDirectory: parent, RecordBytes: MaximumRecordBytes + 1, ChunkRecords: 1, MaximumRecords: 1},
		{ParentDirectory: parent, RecordBytes: 1, ChunkRecords: 0, MaximumRecords: 1},
		{ParentDirectory: parent, RecordBytes: 1, ChunkRecords: MaximumChunkRecords + 1, MaximumRecords: MaximumChunkRecords + 1},
		{ParentDirectory: parent, RecordBytes: 1, ChunkRecords: 2, MaximumRecords: 1},
		{ParentDirectory: parent, RecordBytes: MaximumChunkBytes, ChunkRecords: 2, MaximumRecords: 2},
		{ParentDirectory: parent, RecordBytes: 1, ChunkRecords: 1, MaximumRecords: MaximumMergeFiles + 1},
	}
	for _, config := range invalid {
		if _, err := NewFactory(config); !errors.Is(
			err,
			ErrInvalidConfiguration,
		) {
			t.Fatalf("NewFactory(%+v) error = %v", config, err)
		}
	}

	worldReadable := t.TempDir()
	if err := os.Chmod(worldReadable, 0o755); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	assertUnsafeParent(t, worldReadable)

	file := filepath.Join(parent, "file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	assertUnsafeParent(t, file)

	link := filepath.Join(parent, "link")
	if err := os.Symlink(parent, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	assertUnsafeParent(t, link)
	assertUnsafeParent(t, filepath.Join(parent, "missing"))
}

func TestStoreRejectsInvalidRequestsLimitsAndReuse(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	factory := newTestFactory(t, parent, 2, 2, 2)
	if _, err := factory.Open(
		nilContextForContractTest(),
		bytes.Repeat([]byte{1}, AES256KeyBytes),
	); !errors.Is(
		err,
		ErrInvalidConfiguration,
	) {
		t.Fatalf("Open(nil) error = %v", err)
	}
	if _, err := factory.Open(context.Background(), []byte("short")); !errors.Is(
		err,
		ErrInvalidKey,
	) {
		t.Fatalf("Open(short key) error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := factory.Open(
		cancelled,
		bytes.Repeat([]byte{1}, AES256KeyBytes),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open(cancelled) error = %v", err)
	}

	store := openTestStore(t, factory)
	if err := store.Add(
		nilContextForContractTest(),
		[]byte{1, 1},
	); !errors.Is(
		err,
		ErrInvalidConfiguration,
	) {
		t.Fatalf("Add(nil) error = %v", err)
	}
	if err := store.Add(context.Background(), []byte{1}); !errors.Is(
		err,
		ErrInvalidRecord,
	) {
		t.Fatalf("Add(invalid record) error = %v", err)
	}
	if err := store.Add(cancelled, []byte{1, 1}); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("Add(cancelled) error = %v", err)
	}
	for _, record := range [][]byte{{2, 2}, {1, 1}} {
		if err := store.Add(context.Background(), record); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}
	if err := store.Add(context.Background(), []byte{3, 3}); !errors.Is(
		err,
		ErrRecordLimit,
	) {
		t.Fatalf("Add(over limit) error = %v", err)
	}
	if err := store.ForEachSorted(
		nilContextForContractTest(),
		func([]byte) error { return nil },
	); !errors.Is(
		err,
		ErrInvalidConfiguration,
	) {
		t.Fatalf("ForEachSorted(nil) error = %v", err)
	}
	if err := store.ForEachSorted(
		context.Background(),
		nil,
	); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("ForEachSorted(nil callback) error = %v", err)
	}
	if err := store.ForEachSorted(
		context.Background(),
		func([]byte) error { return nil },
	); err != nil {
		t.Fatalf("ForEachSorted() error = %v", err)
	}
	if err := store.Add(context.Background(), []byte{4, 4}); !errors.Is(
		err,
		ErrFinalized,
	) {
		t.Fatalf("Add(after iteration) error = %v", err)
	}
	if err := store.ForEachSorted(
		context.Background(),
		func([]byte) error { return nil },
	); !errors.Is(err, ErrFinalized) {
		t.Fatalf("second ForEachSorted() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := store.Add(context.Background(), []byte{4, 4}); !errors.Is(
		err,
		ErrClosed,
	) {
		t.Fatalf("Add(after Close) error = %v", err)
	}
	if err := store.ForEachSorted(
		context.Background(),
		func([]byte) error { return nil },
	); !errors.Is(err, ErrClosed) {
		t.Fatalf("ForEachSorted(after Close) error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	var nilStore *Store
	if err := nilStore.Add(context.Background(), []byte{1}); !errors.Is(
		err,
		ErrInvalidConfiguration,
	) {
		t.Fatalf("nil Add() error = %v", err)
	}
	if err := nilStore.ForEachSorted(
		context.Background(),
		func([]byte) error { return nil },
	); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("nil ForEachSorted() error = %v", err)
	}
	if err := nilStore.Close(); err != nil {
		t.Fatalf("nil Close() error = %v", err)
	}

	zeroStore := &Store{}
	if err := zeroStore.Add(
		context.Background(),
		[]byte{1},
	); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("zero Add() error = %v", err)
	}
	if err := zeroStore.ForEachSorted(
		context.Background(),
		func([]byte) error { return nil },
	); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("zero ForEachSorted() error = %v", err)
	}
	if err := zeroStore.Close(); !errors.Is(
		err,
		ErrInvalidConfiguration,
	) {
		t.Fatalf("zero Close() error = %v", err)
	}
}

func TestFactoryAndStoreReportOwnedStorageFailures(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	key := bytes.Repeat([]byte{1}, AES256KeyBytes)
	var nilFactory *Factory
	if _, err := nilFactory.Open(context.Background(), key); !errors.Is(
		err,
		ErrInvalidConfiguration,
	) {
		t.Fatalf("nil Factory.Open() error = %v", err)
	}

	invalidFactory := newTestFactory(t, parent, 1, 1, 1)
	invalidFactory.entropy = nil
	if _, err := invalidFactory.Open(
		context.Background(),
		key,
	); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("invalid Factory.Open() error = %v", err)
	}

	missingParent := ownerOnlyTemporaryDirectory(t)
	unsafeFactory := newTestFactory(t, missingParent, 1, 1, 1)
	if err := os.Remove(missingParent); err != nil {
		t.Fatalf("Remove(parent) error = %v", err)
	}
	if _, err := unsafeFactory.Open(
		context.Background(),
		key,
	); !errors.Is(err, ErrUnsafeParent) {
		t.Fatalf("unsafe Factory.Open() error = %v", err)
	}

	mkdirFactory := newTestFactory(t, parent, 1, 1, 1)
	mkdirFactory.mkdirTemp = func(string, string) (string, error) {
		return "", errors.New("mkdir failed")
	}
	if _, err := mkdirFactory.Open(
		context.Background(),
		key,
	); !errors.Is(err, ErrStorage) {
		t.Fatalf("mkdir Factory.Open() error = %v", err)
	}

	chmodFactory := newTestFactory(t, parent, 1, 1, 1)
	chmodFactory.chmod = func(string, os.FileMode) error {
		return errors.New("chmod failed")
	}
	var removedAfterChmodFailure bool
	chmodFactory.removeAll = func(path string) error {
		removedAfterChmodFailure = true

		return os.RemoveAll(path)
	}
	if _, err := chmodFactory.Open(
		context.Background(),
		key,
	); !errors.Is(err, ErrStorage) {
		t.Fatalf("chmod Factory.Open() error = %v", err)
	}
	if !removedAfterChmodFailure {
		t.Fatal("Open() did not remove a directory after chmod failure")
	}

	closeStore := openTestStore(t, newTestFactory(t, parent, 1, 1, 1))
	closeStore.removeAll = func(string) error {
		return errors.New("remove failed")
	}
	if err := closeStore.Close(); !errors.Is(err, ErrStorage) {
		t.Fatalf("Close() error = %v, want storage", err)
	}
	if err := closeStore.Add(context.Background(), []byte{1}); !errors.Is(
		err,
		ErrClosed,
	) {
		t.Fatalf("Add(after failed Close) error = %v", err)
	}
	if err := closeStore.ForEachSorted(
		context.Background(),
		func([]byte) error { return nil },
	); !errors.Is(err, ErrClosed) {
		t.Fatalf("ForEachSorted(after failed Close) error = %v", err)
	}
	closeStore.removeAll = os.RemoveAll
	if err := closeStore.Close(); err != nil {
		t.Fatalf("retry Close() error = %v", err)
	}
}

func TestFactoryForcesOwnerOnlyWorkDirectoryPermissions(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	directory := filepath.Join(parent, "work")
	if err := os.Mkdir(directory, 0o000); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	factory := newTestFactory(t, parent, 1, 1, 1)
	factory.mkdirTemp = func(string, string) (string, error) {
		return directory, nil
	}
	store := openTestStore(t, factory)
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("work directory mode = %04o, want 0700", info.Mode().Perm())
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestFactoryOpensIndependentStoresConcurrently(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	factory := newTestFactory(t, parent, 4, 1, 1)
	key := bytes.Repeat([]byte{1}, AES256KeyBytes)
	const workers = 8
	failures := make(chan error, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func(value byte) {
			defer wait.Done()

			store, err := factory.Open(context.Background(), key)
			if err != nil {
				failures <- fmt.Errorf("Open: %w", err)

				return
			}
			record := bytes.Repeat([]byte{value}, 4)
			if err := store.Add(context.Background(), record); err != nil {
				failures <- fmt.Errorf("Add: %w", err)
				_ = store.Close()

				return
			}
			if err := store.ForEachSorted(
				context.Background(),
				func(actual []byte) error {
					if !bytes.Equal(actual, record) {
						return errors.New("unexpected sorted record")
					}

					return nil
				},
			); err != nil {
				failures <- fmt.Errorf("ForEachSorted: %w", err)
				_ = store.Close()

				return
			}
			if err := store.Close(); err != nil {
				failures <- fmt.Errorf("Close: %w", err)
			}
		}(byte(worker + 1))
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

func TestStoreFinalizesEmptyInputAndPropagatesCallbackAndCancellation(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	empty := openTestStore(t, newTestFactory(t, parent, 4, 2, 2))
	if err := empty.ForEachSorted(
		context.Background(),
		func([]byte) error {
			t.Fatal("empty store yielded a record")

			return nil
		},
	); err != nil {
		t.Fatalf("empty ForEachSorted() error = %v", err)
	}
	if err := empty.Close(); err != nil {
		t.Fatalf("empty Close() error = %v", err)
	}

	callbackStore := openTestStore(
		t,
		newTestFactory(t, parent, 4, 1, 2),
	)
	addTestRecords(t, callbackStore, []byte{1, 1, 1, 1})
	sentinel := errors.New("stop")
	if err := callbackStore.ForEachSorted(
		context.Background(),
		func([]byte) error { return sentinel },
	); !errors.Is(err, sentinel) {
		t.Fatalf("callback ForEachSorted() error = %v", err)
	}
	if err := callbackStore.Close(); err != nil {
		t.Fatalf("callback Close() error = %v", err)
	}

	cancelStore := openTestStore(t, newTestFactory(t, parent, 4, 1, 2))
	addTestRecords(
		t,
		cancelStore,
		[]byte{1, 1, 1, 1},
		[]byte{2, 2, 2, 2},
	)
	ctx, cancel := context.WithCancel(context.Background())
	if err := cancelStore.ForEachSorted(
		ctx,
		func([]byte) error {
			cancel()

			return nil
		},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ForEachSorted() error = %v", err)
	}
	if err := cancelStore.Close(); err != nil {
		t.Fatalf("cancel Close() error = %v", err)
	}

	preCancelledStore := openTestStore(
		t,
		newTestFactory(t, parent, 4, 2, 2),
	)
	if err := preCancelledStore.ForEachSorted(
		cancelledContext(),
		func([]byte) error { return nil },
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled ForEachSorted() error = %v", err)
	}
	if err := preCancelledStore.Close(); err != nil {
		t.Fatalf("pre-cancelled Close() error = %v", err)
	}
}

func TestStoreRejectsEntropyFailureWithoutRetainingRecord(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	factory := newTestFactory(t, parent, 4, 1, 1)
	factory.entropy = errorReader{err: errors.New("entropy unavailable")}
	store := openTestStore(t, factory)
	record := []byte{1, 2, 3, 4}
	if err := store.Add(context.Background(), record); !errors.Is(
		err,
		ErrEntropy,
	) {
		t.Fatalf("Add() error = %v, want entropy", err)
	}
	entries, err := os.ReadDir(store.directory)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entropy failure left %d temporary artifacts", len(entries))
	}
	store.entropy = bytes.NewReader(make([]byte, store.cipher.NonceSize()))
	if err := store.Add(context.Background(), record); err != nil {
		t.Fatalf("retry Add() error = %v", err)
	}
	if store.total != 1 {
		t.Fatalf("record count = %d, want 1", store.total)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	iterationFactory := newTestFactory(t, parent, 4, 2, 2)
	iterationStore := openTestStore(t, iterationFactory)
	addTestRecords(t, iterationStore, record)
	iterationStore.entropy = errorReader{
		err: errors.New("entropy unavailable"),
	}
	if err := iterationStore.ForEachSorted(
		context.Background(),
		func([]byte) error { return nil },
	); !errors.Is(err, ErrEntropy) {
		t.Fatalf("ForEachSorted() error = %v, want entropy", err)
	}
	if err := iterationStore.Close(); err != nil {
		t.Fatalf("iteration Close() error = %v", err)
	}

	rollbackFactory := newTestFactory(t, parent, 4, 2, 2)
	rollbackStore := openTestStore(t, rollbackFactory)
	previous := []byte{9, 9, 9, 9}
	failed := []byte{1, 1, 1, 1}
	addTestRecords(t, rollbackStore, previous)
	rollbackStore.entropy = errorReader{
		err: errors.New("entropy unavailable"),
	}
	if err := rollbackStore.Add(
		context.Background(),
		failed,
	); !errors.Is(err, ErrEntropy) {
		t.Fatalf("rollback Add() error = %v, want entropy", err)
	}
	if rollbackStore.total != 1 ||
		rollbackStore.buffer.Len() != 1 ||
		!bytes.Equal(rollbackStore.buffer.record(0), previous) {
		t.Fatalf(
			"failed Add retained %v with total=%d, want prior record %v",
			rollbackStore.buffer.data,
			rollbackStore.total,
			previous,
		)
	}
	if err := rollbackStore.Close(); err != nil {
		t.Fatalf("rollback Close() error = %v", err)
	}
}

func TestStoreReadsAFreshNonceForEveryTemporaryRecord(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	store := openTestStore(t, newTestFactory(t, parent, 4, 2, 2))
	nonceBytes := store.cipher.NonceSize()
	firstNonce := bytes.Repeat([]byte{1}, nonceBytes)
	secondNonce := bytes.Repeat([]byte{2}, nonceBytes)
	store.entropy = bytes.NewReader(append(bytes.Clone(firstNonce), secondNonce...))
	addTestRecords(t, store, []byte{2, 2, 2, 2}, []byte{1, 1, 1, 1})

	contents, err := os.ReadFile(store.chunks[0])
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	encryptedRecordBytes := nonceBytes + store.config.RecordBytes + store.cipher.Overhead()
	if len(contents) != 2*encryptedRecordBytes {
		t.Fatalf("chunk bytes = %d, want %d", len(contents), 2*encryptedRecordBytes)
	}
	actualFirst := contents[:nonceBytes]
	actualSecond := contents[encryptedRecordBytes : encryptedRecordBytes+nonceBytes]
	if !bytes.Equal(actualFirst, firstNonce) || !bytes.Equal(actualSecond, secondNonce) {
		t.Fatalf("temporary records did not consume distinct nonce bytes")
	}
}

func TestStoreFailsClosedForChunkWriteSyncCloseAndCancellation(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		file    *fakeChunkFile
		wantErr error
	}{
		"chmod": {
			file: &fakeChunkFile{
				chmodErr: errors.New("chmod failed"),
			},
			wantErr: ErrStorage,
		},
		"write": {
			file: &fakeChunkFile{
				writeErr: errors.New("write failed"),
			},
			wantErr: ErrStorage,
		},
		"sync": {
			file: &fakeChunkFile{
				syncErr: errors.New("sync failed"),
			},
			wantErr: ErrStorage,
		},
		"close": {
			file: &fakeChunkFile{
				closeErr: errors.New("close failed"),
			},
			wantErr: ErrStorage,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			parent := ownerOnlyTemporaryDirectory(t)
			store := openTestStore(
				t,
				newTestFactory(t, parent, 4, 1, 1),
			)
			testCase.file.name = filepath.Join(parent, "fake")
			store.createTemp = func(string, string) (chunkFile, error) {
				return testCase.file, nil
			}
			if err := store.Add(
				context.Background(),
				[]byte{1, 2, 3, 4},
			); !errors.Is(err, testCase.wantErr) {
				t.Fatalf("Add() error = %v, want %v", err, testCase.wantErr)
			}
			if store.total != 0 || store.buffer.Len() != 0 {
				t.Fatalf(
					"failed Add retained total=%d buffered=%d",
					store.total,
					store.buffer.Len(),
				)
			}
		})
	}

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		parent := ownerOnlyTemporaryDirectory(t)
		store := openTestStore(t, newTestFactory(t, parent, 4, 1, 1))
		store.createTemp = func(string, string) (chunkFile, error) {
			return nil, errors.New("create failed")
		}
		if err := store.Add(
			context.Background(),
			[]byte{1, 2, 3, 4},
		); !errors.Is(err, ErrStorage) {
			t.Fatalf("Add() error = %v, want storage", err)
		}
	})

	t.Run("cleanup failure seals store", func(t *testing.T) {
		t.Parallel()

		parent := ownerOnlyTemporaryDirectory(t)
		store := openTestStore(t, newTestFactory(t, parent, 4, 1, 1))
		store.entropy = errorReader{err: errors.New("entropy unavailable")}
		store.remove = func(string) error {
			return errors.New("remove failed")
		}
		if err := store.Add(
			context.Background(),
			[]byte{1, 2, 3, 4},
		); !errors.Is(err, ErrStorage) {
			t.Fatalf("Add() error = %v, want storage", err)
		}
		if err := store.Add(
			context.Background(),
			[]byte{1, 2, 3, 4},
		); !errors.Is(err, ErrClosed) {
			t.Fatalf("Add(after cleanup failure) error = %v, want closed", err)
		}
		workDirectory := store.directory
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if _, err := os.Stat(workDirectory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temporary directory remains after Close: %v", err)
		}
	})

	t.Run("cancel before spill", func(t *testing.T) {
		t.Parallel()

		parent := ownerOnlyTemporaryDirectory(t)
		store := openTestStore(t, newTestFactory(t, parent, 4, 2, 2))
		store.buffer.append([]byte{1, 2, 3, 4})
		store.total = 1
		if err := store.spill(cancelledContext()); !errors.Is(
			err,
			context.Canceled,
		) {
			t.Fatalf("spill() error = %v, want cancelled", err)
		}
	})

	t.Run("cancel between records", func(t *testing.T) {
		t.Parallel()

		parent := ownerOnlyTemporaryDirectory(t)
		store := openTestStore(t, newTestFactory(t, parent, 4, 2, 2))
		ctx, cancel := context.WithCancel(context.Background())
		store.entropy = &cancellingReader{
			reader: bytes.NewReader(make([]byte, store.cipher.NonceSize())),
			cancel: cancel,
		}
		addTestRecords(t, store, []byte{1, 1, 1, 1})
		if err := store.Add(
			ctx,
			[]byte{2, 2, 2, 2},
		); !errors.Is(err, context.Canceled) {
			t.Fatalf("Add() error = %v, want cancelled", err)
		}
		if store.total != 1 || store.buffer.Len() != 1 {
			t.Fatalf(
				"cancelled Add retained total=%d buffered=%d",
				store.total,
				store.buffer.Len(),
			)
		}
	})
}

func TestStoreAuthenticatesChunkPositionAndRejectsCorruption(t *testing.T) {
	t.Parallel()

	testCases := map[string]func(*testing.T, string, int){
		"truncated": func(t *testing.T, path string, _ int) {
			t.Helper()
			if err := os.Truncate(path, 1); err != nil {
				t.Fatalf("Truncate() error = %v", err)
			}
		},
		"complete final record removed": func(t *testing.T, path string, recordBytes int) {
			t.Helper()
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("Stat() error = %v", err)
			}
			encryptedBytes := int64(12 + recordBytes + 16)
			if err := os.Truncate(path, info.Size()-encryptedBytes); err != nil {
				t.Fatalf("Truncate() error = %v", err)
			}
		},
		"trailing byte appended": func(t *testing.T, path string, _ int) {
			t.Helper()
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatalf("OpenFile() error = %v", err)
			}
			if _, err := file.Write([]byte{0}); err != nil {
				_ = file.Close()
				t.Fatalf("Write() error = %v", err)
			}
			if err := file.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
		},
		"ciphertext changed": func(t *testing.T, path string, _ int) {
			t.Helper()
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			contents[len(contents)-1] ^= 0xff
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
		},
		"record reordered": func(t *testing.T, path string, recordBytes int) {
			t.Helper()
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			encryptedBytes := 12 + recordBytes + 16
			first := append([]byte(nil), contents[:encryptedBytes]...)
			copy(contents[:encryptedBytes], contents[encryptedBytes:])
			copy(contents[encryptedBytes:], first)
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
		},
	}
	for name, mutate := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			parent := ownerOnlyTemporaryDirectory(t)
			factory := newTestFactory(t, parent, 4, 2, 2)
			store := openTestStore(t, factory)
			addTestRecords(
				t,
				store,
				[]byte{2, 2, 2, 2},
				[]byte{1, 1, 1, 1},
			)
			mutate(t, store.chunks[0], factory.config.RecordBytes)
			yielded := 0
			unexpectedYield := errors.New("corrupt chunk yielded excess records")
			if err := store.ForEachSorted(
				context.Background(),
				func([]byte) error {
					yielded++
					if yielded > factory.config.MaximumRecords {
						return unexpectedYield
					}

					return nil
				},
			); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("ForEachSorted() error = %v, want corrupt", err)
			}
			if err := store.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
		})
	}
}

func TestStoreRejectsCrossChunkSubstitution(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	store := openTestStore(t, newTestFactory(t, parent, 4, 1, 2))
	addTestRecords(t, store, []byte{1, 1, 1, 1}, []byte{2, 2, 2, 2})
	first, err := os.ReadFile(store.chunks[0])
	if err != nil {
		t.Fatalf("ReadFile(first) error = %v", err)
	}
	second, err := os.ReadFile(store.chunks[1])
	if err != nil {
		t.Fatalf("ReadFile(second) error = %v", err)
	}
	if err := os.WriteFile(store.chunks[0], second, 0o600); err != nil {
		t.Fatalf("WriteFile(first) error = %v", err)
	}
	if err := os.WriteFile(store.chunks[1], first, 0o600); err != nil {
		t.Fatalf("WriteFile(second) error = %v", err)
	}

	if err := store.ForEachSorted(
		context.Background(),
		func([]byte) error { return nil },
	); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("ForEachSorted() error = %v, want corrupt", err)
	}
}

func TestStoreReportsMissingChunksWithoutExposingPathsOrRecords(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	store := openTestStore(t, newTestFactory(t, parent, 4, 1, 1))
	record := []byte{9, 9, 9, 9}
	addTestRecords(t, store, record)
	if err := os.Remove(store.chunks[0]); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	err := store.ForEachSorted(
		context.Background(),
		func([]byte) error { return nil },
	)
	if err == nil {
		t.Fatal("ForEachSorted() error = nil, want storage")
	}
	if !errors.Is(err, ErrStorage) {
		t.Fatalf("ForEachSorted() error = %v, want storage", err)
	}
	if strings.Contains(err.Error(), parent) ||
		strings.Contains(err.Error(), string(record)) {
		t.Fatalf("error exposed a path or record: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestFactoryAndStoreRepresentationsRedactStorageAndRecords(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	factory := newTestFactory(t, parent, 4, 2, 2)
	store := openTestStore(t, factory)
	record := []byte{91, 92, 93, 94}
	addTestRecords(t, store, record)

	factoryJSON, err := json.Marshal(factory)
	if err != nil {
		t.Fatalf("Marshal(factory) error = %v", err)
	}
	storeJSON, err := json.Marshal(store)
	if err != nil {
		t.Fatalf("Marshal(store) error = %v", err)
	}
	factoryValueJSON, err := json.Marshal(*factory)
	if err != nil {
		t.Fatalf("Marshal(factory value) error = %v", err)
	}
	storeValueJSON, err := json.Marshal(*store)
	if err != nil {
		t.Fatalf("Marshal(store value) error = %v", err)
	}
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, nil))
	logger.Info("values", "factory", factory, "store", store)

	representations := []string{
		fmt.Sprint(factory),
		fmt.Sprintf("%+v", factory),
		fmt.Sprintf("%#v", factory),
		fmt.Sprint(*factory),
		fmt.Sprintf("%+v", *factory),
		fmt.Sprintf("%#v", *factory),
		fmt.Sprint(store),
		fmt.Sprintf("%+v", store),
		fmt.Sprintf("%#v", store),
		fmt.Sprint(*store),
		fmt.Sprintf("%+v", *store),
		fmt.Sprintf("%#v", *store),
		string(factoryJSON),
		string(storeJSON),
		string(factoryValueJSON),
		string(storeValueJSON),
		log.String(),
	}
	for _, representation := range representations {
		if !strings.Contains(representation, "[REDACTED]") {
			t.Fatalf("representation was not redacted: %q", representation)
		}
		if strings.Contains(representation, parent) ||
			strings.Contains(representation, string(record)) ||
			strings.Contains(representation, storePrefix) {
			t.Fatalf(
				"representation exposed storage or records: %q",
				representation,
			)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStoreReportsReaderCloseFailuresAfterCompleteIteration(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	store := openTestStore(t, newTestFactory(t, parent, 4, 1, 1))
	addTestRecords(t, store, []byte{1, 1, 1, 1})
	store.openFile = func(path string) (chunkFile, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}

		return &fakeChunkFile{
			name:     file.Name(),
			reader:   file,
			closeErr: errors.New("close failed"),
		}, nil
	}
	if err := store.ForEachSorted(
		context.Background(),
		func([]byte) error { return nil },
	); !errors.Is(err, ErrStorage) {
		t.Fatalf("ForEachSorted() error = %v, want storage", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStoreClosesChunkReadersWhenCallbackPanics(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	store := openTestStore(t, newTestFactory(t, parent, 4, 1, 1))
	addTestRecords(t, store, []byte{1, 1, 1, 1})
	var closes int
	store.openFile = func(path string) (chunkFile, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}

		return &fakeChunkFile{
			name:       file.Name(),
			reader:     file,
			closeCount: &closes,
		}, nil
	}

	var yielded []byte
	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("ForEachSorted() did not propagate callback panic")
			}
		}()
		_ = store.ForEachSorted(
			context.Background(),
			func(record []byte) error {
				yielded = record
				panic("callback panic")
			},
		)
	}()
	if closes != 1 {
		t.Fatalf("reader closes = %d, want 1", closes)
	}
	if !bytes.Equal(yielded, make([]byte, len(yielded))) {
		t.Fatalf("yielded plaintext after panic = %v, want cleared", yielded)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestPrivateIOHelpersDetectShortWritesAndCloseFailures(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("write failed")
	if err := writeFull(
		&scriptedWriter{steps: []writeStep{{count: 1}, {err: sentinel}}},
		[]byte{1, 2},
	); !errors.Is(err, sentinel) {
		t.Fatalf("writeFull(error) = %v", err)
	}
	if err := writeFull(
		&scriptedWriter{steps: []writeStep{{}}},
		[]byte{1},
	); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeFull(short) = %v", err)
	}

	path := filepath.Join(t.TempDir(), "reader")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("initial Close() error = %v", err)
	}
	if err := closeReaders([]*chunkReader{{file: file}}); !errors.Is(
		err,
		ErrStorage,
	) {
		t.Fatalf("closeReaders() error = %v", err)
	}

	records := recordHeap{
		{record: []byte{1}, reader: 1},
		{record: []byte{1}, reader: 2},
	}
	if !records.Less(0, 1) {
		t.Fatal("equal heap records were not ordered by reader")
	}
}

func TestMergeAcceptsAnEmptyChunkAndReturnsReaderErrors(t *testing.T) {
	t.Parallel()

	block, err := aes.NewCipher(bytes.Repeat([]byte{1}, AES256KeyBytes))
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	authenticatedCipher, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("NewGCM() error = %v", err)
	}
	emptyFile, err := os.Open(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, os.ErrNotExist) {
		if emptyFile != nil {
			_ = emptyFile.Close()
		}
		t.Fatalf("Open(missing) error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	emptyFile, err = os.Open(path)
	if err != nil {
		t.Fatalf("Open(empty) error = %v", err)
	}
	if err := merge(
		context.Background(),
		[]*chunkReader{{
			file: emptyFile, cipher: authenticatedCipher, recordBytes: 4,
		}},
		func([]byte) error {
			t.Fatal("empty reader yielded a record")

			return nil
		},
	); err != nil {
		t.Fatalf("merge(empty) error = %v", err)
	}
	if err := emptyFile.Close(); err != nil {
		t.Fatalf("Close(empty) error = %v", err)
	}
}

func assertUnsafeParent(t *testing.T, parent string) {
	t.Helper()

	if _, err := NewFactory(Config{
		ParentDirectory: parent,
		RecordBytes:     1,
		ChunkRecords:    1,
		MaximumRecords:  1,
	}); !errors.Is(err, ErrUnsafeParent) {
		t.Fatalf("NewFactory(%q) error = %v", parent, err)
	}
}

func newTestFactory(
	t *testing.T,
	parent string,
	recordBytes int,
	chunkRecords int,
	maximumRecords int,
) *Factory {
	t.Helper()

	factory, err := NewFactory(Config{
		ParentDirectory: parent,
		RecordBytes:     recordBytes,
		ChunkRecords:    chunkRecords,
		MaximumRecords:  maximumRecords,
	})
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}

	return factory
}

func openTestStore(t *testing.T, factory *Factory) *Store {
	t.Helper()

	store, err := factory.Open(
		context.Background(),
		bytes.Repeat([]byte{1}, AES256KeyBytes),
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	return store
}

func addTestRecords(t *testing.T, store *Store, records ...[]byte) {
	t.Helper()

	for _, record := range records {
		if err := store.Add(context.Background(), record); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}

func nilContextForContractTest() context.Context {
	return nil
}

type errorReader struct {
	err error
}

type cancellingReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (reader *cancellingReader) Read(destination []byte) (int, error) {
	count, err := reader.reader.Read(destination)
	reader.cancel()

	return count, err
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

type writeStep struct {
	count int
	err   error
}

type scriptedWriter struct {
	steps []writeStep
}

func (writer *scriptedWriter) Write([]byte) (int, error) {
	step := writer.steps[0]
	writer.steps = writer.steps[1:]

	return step.count, step.err
}

type fakeChunkFile struct {
	name       string
	reader     io.Reader
	writeErr   error
	chmodErr   error
	syncErr    error
	closeErr   error
	closeCount *int
}

func (file *fakeChunkFile) Read(destination []byte) (int, error) {
	if file.reader == nil {
		return 0, io.EOF
	}

	return file.reader.Read(destination)
}

func (file *fakeChunkFile) Write(data []byte) (int, error) {
	if file.writeErr != nil {
		return 0, file.writeErr
	}

	return len(data), nil
}

func (file *fakeChunkFile) Name() string {
	return file.name
}

func (file *fakeChunkFile) Chmod(os.FileMode) error {
	return file.chmodErr
}

func (file *fakeChunkFile) Sync() error {
	return file.syncErr
}

func (file *fakeChunkFile) Close() error {
	if file.closeCount != nil {
		(*file.closeCount)++
	}
	if closer, ok := file.reader.(io.Closer); ok {
		_ = closer.Close()
	}

	return file.closeErr
}

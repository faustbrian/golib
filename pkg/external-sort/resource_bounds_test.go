package externalsort

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestStoreBoundsTemporaryBytesDescriptorsAndCleanupWork(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	store := openTestStore(
		t,
		newTestFactory(t, parent, 1, 1, MaximumMergeFiles),
	)
	workDirectory := store.directory
	for value := MaximumMergeFiles - 1; value >= 0; value-- {
		if err := store.Add(context.Background(), []byte{byte(value)}); err != nil {
			t.Fatalf("Add(%d) error = %v", value, err)
		}
	}
	if len(store.chunks) != MaximumMergeFiles {
		t.Fatalf("chunk count = %d, want %d", len(store.chunks), MaximumMergeFiles)
	}
	wantChunkBytes := int64(
		store.cipher.NonceSize() + store.config.RecordBytes + store.cipher.Overhead(),
	)
	var temporaryBytes int64
	for _, path := range store.chunks {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		temporaryBytes += info.Size()
		if info.Size() != wantChunkBytes {
			t.Fatalf("chunk bytes = %d, want %d", info.Size(), wantChunkBytes)
		}
	}
	if want := int64(MaximumMergeFiles) * wantChunkBytes; temporaryBytes != want {
		t.Fatalf("temporary bytes = %d, want %d", temporaryBytes, want)
	}

	openFile := store.openFile
	activeDescriptors := 1
	maximumDescriptors := 1
	store.openFile = func(path string) (chunkFile, error) {
		file, err := openFile(path)
		if err != nil {
			return nil, err
		}
		activeDescriptors++
		maximumDescriptors = max(maximumDescriptors, activeDescriptors)

		return &countedCloseFile{
			chunkFile: file,
			onClose: func() {
				activeDescriptors--
			},
		}, nil
	}
	yielded := 0
	if err := store.ForEachSorted(
		context.Background(),
		func(record []byte) error {
			if len(record) != 1 || record[0] != byte(yielded) {
				return errors.New("records are not deterministically ordered")
			}
			yielded++

			return nil
		},
	); err != nil {
		t.Fatalf("ForEachSorted() error = %v", err)
	}
	if yielded != MaximumMergeFiles {
		t.Fatalf("yielded records = %d, want %d", yielded, MaximumMergeFiles)
	}
	if maximumDescriptors != MaximumMergeFiles+1 || activeDescriptors != 1 {
		t.Fatalf(
			"descriptors maximum=%d active=%d, want maximum=%d active=1",
			maximumDescriptors,
			activeDescriptors,
			MaximumMergeFiles+1,
		)
	}

	removeAll := store.removeAll
	cleanupCalls := 0
	store.removeAll = func(path string) error {
		cleanupCalls++

		return removeAll(path)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleanupCalls)
	}
	if _, err := os.Stat(workDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("work directory remains after Close: %v", err)
	}
}

func TestStoreClosesOpenedReadersWhenALaterOpenFails(t *testing.T) {
	t.Parallel()

	store := openTestStore(
		t,
		newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 2),
	)
	addTestRecords(t, store, []byte{1}, []byte{2})
	openFile := store.openFile
	opens := 0
	active := 0
	store.openFile = func(path string) (chunkFile, error) {
		opens++
		if opens == 2 {
			return nil, errors.New("descriptor limit")
		}
		file, err := openFile(path)
		if err != nil {
			return nil, err
		}
		active++

		return &countedCloseFile{
			chunkFile: file,
			onClose: func() {
				active--
			},
		}, nil
	}
	if err := store.ForEachSorted(
		context.Background(),
		func([]byte) error { return nil },
	); !errors.Is(err, ErrStorage) {
		t.Fatalf("ForEachSorted() error = %v, want storage", err)
	}
	if active != 0 {
		t.Fatalf("active descriptors after open failure = %d, want 0", active)
	}
}

type countedCloseFile struct {
	chunkFile
	onClose func()
}

func (file *countedCloseFile) Close() error {
	err := file.chunkFile.Close()
	file.onClose()

	return err
}

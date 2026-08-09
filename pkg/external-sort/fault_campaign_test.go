package externalsort

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreMapsHostileFilesystemFailuresWithoutRetainingInput(t *testing.T) {
	t.Parallel()

	testCases := map[string]error{
		"disk full":             faultDiskFull,
		"quota exhausted":       faultQuota,
		"inode exhausted":       faultDiskFull,
		"permission revoked":    faultPermission,
		"directory disappeared": faultMissing,
		"read-only volume":      faultReadOnly,
	}
	for name, failure := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := openTestStore(
				t,
				newTestFactory(t, ownerOnlyTemporaryDirectory(t), 4, 1, 1),
			)
			store.createTemp = func(string, string) (chunkFile, error) {
				return nil, failure
			}
			record := []byte{1, 2, 3, 4}
			err := store.Add(context.Background(), record)
			if !errors.Is(err, ErrStorage) {
				t.Fatalf("Add() error = %v, want storage", err)
			}
			if errors.Is(err, failure) {
				t.Fatalf("Add() exposed filesystem failure %v", failure)
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
}

func TestStoreMapsHostileFilesystemFailuresAtEveryOwnedBoundary(t *testing.T) {
	t.Parallel()

	failures := map[string]error{
		"disk full":             faultDiskFull,
		"quota exhausted":       faultQuota,
		"inode exhausted":       faultDiskFull,
		"permission revoked":    faultPermission,
		"directory disappeared": faultMissing,
		"read-only volume":      faultReadOnly,
	}
	boundaries := map[string]func(*testing.T, *Store, error) error{
		"create": func(_ *testing.T, store *Store, failure error) error {
			store.createTemp = func(string, string) (chunkFile, error) {
				return nil, failure
			}

			return store.Add(context.Background(), []byte{1})
		},
		"chmod": func(_ *testing.T, store *Store, failure error) error {
			store.createTemp = func(string, string) (chunkFile, error) {
				return &fakeChunkFile{name: "chunk", chmodErr: failure}, nil
			}

			return store.Add(context.Background(), []byte{1})
		},
		"write": func(_ *testing.T, store *Store, failure error) error {
			store.createTemp = func(string, string) (chunkFile, error) {
				return &fakeChunkFile{name: "chunk", writeErr: failure}, nil
			}

			return store.Add(context.Background(), []byte{1})
		},
		"sync": func(_ *testing.T, store *Store, failure error) error {
			store.createTemp = func(string, string) (chunkFile, error) {
				return &fakeChunkFile{name: "chunk", syncErr: failure}, nil
			}

			return store.Add(context.Background(), []byte{1})
		},
		"close": func(_ *testing.T, store *Store, failure error) error {
			store.createTemp = func(string, string) (chunkFile, error) {
				return &fakeChunkFile{name: "chunk", closeErr: failure}, nil
			}

			return store.Add(context.Background(), []byte{1})
		},
		"open": func(t *testing.T, store *Store, failure error) error {
			t.Helper()
			addTestRecords(t, store, []byte{1})
			store.openFile = func(string) (chunkFile, error) { return nil, failure }

			return store.ForEachSorted(context.Background(), func([]byte) error { return nil })
		},
		"read": func(t *testing.T, store *Store, failure error) error {
			t.Helper()
			addTestRecords(t, store, []byte{1})
			store.openFile = func(string) (chunkFile, error) {
				return &fakeChunkFile{reader: errorReader{err: failure}}, nil
			}

			return store.ForEachSorted(context.Background(), func([]byte) error { return nil })
		},
		"remove partial chunk": func(_ *testing.T, store *Store, failure error) error {
			store.createTemp = func(string, string) (chunkFile, error) {
				return &fakeChunkFile{name: "chunk", chmodErr: io.ErrUnexpectedEOF}, nil
			}
			store.remove = func(string) error { return failure }

			return store.Add(context.Background(), []byte{1})
		},
		"remove work directory": func(_ *testing.T, store *Store, failure error) error {
			store.removeAll = func(string) error { return failure }

			return store.Close()
		},
	}
	for boundary, exercise := range boundaries {
		for name, failure := range failures {
			t.Run(boundary+"/"+name, func(t *testing.T) {
				t.Parallel()

				store := openTestStore(
					t,
					newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1),
				)
				err := exercise(t, store, failure)
				if !errors.Is(err, ErrStorage) {
					t.Fatalf("operation error = %v, want storage", err)
				}
				if errors.Is(err, failure) {
					t.Fatalf("operation exposed filesystem failure %v", failure)
				}
			})
		}
	}
}

func TestStoreCompletesShortChunkWritesAndReads(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	store := openTestStore(t, newTestFactory(t, parent, 4, 1, 1))
	file := &oneByteChunkFile{name: filepath.Join(parent, "short")}
	store.createTemp = func(string, string) (chunkFile, error) {
		return file, nil
	}
	record := []byte{1, 2, 3, 4}
	if err := store.Add(context.Background(), record); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if file.contents.Len() != store.cipher.NonceSize()+len(record)+store.cipher.Overhead() {
		t.Fatalf("encrypted bytes = %d", file.contents.Len())
	}
	if bytes.Contains(file.contents.Bytes(), record) {
		t.Fatal("short writes exposed plaintext")
	}

	reader := &chunkReader{
		file: &fakeChunkFile{reader: &oneByteReader{
			reader: bytes.NewReader(file.contents.Bytes()),
		}},
		cipher:          store.cipher,
		identity:        store.identity,
		nonceDomain:     store.nonceDomain,
		recordBytes:     len(record),
		expectedRecords: 1,
	}
	actual, err := reader.next()
	if err != nil {
		t.Fatalf("next() error = %v", err)
	}
	if !bytes.Equal(actual, record) {
		t.Fatalf("next() = %v, want %v", actual, record)
	}
}

func TestStoreNeverReusesANonceAfterAPartialWriteFailure(t *testing.T) {
	t.Parallel()

	store := openTestStore(
		t,
		newTestFactory(t, ownerOnlyTemporaryDirectory(t), 4, 1, 1),
	)
	failedFile := &partialFailureChunkFile{name: filepath.Join(store.directory, "failed")}
	createTemp := store.createTemp
	createCalls := 0
	store.createTemp = func(directory string, pattern string) (chunkFile, error) {
		createCalls++
		if createCalls == 1 {
			return failedFile, nil
		}

		return createTemp(directory, pattern)
	}
	remove := store.remove
	store.remove = func(path string) error {
		if path == failedFile.name {
			return nil
		}

		return remove(path)
	}
	if err := store.Add(
		context.Background(),
		[]byte{1, 1, 1, 1},
	); !errors.Is(err, ErrStorage) {
		t.Fatalf("first Add() error = %v, want storage", err)
	}
	if err := store.Add(
		context.Background(),
		[]byte{2, 2, 2, 2},
	); err != nil {
		t.Fatalf("retry Add() error = %v", err)
	}
	contents, err := os.ReadFile(store.chunks[0])
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	failedNonce := failedFile.contents.Bytes()[:store.cipher.NonceSize()]
	retryNonce := contents[:store.cipher.NonceSize()]
	if bytes.Equal(failedNonce, retryNonce) {
		t.Fatal("retry reused the partially written GCM nonce")
	}
	if binary.BigEndian.Uint32(failedNonce[nonceCounterOffset:]) != 0 ||
		binary.BigEndian.Uint32(retryNonce[nonceCounterOffset:]) != 1 {
		t.Fatalf("nonce counters = %x and %x, want 0 and 1", failedNonce, retryNonce)
	}
}

func TestStoreFailsClosedBeforeTheNonceCounterCanWrap(t *testing.T) {
	t.Parallel()

	lastCounterStore := openTestStore(
		t,
		newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1),
	)
	lastCounterStore.nonceCount = uint64(^uint32(0))
	if err := lastCounterStore.Add(context.Background(), []byte{1}); err != nil {
		t.Fatalf("Add(last counter) error = %v", err)
	}
	contents, err := os.ReadFile(lastCounterStore.chunks[0])
	if err != nil {
		t.Fatalf("ReadFile(last counter) error = %v", err)
	}
	if got := binary.BigEndian.Uint32(contents[nonceCounterOffset:]); got != ^uint32(0) {
		t.Fatalf("last nonce counter = %d, want %d", got, ^uint32(0))
	}

	store := openTestStore(
		t,
		newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1),
	)
	store.nonceCount = uint64(^uint32(0)) + 1
	if err := store.Add(context.Background(), []byte{1}); !errors.Is(
		err,
		ErrEntropy,
	) {
		t.Fatalf("Add() error = %v, want entropy", err)
	}
	if store.total != 0 || store.buffer.Len() != 0 || len(store.chunks) != 0 {
		t.Fatal("nonce exhaustion retained input or a committed chunk")
	}
	store.nonceCount = 0
	if err := store.Add(context.Background(), []byte{2}); err != nil {
		t.Fatalf("retry Add() error = %v", err)
	}
}

func TestFactoryRevalidatesParentPermissionsAndExistenceAtOpen(t *testing.T) {
	t.Parallel()

	parent := ownerOnlyTemporaryDirectory(t)
	factory := newTestFactory(t, parent, 1, 1, 1)
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	if _, err := factory.Open(
		context.Background(),
		bytes.Repeat([]byte{1}, AES256KeyBytes),
	); !errors.Is(err, ErrUnsafeParent) {
		t.Fatalf("Open() error = %v, want unsafe parent", err)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatalf("restore Chmod() error = %v", err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := factory.Open(
		context.Background(),
		bytes.Repeat([]byte{1}, AES256KeyBytes),
	); !errors.Is(err, ErrUnsafeParent) {
		t.Fatalf("Open(missing parent) error = %v, want unsafe parent", err)
	}
}

func TestFactoryRejectsAReplacedCanonicalParentAtOpen(t *testing.T) {
	t.Parallel()

	container := ownerOnlyTemporaryDirectory(t)
	parent := filepath.Join(container, "parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("Mkdir(parent) error = %v", err)
	}
	factory := newTestFactory(t, parent, 1, 1, 1)
	if err := os.Rename(parent, filepath.Join(container, "original")); err != nil {
		t.Fatalf("Rename(parent) error = %v", err)
	}
	replacement := ownerOnlyTemporaryDirectory(t)
	if err := os.Symlink(replacement, parent); err != nil {
		t.Fatalf("Symlink(replacement) error = %v", err)
	}
	if _, err := factory.Open(
		context.Background(),
		bytes.Repeat([]byte{1}, AES256KeyBytes),
	); !errors.Is(err, ErrUnsafeParent) {
		t.Fatalf("Open(replaced parent) error = %v, want unsafe parent", err)
	}
}

func TestParentIdentityRequiresTheSameOwnerOnlyDirectory(t *testing.T) {
	t.Parallel()

	expectedPath := ownerOnlyTemporaryDirectory(t)
	expected, err := os.Stat(expectedPath)
	if err != nil {
		t.Fatalf("Stat(expected) error = %v", err)
	}
	actual, err := os.Stat(expectedPath)
	if err != nil {
		t.Fatalf("Stat(actual) error = %v", err)
	}
	if !sameSafeDirectory(expected, actual) {
		t.Fatal("same owner-only directory was rejected")
	}

	otherPath := ownerOnlyTemporaryDirectory(t)
	other, err := os.Stat(otherPath)
	if err != nil {
		t.Fatalf("Stat(other) error = %v", err)
	}
	if sameSafeDirectory(expected, other) {
		t.Fatal("different owner-only directory was accepted")
	}
	if err := os.Chmod(otherPath, 0o755); err != nil {
		t.Fatalf("Chmod(other) error = %v", err)
	}
	unsafe, err := os.Stat(otherPath)
	if err != nil {
		t.Fatalf("Stat(unsafe) error = %v", err)
	}
	if sameSafeDirectory(expected, unsafe) {
		t.Fatal("different unsafe directory was accepted")
	}

	filePath := filepath.Join(expectedPath, "file")
	if err := os.WriteFile(filePath, nil, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	file, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Stat(file) error = %v", err)
	}
	if sameSafeDirectory(expected, file) {
		t.Fatal("regular file was accepted as a parent directory")
	}
	if sameSafeDirectory(nil, actual) || sameSafeDirectory(expected, nil) {
		t.Fatal("missing parent identity was accepted")
	}
}

func TestFactoryBoundsSecureDirectoryNameCollisionsAndCancellation(t *testing.T) {
	t.Parallel()

	key := bytes.Repeat([]byte{1}, AES256KeyBytes)
	t.Run("entropy failure", func(t *testing.T) {
		factory := newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1)
		factory.nameEntropy = errorReader{err: io.ErrUnexpectedEOF}
		if _, err := factory.Open(context.Background(), key); !errors.Is(err, ErrEntropy) {
			t.Fatalf("Open() error = %v, want entropy", err)
		}
	})
	t.Run("bounded collisions", func(t *testing.T) {
		factory := newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1)
		factory.nameEntropy = bytes.NewReader(make([]byte, temporaryAttempts*temporaryNameBytes))
		calls := 0
		factory.mkdir = func(rootDirectory, string, os.FileMode) error {
			calls++

			return os.ErrExist
		}
		if _, err := factory.Open(context.Background(), key); !errors.Is(err, ErrStorage) {
			t.Fatalf("Open() error = %v, want storage", err)
		}
		if calls != temporaryAttempts {
			t.Fatalf("directory attempts = %d, want %d", calls, temporaryAttempts)
		}
	})
	t.Run("cancellation between collisions", func(t *testing.T) {
		factory := newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1)
		factory.nameEntropy = bytes.NewReader(make([]byte, 2*temporaryNameBytes))
		ctx, cancel := context.WithCancel(context.Background())
		factory.mkdir = func(rootDirectory, string, os.FileMode) error {
			cancel()

			return os.ErrExist
		}
		if _, err := factory.Open(ctx, key); !errors.Is(err, context.Canceled) {
			t.Fatalf("Open() error = %v, want canceled", err)
		}
	})
}

func TestStoreBoundsSecureChunkNameCollisionsAndRootCloseRetries(t *testing.T) {
	t.Parallel()

	t.Run("chunk name entropy failure", func(t *testing.T) {
		store := openTestStore(
			t,
			newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1),
		)
		store.nameEntropy = errorReader{err: io.ErrUnexpectedEOF}
		if err := store.Add(context.Background(), []byte{1}); !errors.Is(err, ErrEntropy) {
			t.Fatalf("Add() error = %v, want entropy", err)
		}
	})
	t.Run("bounded chunk collisions", func(t *testing.T) {
		store := openTestStore(
			t,
			newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1),
		)
		calls := 0
		collisionRoot := &openFileFaultRoot{
			rootDirectory: store.root,
			openFile: func(string, int, os.FileMode) (*os.File, error) {
				calls++

				return nil, os.ErrExist
			},
		}
		store.nameEntropy = bytes.NewReader(make([]byte, temporaryAttempts*temporaryNameBytes))
		store.createTemp = func(string, string) (chunkFile, error) {
			return createRootTemporaryChunk(
				collisionRoot,
				store.directoryName,
				store.nameEntropy,
			)
		}
		if err := store.Add(context.Background(), []byte{1}); !errors.Is(err, ErrStorage) {
			t.Fatalf("Add() error = %v, want storage", err)
		}
		if calls != temporaryAttempts {
			t.Fatalf("chunk attempts = %d, want %d", calls, temporaryAttempts)
		}
	})
	t.Run("root close failure still finalizes", func(t *testing.T) {
		factory := newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1)
		openRoot := factory.openRoot
		closeCalls := 0
		factory.openRoot = func(path string) (rootDirectory, error) {
			root, err := openRoot(path)
			if err != nil {
				return nil, err
			}

			return &closeFaultRoot{
				rootDirectory: root,
				close: func() error {
					closeCalls++
					if closeCalls == 1 {
						return faultIO
					}

					return root.Close()
				},
			}, nil
		}
		store := openTestStore(t, factory)
		if err := store.Close(); !errors.Is(err, ErrStorage) {
			t.Fatalf("Close() error = %v, want storage", err)
		}
		if store.identity != nil || store.cipher != nil || store.directory != "" {
			t.Fatal("root close failure retained sensitive or cleanup state")
		}
		if err := store.Close(); err != nil {
			t.Fatalf("idempotent Close() error = %v", err)
		}
		if closeCalls != 1 {
			t.Fatalf("root close calls = %d, want 1", closeCalls)
		}
	})
}

func TestFactoryPinsTheResolvedParentAgainstAncestorLinkReplacement(t *testing.T) {
	t.Parallel()

	realRoot := ownerOnlyTemporaryDirectory(t)
	realParent := filepath.Join(realRoot, "parent")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	linkRoot := ownerOnlyTemporaryDirectory(t)
	link := filepath.Join(linkRoot, "alias")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	factory, err := NewFactory(Config{
		ParentDirectory: filepath.Join(link, "parent"),
		RecordBytes:     1,
		ChunkRecords:    1,
		MaximumRecords:  1,
	})
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatalf("Remove(link) error = %v", err)
	}
	store, err := factory.Open(
		context.Background(),
		bytes.Repeat([]byte{1}, AES256KeyBytes),
	)
	if err != nil {
		t.Fatalf("Open() after ancestor replacement error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStoreNeverDeletesThroughAReplacedParentAncestor(t *testing.T) {
	t.Parallel()

	container := ownerOnlyTemporaryDirectory(t)
	parent := filepath.Join(container, "parent")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("Mkdir(parent) error = %v", err)
	}
	store := openTestStore(t, newTestFactory(t, parent, 1, 1, 1))
	workName := filepath.Base(store.directory)
	anchoredParent := filepath.Join(container, "anchored-parent")
	if err := os.Rename(parent, anchoredParent); err != nil {
		t.Fatalf("Rename(parent) error = %v", err)
	}

	outside := ownerOnlyTemporaryDirectory(t)
	outsideWork := filepath.Join(outside, workName)
	if err := os.Mkdir(outsideWork, 0o700); err != nil {
		t.Fatalf("Mkdir(outside work) error = %v", err)
	}
	marker := filepath.Join(outsideWork, "must-survive")
	if err := os.WriteFile(marker, []byte("outside"), 0o600); err != nil {
		t.Fatalf("WriteFile(marker) error = %v", err)
	}
	if err := os.Symlink(outside, parent); err != nil {
		t.Fatalf("Symlink(replacement parent) error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("Close() escaped the trusted root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(anchoredParent, workName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("anchored work directory remains after Close: %v", err)
	}
}

func TestStoreClosesAfterItsWorkDirectoryDisappears(t *testing.T) {
	t.Parallel()

	store := openTestStore(
		t,
		newTestFactory(t, ownerOnlyTemporaryDirectory(t), 1, 1, 1),
	)
	if err := os.Remove(store.directory); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := store.Add(context.Background(), []byte{1}); !errors.Is(
		err,
		ErrStorage,
	) {
		t.Fatalf("Add() error = %v, want storage", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

type oneByteChunkFile struct {
	name     string
	contents bytes.Buffer
}

type openFileFaultRoot struct {
	rootDirectory
	openFile func(string, int, os.FileMode) (*os.File, error)
}

func (root *openFileFaultRoot) OpenFile(
	name string,
	flag int,
	mode os.FileMode,
) (*os.File, error) {
	return root.openFile(name, flag, mode)
}

type closeFaultRoot struct {
	rootDirectory
	close func() error
}

func (root *closeFaultRoot) Close() error {
	return root.close()
}

func (file *oneByteChunkFile) Read(destination []byte) (int, error) {
	return file.contents.Read(destination)
}

func (file *oneByteChunkFile) Write(data []byte) (int, error) {
	return file.contents.Write(data[:min(1, len(data))])
}

func (file *oneByteChunkFile) Name() string {
	return file.name
}

func (*oneByteChunkFile) Chmod(os.FileMode) error {
	return nil
}

func (*oneByteChunkFile) Sync() error {
	return nil
}

func (*oneByteChunkFile) Close() error {
	return nil
}

type oneByteReader struct {
	reader io.Reader
}

type partialFailureChunkFile struct {
	name     string
	contents bytes.Buffer
}

func (file *partialFailureChunkFile) Read(destination []byte) (int, error) {
	return file.contents.Read(destination)
}

func (file *partialFailureChunkFile) Write(data []byte) (int, error) {
	written, _ := file.contents.Write(data[:min(len(data), 12)])

	return written, faultDiskFull
}

func (file *partialFailureChunkFile) Name() string {
	return file.name
}

func (*partialFailureChunkFile) Chmod(os.FileMode) error {
	return nil
}

func (*partialFailureChunkFile) Sync() error {
	return nil
}

func (*partialFailureChunkFile) Close() error {
	return nil
}

func (reader *oneByteReader) Read(destination []byte) (int, error) {
	return reader.reader.Read(destination[:min(1, len(destination))])
}

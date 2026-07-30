package filesystem

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestStoreCASIterationAndClosedLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "trie"),
		DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	base, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	first, err := base.Update(
		ctx,
		[]byte("first"),
		[]byte("first persistent value"),
	)
	if err != nil {
		t.Fatalf("Update(first) error = %v", err)
	}
	second, err := base.Update(
		ctx,
		[]byte("second"),
		[]byte("second persistent value"),
	)
	if err != nil {
		t.Fatalf("Update(second) error = %v", err)
	}
	first, err = first.Commit(ctx, store)
	if err != nil {
		t.Fatalf("Commit(first) error = %v", err)
	}
	if _, err := second.Commit(ctx, store); !errors.Is(err, mpt.ErrStaleRoot) {
		t.Fatalf("Commit(stale) error = %v", err)
	}
	first, err = first.Update(
		ctx,
		[]byte("third"),
		[]byte("third persistent value"),
	)
	if err != nil {
		t.Fatalf("Update(third) error = %v", err)
	}
	if _, err := first.Commit(ctx, store); err != nil {
		t.Fatalf("Commit(third) error = %v", err)
	}

	var visited []mpt.Root
	callbackErr := errors.New("stop")
	err = store.IterateNodes(ctx, DefaultLimits().MaxStoredNodes, func(
		hash mpt.Root,
		encoded []byte,
	) error {
		if store.Root() == (mpt.Root{}) {
			t.Fatal("Root() returned zero during callback")
		}
		visited = append(visited, hash)
		encoded[0] ^= 0xff
		return nil
	})
	if err != nil {
		t.Fatalf("IterateNodes() error = %v", err)
	}
	if len(visited) == 0 || !slices.IsSortedFunc(
		visited,
		func(left, right mpt.Root) int {
			return bytes.Compare(left[:], right[:])
		},
	) {
		t.Fatalf("IterateNodes() hashes = %x", visited)
	}
	if len(visited) < 2 {
		t.Fatalf("IterateNodes() visited %d nodes, want at least 2", len(visited))
	}
	if _, err := store.GetNode(ctx, visited[0]); err != nil {
		t.Fatalf("GetNode(after callback mutation) error = %v", err)
	}
	if err := store.IterateNodes(ctx, len(visited)-1, func(
		mpt.Root,
		[]byte,
	) error {
		return nil
	}); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("IterateNodes(bound) error = %v", err)
	}
	if err := store.IterateNodes(ctx, len(visited), func(
		mpt.Root,
		[]byte,
	) error {
		return callbackErr
	}); !errors.Is(err, callbackErr) {
		t.Fatalf("IterateNodes(callback) error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := store.Close(); !errors.Is(err, mpt.ErrClosedStore) {
		t.Fatalf("Close(second) error = %v", err)
	}
	if _, err := store.GetNode(ctx, visited[0]); !errors.Is(err, mpt.ErrClosedStore) {
		t.Fatalf("GetNode(closed) error = %v", err)
	}
	if err := store.IterateNodes(ctx, 1, func(mpt.Root, []byte) error {
		return nil
	}); !errors.Is(err, mpt.ErrClosedStore) {
		t.Fatalf("IterateNodes(closed) error = %v", err)
	}
	commit := captureCommit(t)
	if err := store.CommitTrie(ctx, commit); !errors.Is(err, mpt.ErrClosedStore) {
		t.Fatalf("CommitTrie(closed) error = %v", err)
	}
}

func TestStoreValidatesContextsReceiversAndIteratorInputs(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var nilContext context.Context
	path := filepath.Join(t.TempDir(), "trie")
	if _, err := Open(nilContext, path, DefaultLimits()); !errors.Is(
		err,
		mpt.ErrInvalidContext,
	) {
		t.Fatalf("Open(nil context) error = %v", err)
	}
	if _, err := Open(ctx, path, DefaultLimits()); !errors.Is(
		err,
		mpt.ErrCanceled,
	) {
		t.Fatalf("Open(canceled) error = %v", err)
	}
	if _, err := Open(context.Background(), "", DefaultLimits()); !errors.Is(
		err,
		mpt.ErrInvalidStore,
	) {
		t.Fatalf("Open(empty path) error = %v", err)
	}
	if _, err := Open(context.Background(), path, Limits{}); !errors.Is(
		err,
		mpt.ErrResourceLimit,
	) {
		t.Fatalf("Open(zero limits) error = %v", err)
	}
	validLimits := DefaultLimits()
	for _, test := range []struct {
		name   string
		mutate func(*Limits)
	}{
		{name: "node bytes zero", mutate: func(limits *Limits) {
			limits.MaxNodeBytes = 0
		}},
		{name: "node bytes negative", mutate: func(limits *Limits) {
			limits.MaxNodeBytes = -1
		}},
		{name: "node bytes maximum integer", mutate: func(limits *Limits) {
			limits.MaxNodeBytes = int(^uint(0) >> 1)
		}},
		{name: "commit nodes zero", mutate: func(limits *Limits) {
			limits.MaxCommitNodes = 0
		}},
		{name: "commit nodes negative", mutate: func(limits *Limits) {
			limits.MaxCommitNodes = -1
		}},
		{name: "commit bytes zero", mutate: func(limits *Limits) {
			limits.MaxCommitBytes = 0
		}},
		{name: "commit bytes negative", mutate: func(limits *Limits) {
			limits.MaxCommitBytes = -1
		}},
		{name: "stored nodes zero", mutate: func(limits *Limits) {
			limits.MaxStoredNodes = 0
		}},
		{name: "stored nodes negative", mutate: func(limits *Limits) {
			limits.MaxStoredNodes = -1
		}},
		{name: "stored nodes maximum integer", mutate: func(limits *Limits) {
			limits.MaxStoredNodes = int(^uint(0) >> 1)
		}},
		{name: "retentions zero", mutate: func(limits *Limits) {
			limits.MaxRetentions = 0
		}},
		{name: "retentions negative", mutate: func(limits *Limits) {
			limits.MaxRetentions = -1
		}},
		{name: "retentions recovery overflow", mutate: func(limits *Limits) {
			limits.MaxRetentions = int(^uint(0)>>1) - 1
		}},
		{name: "retentions maximum integer", mutate: func(limits *Limits) {
			limits.MaxRetentions = int(^uint(0) >> 1)
		}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			limits := validLimits
			test.mutate(&limits)
			if err := validateLimits(limits); !errors.Is(
				err,
				mpt.ErrResourceLimit,
			) {
				t.Fatalf("validateLimits() error = %v", err)
			}
			if _, err := Open(
				context.Background(),
				filepath.Join(t.TempDir(), "trie"),
				limits,
			); !errors.Is(err, mpt.ErrResourceLimit) {
				t.Fatalf("Open() error = %v", err)
			}
		})
	}
	if err := validateLimits(validLimits); err != nil {
		t.Fatalf("validateLimits(valid) error = %v", err)
	}
	if strconv.IntSize == 64 {
		maximumUint64 := ^uint64(0)
		exactProduct := validLimits
		exactProduct.MaxNodeBytes = 3
		exactProduct.MaxStoredNodes = int(maximumUint64 / 3)
		if err := validateLimits(exactProduct); err != nil {
			t.Fatalf("validateLimits(exact stored bytes) error = %v", err)
		}
		overflowProduct := exactProduct
		overflowProduct.MaxStoredNodes++
		if err := validateLimits(overflowProduct); !errors.Is(
			err,
			mpt.ErrResourceLimit,
		) {
			t.Fatalf("validateLimits(stored byte overflow) error = %v", err)
		}
	}

	store, err := Open(context.Background(), path, DefaultLimits())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	})
	if _, err := store.GetNode(nilContext, mpt.Root{}); !errors.Is(
		err,
		mpt.ErrInvalidContext,
	) {
		t.Fatalf("GetNode(nil context) error = %v", err)
	}
	if _, err := store.GetNode(ctx, mpt.Root{}); !errors.Is(
		err,
		mpt.ErrCanceled,
	) {
		t.Fatalf("GetNode(canceled) error = %v", err)
	}
	if _, err := store.GetNode(
		context.Background(),
		mpt.Root{},
	); !errors.Is(err, mpt.ErrMissingNode) {
		t.Fatalf("GetNode(missing) error = %v", err)
	}
	if err := store.IterateNodes(nilContext, 1, func(
		mpt.Root,
		[]byte,
	) error {
		return nil
	}); !errors.Is(err, mpt.ErrInvalidContext) {
		t.Fatalf("IterateNodes(nil context) error = %v", err)
	}
	if err := store.IterateNodes(ctx, 1, func(mpt.Root, []byte) error {
		return nil
	}); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("IterateNodes(canceled) error = %v", err)
	}
	if err := store.IterateNodes(
		context.Background(),
		0,
		func(mpt.Root, []byte) error { return nil },
	); !errors.Is(err, mpt.ErrInvalidIterator) {
		t.Fatalf("IterateNodes(zero maximum) error = %v", err)
	}
	if err := store.IterateNodes(
		context.Background(),
		1,
		nil,
	); !errors.Is(err, mpt.ErrInvalidIterator) {
		t.Fatalf("IterateNodes(nil callback) error = %v", err)
	}

	var nilStore *Store
	if nilStore.Root() != (mpt.Root{}) {
		t.Fatal("nil Store.Root() did not return zero")
	}
	if _, err := nilStore.GetNode(
		context.Background(),
		mpt.Root{},
	); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("nil Store.GetNode() error = %v", err)
	}
	if err := nilStore.CommitTrie(
		context.Background(),
		mpt.StoreCommit{},
	); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("nil Store.CommitTrie() error = %v", err)
	}
	if err := nilStore.IterateNodes(
		context.Background(),
		1,
		func(mpt.Root, []byte) error { return nil },
	); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("nil Store.IterateNodes() error = %v", err)
	}
	if err := nilStore.Close(); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("nil Store.Close() error = %v", err)
	}
}

func TestOpenAndIterationRejectCorruptFilesystemState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, test := range []struct {
		name    string
		prepare func(t *testing.T, path string)
	}{
		{
			name: "store path is file",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			},
		},
		{
			name: "store path is symlink",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				target := path + "-target"
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatalf("Mkdir() error = %v", err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatalf("Symlink() error = %v", err)
				}
			},
		},
		{
			name: "node path is file",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatalf("Mkdir() error = %v", err)
				}
				if err := os.WriteFile(
					filepath.Join(path, nodeDirectory),
					[]byte("file"),
					0o600,
				); err != nil {
					t.Fatalf("WriteFile(nodes) error = %v", err)
				}
			},
		},
		{
			name: "malformed root",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				if err := os.MkdirAll(
					filepath.Join(path, nodeDirectory),
					0o700,
				); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				if err := os.WriteFile(
					filepath.Join(path, rootFileName),
					[]byte("bad"),
					0o600,
				); err != nil {
					t.Fatalf("WriteFile(ROOT) error = %v", err)
				}
			},
		},
		{
			name: "bad root checksum",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				if err := os.MkdirAll(
					filepath.Join(path, nodeDirectory),
					0o700,
				); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				record := encodeRootRecord(mpt.EmptyRoot())
				record[len(record)-1] ^= 0xff
				if err := os.WriteFile(
					filepath.Join(path, rootFileName),
					record,
					0o600,
				); err != nil {
					t.Fatalf("WriteFile(ROOT) error = %v", err)
				}
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "trie")
			test.prepare(t, path)
			if _, err := Open(ctx, path, DefaultLimits()); err == nil {
				t.Fatal("Open() error = nil")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "trie")
	store, err := Open(ctx, path, DefaultLimits())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	}()
	if err := os.WriteFile(
		filepath.Join(path, nodeDirectory, "junk"),
		[]byte("junk"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(junk) error = %v", err)
	}
	if err := store.IterateNodes(ctx, 10, func(mpt.Root, []byte) error {
		return nil
	}); !errors.Is(err, mpt.ErrCorruptNode) {
		t.Fatalf("IterateNodes(junk) error = %v", err)
	}
}

func TestStoreRejectsSymlinkedRootAndNodeFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootPath := filepath.Join(t.TempDir(), "trie")
	if err := os.MkdirAll(
		filepath.Join(rootPath, nodeDirectory),
		0o700,
	); err != nil {
		t.Fatalf("MkdirAll(root path) error = %v", err)
	}
	rootTarget := filepath.Join(t.TempDir(), "root-record")
	if err := os.WriteFile(
		rootTarget,
		encodeRootRecord(mpt.EmptyRoot()),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(root target) error = %v", err)
	}
	if err := os.Symlink(
		rootTarget,
		filepath.Join(rootPath, rootFileName),
	); err != nil {
		t.Fatalf("Symlink(ROOT) error = %v", err)
	}
	if _, err := Open(
		ctx,
		rootPath,
		DefaultLimits(),
	); !errors.Is(err, mpt.ErrStorageRead) {
		t.Fatalf("Open(symlink ROOT) error = %v", err)
	}

	nodePath := filepath.Join(t.TempDir(), "trie")
	store, err := Open(ctx, nodePath, DefaultLimits())
	if err != nil {
		t.Fatalf("Open(node path) error = %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	}()
	encoded := []byte{0xc2, 0x20, 0x01}
	hash := keccakRoot(encoded)
	nodeTarget := filepath.Join(t.TempDir(), "node")
	if err := os.WriteFile(nodeTarget, encoded, 0o600); err != nil {
		t.Fatalf("WriteFile(node target) error = %v", err)
	}
	if err := os.Symlink(nodeTarget, store.nodePath(hash)); err != nil {
		t.Fatalf("Symlink(node) error = %v", err)
	}
	if _, err := store.GetNode(ctx, hash); !errors.Is(
		err,
		mpt.ErrStorageRead,
	) {
		t.Fatalf("GetNode(symlink) error = %v", err)
	}
	if err := store.IterateNodes(
		ctx,
		1,
		func(mpt.Root, []byte) error { return nil },
	); !errors.Is(err, mpt.ErrCorruptNode) {
		t.Fatalf("IterateNodes(symlink) error = %v", err)
	}
}

func TestOpenCollectsOnlyInterruptedAtomicWriteFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "trie")
	store, err := Open(ctx, path, DefaultLimits())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	root := store.Root()
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	rootTemporary := filepath.Join(path, ".ROOT.tmp-interrupted")
	emptySuffix := filepath.Join(path, ".ROOT.tmp-")
	nodeTemporary := filepath.Join(
		path,
		nodeDirectory,
		"."+strings.Repeat("0", mpt.RootBytes*2)+".tmp-interrupted",
	)
	unrelated := filepath.Join(path, "application-owned")
	for _, file := range []string{
		rootTemporary,
		emptySuffix,
		nodeTemporary,
		unrelated,
	} {
		if err := os.WriteFile(file, []byte("partial"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", file, err)
		}
	}
	reopened, err := Open(ctx, path, DefaultLimits())
	if err != nil {
		t.Fatalf("Open(recovery) error = %v", err)
	}
	defer func() {
		if closeErr := reopened.Close(); closeErr != nil {
			t.Errorf("Close(recovery) error = %v", closeErr)
		}
	}()
	if reopened.Root() != root {
		t.Fatalf("Root(recovery) = %x, want %x", reopened.Root(), root)
	}
	for _, temporary := range []string{rootTemporary, nodeTemporary} {
		if _, err := os.Lstat(temporary); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Lstat(%s) error = %v, want not exist", temporary, err)
		}
	}
	if _, err := os.Lstat(unrelated); err != nil {
		t.Fatalf("Lstat(unrelated) error = %v", err)
	}
	if _, err := os.Lstat(emptySuffix); err != nil {
		t.Fatalf("Lstat(empty-suffix root temporary) error = %v", err)
	}
}

func TestIterationIgnoresAnInProgressAtomicNodeWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "trie"),
		DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	}()
	temporary := filepath.Join(
		store.nodesPath,
		"."+strings.Repeat("0", mpt.RootBytes*2)+".tmp-in-progress",
	)
	if err := os.WriteFile(temporary, []byte("partial"), 0o600); err != nil {
		t.Fatalf("WriteFile(temporary) error = %v", err)
	}
	node := captureCommit(t).Nodes()[0]
	if err := store.writeNode(node); err != nil {
		t.Fatalf("writeNode() error = %v", err)
	}
	visited := 0
	if err := store.IterateNodes(
		ctx,
		1,
		func(hash mpt.Root, encoded []byte) error {
			visited++
			if hash != node.Hash() || !bytes.Equal(encoded, node.Encoded()) {
				t.Fatal("iteration returned the wrong persistent node")
			}
			return nil
		},
	); err != nil {
		t.Fatalf("IterateNodes() error = %v", err)
	}
	if visited != 1 {
		t.Fatalf("IterateNodes() visited %d nodes, want 1", visited)
	}
}

func TestIterationRejectsUnsafeAndOversizedDirectoryBounds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := &Store{
		nodesPath: t.TempDir(),
		limits: Limits{
			MaxNodeBytes:   1,
			MaxCommitNodes: 1,
			MaxCommitBytes: 1,
			MaxStoredNodes: int(^uint(0) >> 1),
		},
	}
	if err := store.IterateNodes(
		ctx,
		int(^uint(0)>>1),
		func(mpt.Root, []byte) error { return nil },
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("IterateNodes(maximum integer) error = %v", err)
	}

	limits := DefaultLimits()
	limits.MaxStoredNodes = 3
	store, err := Open(ctx, filepath.Join(t.TempDir(), "trie"), limits)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	}()
	nodes := captureCommit(t).Nodes()
	if len(nodes) < 3 {
		t.Fatalf("captureCommit() produced %d nodes, want at least 3", len(nodes))
	}
	for _, node := range nodes[:3] {
		if err := store.writeNode(node); err != nil {
			t.Fatalf("writeNode() error = %v", err)
		}
	}
	if err := store.IterateNodes(
		ctx,
		1,
		func(mpt.Root, []byte) error { return nil },
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("IterateNodes(oversized directory) error = %v", err)
	}
}

func TestCommitLimitsAndImmutableNodeIntegrity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	commit := captureCommit(t)
	nodes := commit.Nodes()
	if len(nodes) == 0 {
		t.Fatal("captured commit contains no nodes")
	}
	totalBytes := 0
	maxNodeBytes := 0
	for _, node := range nodes {
		size := len(node.Encoded())
		totalBytes += size
		if size > maxNodeBytes {
			maxNodeBytes = size
		}
	}
	for _, test := range []struct {
		name   string
		limits Limits
	}{
		{
			name: "node count",
			limits: Limits{
				MaxNodeBytes: 1 << 20, MaxCommitNodes: len(nodes) - 1,
				MaxCommitBytes: 1 << 20, MaxStoredNodes: 100,
				MaxRetentions: 1,
			},
		},
		{
			name: "node bytes",
			limits: Limits{
				MaxNodeBytes:   len(nodes[0].Encoded()) - 1,
				MaxCommitNodes: 100, MaxCommitBytes: 1 << 20,
				MaxStoredNodes: 100,
				MaxRetentions:  1,
			},
		},
		{
			name: "commit bytes",
			limits: Limits{
				MaxNodeBytes: 1 << 20, MaxCommitNodes: 100,
				MaxCommitBytes: len(nodes[0].Encoded()) - 1,
				MaxStoredNodes: 100,
				MaxRetentions:  1,
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store, err := Open(
				ctx,
				filepath.Join(t.TempDir(), "trie"),
				test.limits,
			)
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			defer func() {
				if closeErr := store.Close(); closeErr != nil {
					t.Errorf("Close() error = %v", closeErr)
				}
			}()
			if err := store.CommitTrie(ctx, commit); !errors.Is(
				err,
				mpt.ErrResourceLimit,
			) {
				t.Fatalf("CommitTrie() error = %v", err)
			}
		})
	}

	aggregateLimitStore, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "trie"),
		Limits{
			MaxNodeBytes:   maxNodeBytes,
			MaxCommitNodes: len(nodes),
			MaxCommitBytes: totalBytes - 1,
			MaxStoredNodes: len(nodes),
			MaxRetentions:  1,
		},
	)
	if err != nil {
		t.Fatalf("Open(aggregate limit) error = %v", err)
	}
	if err := aggregateLimitStore.CommitTrie(ctx, commit); !errors.Is(
		err,
		mpt.ErrResourceLimit,
	) {
		t.Fatalf("CommitTrie(aggregate limit) error = %v", err)
	}
	if err := aggregateLimitStore.Close(); err != nil {
		t.Fatalf("Close(aggregate limit) error = %v", err)
	}

	exactStore, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "trie"),
		Limits{
			MaxNodeBytes:   maxNodeBytes,
			MaxCommitNodes: len(nodes),
			MaxCommitBytes: totalBytes,
			MaxStoredNodes: len(nodes),
			MaxRetentions:  1,
		},
	)
	if err != nil {
		t.Fatalf("Open(exact limits) error = %v", err)
	}
	if err := exactStore.CommitTrie(ctx, commit); err != nil {
		t.Fatalf("CommitTrie(exact limits) error = %v", err)
	}
	if err := exactStore.IterateNodes(
		ctx,
		len(nodes),
		func(mpt.Root, []byte) error { return nil },
	); err != nil {
		t.Fatalf("IterateNodes(exact limits) error = %v", err)
	}
	if err := exactStore.Close(); err != nil {
		t.Fatalf("Close(exact limits) error = %v", err)
	}

	store, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "trie"),
		DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("Open(conflict) error = %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close(conflict) error = %v", closeErr)
		}
	}()
	conflict := nodes[0]
	if err := os.WriteFile(
		store.nodePath(conflict.Hash()),
		bytes.Repeat([]byte{0xff}, len(conflict.Encoded())),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(conflict) error = %v", err)
	}
	if err := store.writeNode(conflict); !errors.Is(
		err,
		mpt.ErrCorruptNode,
	) {
		t.Fatalf("writeNode(conflict) error = %v", err)
	}
	if err := store.CommitTrie(ctx, commit); !errors.Is(
		err,
		mpt.ErrCorruptNode,
	) {
		t.Fatalf("CommitTrie(conflict) error = %v", err)
	}
	if _, err := store.GetNode(
		ctx,
		conflict.Hash(),
	); !errors.Is(err, mpt.ErrCorruptNode) {
		t.Fatalf("GetNode(conflict) error = %v", err)
	}
}

func TestCommitEnforcesStoredNodeLimitAcrossReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "trie")
	limits := DefaultLimits()
	limits.MaxStoredNodes = 1
	store, err := Open(ctx, path, limits)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	trie, err = trie.Update(
		ctx,
		[]byte("first"),
		[]byte("first persistent value long enough to hash"),
	)
	if err != nil {
		t.Fatalf("Update(first) error = %v", err)
	}
	_, err = trie.Commit(ctx, store)
	if err != nil {
		t.Fatalf("Commit(first) error = %v", err)
	}
	published := store.Root()
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	store, err = Open(ctx, path, limits)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close(reopened) error = %v", closeErr)
		}
	}()
	trie, err = mpt.LoadRawTrie(published, store, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("LoadRawTrie() error = %v", err)
	}
	next, err := trie.Update(
		ctx,
		[]byte("second"),
		[]byte("second persistent value long enough to hash"),
	)
	if err != nil {
		t.Fatalf("Update(second) error = %v", err)
	}
	if _, err := next.Commit(ctx, store); !errors.Is(
		err,
		mpt.ErrResourceLimit,
	) {
		t.Fatalf("Commit(second) error = %v", err)
	}
	if got := store.Root(); got != published {
		t.Fatalf("Root(after rejected commit) = %x, want %x", got, published)
	}
	entries, err := os.ReadDir(store.nodesPath)
	if err != nil {
		t.Fatalf("ReadDir(nodes) error = %v", err)
	}
	if len(entries) != limits.MaxStoredNodes {
		t.Fatalf(
			"stored node count = %d, want %d",
			len(entries),
			limits.MaxStoredNodes,
		)
	}
}

func TestCommitRejectsStoredNodeBatchBeforeWriting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	commit := captureCommit(t)
	nodes := commit.Nodes()
	if len(nodes) < 2 {
		t.Fatalf("captureCommit() produced %d nodes, want at least 2", len(nodes))
	}
	limits := DefaultLimits()
	limits.MaxStoredNodes = len(nodes) - 1
	store, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "trie"),
		limits,
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	}()
	if err := store.CommitTrie(ctx, commit); !errors.Is(
		err,
		mpt.ErrResourceLimit,
	) {
		t.Fatalf("CommitTrie() error = %v", err)
	}
	entries, err := os.ReadDir(store.nodesPath)
	if err != nil {
		t.Fatalf("ReadDir(nodes) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected commit wrote %d nodes, want 0", len(entries))
	}
}

func TestCommitPreflightAndWriteFailureBoundaries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	commit := captureCommit(t)
	nodes := commit.Nodes()

	canceledStore, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "trie"),
		DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("Open(canceled write) error = %v", err)
	}
	cancelDuringWrite := &steppingContext{
		remaining:      1 + len(nodes),
		returnCanceled: true,
	}
	if err := canceledStore.CommitTrie(
		cancelDuringWrite,
		commit,
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("CommitTrie(canceled write) error = %v", err)
	}
	if err := canceledStore.Close(); err != nil {
		t.Fatalf("Close(canceled write) error = %v", err)
	}

	writeFailureStore, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "trie"),
		DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("Open(write failure) error = %v", err)
	}
	movedNodes := writeFailureStore.nodesPath + "-moved"
	failDuringWrite := &steppingContext{
		remaining: 1 + len(nodes),
		onCancel: func() {
			if renameErr := os.Rename(
				writeFailureStore.nodesPath,
				movedNodes,
			); renameErr != nil {
				t.Errorf("Rename(nodes) error = %v", renameErr)
			}
		},
	}
	if err := writeFailureStore.CommitTrie(
		failDuringWrite,
		commit,
	); !errors.Is(err, mpt.ErrStorageCommit) {
		t.Fatalf("CommitTrie(write failure) error = %v", err)
	}
	if writeFailureStore.storedNodes != 0 {
		t.Fatalf(
			"stored node count after write failure = %d, want 0",
			writeFailureStore.storedNodes,
		)
	}
	if err := writeFailureStore.Close(); err != nil {
		t.Fatalf("Close(write failure) error = %v", err)
	}

	preflightStore, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "trie"),
		DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("Open(preflight failure) error = %v", err)
	}
	if err := os.Mkdir(
		preflightStore.nodePath(nodes[0].Hash()),
		0o700,
	); err != nil {
		t.Fatalf("Mkdir(preflight node) error = %v", err)
	}
	if err := preflightStore.CommitTrie(
		ctx,
		commit,
	); !errors.Is(err, mpt.ErrStorageRead) {
		t.Fatalf("CommitTrie(preflight failure) error = %v", err)
	}
	if err := preflightStore.writeNode(nodes[0]); !errors.Is(
		err,
		mpt.ErrStorageRead,
	) {
		t.Fatalf("writeNode(preflight directory) error = %v", err)
	}
	if err := preflightStore.Close(); err != nil {
		t.Fatalf("Close(preflight failure) error = %v", err)
	}

	fullStore := &Store{
		path:        t.TempDir(),
		nodesPath:   t.TempDir(),
		limits:      DefaultLimits(),
		storedNodes: DefaultLimits().MaxStoredNodes,
	}
	if err := fullStore.writeNewNode(nodes[0]); !errors.Is(
		err,
		mpt.ErrResourceLimit,
	) {
		t.Fatalf("writeNewNode(full) error = %v", err)
	}

	capacityLimits := DefaultLimits()
	capacityLimits.MaxStoredNodes = 2
	capacityStore := &Store{
		path:        t.TempDir(),
		nodesPath:   t.TempDir(),
		limits:      capacityLimits,
		storedNodes: 1,
	}
	if prepared, err := capacityStore.prepareNodes(
		ctx,
		nodes[:2],
	); !errors.Is(err, mpt.ErrResourceLimit) || prepared != nil {
		t.Fatalf("prepareNodes(capacity) = (%v, %v)", prepared, err)
	}

	writeStore := &Store{
		path:      t.TempDir(),
		nodesPath: t.TempDir(),
		limits:    capacityLimits,
	}
	if err := writeStore.writeNewNode(nodes[0]); err != nil {
		t.Fatalf("writeNewNode(success) error = %v", err)
	}
	if writeStore.storedNodes != 1 {
		t.Fatalf(
			"stored node count after write = %d, want 1",
			writeStore.storedNodes,
		)
	}
}

func TestFilesystemEncodingAndFailureBoundaries(t *testing.T) {
	t.Parallel()

	root := mpt.EmptyRoot()
	record := encodeRootRecord(root)
	decoded, err := decodeRootRecord(record)
	if err != nil || decoded != root {
		t.Fatalf("decodeRootRecord() = (%x, %v)", decoded, err)
	}
	badMagic := append([]byte(nil), record...)
	badMagic[0] ^= 0xff
	if _, err := decodeRootRecord(badMagic); !errors.Is(
		err,
		mpt.ErrCorruptNode,
	) {
		t.Fatalf("decodeRootRecord(bad magic) error = %v", err)
	}
	if _, err := decodeRootRecord(record[:len(record)-1]); !errors.Is(
		err,
		mpt.ErrCorruptNode,
	) {
		t.Fatalf("decodeRootRecord(short) error = %v", err)
	}

	directory := t.TempDir()
	validName := strings.Repeat("00", mpt.RootBytes)
	entry := dirEntry(t, directory, validName, false)
	if parsed, err := parseNodeName(entry); err != nil || parsed != (mpt.Root{}) {
		t.Fatalf("parseNodeName(valid) = (%x, %v)", parsed, err)
	}
	invalidHex := "zz" + validName[2:]
	if _, err := parseNodeName(
		dirEntry(t, directory, invalidHex, false),
	); !errors.Is(err, mpt.ErrCorruptNode) {
		t.Fatalf("parseNodeName(invalid hex) error = %v", err)
	}
	if _, err := parseNodeName(
		dirEntry(t, directory, "AA"+validName[2:], false),
	); !errors.Is(err, mpt.ErrCorruptNode) {
		t.Fatalf("parseNodeName(uppercase) error = %v", err)
	}
	if _, err := parseNodeName(
		dirEntry(t, directory, "short", false),
	); !errors.Is(err, mpt.ErrCorruptNode) {
		t.Fatalf("parseNodeName(short) error = %v", err)
	}
	if _, err := parseNodeName(
		dirEntry(t, directory, validName+"-dir", true),
	); !errors.Is(err, mpt.ErrCorruptNode) {
		t.Fatalf("parseNodeName(directory) error = %v", err)
	}

	if err := rejectSymlink(filepath.Join(directory, "missing")); err != nil {
		t.Fatalf("rejectSymlink(missing) error = %v", err)
	}
	regular := filepath.Join(directory, "regular")
	if err := os.WriteFile(regular, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile(regular) error = %v", err)
	}
	if err := rejectSymlink(regular); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("rejectSymlink(file) error = %v", err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	if err := rejectSymlink(link); !errors.Is(err, mpt.ErrInvalidStore) {
		t.Fatalf("rejectSymlink(link) error = %v", err)
	}
	if err := rejectSymlink(directory); err != nil {
		t.Fatalf("rejectSymlink(directory) error = %v", err)
	}

	oversized := filepath.Join(directory, "oversized")
	if err := os.WriteFile(oversized, []byte("12"), 0o600); err != nil {
		t.Fatalf("WriteFile(oversized) error = %v", err)
	}
	if _, err := readBoundedFile(oversized, 1); !errors.Is(
		err,
		mpt.ErrResourceLimit,
	) {
		t.Fatalf("readBoundedFile(oversized) error = %v", err)
	}
	if _, err := readBoundedFile(directory, 1); err == nil {
		t.Fatal("readBoundedFile(directory) error = nil")
	}
	if err := writeAtomic(
		filepath.Join(directory, "missing", "value"),
		[]byte("x"),
	); err == nil {
		t.Fatal("writeAtomic(missing parent) error = nil")
	}
	if err := syncDirectory(filepath.Join(directory, "missing")); err == nil {
		t.Fatal("syncDirectory(missing) error = nil")
	}
	failure := errors.New("injected directory-open failure")
	if err := syncDirectoryWith(func() (syncFile, error) {
		return nil, failure
	}); !errors.Is(err, failure) {
		t.Fatalf("syncDirectoryWith(open failure) error = %v", err)
	}
	syncFailure := &failingSyncFile{syncErr: failure}
	if err := syncDirectoryWith(func() (syncFile, error) {
		return syncFailure, nil
	}); !errors.Is(err, failure) {
		t.Fatalf("syncDirectoryWith(sync failure) error = %v", err)
	}
	if !syncFailure.closed {
		t.Fatal("syncDirectoryWith(sync failure) did not close directory")
	}
	closeFailure := &failingSyncFile{closeErr: failure}
	if err := syncDirectoryWith(func() (syncFile, error) {
		return closeFailure, nil
	}); !errors.Is(err, failure) {
		t.Fatalf("syncDirectoryWith(close failure) error = %v", err)
	}
	if !errors.Is(storageReadError(os.ErrPermission), mpt.ErrStorageRead) {
		t.Fatal("storageReadError() lost classification")
	}
	if !errors.Is(storageCommitError(os.ErrPermission), mpt.ErrStorageCommit) {
		t.Fatal("storageCommitError() lost classification")
	}
}

func TestStoreCancellationAndStorageFailureBoundaries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	commit := captureCommit(t)
	nodes := commit.Nodes()

	store, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "trie"),
		DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	cancelContext, cancel := context.WithCancel(ctx)
	store.checkpoint = func(stage commitStage) {
		if stage == nodesDurable {
			cancel()
		}
	}
	if err := store.CommitTrie(cancelContext, commit); !errors.Is(
		err,
		mpt.ErrCanceled,
	) {
		t.Fatalf("CommitTrie(cancel after nodes) error = %v", err)
	}
	store.checkpoint = nil
	if err := store.CommitTrie(ctx, commit); err != nil {
		t.Fatalf("CommitTrie(retry) error = %v", err)
	}
	if err := store.writeNode(nodes[0]); err != nil {
		t.Fatalf("writeNode(existing) error = %v", err)
	}

	store.retentionChanging = true
	if err := store.CommitTrie(ctx, commit); !errors.Is(err, mpt.ErrStaleRoot) {
		t.Fatalf("CommitTrie(retention in progress) error = %v", err)
	}
	if err := store.Close(); !errors.Is(err, mpt.ErrStorageCommit) {
		t.Fatalf("Close(retention in progress) error = %v", err)
	}
	store.retentionChanging = false
	store.pruning = true
	if err := store.CommitTrie(ctx, commit); !errors.Is(err, mpt.ErrStaleRoot) {
		t.Fatalf("CommitTrie(prune in progress) error = %v", err)
	}
	store.pruning = false
	store.committing = true
	if err := store.Close(); !errors.Is(err, mpt.ErrStorageCommit) {
		t.Fatalf("Close(commit in progress) error = %v", err)
	}
	store.committing = false

	missingNodes := &Store{
		path:      store.path,
		nodesPath: filepath.Join(store.path, "missing"),
		root:      mpt.Root{},
		limits:    DefaultLimits(),
	}
	if err := missingNodes.IterateNodes(
		ctx,
		1,
		func(mpt.Root, []byte) error { return nil },
	); !errors.Is(err, mpt.ErrStorageRead) {
		t.Fatalf("IterateNodes(missing directory) error = %v", err)
	}
	if err := missingNodes.CommitTrie(ctx, mpt.StoreCommit{}); !errors.Is(
		err,
		mpt.ErrStorageCommit,
	) {
		t.Fatalf("CommitTrie(missing directory) error = %v", err)
	}

	nodeDirectoryPath := store.nodePath(mpt.Root{})
	if err := os.Mkdir(nodeDirectoryPath, 0o700); err != nil {
		t.Fatalf("Mkdir(node path) error = %v", err)
	}
	if _, err := store.GetNode(ctx, mpt.Root{}); !errors.Is(
		err,
		mpt.ErrStorageRead,
	) {
		t.Fatalf("GetNode(directory) error = %v", err)
	}
	if err := store.writeNode(mpt.StoredNode{}); !errors.Is(
		err,
		mpt.ErrStorageRead,
	) {
		t.Fatalf("writeNode(directory) error = %v", err)
	}

	if err := store.validateCommit(
		[]mpt.StoredNode{{}},
	); !errors.Is(err, mpt.ErrCorruptNode) {
		t.Fatalf("validateCommit(corrupt) error = %v", err)
	}

	publishStore := &Store{
		path: filepath.Join(t.TempDir(), "missing", "trie"),
	}
	if _, err := publishStore.publishRoot(
		ctx,
		mpt.EmptyRoot(),
	); !errors.Is(err, mpt.ErrStorageCommit) {
		t.Fatalf("publishRoot(write failure) error = %v", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := publishStore.publishRoot(
		canceled,
		mpt.EmptyRoot(),
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("publishRoot(canceled) error = %v", err)
	}

	rootSyncPath := filepath.Join(t.TempDir(), "trie")
	rootSyncStore, err := Open(ctx, rootSyncPath, DefaultLimits())
	if err != nil {
		t.Fatalf("Open(root sync) error = %v", err)
	}
	moved := rootSyncPath + "-moved"
	rootSyncStore.checkpoint = func(stage commitStage) {
		if stage == rootRenamed {
			if renameErr := os.Rename(rootSyncPath, moved); renameErr != nil {
				t.Fatalf("Rename(root sync) error = %v", renameErr)
			}
		}
	}
	published, err := rootSyncStore.publishRoot(ctx, mpt.EmptyRoot())
	if !published || !errors.Is(err, mpt.ErrStorageCommit) {
		t.Fatalf("publishRoot(sync failure) = (%t, %v)", published, err)
	}
}

func TestStoreObservesCancellationDuringCommitAndIteration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	commit := captureCommit(t)
	store, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "trie"),
		DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	}()

	commitContext := &steppingContext{remaining: 2, returnCanceled: true}
	if err := store.CommitTrie(commitContext, commit); !errors.Is(
		err,
		mpt.ErrCanceled,
	) {
		t.Fatalf("CommitTrie(step cancellation) error = %v", err)
	}
	if err := store.CommitTrie(ctx, commit); err != nil {
		t.Fatalf("CommitTrie() error = %v", err)
	}

	entries, err := os.ReadDir(store.nodesPath)
	if err != nil || len(entries) == 0 {
		t.Fatalf("ReadDir() = (%d, %v)", len(entries), err)
	}
	removeAtRead := &steppingContext{
		remaining: 1 + len(entries),
		onCancel: func() {
			if removeErr := os.Remove(
				filepath.Join(store.nodesPath, entries[0].Name()),
			); removeErr != nil {
				t.Errorf("Remove(node) error = %v", removeErr)
			}
		},
		returnCanceled: false,
	}
	if err := store.IterateNodes(
		removeAtRead,
		len(entries),
		func(mpt.Root, []byte) error { return nil },
	); !errors.Is(err, mpt.ErrStorageRead) {
		t.Fatalf("IterateNodes(disappearing node) error = %v", err)
	}
}

func TestOpenClassifiesCreationAndReadFailures(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parentFile := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile(parent) error = %v", err)
	}
	if _, err := Open(
		ctx,
		filepath.Join(parentFile, "trie"),
		DefaultLimits(),
	); !errors.Is(err, mpt.ErrStorageRead) {
		t.Fatalf("Open(MkdirAll failure) error = %v", err)
	}

	rootDirectoryPath := filepath.Join(t.TempDir(), "trie")
	if err := os.MkdirAll(
		filepath.Join(rootDirectoryPath, nodeDirectory),
		0o700,
	); err != nil {
		t.Fatalf("MkdirAll(root directory) error = %v", err)
	}
	if err := os.Mkdir(
		filepath.Join(rootDirectoryPath, rootFileName),
		0o700,
	); err != nil {
		t.Fatalf("Mkdir(ROOT) error = %v", err)
	}
	if _, err := Open(
		ctx,
		rootDirectoryPath,
		DefaultLimits(),
	); !errors.Is(err, mpt.ErrStorageRead) {
		t.Fatalf("Open(ROOT directory) error = %v", err)
	}

	tooLong := filepath.Join(t.TempDir(), strings.Repeat("x", 4096))
	if err := rejectSymlink(tooLong); !errors.Is(err, mpt.ErrStorageRead) {
		t.Fatalf("rejectSymlink(too long) error = %v", err)
	}

	failure := errors.New("injected open failure")
	for _, scenario := range []struct {
		name   string
		inject func(*openOperations)
	}{
		{
			name: "directory creation",
			inject: func(operations *openOperations) {
				operations.mkdirAll = func(string, os.FileMode) error {
					return failure
				}
			},
		},
		{
			name: "node directory creation",
			inject: func(operations *openOperations) {
				operations.mkdir = func(path string, mode os.FileMode) error {
					if filepath.Base(path) == nodeDirectory {
						return failure
					}
					return os.Mkdir(path, mode)
				}
			},
		},
		{
			name: "retention directory creation",
			inject: func(operations *openOperations) {
				operations.mkdir = func(path string, mode os.FileMode) error {
					if filepath.Base(path) == retentionDirectory {
						return failure
					}
					return os.Mkdir(path, mode)
				}
			},
		},
		{
			name: "initial root publication",
			inject: func(operations *openOperations) {
				operations.publishRoot = func(
					*Store,
					context.Context,
					mpt.Root,
				) (bool, error) {
					return false, storageCommitError(failure)
				}
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			operations := defaultOpenOperations()
			scenario.inject(&operations)
			if _, err := openWith(
				ctx,
				filepath.Join(t.TempDir(), "trie"),
				DefaultLimits(),
				operations,
			); !errors.Is(err, mpt.ErrStorageCommit) ||
				!errors.Is(err, failure) {
				t.Fatalf("openWith(%s) error = %v", scenario.name, err)
			}
		})
	}
}

func TestAtomicPublicationPropagatesEveryFileFailure(t *testing.T) {
	t.Parallel()

	failure := errors.New("injected filesystem failure")
	for _, stage := range []string{
		"create", "chmod", "write", "sync", "close", "rename",
	} {
		stage := stage
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			file := &failingAtomicFile{
				name:  filepath.Join(t.TempDir(), "temporary"),
				stage: stage,
				err:   failure,
			}
			operations := atomicOperations{
				createTemp: func(string, string) (atomicFile, error) {
					if stage == "create" {
						return nil, failure
					}
					return file, nil
				},
				rename: func(string, string) error {
					if stage == "rename" {
						return failure
					}
					return nil
				},
				remove: func(string) error {
					file.removed = true
					return nil
				},
			}
			err := writeAtomicWith(operations, "target", []byte("content"))
			if !errors.Is(err, failure) {
				t.Fatalf("writeAtomicWith(%s) error = %v", stage, err)
			}
			if stage != "create" && !file.removed {
				t.Fatalf("writeAtomicWith(%s) did not remove temporary", stage)
			}
		})
	}
}

func TestAtomicPublicationRejectsShortWrites(t *testing.T) {
	t.Parallel()

	file := &failingAtomicFile{
		name:  filepath.Join(t.TempDir(), "temporary"),
		stage: "short write",
	}
	err := writeAtomicWith(
		atomicOperations{
			createTemp: func(string, string) (atomicFile, error) {
				return file, nil
			},
			rename: func(string, string) error { return nil },
			remove: func(string) error {
				file.removed = true
				return nil
			},
		},
		"target",
		[]byte("content"),
	)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeAtomicWith(short write) error = %v", err)
	}
	if !file.removed {
		t.Fatal("writeAtomicWith(short write) did not remove temporary")
	}
}

func TestBoundedReadPropagatesOpenAndReaderFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("injected read failure")
	for _, maximum := range []int{-1, 0, int(^uint(0) >> 1)} {
		if _, err := readRegularFile(
			func() (readableFile, error) {
				t.Fatal("unsafe bound must be rejected before opening")
				return nil, nil
			},
			maximum,
		); !errors.Is(err, mpt.ErrResourceLimit) {
			t.Fatalf("readRegularFile(%d) error = %v", maximum, err)
		}
	}
	if _, err := readRegularFile(
		func() (readableFile, error) {
			return nil, failure
		},
		1,
	); !errors.Is(err, failure) {
		t.Fatalf("readRegularFile(open) error = %v", err)
	}
	if _, err := readRegularFile(
		func() (readableFile, error) {
			return &failingReadableFile{readErr: failure}, nil
		},
		1,
	); !errors.Is(err, failure) {
		t.Fatalf("readRegularFile(read) error = %v", err)
	}
	if _, err := readRegularFile(
		func() (readableFile, error) {
			return &failingReadableFile{
				content:  []byte("x"),
				closeErr: failure,
			}, nil
		},
		1,
	); !errors.Is(err, failure) {
		t.Fatalf("readRegularFile(close) error = %v", err)
	}
}

func TestDirectoryReadRequestsOnlyOneEntryBeyondTheLimit(t *testing.T) {
	t.Parallel()

	reader := &recordingDirectoryReader{
		entries: []os.DirEntry{
			dirEntry(t, t.TempDir(), "one", false),
			dirEntry(t, t.TempDir(), "two", false),
		},
	}
	entries, err := readDirectoryBoundedWith(
		func() (directoryReader, error) {
			return reader, nil
		},
		1,
	)
	if !errors.Is(err, mpt.ErrResourceLimit) || entries != nil {
		t.Fatalf("readDirectoryBoundedWith() = (%v, %v)", entries, err)
	}
	if reader.requested != 2 {
		t.Fatalf("ReadDir() requested %d entries, want 2", reader.requested)
	}
	if !reader.closed {
		t.Fatal("bounded directory reader was not closed")
	}
}

func TestDirectoryReadFailureBoundaries(t *testing.T) {
	t.Parallel()

	failure := errors.New("injected directory failure")
	for _, maximum := range []int{-1, 0, int(^uint(0) >> 1)} {
		if _, err := readDirectoryBoundedWith(
			func() (directoryReader, error) {
				t.Fatal("invalid bounds must be rejected before opening")
				return nil, nil
			},
			maximum,
		); !errors.Is(err, mpt.ErrResourceLimit) {
			t.Fatalf("readDirectoryBoundedWith(%d) error = %v", maximum, err)
		}
	}
	if _, err := readDirectoryBoundedWith(
		func() (directoryReader, error) {
			return nil, failure
		},
		1,
	); !errors.Is(err, failure) {
		t.Fatalf("readDirectoryBoundedWith(open) error = %v", err)
	}
	if _, err := readDirectoryBoundedWith(
		func() (directoryReader, error) {
			return &recordingDirectoryReader{readErr: failure}, nil
		},
		1,
	); !errors.Is(err, failure) {
		t.Fatalf("readDirectoryBoundedWith(read) error = %v", err)
	}
	if _, err := readDirectoryBoundedWith(
		func() (directoryReader, error) {
			return &recordingDirectoryReader{
				readErr:  io.EOF,
				closeErr: failure,
			}, nil
		},
		1,
	); !errors.Is(err, failure) {
		t.Fatalf("readDirectoryBoundedWith(close) error = %v", err)
	}
	reader := &recordingDirectoryReader{
		entries: []os.DirEntry{
			dirEntry(t, t.TempDir(), "one", false),
		},
		readErr: io.EOF,
	}
	entries, err := readDirectoryBoundedWith(
		func() (directoryReader, error) {
			return reader, nil
		},
		1,
	)
	if err != nil || len(entries) != 1 {
		t.Fatalf("readDirectoryBoundedWith(exact) = (%v, %v)", entries, err)
	}
}

func TestStoredNodeInventoryFailureBoundaries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := countStoredNodes(ctx, missing, 1); !errors.Is(
		err,
		mpt.ErrStorageRead,
	) {
		t.Fatalf("countStoredNodes(missing) error = %v", err)
	}

	oversized := t.TempDir()
	dirEntry(t, oversized, strings.Repeat("0", mpt.RootBytes*2), false)
	dirEntry(t, oversized, strings.Repeat("1", mpt.RootBytes*2), false)
	if _, err := countStoredNodes(ctx, oversized, 1); !errors.Is(
		err,
		mpt.ErrResourceLimit,
	) {
		t.Fatalf("countStoredNodes(oversized) error = %v", err)
	}

	malformed := t.TempDir()
	dirEntry(t, malformed, "junk", false)
	if _, err := countStoredNodes(ctx, malformed, 1); !errors.Is(
		err,
		mpt.ErrCorruptNode,
	) {
		t.Fatalf("countStoredNodes(malformed) error = %v", err)
	}
	openPath := filepath.Join(t.TempDir(), "trie")
	openNodes := filepath.Join(openPath, nodeDirectory)
	if err := os.MkdirAll(openNodes, 0o700); err != nil {
		t.Fatalf("MkdirAll(open inventory) error = %v", err)
	}
	dirEntry(t, openNodes, "junk", false)
	if _, err := Open(ctx, openPath, DefaultLimits()); !errors.Is(
		err,
		mpt.ErrCorruptNode,
	) {
		t.Fatalf("Open(malformed inventory) error = %v", err)
	}

	canceled := t.TempDir()
	dirEntry(t, canceled, strings.Repeat("0", mpt.RootBytes*2), false)
	canceledContext, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := countStoredNodes(
		canceledContext,
		canceled,
		1,
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("countStoredNodes(canceled) error = %v", err)
	}
}

func TestTemporaryRecoveryFailureBoundaries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	failure := errors.New("injected recovery failure")
	rootName := ".ROOT.tmp-interrupted"
	nodeName := "." + strings.Repeat("0", mpt.RootBytes*2) + ".tmp-interrupted"
	baseOperations := recoveryOperations{
		readDirectory: func(path string, maximum int) ([]os.DirEntry, error) {
			return readDirectoryBounded(path, maximum)
		},
		remove: func(string) error { return nil },
		syncDirectory: func(string) error {
			return nil
		},
	}

	rootPath, nodesPath := recoveryFixture(t, rootName, nodeName)
	firstReadFailure := baseOperations
	firstReadFailure.readDirectory = func(string, int) ([]os.DirEntry, error) {
		return nil, failure
	}
	if err := recoverTemporaryFilesWith(
		ctx,
		rootPath,
		nodesPath,
		10,
		firstReadFailure,
	); !errors.Is(err, mpt.ErrStorageRead) {
		t.Fatalf("recoverTemporaryFilesWith(first read) error = %v", err)
	}

	secondReadFailure := baseOperations
	secondReadFailure.readDirectory = func(path string, maximum int) ([]os.DirEntry, error) {
		if path == nodesPath {
			return nil, failure
		}
		return readDirectoryBounded(path, maximum)
	}
	if err := recoverTemporaryFilesWith(
		ctx,
		rootPath,
		nodesPath,
		10,
		secondReadFailure,
	); !errors.Is(err, mpt.ErrStorageRead) {
		t.Fatalf("recoverTemporaryFilesWith(second read) error = %v", err)
	}

	if err := recoverTemporaryFilesWith(
		ctx,
		rootPath,
		nodesPath,
		0,
		baseOperations,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("recoverTemporaryFilesWith(bound) error = %v", err)
	}
	resourceFailure := baseOperations
	resourceFailure.readDirectory = func(string, int) ([]os.DirEntry, error) {
		return nil, mpt.ErrResourceLimit
	}
	if err := recoverTemporaryFilesWith(
		ctx,
		rootPath,
		nodesPath,
		10,
		resourceFailure,
	); !errors.Is(err, mpt.ErrResourceLimit) ||
		errors.Is(err, mpt.ErrStorageRead) {
		t.Fatalf("recoverTemporaryFilesWith(resource) error = %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := recoverTemporaryFilesWith(
		canceled,
		rootPath,
		nodesPath,
		10,
		baseOperations,
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("recoverTemporaryFilesWith(canceled) error = %v", err)
	}

	removeFailure := baseOperations
	removeFailure.remove = func(string) error { return failure }
	if err := recoverTemporaryFilesWith(
		ctx,
		rootPath,
		nodesPath,
		10,
		removeFailure,
	); !errors.Is(err, mpt.ErrStorageCommit) {
		t.Fatalf("recoverTemporaryFilesWith(remove) error = %v", err)
	}

	for _, failedPath := range []string{rootPath, nodesPath} {
		failedPath := failedPath
		t.Run("sync "+filepath.Base(failedPath), func(t *testing.T) {
			operations := baseOperations
			operations.syncDirectory = func(path string) error {
				if path == failedPath {
					return failure
				}
				return nil
			}
			if err := recoverTemporaryFilesWith(
				ctx,
				rootPath,
				nodesPath,
				10,
				operations,
			); !errors.Is(err, mpt.ErrStorageCommit) {
				t.Fatalf("recoverTemporaryFilesWith(sync) error = %v", err)
			}
		})
	}

	symlinkRoot, _ := recoveryFixture(t, "", "")
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("partial"), 0o600); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	if err := os.Symlink(
		target,
		filepath.Join(symlinkRoot, rootName),
	); err != nil {
		t.Fatalf("Symlink(temporary) error = %v", err)
	}
	if _, err := Open(
		ctx,
		symlinkRoot,
		DefaultLimits(),
	); !errors.Is(err, mpt.ErrCorruptNode) {
		t.Fatalf("Open(temporary symlink) error = %v", err)
	}

	for _, name := range []string{
		"",
		".short.tmp-x",
		"." + strings.Repeat("0", mpt.RootBytes*2) + ".tmp-",
		"." + strings.Repeat("0", mpt.RootBytes*2) + ".bad-x",
		"." + strings.Repeat("z", mpt.RootBytes*2) + ".tmp-x",
		nodeName,
	} {
		want := name == nodeName
		if got := isTemporaryNodeFile(name); got != want {
			t.Fatalf("isTemporaryNodeFile(%q) = %t, want %t", name, got, want)
		}
	}
	for _, test := range []struct {
		name string
		want bool
	}{
		{name: ".ROOT.tmp-", want: false},
		{name: ".ROOT.tmp-x", want: true},
		{name: "ROOT.tmp-x", want: false},
		{name: ".ROOT.bad-x", want: false},
	} {
		if got := isTemporaryRootFile(test.name); got != test.want {
			t.Fatalf(
				"isTemporaryRootFile(%q) = %t, want %t",
				test.name,
				got,
				test.want,
			)
		}
	}

	exactRoot, exactNodes := recoveryFixture(t, rootName, "")
	if err := os.WriteFile(
		filepath.Join(exactRoot, "!unmatched"),
		[]byte("unrelated"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(unmatched) error = %v", err)
	}
	if err := recoverTemporaryFilesWith(
		ctx,
		exactRoot,
		exactNodes,
		3,
		defaultRecoveryOperations(),
	); err != nil {
		t.Fatalf("recoverTemporaryFilesWith(exact bound) error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(exactRoot, rootName)); !errors.Is(
		err,
		os.ErrNotExist,
	) {
		t.Fatalf("Lstat(exact-bound temporary) error = %v", err)
	}
}

func TestCommitAndIterationObserveMidOperationFailures(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	commit := captureCommit(t)
	nodes := commit.Nodes()

	writeFailureStore := &Store{
		path:      t.TempDir(),
		nodesPath: filepath.Join(t.TempDir(), "missing", "nodes"),
		root:      commit.PreviousRoot(),
		limits:    DefaultLimits(),
	}
	if err := writeFailureStore.writeNode(nodes[0]); !errors.Is(
		err,
		mpt.ErrStorageCommit,
	) {
		t.Fatalf("writeNode(write failure) error = %v", err)
	}

	storePath := filepath.Join(t.TempDir(), "trie")
	store, err := Open(ctx, storePath, DefaultLimits())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	var nilContext context.Context
	if err := store.CommitTrie(nilContext, commit); !errors.Is(
		err,
		mpt.ErrInvalidContext,
	) {
		t.Fatalf("CommitTrie(nil context) error = %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.CommitTrie(canceled, commit); !errors.Is(
		err,
		mpt.ErrCanceled,
	) {
		t.Fatalf("CommitTrie(canceled) error = %v", err)
	}

	moved := storePath + "-moved"
	store.checkpoint = func(stage commitStage) {
		if stage == rootRenamed {
			if renameErr := os.Rename(storePath, moved); renameErr != nil {
				t.Fatalf("Rename(store) error = %v", renameErr)
			}
		}
	}
	if err := store.CommitTrie(ctx, commit); !errors.Is(
		err,
		mpt.ErrStorageCommit,
	) {
		t.Fatalf("CommitTrie(root sync failure) error = %v", err)
	}
	if store.Root() != commit.Root() {
		t.Fatalf("Root() = %x, want published %x", store.Root(), commit.Root())
	}

	reopened, err := Open(ctx, moved, DefaultLimits())
	if err != nil {
		t.Fatalf("Open(moved) error = %v", err)
	}
	defer func() {
		if closeErr := reopened.Close(); closeErr != nil {
			t.Errorf("Close(reopened) error = %v", closeErr)
		}
	}()
	entries, err := os.ReadDir(reopened.nodesPath)
	if err != nil || len(entries) < 2 {
		t.Fatalf("ReadDir() = (%d, %v), want at least 2", len(entries), err)
	}

	cancelDuringHashes := &steppingContext{
		remaining:      1 + len(entries),
		returnCanceled: true,
	}
	if err := reopened.IterateNodes(
		cancelDuringHashes,
		len(entries),
		func(mpt.Root, []byte) error { return nil },
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("IterateNodes(mid-operation cancel) error = %v", err)
	}

	cancelDuringNames := &steppingContext{
		remaining:      1,
		returnCanceled: true,
	}
	if err := reopened.IterateNodes(
		cancelDuringNames,
		len(entries),
		func(mpt.Root, []byte) error { return nil },
	); !errors.Is(err, mpt.ErrCanceled) {
		t.Fatalf("IterateNodes(name cancellation) error = %v", err)
	}

	corruptPath := filepath.Join(reopened.nodesPath, entries[0].Name())
	content, err := os.ReadFile(corruptPath)
	if err != nil {
		t.Fatalf("ReadFile(node) error = %v", err)
	}
	content[0] ^= 0xff
	if err := os.WriteFile(corruptPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile(corrupt node) error = %v", err)
	}
	if err := reopened.IterateNodes(
		ctx,
		len(entries),
		func(mpt.Root, []byte) error { return nil },
	); !errors.Is(err, mpt.ErrCorruptNode) {
		t.Fatalf("IterateNodes(corrupt node) error = %v", err)
	}
}

func TestConcurrentWritersSerializeWithoutBlockingReaders(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "trie"),
		DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	first := captureCommit(t)
	second := captureCommitWithSuffix(t, "second")
	reached := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	store.checkpoint = func(stage commitStage) {
		if stage == nodesDurable {
			once.Do(func() {
				close(reached)
				<-release
			})
		}
	}
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- store.CommitTrie(ctx, first)
	}()
	select {
	case <-reached:
	case <-time.After(2 * time.Second):
		t.Fatal("first commit did not reach durable-node checkpoint")
	}

	if store.Root() != mpt.EmptyRoot() {
		t.Fatalf("Root(during commit) = %x", store.Root())
	}
	if _, err := store.GetNode(ctx, mpt.Root{}); !errors.Is(
		err,
		mpt.ErrMissingNode,
	) {
		t.Fatalf("GetNode(during commit) error = %v", err)
	}
	secondResult := make(chan error, 1)
	go func() {
		secondResult <- store.CommitTrie(ctx, second)
	}()
	select {
	case err := <-secondResult:
		if !errors.Is(err, mpt.ErrStaleRoot) {
			t.Fatalf("CommitTrie(concurrent writer) error = %v", err)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("concurrent writer did not reject without blocking")
	}
	if err := store.Close(); !errors.Is(err, mpt.ErrStorageCommit) {
		t.Fatalf("Close(during commit) error = %v", err)
	}
	close(release)
	select {
	case err := <-firstResult:
		if err != nil {
			t.Fatalf("CommitTrie(first) error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first commit did not finish after checkpoint release")
	}
	store.checkpoint = nil
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

type failingAtomicFile struct {
	name    string
	stage   string
	err     error
	removed bool
}

type failingReadableFile struct {
	content  []byte
	readErr  error
	closeErr error
}

func (file *failingReadableFile) Read(destination []byte) (int, error) {
	if file.readErr != nil {
		return 0, file.readErr
	}
	if len(file.content) == 0 {
		return 0, io.EOF
	}
	copied := copy(destination, file.content)
	file.content = file.content[copied:]
	return copied, nil
}

func (file *failingReadableFile) Close() error {
	return file.closeErr
}

var _ io.ReadCloser = (*failingReadableFile)(nil)

type recordingDirectoryReader struct {
	entries   []os.DirEntry
	readErr   error
	closeErr  error
	requested int
	closed    bool
}

func (reader *recordingDirectoryReader) ReadDir(
	maximum int,
) ([]os.DirEntry, error) {
	reader.requested = maximum
	return reader.entries, reader.readErr
}

func (reader *recordingDirectoryReader) Close() error {
	reader.closed = true
	return reader.closeErr
}

var _ directoryReader = (*recordingDirectoryReader)(nil)

type failingSyncFile struct {
	syncErr  error
	closeErr error
	closed   bool
}

func (file *failingSyncFile) Sync() error {
	return file.syncErr
}

func (file *failingSyncFile) Close() error {
	file.closed = true
	return file.closeErr
}

func recoveryFixture(
	t *testing.T,
	rootName, nodeName string,
) (string, string) {
	t.Helper()
	rootPath := filepath.Join(t.TempDir(), "trie")
	nodesPath := filepath.Join(rootPath, nodeDirectory)
	if err := os.MkdirAll(nodesPath, 0o700); err != nil {
		t.Fatalf("MkdirAll(recovery fixture) error = %v", err)
	}
	if rootName != "" {
		if err := os.WriteFile(
			filepath.Join(rootPath, rootName),
			[]byte("partial"),
			0o600,
		); err != nil {
			t.Fatalf("WriteFile(root temporary) error = %v", err)
		}
	}
	if nodeName != "" {
		if err := os.WriteFile(
			filepath.Join(nodesPath, nodeName),
			[]byte("partial"),
			0o600,
		); err != nil {
			t.Fatalf("WriteFile(node temporary) error = %v", err)
		}
	}
	return rootPath, nodesPath
}

func (file *failingAtomicFile) Name() string {
	return file.name
}

func (file *failingAtomicFile) Chmod(os.FileMode) error {
	if file.stage == "chmod" {
		return file.err
	}
	return nil
}

func (file *failingAtomicFile) Write(content []byte) (int, error) {
	if file.stage == "write" {
		return 0, file.err
	}
	if file.stage == "short write" {
		return len(content) - 1, nil
	}
	return len(content), nil
}

func (file *failingAtomicFile) Sync() error {
	if file.stage == "sync" {
		return file.err
	}
	return nil
}

func (file *failingAtomicFile) Close() error {
	if file.stage == "close" {
		return file.err
	}
	return nil
}

type steppingContext struct {
	mutex          sync.Mutex
	remaining      int
	onCancel       func()
	returnCanceled bool
	triggered      bool
}

func (ctx *steppingContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *steppingContext) Done() <-chan struct{} {
	return nil
}

func (ctx *steppingContext) Err() error {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	if ctx.remaining > 0 {
		ctx.remaining--
		return nil
	}
	if !ctx.triggered && ctx.onCancel != nil {
		ctx.onCancel()
		ctx.triggered = true
	}
	if ctx.returnCanceled {
		return context.Canceled
	}
	return nil
}

func (ctx *steppingContext) Value(any) any {
	return nil
}

type commitCapture struct {
	commit mpt.StoreCommit
}

func (capture *commitCapture) GetNode(
	context.Context,
	mpt.Root,
) ([]byte, error) {
	return nil, mpt.ErrMissingNode
}

func (capture *commitCapture) CommitTrie(
	_ context.Context,
	commit mpt.StoreCommit,
) error {
	capture.commit = commit
	return nil
}

func captureCommit(t *testing.T) mpt.StoreCommit {
	return captureCommitWithSuffix(t, "")
}

func captureCommitWithSuffix(t *testing.T, suffix string) mpt.StoreCommit {
	t.Helper()
	ctx := context.Background()
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	for _, key := range []string{"alpha", "beta", "gamma"} {
		trie, err = trie.Update(
			ctx,
			[]byte(key+suffix),
			[]byte(key+" persistent value long enough to hash"),
		)
		if err != nil {
			t.Fatalf("Update(%q) error = %v", key, err)
		}
	}
	capture := &commitCapture{}
	if _, err := trie.Commit(ctx, capture); err != nil {
		t.Fatalf("Commit(capture) error = %v", err)
	}
	return capture.commit
}

func dirEntry(
	t *testing.T,
	directory, name string,
	isDirectory bool,
) os.DirEntry {
	t.Helper()
	path := filepath.Join(directory, name)
	var err error
	if isDirectory {
		err = os.Mkdir(path, 0o700)
	} else {
		err = os.WriteFile(path, nil, 0o600)
	}
	if err != nil {
		t.Fatalf("create directory entry error = %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == name {
			return entry
		}
	}
	t.Fatalf("directory entry %q not found", name)
	return nil
}

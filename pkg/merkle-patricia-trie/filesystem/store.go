// Package filesystem provides a durable directory-backed MPT node store.
package filesystem

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"golang.org/x/crypto/sha3"
)

const (
	rootFileName  = "ROOT"
	nodeDirectory = "nodes"
	rootRecordLen = 8 + mpt.RootBytes + sha256.Size
)

var rootMagic = [8]byte{'M', 'P', 'T', 'R', 'O', 'O', 'T', 1}

// Limits bounds filesystem reads, writes, and iteration before allocation or
// storage fan-out.
type Limits struct {
	// MaxNodeBytes bounds one encoded node read or write.
	MaxNodeBytes int
	// MaxCommitNodes bounds the nodes accepted in one atomic commit.
	MaxCommitNodes int
	// MaxCommitBytes bounds aggregate encoded-node bytes in one commit.
	MaxCommitBytes int
	// MaxStoredNodes bounds immutable node files retained by the store.
	MaxStoredNodes int
}

// DefaultLimits returns the default explicit filesystem resource bounds.
func DefaultLimits() Limits {
	return Limits{
		MaxNodeBytes:   16 << 20,
		MaxCommitNodes: 1 << 20,
		MaxCommitBytes: 256 << 20,
		MaxStoredNodes: 1 << 22,
	}
}

// Store persists immutable hash-addressed nodes as individual files and
// publishes one root with atomic rename. A Store is safe for concurrent use
// within one process. Callers must ensure one Store owns a directory at a time.
type Store struct {
	mutex       sync.RWMutex
	path        string
	nodesPath   string
	root        mpt.Root
	limits      Limits
	storedNodes int
	closed      bool
	committing  bool
	checkpoint  func(commitStage)
}

type commitStage uint8

const (
	nodesDurable commitStage = iota
	rootRenamed
)

// Open creates or opens an exclusively owned path, recovers interrupted
// adapter writes, inventories stored nodes, and validates the published root.
// It returns typed MPT storage, corruption, cancellation, or limit errors.
func Open(ctx context.Context, path string, limits Limits) (*Store, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, fmt.Errorf("%w: empty filesystem path", mpt.ErrInvalidStore)
	}
	if err := validateLimits(limits); err != nil {
		return nil, err
	}
	if err := rejectSymlink(path); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, storageCommitError(err)
	}
	nodesPath := filepath.Join(path, nodeDirectory)
	if err := rejectSymlink(nodesPath); err != nil {
		return nil, err
	}
	if err := os.Mkdir(nodesPath, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, storageCommitError(err)
	}
	if err := recoverTemporaryFiles(
		ctx,
		path,
		nodesPath,
		limits.MaxStoredNodes,
	); err != nil {
		return nil, err
	}
	storedNodes, err := countStoredNodes(
		ctx,
		nodesPath,
		limits.MaxStoredNodes,
	)
	if err != nil {
		return nil, err
	}

	store := &Store{
		path: path, nodesPath: nodesPath, limits: limits,
		root: mpt.EmptyRoot(), storedNodes: storedNodes,
	}
	rootPath := filepath.Join(path, rootFileName)
	record, err := readBoundedFile(rootPath, rootRecordLen)
	switch {
	case err == nil:
		root, decodeErr := decodeRootRecord(record)
		if decodeErr != nil {
			return nil, decodeErr
		}
		store.root = root
	case errors.Is(err, os.ErrNotExist):
		if _, writeErr := store.publishRoot(ctx, mpt.EmptyRoot()); writeErr != nil {
			return nil, writeErr
		}
	default:
		return nil, storageReadError(err)
	}
	return store, nil
}

func recoverTemporaryFiles(
	ctx context.Context,
	path, nodesPath string,
	maximum int,
) error {
	return recoverTemporaryFilesWith(
		ctx,
		path,
		nodesPath,
		maximum,
		defaultRecoveryOperations(),
	)
}

type recoveryOperations struct {
	readDirectory func(path string, maximum int) ([]os.DirEntry, error)
	remove        func(path string) error
	syncDirectory func(path string) error
}

func defaultRecoveryOperations() recoveryOperations {
	return recoveryOperations{
		readDirectory: readDirectoryBounded,
		remove:        os.Remove,
		syncDirectory: syncDirectory,
	}
}

func recoverTemporaryFilesWith(
	ctx context.Context,
	path, nodesPath string,
	maximum int,
	operations recoveryOperations,
) error {
	if maximum <= 0 || maximum == int(^uint(0)>>1) {
		return fmt.Errorf(
			"%w: invalid temporary-file bound",
			mpt.ErrResourceLimit,
		)
	}
	removedRoot, err := removeTemporaryFiles(
		ctx,
		path,
		max(maximum, 3),
		operations,
		isTemporaryRootFile,
	)
	if err != nil {
		return err
	}
	removedNodes, err := removeTemporaryFiles(
		ctx,
		nodesPath,
		maximum+1,
		operations,
		isTemporaryNodeFile,
	)
	if err != nil {
		return err
	}
	if removedRoot {
		if err := operations.syncDirectory(path); err != nil {
			return storageCommitError(err)
		}
	}
	if removedNodes {
		if err := operations.syncDirectory(nodesPath); err != nil {
			return storageCommitError(err)
		}
	}
	return nil
}

func countStoredNodes(
	ctx context.Context,
	nodesPath string,
	maximum int,
) (int, error) {
	entries, err := readDirectoryBounded(nodesPath, maximum)
	if err != nil {
		if errors.Is(err, mpt.ErrResourceLimit) {
			return 0, err
		}
		return 0, storageReadError(err)
	}
	for _, entry := range entries {
		if err := checkContext(ctx); err != nil {
			return 0, err
		}
		if _, err := parseNodeName(entry); err != nil {
			return 0, err
		}
	}
	return len(entries), nil
}

func removeTemporaryFiles(
	ctx context.Context,
	directory string,
	maximum int,
	operations recoveryOperations,
	matches func(name string) bool,
) (bool, error) {
	entries, err := operations.readDirectory(directory, maximum)
	if err != nil {
		if errors.Is(err, mpt.ErrResourceLimit) {
			return false, err
		}
		return false, storageReadError(err)
	}
	removed := false
	for _, entry := range entries {
		if err := checkContext(ctx); err != nil {
			return false, err
		}
		if matches(entry.Name()) {
			if !entry.Type().IsRegular() {
				return false, &mpt.CorruptNodeError{
					Cause: fmt.Errorf(
						"%w: non-regular temporary file",
						mpt.ErrCorruptNode,
					),
				}
			}
			if err := operations.remove(
				filepath.Join(directory, entry.Name()),
			); err != nil {
				return false, storageCommitError(err)
			}
			removed = true
		}
	}
	return removed, nil
}

func isTemporaryNodeFile(name string) bool {
	const prefixBytes = mpt.RootBytes * 2
	prefix, suffix, found := strings.Cut(name, ".tmp-")
	if !found || suffix == "" || len(prefix) != 1+prefixBytes ||
		prefix[0] != '.' {
		return false
	}
	_, err := hex.DecodeString(prefix[1:])
	return err == nil
}

func isTemporaryRootFile(name string) bool {
	prefix := "." + rootFileName + ".tmp-"
	return strings.HasPrefix(name, prefix) && len(name) > len(prefix)
}

// Root returns the currently published root. It remains available after Close
// so callers can retain the last observed commitment without storage I/O.
func (store *Store) Root() mpt.Root {
	if store == nil {
		return mpt.Root{}
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	return store.root
}

// GetNode reads and integrity-checks the immutable node stored under hash.
func (store *Store) GetNode(ctx context.Context, hash mpt.Root) ([]byte, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, mpt.ErrInvalidStore
	}
	store.mutex.RLock()
	if store.closed {
		store.mutex.RUnlock()
		return nil, mpt.ErrClosedStore
	}
	path := store.nodePath(hash)
	maximum := store.limits.MaxNodeBytes
	store.mutex.RUnlock()
	encoded, err := readBoundedFile(path, maximum)
	if errors.Is(err, os.ErrNotExist) {
		return nil, &mpt.MissingNodeError{Hash: hash, Cause: mpt.ErrMissingNode}
	}
	if err != nil {
		return nil, storageReadError(err)
	}
	if actual := keccakRoot(encoded); actual != hash {
		return nil, &mpt.CorruptNodeError{
			Hash: hash, Cause: fmt.Errorf("%w: hash mismatch", mpt.ErrCorruptNode),
		}
	}
	return encoded, nil
}

// CommitTrie makes every supplied node durable before atomically publishing
// its root. Failures can leave unreachable immutable node files, but never a
// published root whose supplied nodes were not synced first.
func (store *Store) CommitTrie(
	ctx context.Context,
	commit mpt.StoreCommit,
) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if store == nil {
		return mpt.ErrInvalidStore
	}
	nodes := commit.Nodes()
	if err := store.validateCommit(nodes); err != nil {
		return err
	}

	store.mutex.Lock()
	if store.closed {
		store.mutex.Unlock()
		return mpt.ErrClosedStore
	}
	if store.committing || store.root != commit.PreviousRoot() {
		store.mutex.Unlock()
		return mpt.ErrStaleRoot
	}
	store.committing = true
	store.mutex.Unlock()
	defer store.finishCommit()

	newNodes, err := store.prepareNodes(ctx, nodes)
	if err != nil {
		return err
	}
	for _, node := range newNodes {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if err := store.writeNewNode(node); err != nil {
			return err
		}
	}
	if err := syncDirectory(store.nodesPath); err != nil {
		return storageCommitError(err)
	}
	store.reachCheckpoint(nodesDurable)
	if err := checkContext(ctx); err != nil {
		return err
	}
	published, err := store.publishRoot(ctx, commit.Root())
	if published {
		store.mutex.Lock()
		store.root = commit.Root()
		store.mutex.Unlock()
	}
	if err != nil {
		return err
	}
	return nil
}

// IterateNodes visits an immutable directory snapshot in ascending hash order.
func (store *Store) IterateNodes(
	ctx context.Context,
	maximum int,
	yield func(hash mpt.Root, encoded []byte) error,
) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if store == nil {
		return mpt.ErrInvalidStore
	}
	if maximum <= 0 || yield == nil {
		return mpt.ErrInvalidIterator
	}
	store.mutex.RLock()
	if store.closed {
		store.mutex.RUnlock()
		return mpt.ErrClosedStore
	}
	nodesPath := store.nodesPath
	maxNodeBytes := store.limits.MaxNodeBytes
	maxStoredNodes := store.limits.MaxStoredNodes
	store.mutex.RUnlock()
	resultLimit := min(maximum, maxStoredNodes)
	if resultLimit == int(^uint(0)>>1) {
		return fmt.Errorf(
			"%w: invalid stored node bound",
			mpt.ErrResourceLimit,
		)
	}
	entries, err := readDirectoryBounded(nodesPath, resultLimit+1)
	if err != nil {
		if errors.Is(err, mpt.ErrResourceLimit) {
			return err
		}
		return storageReadError(err)
	}
	hashes := make([]mpt.Root, 0, min(len(entries), resultLimit))
	for _, entry := range entries {
		if !isTemporaryNodeFile(entry.Name()) {
			if len(hashes) == resultLimit {
				return fmt.Errorf(
					"%w: stored node bound exceeded",
					mpt.ErrResourceLimit,
				)
			}
			if err := checkContext(ctx); err != nil {
				return err
			}
			hash, parseErr := parseNodeName(entry)
			if parseErr != nil {
				return parseErr
			}
			hashes = append(hashes, hash)
		}
	}
	slices.SortFunc(hashes, func(left, right mpt.Root) int {
		return bytes.Compare(left[:], right[:])
	})
	for _, hash := range hashes {
		if err := checkContext(ctx); err != nil {
			return err
		}
		encoded, readErr := readBoundedFile(
			filepath.Join(nodesPath, hex.EncodeToString(hash[:])),
			maxNodeBytes,
		)
		if readErr != nil {
			return storageReadError(readErr)
		}
		if actual := keccakRoot(encoded); actual != hash {
			return &mpt.CorruptNodeError{
				Hash:  hash,
				Cause: fmt.Errorf("%w: hash mismatch", mpt.ErrCorruptNode),
			}
		}
		if err := yield(hash, encoded); err != nil {
			return err
		}
	}
	return nil
}

// Close releases this process's store handle. It does not delete durable data.
func (store *Store) Close() error {
	if store == nil {
		return mpt.ErrInvalidStore
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.closed {
		return mpt.ErrClosedStore
	}
	if store.committing {
		return fmt.Errorf("%w: commit in progress", mpt.ErrStorageCommit)
	}
	store.closed = true
	return nil
}

func (store *Store) validateCommit(nodes []mpt.StoredNode) error {
	if len(nodes) > store.limits.MaxCommitNodes {
		return fmt.Errorf("%w: commit node bound exceeded", mpt.ErrResourceLimit)
	}
	total := 0
	for _, node := range nodes {
		encoded := node.Encoded()
		if len(encoded) > store.limits.MaxNodeBytes ||
			len(encoded) > store.limits.MaxCommitBytes-total {
			return fmt.Errorf("%w: commit byte bound exceeded", mpt.ErrResourceLimit)
		}
		total += len(encoded)
		if actual := keccakRoot(encoded); actual != node.Hash() {
			return &mpt.CorruptNodeError{
				Hash:  node.Hash(),
				Cause: fmt.Errorf("%w: hash mismatch", mpt.ErrCorruptNode),
			}
		}
	}
	return nil
}

func (store *Store) writeNode(node mpt.StoredNode) error {
	path := store.nodePath(node.Hash())
	existing, err := readBoundedFile(path, store.limits.MaxNodeBytes)
	if err == nil {
		if !bytes.Equal(existing, node.Encoded()) {
			return &mpt.CorruptNodeError{
				Hash:  node.Hash(),
				Cause: fmt.Errorf("%w: immutable node conflict", mpt.ErrCorruptNode),
			}
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return storageReadError(err)
	}
	return store.writeNewNode(node)
}

func (store *Store) prepareNodes(
	ctx context.Context,
	nodes []mpt.StoredNode,
) ([]mpt.StoredNode, error) {
	newNodes := make([]mpt.StoredNode, 0, len(nodes))
	for _, node := range nodes {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		existing, err := readBoundedFile(
			store.nodePath(node.Hash()),
			store.limits.MaxNodeBytes,
		)
		switch {
		case err == nil:
			if !bytes.Equal(existing, node.Encoded()) {
				return nil, &mpt.CorruptNodeError{
					Hash: node.Hash(),
					Cause: fmt.Errorf(
						"%w: immutable node conflict",
						mpt.ErrCorruptNode,
					),
				}
			}
		case errors.Is(err, os.ErrNotExist):
			newNodes = append(newNodes, node)
		default:
			return nil, storageReadError(err)
		}
	}
	store.mutex.RLock()
	available := store.limits.MaxStoredNodes - store.storedNodes
	store.mutex.RUnlock()
	if len(newNodes) > available {
		return nil, fmt.Errorf(
			"%w: stored node bound exceeded",
			mpt.ErrResourceLimit,
		)
	}
	return newNodes, nil
}

func (store *Store) writeNewNode(node mpt.StoredNode) error {
	path := store.nodePath(node.Hash())
	store.mutex.Lock()
	if store.storedNodes >= store.limits.MaxStoredNodes {
		store.mutex.Unlock()
		return fmt.Errorf(
			"%w: stored node bound exceeded",
			mpt.ErrResourceLimit,
		)
	}
	store.storedNodes++
	store.mutex.Unlock()
	if err := writeAtomic(path, node.Encoded()); err != nil {
		store.mutex.Lock()
		store.storedNodes--
		store.mutex.Unlock()
		return storageCommitError(err)
	}
	return nil
}

func (store *Store) finishCommit() {
	store.mutex.Lock()
	store.committing = false
	store.mutex.Unlock()
}

func (store *Store) publishRoot(
	ctx context.Context,
	root mpt.Root,
) (bool, error) {
	if err := checkContext(ctx); err != nil {
		return false, err
	}
	if err := writeAtomic(
		filepath.Join(store.path, rootFileName),
		encodeRootRecord(root),
	); err != nil {
		return false, storageCommitError(err)
	}
	store.reachCheckpoint(rootRenamed)
	if err := syncDirectory(store.path); err != nil {
		return true, storageCommitError(err)
	}
	return true, nil
}

func (store *Store) reachCheckpoint(stage commitStage) {
	if store.checkpoint != nil {
		store.checkpoint(stage)
	}
}

func (store *Store) nodePath(hash mpt.Root) string {
	return filepath.Join(store.nodesPath, hex.EncodeToString(hash[:]))
}

func validateLimits(limits Limits) error {
	if limits.MaxNodeBytes <= 0 ||
		limits.MaxNodeBytes == int(^uint(0)>>1) ||
		limits.MaxCommitNodes <= 0 ||
		limits.MaxCommitBytes <= 0 ||
		limits.MaxStoredNodes <= 0 ||
		limits.MaxStoredNodes == int(^uint(0)>>1) {
		return fmt.Errorf("%w: invalid filesystem limits", mpt.ErrResourceLimit)
	}
	return nil
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return storageReadError(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: symbolic-link store path", mpt.ErrInvalidStore)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: store path is not a directory", mpt.ErrInvalidStore)
	}
	return nil
}

func writeAtomic(path string, content []byte) error {
	return writeAtomicWith(defaultAtomicOperations(), path, content)
}

type atomicFile interface {
	Name() string
	Chmod(mode os.FileMode) error
	Write(content []byte) (int, error)
	Sync() error
	Close() error
}

type atomicOperations struct {
	createTemp func(directory, pattern string) (atomicFile, error)
	rename     func(oldPath, newPath string) error
	remove     func(path string) error
}

func defaultAtomicOperations() atomicOperations {
	return atomicOperations{
		createTemp: func(directory, pattern string) (atomicFile, error) {
			return os.CreateTemp(directory, pattern)
		},
		rename: os.Rename,
		remove: os.Remove,
	}
}

func writeAtomicWith(
	operations atomicOperations,
	path string,
	content []byte,
) error {
	directory := filepath.Dir(path)
	file, err := operations.createTemp(
		directory,
		"."+filepath.Base(path)+".tmp-*",
	)
	if err != nil {
		return err
	}
	temporary := file.Name()
	remove := true
	defer func() {
		if remove {
			_ = operations.remove(temporary)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	written, err := file.Write(content)
	if err != nil {
		_ = file.Close()
		return err
	}
	if written != len(content) {
		_ = file.Close()
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := operations.rename(temporary, path); err != nil {
		return err
	}
	remove = false
	return nil
}

func syncDirectory(path string) error {
	return syncDirectoryWith(func() (syncFile, error) {
		return os.Open(path)
	})
}

type syncFile interface {
	Sync() error
	Close() error
}

func syncDirectoryWith(open func() (syncFile, error)) error {
	directory, err := open()
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

type directoryReader interface {
	ReadDir(maximum int) ([]os.DirEntry, error)
	Close() error
}

func readDirectoryBounded(
	path string,
	maximum int,
) ([]os.DirEntry, error) {
	return readDirectoryBoundedWith(
		func() (directoryReader, error) {
			return os.Open(path)
		},
		maximum,
	)
}

func readDirectoryBoundedWith(
	open func() (directoryReader, error),
	maximum int,
) ([]os.DirEntry, error) {
	if maximum <= 0 || maximum == int(^uint(0)>>1) {
		return nil, fmt.Errorf(
			"%w: invalid directory entry bound",
			mpt.ErrResourceLimit,
		)
	}
	directory, err := open()
	if err != nil {
		return nil, err
	}
	entries, readErr := directory.ReadDir(maximum + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(entries) > maximum {
		return nil, fmt.Errorf(
			"%w: directory entry bound exceeded",
			mpt.ErrResourceLimit,
		)
	}
	return entries, nil
}

func readBoundedFile(path string, maximum int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("filesystem: non-regular file")
	}
	return readRegularFile(
		func() (readableFile, error) {
			return os.Open(path)
		},
		maximum,
	)
}

type readableFile interface {
	io.Reader
	Close() error
}

func readRegularFile(
	open func() (readableFile, error),
	maximum int,
) ([]byte, error) {
	if maximum <= 0 || maximum == int(^uint(0)>>1) {
		return nil, fmt.Errorf(
			"%w: invalid file byte bound",
			mpt.ErrResourceLimit,
		)
	}
	file, err := open()
	if err != nil {
		return nil, err
	}
	reader := io.LimitReader(file, int64(maximum)+1)
	content, err := io.ReadAll(reader)
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(content) > maximum {
		return nil, fmt.Errorf("%w: file byte bound exceeded", mpt.ErrResourceLimit)
	}
	return content, nil
}

func encodeRootRecord(root mpt.Root) []byte {
	record := make([]byte, rootRecordLen)
	copy(record, rootMagic[:])
	copy(record[len(rootMagic):], root[:])
	checksum := sha256.Sum256(record[:len(rootMagic)+mpt.RootBytes])
	copy(record[len(rootMagic)+mpt.RootBytes:], checksum[:])
	return record
}

func decodeRootRecord(record []byte) (mpt.Root, error) {
	if len(record) != rootRecordLen {
		return mpt.Root{}, &mpt.CorruptNodeError{
			Cause: fmt.Errorf("%w: malformed root record", mpt.ErrCorruptNode),
		}
	}
	if !bytes.Equal(record[:len(rootMagic)], rootMagic[:]) {
		return mpt.Root{}, &mpt.CorruptNodeError{
			Cause: fmt.Errorf("%w: malformed root record", mpt.ErrCorruptNode),
		}
	}
	payloadEnd := len(rootMagic) + mpt.RootBytes
	checksum := sha256.Sum256(record[:payloadEnd])
	if !bytes.Equal(record[payloadEnd:], checksum[:]) {
		return mpt.Root{}, &mpt.CorruptNodeError{
			Cause: fmt.Errorf("%w: root record checksum", mpt.ErrCorruptNode),
		}
	}
	var root mpt.Root
	copy(root[:], record[len(rootMagic):payloadEnd])
	return root, nil
}

func parseNodeName(entry os.DirEntry) (mpt.Root, error) {
	if !entry.Type().IsRegular() {
		return mpt.Root{}, &mpt.CorruptNodeError{
			Cause: fmt.Errorf("%w: malformed node filename", mpt.ErrCorruptNode),
		}
	}
	return decodeNodeName(entry.Name())
}

func decodeNodeName(name string) (mpt.Root, error) {
	if len(name) != hex.EncodedLen(mpt.RootBytes) {
		return mpt.Root{}, &mpt.CorruptNodeError{
			Cause: fmt.Errorf("%w: malformed node filename", mpt.ErrCorruptNode),
		}
	}
	var root mpt.Root
	_, err := hex.Decode(root[:], []byte(name))
	if err != nil || hex.EncodeToString(root[:]) != name {
		return mpt.Root{}, &mpt.CorruptNodeError{
			Cause: fmt.Errorf("%w: malformed node filename", mpt.ErrCorruptNode),
		}
	}
	return root, nil
}

func keccakRoot(encoded []byte) mpt.Root {
	hash := sha3.NewLegacyKeccak256()
	_, _ = hash.Write(encoded)
	var root mpt.Root
	hash.Sum(root[:0])
	return root
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return mpt.ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", mpt.ErrCanceled, err)
	}
	return nil
}

func storageReadError(err error) error {
	return fmt.Errorf("%w: %w", mpt.ErrStorageRead, err)
}

func storageCommitError(err error) error {
	return fmt.Errorf("%w: %w", mpt.ErrStorageCommit, err)
}

var _ mpt.NodeStore = (*Store)(nil)
var _ mpt.NodeIterator = (*Store)(nil)

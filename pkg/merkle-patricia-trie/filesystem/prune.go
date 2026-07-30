package filesystem

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

const (
	prunePendingDirectory   = ".PRUNE.pending"
	pruneCommittedDirectory = ".PRUNE.committed"
)

type pruneStage uint8

const (
	pruneNodesStaged pruneStage = iota
	pruneCommitted
)

type pruneOperations struct {
	mkdir         func(string, os.FileMode) error
	rename        func(string, string) error
	remove        func(string) error
	syncDirectory func(string) error
	lstat         func(string) (os.FileInfo, error)
	readDirectory func(string, int) ([]os.DirEntry, error)
	readFile      func(string, int) ([]byte, error)
}

func defaultPruneOperations() pruneOperations {
	return pruneOperations{
		mkdir:         os.Mkdir,
		rename:        os.Rename,
		remove:        os.Remove,
		syncDirectory: syncDirectory,
		lstat:         os.Lstat,
		readDirectory: readDirectoryBounded,
		readFile:      readBoundedFile,
	}
}

// Prune durably removes nodes unreachable from the published root and every
// durable retention. The mark phase validates the complete reachable graph.
// A crash before the prune commit point restores staged nodes during Open; a
// crash after it completes deletion without making any retained root unreadable.
// A non-zero result returned with an error means the commit point passed and
// reports the nodes logically removed; a zero result with an error reports no
// committed removal. Open reconciles any transaction artifact after a crash.
func (store *Store) Prune(
	ctx context.Context,
	limits mpt.ReachabilityLimits,
) (mpt.PruneResult, error) {
	if err := checkContext(ctx); err != nil {
		return mpt.PruneResult{}, err
	}
	if store == nil {
		return mpt.PruneResult{}, mpt.ErrInvalidStore
	}
	if store.path == "" {
		return mpt.PruneResult{}, mpt.ErrInvalidStore
	}
	store.mutex.Lock()
	if store.closed {
		store.mutex.Unlock()
		return mpt.PruneResult{}, mpt.ErrClosedStore
	}
	if store.committing || store.retentionChanging || store.pruning {
		store.mutex.Unlock()
		return mpt.PruneResult{}, mpt.ErrStaleRoot
	}
	if len(store.retentions) > limits.MaxRetentions {
		store.mutex.Unlock()
		return mpt.PruneResult{}, fmt.Errorf(
			"%w: retained root bound exceeded",
			mpt.ErrResourceLimit,
		)
	}
	store.pruning = true
	roots := []mpt.Root{store.root}
	for _, root := range store.retentions {
		roots = append(roots, root)
	}
	before := store.storedNodes
	store.mutex.Unlock()
	defer store.finishPrune()

	if err := recoverPruneArtifacts(
		ctx,
		store.path,
		store.nodesPath,
		store.limits,
		store.pruneOperations,
	); err != nil {
		return mpt.PruneResult{}, err
	}
	reachableNodes, err := mpt.CollectReachableNodes(
		ctx,
		roots,
		store,
		limits,
	)
	if err != nil {
		return mpt.PruneResult{}, err
	}
	reachable := make(map[mpt.Root]struct{}, len(reachableNodes))
	for _, node := range reachableNodes {
		reachable[node.Hash()] = struct{}{}
	}
	unreachable, removedBytes, err := store.unreachableNodes(ctx, reachable)
	if err != nil {
		return mpt.PruneResult{}, err
	}
	if len(unreachable) == 0 {
		return mpt.NewPruneResult(before, before, 0), nil
	}

	committed, err := stagePrune(
		ctx,
		store.path,
		store.nodesPath,
		unreachable,
		store.limits.MaxStoredNodes,
		store.limits.MaxNodeBytes,
		store.reachPruneCheckpoint,
		store.pruneOperations,
	)
	if committed {
		store.mutex.Lock()
		store.storedNodes -= len(unreachable)
		after := store.storedNodes
		store.mutex.Unlock()
		result := mpt.NewPruneResult(before, after, removedBytes)
		return result, err
	}
	return mpt.PruneResult{}, err
}

func (store *Store) finishPrune() {
	store.mutex.Lock()
	store.pruning = false
	store.mutex.Unlock()
}

func (store *Store) unreachableNodes(
	ctx context.Context,
	reachable map[mpt.Root]struct{},
) ([]mpt.Root, uint64, error) {
	entries, err := store.pruneOperations.readDirectory(
		store.nodesPath,
		store.limits.MaxStoredNodes,
	)
	if err != nil {
		if errors.Is(err, mpt.ErrResourceLimit) {
			return nil, 0, err
		}
		return nil, 0, storageReadError(err)
	}
	unreachable := make([]mpt.Root, 0)
	var removedBytes uint64
	for _, entry := range entries {
		if err := checkContext(ctx); err != nil {
			return nil, 0, err
		}
		hash, err := parseNodeName(entry)
		if err != nil {
			return nil, 0, err
		}
		if _, retained := reachable[hash]; retained {
			continue
		}
		encoded, err := store.pruneOperations.readFile(
			store.nodePath(hash),
			store.limits.MaxNodeBytes,
		)
		if err != nil {
			return nil, 0, storageReadError(err)
		}
		if actual := keccakRoot(encoded); actual != hash {
			return nil, 0, &mpt.CorruptNodeError{
				Hash:  hash,
				Cause: fmt.Errorf("%w: hash mismatch", mpt.ErrCorruptNode),
			}
		}
		removedBytes += uint64(len(encoded))
		unreachable = append(unreachable, hash)
	}
	store.mutex.RLock()
	inventoryMatches := len(entries) == store.storedNodes
	store.mutex.RUnlock()
	if !inventoryMatches {
		return nil, 0, &mpt.CorruptNodeError{
			Cause: fmt.Errorf(
				"%w: stored node inventory changed",
				mpt.ErrCorruptNode,
			),
		}
	}
	slices.SortFunc(unreachable, func(left, right mpt.Root) int {
		return bytes.Compare(left[:], right[:])
	})
	return unreachable, removedBytes, nil
}

func stagePrune(
	ctx context.Context,
	storePath, nodesPath string,
	hashes []mpt.Root,
	maximum, maxNodeBytes int,
	checkpoint func(pruneStage),
	operations pruneOperations,
) (committed bool, resultErr error) {
	pending := filepath.Join(storePath, prunePendingDirectory)
	committedPath := filepath.Join(storePath, pruneCommittedDirectory)
	if err := operations.mkdir(pending, 0o700); err != nil {
		return false, storageCommitError(err)
	}
	rollback := true
	defer func() {
		if !rollback {
			return
		}
		if err := restorePendingPrune(
			nodesPath,
			pending,
			maximum,
			maxNodeBytes,
			operations,
		); err != nil {
			resultErr = errors.Join(resultErr, storageCommitError(err))
		}
	}()
	if err := operations.syncDirectory(storePath); err != nil {
		return false, storageCommitError(err)
	}
	for _, hash := range hashes {
		if err := checkContext(ctx); err != nil {
			return false, err
		}
		name := fmt.Sprintf("%x", hash)
		if err := operations.rename(
			filepath.Join(nodesPath, name),
			filepath.Join(pending, name),
		); err != nil {
			return false, storageCommitError(err)
		}
	}
	if err := operations.syncDirectory(nodesPath); err != nil {
		return false, storageCommitError(err)
	}
	if err := operations.syncDirectory(pending); err != nil {
		return false, storageCommitError(err)
	}
	checkpoint(pruneNodesStaged)
	if err := checkContext(ctx); err != nil {
		return false, err
	}
	if err := operations.rename(pending, committedPath); err != nil {
		return false, storageCommitError(err)
	}
	rollback = false
	checkpoint(pruneCommitted)
	if err := operations.syncDirectory(storePath); err != nil {
		return true, storageCommitError(err)
	}
	if err := removeCommittedPruneWith(
		committedPath,
		maximum,
		operations,
	); err != nil {
		return true, storageCommitError(err)
	}
	if err := operations.syncDirectory(storePath); err != nil {
		return true, storageCommitError(err)
	}
	return true, nil
}

func recoverPruneArtifacts(
	ctx context.Context,
	storePath, nodesPath string,
	limits Limits,
	operations ...pruneOperations,
) error {
	ops := defaultPruneOperations()
	if len(operations) != 0 {
		ops = operations[0]
	}
	pending := filepath.Join(storePath, prunePendingDirectory)
	committed := filepath.Join(storePath, pruneCommittedDirectory)
	pendingExists, err := directoryExistsWith(pending, ops)
	if err != nil {
		return storageReadError(err)
	}
	committedExists, err := directoryExistsWith(committed, ops)
	if err != nil {
		return storageReadError(err)
	}
	if pendingExists && committedExists {
		return &mpt.CorruptNodeError{
			Cause: fmt.Errorf(
				"%w: conflicting prune transactions",
				mpt.ErrCorruptNode,
			),
		}
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	if pendingExists {
		if err := restorePendingPrune(
			nodesPath,
			pending,
			limits.MaxStoredNodes,
			limits.MaxNodeBytes,
			ops,
		); err != nil {
			return storageCommitError(err)
		}
		if err := ops.syncDirectory(storePath); err != nil {
			return storageCommitError(err)
		}
	}
	if committedExists {
		if err := removeCommittedPruneWith(
			committed,
			limits.MaxStoredNodes,
			ops,
		); err != nil {
			return storageCommitError(err)
		}
		if err := ops.syncDirectory(storePath); err != nil {
			return storageCommitError(err)
		}
	}
	return nil
}

func restorePendingPrune(
	nodesPath, pendingPath string,
	maximum, maxNodeBytes int,
	operations ...pruneOperations,
) error {
	ops := defaultPruneOperations()
	if len(operations) != 0 {
		ops = operations[0]
	}
	entries, err := readPruneEntriesWith(pendingPath, maximum, ops)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		source := filepath.Join(pendingPath, entry.Name())
		target := filepath.Join(nodesPath, entry.Name())
		if err := restorePrunedNodeWith(
			source,
			target,
			maxNodeBytes,
			ops,
		); err != nil {
			return err
		}
	}
	if err := ops.syncDirectory(nodesPath); err != nil {
		return err
	}
	return ops.remove(pendingPath)
}

func restorePrunedNode(source, target string, maximum int) error {
	return restorePrunedNodeWith(
		source,
		target,
		maximum,
		defaultPruneOperations(),
	)
}

func restorePrunedNodeWith(
	source, target string,
	maximum int,
	operations pruneOperations,
) error {
	_, err := operations.lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return operations.rename(source, target)
	}
	if err != nil {
		return err
	}
	sourceBytes, err := operations.readFile(source, maximum)
	if err != nil {
		return err
	}
	targetBytes, err := operations.readFile(target, maximum)
	if err != nil {
		return err
	}
	if !bytes.Equal(sourceBytes, targetBytes) {
		return fmt.Errorf("filesystem: conflicting restored prune node")
	}
	return operations.remove(source)
}

func (store *Store) reachPruneCheckpoint(stage pruneStage) {
	if store.pruneCheckpoint != nil {
		store.pruneCheckpoint(stage)
	}
}

func removeCommittedPrune(path string, maximum int) error {
	return removeCommittedPruneWith(
		path,
		maximum,
		defaultPruneOperations(),
	)
}

func removeCommittedPruneWith(
	path string,
	maximum int,
	operations pruneOperations,
) error {
	entries, err := readPruneEntriesWith(path, maximum, operations)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := operations.remove(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return operations.remove(path)
}

func readPruneEntries(path string, maximum int) ([]os.DirEntry, error) {
	return readPruneEntriesWith(
		path,
		maximum,
		defaultPruneOperations(),
	)
}

func readPruneEntriesWith(
	path string,
	maximum int,
	operations pruneOperations,
) ([]os.DirEntry, error) {
	entries, err := operations.readDirectory(path, maximum)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if _, err := parseNodeName(entry); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func directoryExists(path string) (bool, error) {
	return directoryExistsWith(path, defaultPruneOperations())
}

func directoryExistsWith(
	path string,
	operations pruneOperations,
) (bool, error) {
	info, err := operations.lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("filesystem: invalid prune transaction path")
	}
	return true, nil
}

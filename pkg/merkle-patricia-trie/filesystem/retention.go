package filesystem

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

type retentionID [retentionIDBytes]byte

type retentionStage uint8

const (
	retentionValidated retentionStage = iota
	retentionPublished
	retentionReleased
)

type rootRetention struct {
	store *Store
	id    retentionID
	root  mpt.Root
}

type retentionOperations struct {
	newID         func(map[retentionID]mpt.Root) (retentionID, error)
	writeAtomic   func(string, []byte) error
	syncDirectory func(string) error
	remove        func(string) error
}

func defaultRetentionOperations() retentionOperations {
	return retentionOperations{
		newID:         newRetentionID,
		writeAtomic:   writeAtomic,
		syncDirectory: syncDirectory,
		remove:        os.Remove,
	}
}

// RetainRoot durably records an independent lease for a complete historical
// root. The returned lease remains effective across Store.Close and process
// restart; use Retentions after reopening to recover durable lease handles.
func (store *Store) RetainRoot(
	ctx context.Context,
	root mpt.Root,
	limits mpt.ReachabilityLimits,
) (mpt.RootRetention, error) {
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
	if store.committing || store.retentionChanging || store.pruning {
		store.mutex.RUnlock()
		return nil, mpt.ErrStaleRoot
	}
	baseRoot := store.root
	retentionCount := len(store.retentions)
	store.mutex.RUnlock()
	if retentionCount >= store.limits.MaxRetentions {
		return nil, fmt.Errorf(
			"%w: retained root bound exceeded",
			mpt.ErrResourceLimit,
		)
	}
	if _, err := mpt.CollectReachableNodes(
		ctx,
		[]mpt.Root{root},
		store,
		limits,
	); err != nil {
		return nil, err
	}
	store.reachRetentionCheckpoint(retentionValidated)

	store.mutex.Lock()
	if store.closed {
		store.mutex.Unlock()
		return nil, mpt.ErrClosedStore
	}
	if store.committing || store.retentionChanging || store.pruning ||
		store.root != baseRoot {
		store.mutex.Unlock()
		return nil, mpt.ErrStaleRoot
	}
	if len(store.retentions) >= store.limits.MaxRetentions {
		store.mutex.Unlock()
		return nil, fmt.Errorf(
			"%w: retained root bound exceeded",
			mpt.ErrResourceLimit,
		)
	}
	store.retentionChanging = true
	store.mutex.Unlock()
	defer store.finishRetentionChange()

	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	operations := store.retentionOperations
	id, err := operations.newID(store.retentions)
	if err != nil {
		return nil, storageCommitError(err)
	}
	retention := &rootRetention{store: store, id: id, root: root}
	if err := operations.writeAtomic(
		store.retentionPath(id),
		encodeRetentionRecord(root),
	); err != nil {
		return nil, storageCommitError(err)
	}
	store.reachRetentionCheckpoint(retentionPublished)
	if err := operations.syncDirectory(store.retentionsPath); err != nil {
		syncErr := storageCommitError(err)
		if removeErr := operations.remove(store.retentionPath(id)); removeErr == nil {
			if rollbackErr := operations.syncDirectory(
				store.retentionsPath,
			); rollbackErr != nil {
				return nil, errors.Join(
					syncErr,
					storageCommitError(rollbackErr),
				)
			}
			return nil, syncErr
		} else {
			store.mutex.Lock()
			store.retentions[id] = root
			store.mutex.Unlock()
			return retention, errors.Join(
				syncErr,
				storageCommitError(removeErr),
			)
		}
	}
	store.mutex.Lock()
	store.retentions[id] = root
	store.mutex.Unlock()
	return retention, nil
}

// Retentions returns durable lease handles in stable identifier order. The
// maximum must cover every retained lease; a smaller bound fails without
// returning a partial inventory.
func (store *Store) Retentions(
	ctx context.Context,
	maximum int,
) ([]mpt.RootRetention, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, mpt.ErrInvalidStore
	}
	if maximum <= 0 {
		return nil, fmt.Errorf(
			"%w: invalid retention inventory bound",
			mpt.ErrResourceLimit,
		)
	}
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	if store.closed {
		return nil, mpt.ErrClosedStore
	}
	if len(store.retentions) > maximum {
		return nil, fmt.Errorf(
			"%w: retained root bound exceeded",
			mpt.ErrResourceLimit,
		)
	}
	ids := make([]retentionID, 0, len(store.retentions))
	for id := range store.retentions {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(left, right retentionID) int {
		return bytes.Compare(left[:], right[:])
	})
	retentions := make([]mpt.RootRetention, 0, len(ids))
	for _, id := range ids {
		retentions = append(retentions, &rootRetention{
			store: store,
			id:    id,
			root:  store.retentions[id],
		})
	}
	return retentions, nil
}

func (retention *rootRetention) Root() mpt.Root {
	if retention == nil {
		return mpt.Root{}
	}
	return retention.root
}

func (retention *rootRetention) Release(ctx context.Context) error {
	if retention == nil || retention.store == nil {
		return mpt.ErrReleasedRetention
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	store := retention.store
	store.mutex.Lock()
	if store.closed {
		store.mutex.Unlock()
		return mpt.ErrClosedStore
	}
	if store.committing || store.retentionChanging || store.pruning {
		store.mutex.Unlock()
		return mpt.ErrStaleRoot
	}
	if _, exists := store.retentions[retention.id]; !exists {
		store.mutex.Unlock()
		return mpt.ErrReleasedRetention
	}
	store.retentionChanging = true
	operations := store.retentionOperations
	store.mutex.Unlock()
	defer store.finishRetentionChange()

	if err := operations.remove(store.retentionPath(retention.id)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mpt.ErrReleasedRetention
		}
		return storageCommitError(err)
	}
	store.reachRetentionCheckpoint(retentionReleased)
	store.mutex.Lock()
	delete(store.retentions, retention.id)
	store.mutex.Unlock()
	if err := operations.syncDirectory(store.retentionsPath); err != nil {
		return storageCommitError(err)
	}
	return nil
}

func (store *Store) finishRetentionChange() {
	store.mutex.Lock()
	store.retentionChanging = false
	store.mutex.Unlock()
}

func (store *Store) reachRetentionCheckpoint(stage retentionStage) {
	if store.retentionCheckpoint != nil {
		store.retentionCheckpoint(stage)
	}
}

func (store *Store) retentionPath(id retentionID) string {
	return filepath.Join(store.retentionsPath, hex.EncodeToString(id[:]))
}

func newRetentionID(existing map[retentionID]mpt.Root) (retentionID, error) {
	return newRetentionIDWith(existing, func(destination []byte) error {
		_, err := rand.Read(destination)
		return err
	})
}

func newRetentionIDWith(
	existing map[retentionID]mpt.Root,
	fill func([]byte) error,
) (retentionID, error) {
	for range 16 {
		var id retentionID
		if err := fill(id[:]); err != nil {
			return retentionID{}, err
		}
		if _, exists := existing[id]; !exists {
			return id, nil
		}
	}
	return retentionID{}, fmt.Errorf("filesystem: retention identifier collision")
}

func recoverTemporaryRetentions(
	ctx context.Context,
	path string,
	maximum int,
) error {
	removed, err := removeTemporaryFiles(
		ctx,
		path,
		maximum+1,
		defaultRecoveryOperations(),
		isTemporaryRetentionFile,
	)
	if err != nil {
		return err
	}
	if removed {
		if err := syncDirectory(path); err != nil {
			return storageCommitError(err)
		}
	}
	return nil
}

func isTemporaryRetentionFile(name string) bool {
	const encodedIDBytes = retentionIDBytes * 2
	prefix, suffix, found := strings.Cut(name, ".tmp-")
	if !found || suffix == "" || len(prefix) != 1+encodedIDBytes ||
		prefix[0] != '.' {
		return false
	}
	_, err := hex.DecodeString(prefix[1:])
	return err == nil
}

func readRetentions(
	ctx context.Context,
	path string,
	maximum int,
) (map[retentionID]mpt.Root, error) {
	entries, err := readDirectoryBounded(path, maximum)
	if err != nil {
		if errors.Is(err, mpt.ErrResourceLimit) {
			return nil, err
		}
		return nil, storageReadError(err)
	}
	retentions := make(map[retentionID]mpt.Root, len(entries))
	for _, entry := range entries {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		id, err := parseRetentionName(entry)
		if err != nil {
			return nil, err
		}
		record, err := readBoundedFile(
			filepath.Join(path, entry.Name()),
			retentionRecordLen,
		)
		if err != nil {
			return nil, storageReadError(err)
		}
		root, err := decodeRetentionRecord(record)
		if err != nil {
			return nil, err
		}
		retentions[id] = root
	}
	return retentions, nil
}

func parseRetentionName(entry os.DirEntry) (retentionID, error) {
	if !entry.Type().IsRegular() {
		return retentionID{}, corruptRetentionError("non-regular retention file")
	}
	name := entry.Name()
	if len(name) != hex.EncodedLen(retentionIDBytes) ||
		name != strings.ToLower(name) {
		return retentionID{}, corruptRetentionError("invalid retention filename")
	}
	var id retentionID
	if _, err := hex.Decode(id[:], []byte(name)); err != nil {
		return retentionID{}, corruptRetentionError("invalid retention filename")
	}
	return id, nil
}

func encodeRetentionRecord(root mpt.Root) []byte {
	record := make([]byte, retentionRecordLen)
	copy(record, retentionMagic[:])
	copy(record[len(retentionMagic):], root[:])
	checksum := sha256.Sum256(
		record[:len(retentionMagic)+mpt.RootBytes],
	)
	copy(record[len(retentionMagic)+mpt.RootBytes:], checksum[:])
	return record
}

func decodeRetentionRecord(record []byte) (mpt.Root, error) {
	if len(record) != retentionRecordLen {
		return mpt.Root{}, corruptRetentionError("invalid retention record length")
	}
	if !bytes.Equal(record[:len(retentionMagic)], retentionMagic[:]) {
		return mpt.Root{}, corruptRetentionError("invalid retention record magic")
	}
	payloadEnd := len(retentionMagic) + mpt.RootBytes
	checksum := sha256.Sum256(record[:payloadEnd])
	if !bytes.Equal(record[payloadEnd:], checksum[:]) {
		return mpt.Root{}, corruptRetentionError("invalid retention checksum")
	}
	var root mpt.Root
	copy(root[:], record[len(retentionMagic):payloadEnd])
	return root, nil
}

func corruptRetentionError(message string) error {
	return &mpt.CorruptNodeError{
		Cause: fmt.Errorf("%w: %s", mpt.ErrCorruptNode, message),
	}
}

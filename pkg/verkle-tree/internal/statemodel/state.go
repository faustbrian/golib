package statemodel

import (
	"bytes"
	"context"
	"slices"
)

const (
	// These architecture-independent upper bounds cover each owned scratch
	// element. Counts are capped at MaxInt32, so every product and sum below
	// fits in uint64 before any allocation.
	workingUpdateBytes = uint64(128)
	workingEntryBytes  = uint64(64)
	maxSupportedCount  = uint32(2_147_483_647)
)

// Key is one fixed-length raw key in the experimental profile.
type Key [32]byte

// Value is one fixed-length raw value. Its zero value remains a present value.
type Value [32]byte

// UpdateKind distinguishes writing a value from deleting its key.
type UpdateKind uint8

const (
	// UpdateSet inserts or replaces one value, including the all-zero value.
	UpdateSet UpdateKind = iota + 1

	// UpdateDelete removes one key and is distinct from UpdateSet.
	UpdateDelete
)

// Update is one caller-owned state transition. Its array fields cannot alias
// caller byte slices.
type Update struct {
	kind  UpdateKind
	key   Key
	value Value
}

// Set returns an update that inserts or replaces key with value.
func Set(key Key, value Value) Update {
	return Update{kind: UpdateSet, key: key, value: value}
}

// Delete returns an update that removes key. Deleting an absent key is a
// deterministic no-op.
func Delete(key Key) Update {
	return Update{kind: UpdateDelete, key: key}
}

func (update Update) validate() error {
	switch update.kind {
	case UpdateSet:
		return nil
	case UpdateDelete:
		if update.value == (Value{}) {
			return nil
		}
	}

	return errInvalidUpdate
}

// Limits bounds every allocation-amplifying operation in the reference model.
// Zero values are invalid and no field means unbounded.
type Limits struct {
	MaxBatchUpdates   uint32
	MaxEntries        uint32
	MaxTemporaryBytes uint64
}

func (limits Limits) validate() error {
	if limits.MaxBatchUpdates == 0 ||
		limits.MaxEntries == 0 ||
		limits.MaxTemporaryBytes == 0 ||
		limits.MaxBatchUpdates > maxSupportedCount ||
		limits.MaxEntries > maxSupportedCount {
		return errInvalidLimits
	}

	return nil
}

type entry struct {
	key   Key
	value Value
}

// Snapshot is an immutable ordered state value used as a slow transition
// oracle. Copies are safe for concurrent reads because entries are never
// mutated after publication.
type Snapshot struct {
	limits  Limits
	entries []entry
	valid   bool
}

// NewSnapshot validates limits and returns an empty immutable snapshot.
func NewSnapshot(limits Limits) (Snapshot, error) {
	if err := limits.validate(); err != nil {
		return Snapshot{}, err
	}

	return Snapshot{limits: limits, valid: true}, nil
}

// Get returns a defensively copied array value and whether key is present.
// Absence is distinct from a present all-zero value.
func (snapshot Snapshot) Get(
	ctx context.Context,
	key Key,
) (Value, bool, error) {
	if err := snapshot.validate(); err != nil {
		return Value{}, false, err
	}
	if err := checkContext(ctx); err != nil {
		return Value{}, false, err
	}

	index, found := findEntry(snapshot.entries, key)
	if !found {
		return Value{}, false, nil
	}

	return snapshot.entries[index].value, true, nil
}

// Keys returns all present keys in ascending bytewise order.
func (snapshot Snapshot) Keys(ctx context.Context) ([]Key, error) {
	if err := snapshot.validate(); err != nil {
		return nil, err
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	keys := make([]Key, len(snapshot.entries))
	for index := range snapshot.entries {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		keys[index] = snapshot.entries[index].key
	}

	return keys, nil
}

// Apply validates and atomically applies one batch in ascending key order.
// Duplicate keys fail the complete batch. The receiver remains unchanged on
// every success and failure path.
func (snapshot Snapshot) Apply(
	ctx context.Context,
	updates []Update,
) (Snapshot, error) {
	if err := snapshot.validate(); err != nil {
		return Snapshot{}, err
	}
	if err := checkContext(ctx); err != nil {
		return Snapshot{}, err
	}
	if len(updates) == 0 {
		return snapshot, nil
	}
	if uint64(len(updates)) > uint64(snapshot.limits.MaxBatchUpdates) {
		return Snapshot{}, &ResourceError{
			Kind:   ResourceBatchUpdates,
			Limit:  uint64(snapshot.limits.MaxBatchUpdates),
			Actual: uint64(len(updates)),
		}
	}

	updateBytes := uint64(len(updates)) * workingUpdateBytes
	if updateBytes > snapshot.limits.MaxTemporaryBytes {
		return Snapshot{}, &ResourceError{
			Kind:   ResourceTemporaryBytes,
			Limit:  snapshot.limits.MaxTemporaryBytes,
			Actual: updateBytes,
		}
	}

	ordered := append([]Update(nil), updates...)
	slices.SortFunc(ordered, func(left, right Update) int {
		return compareKey(left.key, right.key)
	})
	for index := range ordered {
		if err := checkContext(ctx); err != nil {
			return Snapshot{}, err
		}
		if err := ordered[index].validate(); err != nil {
			return Snapshot{}, err
		}
		if index > 0 && ordered[index-1].key == ordered[index].key {
			return Snapshot{}, errDuplicateKey
		}
	}

	finalCount := uint64(len(snapshot.entries))
	for _, update := range ordered {
		_, found := findEntry(snapshot.entries, update.key)
		switch {
		case update.kind == UpdateSet && !found:
			finalCount++
		case update.kind == UpdateDelete && found:
			finalCount--
		}
	}
	if finalCount > uint64(snapshot.limits.MaxEntries) {
		return Snapshot{}, &ResourceError{
			Kind:   ResourceEntries,
			Limit:  uint64(snapshot.limits.MaxEntries),
			Actual: finalCount,
		}
	}

	temporaryBytes := updateBytes + finalCount*workingEntryBytes
	if temporaryBytes > snapshot.limits.MaxTemporaryBytes {
		return Snapshot{}, &ResourceError{
			Kind:   ResourceTemporaryBytes,
			Limit:  snapshot.limits.MaxTemporaryBytes,
			Actual: temporaryBytes,
		}
	}

	result := make([]entry, 0, int(finalCount))
	oldIndex := 0
	updateIndex := 0
	for oldIndex < len(snapshot.entries) || updateIndex < len(ordered) {
		if err := checkContext(ctx); err != nil {
			return Snapshot{}, err
		}

		switch {
		case oldIndex == len(snapshot.entries):
			update := ordered[updateIndex]
			if update.kind == UpdateSet {
				result = append(result, entry{key: update.key, value: update.value})
			}
			updateIndex++
		case updateIndex == len(ordered):
			result = append(result, snapshot.entries[oldIndex])
			oldIndex++
		default:
			old := snapshot.entries[oldIndex]
			update := ordered[updateIndex]
			comparison := compareKey(old.key, update.key)
			switch {
			case comparison < 0:
				result = append(result, old)
				oldIndex++
			case comparison > 0:
				if update.kind == UpdateSet {
					result = append(
						result,
						entry{key: update.key, value: update.value},
					)
				}
				updateIndex++
			default:
				if update.kind == UpdateSet {
					result = append(
						result,
						entry{key: update.key, value: update.value},
					)
				}
				oldIndex++
				updateIndex++
			}
		}
	}
	if err := validateResultCount(result, finalCount); err != nil {
		return Snapshot{}, err
	}

	return Snapshot{
		limits:  snapshot.limits,
		entries: result,
		valid:   true,
	}, nil
}

func validateResultCount(result []entry, expected uint64) error {
	if uint64(len(result)) != expected {
		return errInvalidSnapshot
	}

	return nil
}

func (snapshot Snapshot) validate() error {
	if !snapshot.valid || snapshot.limits.validate() != nil {
		return errInvalidSnapshot
	}

	return nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return errInvalidContext
	}

	return ctx.Err()
}

func compareKey(left, right Key) int {
	return bytes.Compare(left[:], right[:])
}

func findEntry(entries []entry, key Key) (int, bool) {
	index, found := slices.BinarySearchFunc(entries, key, func(
		entry entry,
		key Key,
	) int {
		return compareKey(entry.key, key)
	})

	return index, found
}

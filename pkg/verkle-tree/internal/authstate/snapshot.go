// Package authstate binds immutable key/value snapshots to pre-v1 profile
// Verkle commitments. It remains an internal pre-v1 construction boundary.
package authstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/profile"
)

const (
	maxSupportedCount  = uint32(2_147_483_647)
	entryWorkingBytes  = uint64(64)
	updateWorkingBytes = uint64(96)
)

var (
	errInvalidContext    = errors.New("invalid authenticated-state context")
	errInvalidLimits     = errors.New("invalid authenticated-state limits")
	errInvalidSnapshot   = errors.New("invalid authenticated snapshot")
	errInvalidUpdate     = errors.New("invalid authenticated-state update")
	errDuplicateKey      = errors.New("duplicate authenticated-state key")
	errInvalidTransition = errors.New("invalid authenticated-state transition")
	errResource          = errors.New("authenticated-state resource limit exceeded")
)

// Key is one fixed-length raw key in the pre-v1 profile.
type Key = committedtree.Key

// Value is one fixed-length raw value. Its zero value remains present.
type Value = committedtree.Value

// Entry is one present key/value pair.
type Entry = committedtree.Entry

// UpdateKind distinguishes setting a value from deleting a key.
type UpdateKind uint8

const (
	// UpdateSet inserts or replaces one value, including the all-zero value.
	UpdateSet UpdateKind = iota + 1

	// UpdateDelete removes one key and is distinct from UpdateSet.
	UpdateDelete
)

// Update is one caller-owned state transition. Its array fields cannot alias
// caller buffers.
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

// Kind returns the validated update kind.
func (update Update) Kind() (UpdateKind, error) {
	if err := update.validate(); err != nil {
		return 0, err
	}

	return update.kind, nil
}

// Key returns the validated update key.
func (update Update) Key() (Key, error) {
	if err := update.validate(); err != nil {
		return Key{}, err
	}

	return update.key, nil
}

// Value returns the Set value and whether one is present.
func (update Update) Value() (Value, bool, error) {
	if err := update.validate(); err != nil {
		return Value{}, false, err
	}
	if update.kind == UpdateDelete {
		return Value{}, false, nil
	}

	return update.value, true, nil
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

// Limits bounds authenticated-state allocation work. Zero fields are invalid
// and no field denotes an unbounded resource.
type Limits struct {
	MaxEntries        uint32
	MaxBatchUpdates   uint32
	MaxTemporaryBytes uint64
}

func (limits Limits) validate() error {
	if limits.MaxEntries == 0 ||
		limits.MaxBatchUpdates == 0 ||
		limits.MaxTemporaryBytes == 0 ||
		limits.MaxEntries > maxSupportedCount ||
		limits.MaxBatchUpdates > maxSupportedCount {
		return errInvalidLimits
	}

	return nil
}

// Resource identifies one bounded authenticated-state resource.
type Resource uint8

const (
	// ResourceEntries counts retained present entries.
	ResourceEntries Resource = iota + 1

	// ResourceBatchUpdates counts updates in one atomic operation.
	ResourceBatchUpdates

	// ResourceTemporaryBytes counts deterministic transition scratch space.
	ResourceTemporaryBytes
)

// ResourceError reports a rejected resource without disclosing keys or values.
type ResourceError struct {
	Resource Resource
	Limit    uint64
	Actual   uint64
}

type treeBuilder interface {
	Build(context.Context, []committedtree.Entry) (committedtree.Tree, error)
	Update(context.Context, committedtree.Tree, []committedtree.Entry) (committedtree.Tree, error)
}

// Error implements error.
func (err *ResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes ResourceError match the package resource sentinel.
func (err *ResourceError) Unwrap() error {
	return errResource
}

// Snapshot is one immutable ordered state and its exact committed tree. Copies
// are safe for concurrent reads because all retained state is immutable.
type Snapshot struct {
	limits  Limits
	entries []Entry
	builder treeBuilder
	tree    committedtree.Tree
	valid   bool
}

// Transition binds one successful atomic operation to its exact pre-state and
// post-state roots.
type Transition struct {
	preRoot  backend.VectorCommitment
	postRoot backend.VectorCommitment
	valid    bool
}

// NewSnapshot validates and owns entries before constructing their exact
// committed root. Input order does not affect the result.
func NewSnapshot(
	ctx context.Context,
	entries []Entry,
	limits Limits,
	treeLimits committedtree.Limits,
	commitmentLimits backend.CommitmentLimits,
) (Snapshot, error) {
	if err := checkContext(ctx); err != nil {
		return Snapshot{}, err
	}
	if err := limits.validate(); err != nil {
		return Snapshot{}, err
	}
	owned, err := prepareInitialEntries(ctx, entries, limits)
	if err != nil {
		return Snapshot{}, err
	}
	builder, err := committedtree.NewBuilder(ctx, treeLimits, commitmentLimits)
	if err != nil {
		return Snapshot{}, err
	}
	committed, err := builder.Build(ctx, owned)
	if err != nil {
		return Snapshot{}, err
	}

	return Snapshot{
		limits:  limits,
		entries: owned,
		builder: builder,
		tree:    committed,
		valid:   true,
	}, nil
}

// PreflightInitialEntries rejects initial-state count and scratch budgets
// before a caller copies or otherwise scans the entry slice.
func PreflightInitialEntries(entryCount uint64, limits Limits) error {
	if err := limits.validate(); err != nil {
		return err
	}
	if err := checkResource(
		ResourceEntries,
		uint64(limits.MaxEntries),
		entryCount,
	); err != nil {
		return err
	}

	return checkResource(
		ResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		entryCount*2*entryWorkingBytes,
	)
}

// Get returns the present value for key. Absence is distinct from a present
// all-zero value.
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

	return snapshot.entries[index].Value, true, nil
}

// Root returns the exact opaque commitment bound to this snapshot.
func (snapshot Snapshot) Root() (backend.VectorCommitment, error) {
	if err := snapshot.validate(); err != nil {
		return backend.VectorCommitment{}, err
	}

	return snapshot.tree.Root()
}

// RootContainer returns the exact canonical profile-bound root. The empty tree
// is encoded explicitly rather than as an identity point.
func (snapshot Snapshot) RootContainer(ctx context.Context) (backend.Root, error) {
	root, err := snapshot.Root()
	if err != nil {
		return backend.Root{}, err
	}

	return backend.NewRoot(
		ctx,
		profile.BandersnatchIPA256V0Profile(),
		root,
	)
}

// EntryCount returns the exact number of retained present entries without
// allocating or exposing the immutable backing slice.
func (snapshot Snapshot) EntryCount() (uint32, error) {
	if err := snapshot.validate(); err != nil {
		return 0, err
	}

	return uint32(len(snapshot.entries)), nil
}

// CopyEntries returns an owned canonical entry sequence after preflighting the
// result count and copy allocation. The caller must separately budget any
// simultaneously live encoding or decoding buffers.
func (snapshot Snapshot) CopyEntries(
	ctx context.Context,
	maxEntries uint32,
	maxTemporaryBytes uint64,
) ([]Entry, error) {
	if err := snapshot.validate(); err != nil {
		return nil, err
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if maxEntries == 0 ||
		maxEntries > maxSupportedCount ||
		maxTemporaryBytes == 0 {
		return nil, errInvalidLimits
	}
	if err := checkResource(
		ResourceEntries,
		uint64(maxEntries),
		uint64(len(snapshot.entries)),
	); err != nil {
		return nil, err
	}
	if err := checkResource(
		ResourceTemporaryBytes,
		maxTemporaryBytes,
		uint64(len(snapshot.entries))*entryWorkingBytes,
	); err != nil {
		return nil, err
	}

	entries := make([]Entry, len(snapshot.entries))
	for index := range snapshot.entries {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		entries[index] = snapshot.entries[index]
	}

	return entries, nil
}

// StorageImage returns a complete immutable canonical node image for the exact
// snapshot without publishing it.
func (snapshot Snapshot) StorageImage(
	ctx context.Context,
	limits committedtree.StorageEncodingLimits,
) (committedtree.StorageImage, error) {
	if err := snapshot.validate(); err != nil {
		return committedtree.StorageImage{}, err
	}
	if err := checkContext(ctx); err != nil {
		return committedtree.StorageImage{}, err
	}

	return snapshot.tree.StorageImage(ctx, limits)
}

// Apply validates the complete batch, applies it in ascending key order, and
// constructs a new immutable snapshot. Every failure leaves the receiver
// unchanged and returns no usable transition.
func (snapshot Snapshot) Apply(
	ctx context.Context,
	updates []Update,
) (Snapshot, Transition, error) {
	if err := snapshot.validate(); err != nil {
		return Snapshot{}, Transition{}, err
	}
	if err := checkContext(ctx); err != nil {
		return Snapshot{}, Transition{}, err
	}
	preRoot, err := snapshot.tree.Root()
	if err != nil {
		return Snapshot{}, Transition{}, err
	}
	if len(updates) == 0 {
		return snapshot, Transition{preRoot: preRoot, postRoot: preRoot, valid: true}, nil
	}
	if err := snapshot.PreflightApply(uint64(len(updates))); err != nil {
		return Snapshot{}, Transition{}, err
	}
	updateBytes := uint64(len(updates)) * 2 * updateWorkingBytes

	ordered := make([]Update, len(updates))
	for index := range updates {
		if err := checkContext(ctx); err != nil {
			return Snapshot{}, Transition{}, err
		}
		ordered[index] = updates[index]
	}
	if err := sortUpdates(ctx, ordered); err != nil {
		return Snapshot{}, Transition{}, err
	}
	for index := range ordered {
		if err := checkContext(ctx); err != nil {
			return Snapshot{}, Transition{}, err
		}
		if err := ordered[index].validate(); err != nil {
			return Snapshot{}, Transition{}, err
		}
		if index > 0 && ordered[index-1].key == ordered[index].key {
			return Snapshot{}, Transition{}, errDuplicateKey
		}
	}

	finalCount := uint64(len(snapshot.entries))
	for index := range ordered {
		if err := checkContext(ctx); err != nil {
			return Snapshot{}, Transition{}, err
		}
		_, found := findEntry(snapshot.entries, ordered[index].key)
		switch {
		case ordered[index].kind == UpdateSet && !found:
			finalCount++
		case ordered[index].kind == UpdateDelete && found:
			finalCount--
		}
	}
	if err := checkResource(
		ResourceEntries,
		uint64(snapshot.limits.MaxEntries),
		finalCount,
	); err != nil {
		return Snapshot{}, Transition{}, err
	}
	temporaryBytes := updateBytes + finalCount*entryWorkingBytes
	if err := checkResource(
		ResourceTemporaryBytes,
		snapshot.limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return Snapshot{}, Transition{}, err
	}

	result := make([]Entry, 0, int(finalCount))
	oldIndex := 0
	updateIndex := 0
	for oldIndex < len(snapshot.entries) || updateIndex < len(ordered) {
		if err := checkContext(ctx); err != nil {
			return Snapshot{}, Transition{}, err
		}
		switch {
		case oldIndex == len(snapshot.entries):
			update := ordered[updateIndex]
			if update.kind == UpdateSet {
				result = append(result, Entry{Key: update.key, Value: update.value})
			}
			updateIndex++
		case updateIndex == len(ordered):
			result = append(result, snapshot.entries[oldIndex])
			oldIndex++
		default:
			old := snapshot.entries[oldIndex]
			update := ordered[updateIndex]
			comparison := compareKey(old.Key, update.key)
			switch {
			case comparison < 0:
				result = append(result, old)
				oldIndex++
			case comparison > 0:
				if update.kind == UpdateSet {
					result = append(result, Entry{Key: update.key, Value: update.value})
				}
				updateIndex++
			default:
				if update.kind == UpdateSet {
					result = append(result, Entry{Key: update.key, Value: update.value})
				}
				oldIndex++
				updateIndex++
			}
		}
	}
	if err := validateResultCount(result, finalCount); err != nil {
		return Snapshot{}, Transition{}, err
	}

	committed, err := snapshot.builder.Update(ctx, snapshot.tree, result)
	if err != nil {
		return Snapshot{}, Transition{}, err
	}
	postRoot, err := committed.Root()
	if err != nil {
		return Snapshot{}, Transition{}, err
	}
	next := Snapshot{
		limits:  snapshot.limits,
		entries: result,
		builder: snapshot.builder,
		tree:    committed,
		valid:   true,
	}

	return next, Transition{preRoot: preRoot, postRoot: postRoot, valid: true}, nil
}

// PreflightApply rejects batch-count and initial scratch budgets before a
// caller copies or otherwise scans the update slice.
func (snapshot Snapshot) PreflightApply(updateCount uint64) error {
	if err := snapshot.validate(); err != nil {
		return err
	}
	if updateCount == 0 {
		return nil
	}
	if err := checkResource(
		ResourceBatchUpdates,
		uint64(snapshot.limits.MaxBatchUpdates),
		updateCount,
	); err != nil {
		return err
	}

	return checkResource(
		ResourceTemporaryBytes,
		snapshot.limits.MaxTemporaryBytes,
		updateCount*2*updateWorkingBytes,
	)
}

func validateResultCount(result []Entry, expected uint64) error {
	if uint64(len(result)) != expected {
		return errInvalidSnapshot
	}

	return nil
}

// PreRoot returns the exact root of the snapshot to which the transition was
// applied.
func (transition Transition) PreRoot() (backend.VectorCommitment, error) {
	if !transition.valid {
		return backend.VectorCommitment{}, errInvalidTransition
	}

	return transition.preRoot, nil
}

// PreRootContainer returns the canonical profile-bound pre-state root.
func (transition Transition) PreRootContainer(
	ctx context.Context,
) (backend.Root, error) {
	root, err := transition.PreRoot()
	if err != nil {
		return backend.Root{}, err
	}

	return backend.NewRoot(
		ctx,
		profile.BandersnatchIPA256V0Profile(),
		root,
	)
}

// PostRoot returns the exact root of the newly constructed snapshot.
func (transition Transition) PostRoot() (backend.VectorCommitment, error) {
	if !transition.valid {
		return backend.VectorCommitment{}, errInvalidTransition
	}

	return transition.postRoot, nil
}

// PostRootContainer returns the canonical profile-bound post-state root.
func (transition Transition) PostRootContainer(
	ctx context.Context,
) (backend.Root, error) {
	root, err := transition.PostRoot()
	if err != nil {
		return backend.Root{}, err
	}

	return backend.NewRoot(
		ctx,
		profile.BandersnatchIPA256V0Profile(),
		root,
	)
}

func (snapshot Snapshot) validate() error {
	if !snapshot.valid ||
		snapshot.builder == nil ||
		snapshot.limits.validate() != nil {
		return errInvalidSnapshot
	}

	return nil
}

func prepareInitialEntries(
	ctx context.Context,
	entries []Entry,
	limits Limits,
) ([]Entry, error) {
	if err := PreflightInitialEntries(uint64(len(entries)), limits); err != nil {
		return nil, err
	}
	owned := make([]Entry, len(entries))
	for index := range entries {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		owned[index] = entries[index]
	}
	if err := sortEntries(ctx, owned); err != nil {
		return nil, err
	}
	for index := 1; index < len(owned); index++ {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if owned[index-1].Key == owned[index].Key {
			return nil, errDuplicateKey
		}
	}

	return owned, nil
}

func sortEntries(ctx context.Context, entries []Entry) error {
	if len(entries) < 2 {
		return checkContext(ctx)
	}
	scratch := make([]Entry, len(entries))

	return mergeSortEntries(ctx, entries, scratch, 0, len(entries))
}

func mergeSortEntries(
	ctx context.Context,
	entries []Entry,
	scratch []Entry,
	start int,
	end int,
) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if end-start < 2 {
		return nil
	}
	middle := start + (end-start)/2
	if err := mergeSortEntries(ctx, entries, scratch, start, middle); err != nil {
		return err
	}
	if err := mergeSortEntries(ctx, entries, scratch, middle, end); err != nil {
		return err
	}
	left := start
	right := middle
	for output := start; output < end; output++ {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if right == end ||
			(left < middle && compareKey(entries[left].Key, entries[right].Key) <= 0) {
			scratch[output] = entries[left]
			left++
		} else {
			scratch[output] = entries[right]
			right++
		}
	}
	for index := start; index < end; index++ {
		if err := checkContext(ctx); err != nil {
			return err
		}
		entries[index] = scratch[index]
	}

	return nil
}

func sortUpdates(ctx context.Context, updates []Update) error {
	if len(updates) < 2 {
		return checkContext(ctx)
	}
	scratch := make([]Update, len(updates))

	return mergeSortUpdates(ctx, updates, scratch, 0, len(updates))
}

func mergeSortUpdates(
	ctx context.Context,
	updates []Update,
	scratch []Update,
	start int,
	end int,
) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if end-start < 2 {
		return nil
	}
	middle := start + (end-start)/2
	if err := mergeSortUpdates(ctx, updates, scratch, start, middle); err != nil {
		return err
	}
	if err := mergeSortUpdates(ctx, updates, scratch, middle, end); err != nil {
		return err
	}
	left := start
	right := middle
	for output := start; output < end; output++ {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if right == end ||
			(left < middle && compareKey(updates[left].key, updates[right].key) <= 0) {
			scratch[output] = updates[left]
			left++
		} else {
			scratch[output] = updates[right]
			right++
		}
	}
	for index := start; index < end; index++ {
		if err := checkContext(ctx); err != nil {
			return err
		}
		updates[index] = scratch[index]
	}

	return nil
}

func findEntry(entries []Entry, key Key) (int, bool) {
	return slices.BinarySearchFunc(entries, key, func(entry Entry, key Key) int {
		return compareKey(entry.Key, key)
	})
}

func compareKey(left Key, right Key) int {
	return bytes.Compare(left[:], right[:])
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return errInvalidContext
	}

	return ctx.Err()
}

func checkResource(resource Resource, limit uint64, actual uint64) error {
	if actual <= limit {
		return nil
	}

	return &ResourceError{Resource: resource, Limit: limit, Actual: actual}
}

// IsDuplicateKeyError reports whether err identifies a duplicate state key.
func IsDuplicateKeyError(err error) bool {
	return errors.Is(err, errDuplicateKey) ||
		errors.Is(err, errDuplicateClaimKey)
}

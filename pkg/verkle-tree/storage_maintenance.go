package verkletree

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// NodeMaintenanceStore combines one profile-bound isolated audit namespace
// with an atomic retention and deletion operation. MaintenanceProfile must
// identify the exclusive profile of every publication and node returned by the
// audit namespace. ApplyMaintenance must compare the exact current and previous
// retained publications in the request with live state. On a mismatch or any
// failure it must leave publications and nodes unchanged. Deletion must not
// invalidate a read or audit snapshot that was opened before the atomic
// operation; adapters may defer physical reclamation until those snapshots
// close.
type NodeMaintenanceStore interface {
	NodeAuditStore
	// MaintenanceProfile identifies the exclusive profile of the namespace.
	MaintenanceProfile() Profile
	// ApplyMaintenance atomically compares, retains, and deletes as requested.
	ApplyMaintenance(ctx context.Context, maintenance StoreMaintenance) error
}

// StoreMaintenance is one opaque atomic compare, retention, and deletion
// request. Its zero value rejects use through every validating accessor.
type StoreMaintenance struct {
	profile          Profile
	current          StorePublication
	previousRetained []StorePublication
	retained         []StorePublication
	deleted          []NodeID
	hasCurrent       bool
	valid            bool
}

// Profile returns the immutable profile bound to the maintenance namespace and
// every publication and node in the request.
func (maintenance StoreMaintenance) Profile() (Profile, error) {
	if err := maintenance.validate(); err != nil {
		return Profile{}, err
	}

	return maintenance.profile, nil
}

// CurrentPublication returns the exact current-publication expectation. A
// false present value requires that no current publication exists.
func (maintenance StoreMaintenance) CurrentPublication() (
	publication StorePublication,
	present bool,
	err error,
) {
	if err := maintenance.validate(); err != nil {
		return StorePublication{}, false, err
	}
	if !maintenance.hasCurrent {
		return StorePublication{}, false, nil
	}

	return maintenance.current, true, nil
}

// PreviousRetainedPublications returns an owned canonical copy of the exact
// retained-publication set that the adapter must compare with live state.
func (maintenance StoreMaintenance) PreviousRetainedPublications(
	ctx context.Context,
) ([]StorePublication, error) {
	if err := maintenance.validate(); err != nil {
		return nil, err
	}

	return copyMaintenancePublications(ctx, maintenance.previousRetained)
}

// RetainedPublications returns an owned canonical copy of the retained subset
// that the adapter must install atomically.
func (maintenance StoreMaintenance) RetainedPublications(
	ctx context.Context,
) ([]StorePublication, error) {
	if err := maintenance.validate(); err != nil {
		return nil, err
	}

	return copyMaintenancePublications(ctx, maintenance.retained)
}

// DeletedNodes returns an owned ascending copy of the nodes that the adapter
// must delete atomically with the retention change.
func (maintenance StoreMaintenance) DeletedNodes(
	ctx context.Context,
) ([]NodeID, error) {
	if err := maintenance.validate(); err != nil {
		return nil, err
	}

	return copyMaintenanceNodes(ctx, maintenance.deleted)
}

func (maintenance StoreMaintenance) validate() error {
	if !maintenance.valid || maintenance.profile.Validate() != nil {
		return ErrStorageMaintenance
	}
	if maintenance.hasCurrent {
		if _, _, err := maintenance.current.values(); err != nil {
			return ErrStorageMaintenance
		}
	}

	return nil
}

// StorageMaintenanceResult is one immutable successful retention and pruning
// result. Deleted node identifiers are ordered by content address.
type StorageMaintenanceResult struct {
	previousRetained uint32
	retained         uint32
	deleted          []NodeID
	valid            bool
}

// PreviousRetainedCount returns the number of retained publications compared
// by the atomic operation.
func (result StorageMaintenanceResult) PreviousRetainedCount() uint32 {
	if !result.valid {
		return 0
	}

	return result.previousRetained
}

// RetainedCount returns the number of retained publications installed by the
// atomic operation.
func (result StorageMaintenanceResult) RetainedCount() uint32 {
	if !result.valid {
		return 0
	}

	return result.retained
}

// DeletedNodeCount returns the number of nodes deleted atomically.
func (result StorageMaintenanceResult) DeletedNodeCount() uint32 {
	if !result.valid {
		return 0
	}

	return uint32(len(result.deleted))
}

// DeletedNodes returns an owned ascending copy of every deleted node.
func (result StorageMaintenanceResult) DeletedNodes(
	ctx context.Context,
) ([]NodeID, error) {
	if !result.valid {
		return nil, ErrStorageMaintenance
	}

	return copyMaintenanceNodes(ctx, result.deleted)
}

// MaintainStorage verifies the complete current and retained publication set,
// proves a complete canonical node inventory, and then asks store to atomically
// retain the requested subset and delete only nodes unreachable from the
// current publication and that subset. retained is treated as a set and is
// canonicalized; every member must exactly match an observed retained
// publication. The isolated audit view is closed before ApplyMaintenance.
func MaintainStorage(
	ctx context.Context,
	profile Profile,
	store NodeMaintenanceStore,
	retained []StorePublication,
	limits StorageAuditLimits,
) (StorageMaintenanceResult, error) {
	return runStorageMaintenance(
		ctx, profile, store, retained, limits, false,
	)
}

// RecoverStorage verifies every current and retained publication, preserves
// that exact publication set, and atomically deletes only inventoried nodes
// unreachable from all of them. This recovers node-only writes left by an
// interrupted unpublished commit; missing or corrupt reachable state fails
// closed and requires adapter-specific restoration.
func RecoverStorage(
	ctx context.Context,
	profile Profile,
	store NodeMaintenanceStore,
	limits StorageAuditLimits,
) (StorageMaintenanceResult, error) {
	return runStorageMaintenance(ctx, profile, store, nil, limits, true)
}

func runStorageMaintenance(
	ctx context.Context,
	profile Profile,
	store NodeMaintenanceStore,
	retained []StorePublication,
	limits StorageAuditLimits,
	recoverAll bool,
) (StorageMaintenanceResult, error) {
	if err := checkPublicContext(ctx); err != nil {
		return StorageMaintenanceResult{}, err
	}
	if err := profile.Validate(); err != nil {
		return StorageMaintenanceResult{}, ErrUnsupportedProfile
	}
	if !validStorageValue(store) {
		return StorageMaintenanceResult{}, ErrInvalidStore
	}
	storeProfile := store.MaintenanceProfile()
	if storeProfile != profile {
		return StorageMaintenanceResult{}, ErrUnsupportedProfile
	}
	if err := limits.validate(); err != nil {
		return StorageMaintenanceResult{}, err
	}
	available := store.Capabilities()
	if !available.Supports(RequiredMaintenanceStoreCapabilities) {
		return StorageMaintenanceResult{}, &StoreCapabilityError{
			Required:  RequiredMaintenanceStoreCapabilities,
			Available: available,
			Missing:   RequiredMaintenanceStoreCapabilities &^ available,
		}
	}
	if err := checkPublicContext(ctx); err != nil {
		return StorageMaintenanceResult{}, err
	}
	var ownedRetained []StorePublication
	var retainedCapacity uint64
	if !recoverAll {
		var err error
		ownedRetained, retainedCapacity, err = copyRequestedRetained(
			ctx, retained, limits,
		)
		if err != nil {
			return StorageMaintenanceResult{}, err
		}
	}
	view, err := store.OpenAudit(ctx)
	if err != nil {
		return StorageMaintenanceResult{}, wrapStorageMaintenanceError("open audit", err)
	}
	if !validStorageValue(view) {
		return StorageMaintenanceResult{}, fmt.Errorf("open audit: %w", ErrStorageMaintenance)
	}

	var maintenance StoreMaintenance
	var planErr error
	if recoverAll {
		maintenance, planErr = planStorageRecovery(
			ctx, profile, view, limits,
		)
	} else {
		maintenance, planErr = planStorageMaintenance(
			ctx, profile, view, ownedRetained, retainedCapacity, limits,
		)
	}
	closeErr := view.Close(ctx)
	if closeErr != nil {
		wrapped := wrapStorageMaintenanceError("close audit", closeErr)
		if planErr != nil {
			planErr = errors.Join(planErr, wrapped)
		} else {
			planErr = wrapped
		}
	}
	if planErr != nil {
		return StorageMaintenanceResult{}, planErr
	}
	if err := checkPublicContext(ctx); err != nil {
		return StorageMaintenanceResult{}, err
	}
	if err := store.ApplyMaintenance(ctx, maintenance); err != nil {
		return StorageMaintenanceResult{}, wrapStorageMaintenanceError("apply maintenance", err)
	}

	return StorageMaintenanceResult{
		previousRetained: uint32(len(maintenance.previousRetained)),
		retained:         uint32(len(maintenance.retained)),
		deleted:          maintenance.deleted,
		valid:            true,
	}, nil
}

func planStorageMaintenance(
	ctx context.Context,
	profile Profile,
	view NodeAuditSnapshot,
	requested []StorePublication,
	requestedCapacity uint64,
	limits StorageAuditLimits,
) (StoreMaintenance, error) {
	publications, hasCurrent, err := auditPublicationSet(ctx, view, limits)
	if err != nil {
		return StoreMaintenance{}, wrapStorageMaintenanceError("read publications", err)
	}

	return planStorageMaintenanceObserved(
		ctx, profile, view, publications, hasCurrent,
		requested, requestedCapacity, limits,
	)
}

func planStorageRecovery(
	ctx context.Context,
	profile Profile,
	view NodeAuditSnapshot,
	limits StorageAuditLimits,
) (StoreMaintenance, error) {
	publications, hasCurrent, err := auditPublicationSet(ctx, view, limits)
	if err != nil {
		return StoreMaintenance{}, wrapStorageMaintenanceError(
			"read publications", err,
		)
	}
	retainedStart := 0
	if hasCurrent {
		retainedStart = 1
	}
	requested := publications[retainedStart:len(publications):len(publications)]

	return planStorageMaintenanceObserved(
		ctx, profile, view, publications, hasCurrent,
		requested, uint64(cap(requested)), limits,
	)
}

func planStorageMaintenanceObserved(
	ctx context.Context,
	profile Profile,
	view NodeAuditSnapshot,
	publications []StorePublication,
	hasCurrent bool,
	requested []StorePublication,
	requestedCapacity uint64,
	limits StorageAuditLimits,
) (StoreMaintenance, error) {
	var current StorePublication
	retainedStart := 0
	if hasCurrent {
		current = publications[0]
		retainedStart = 1
	}
	previousRetained := publications[retainedStart:]
	normalized, err := normalizeRetainedPublications(
		ctx, requested, requestedCapacity, current, hasCurrent,
		previousRetained, limits,
	)
	if err != nil {
		return StoreMaintenance{}, err
	}

	allReachable := make(map[NodeID]struct{})
	keptReachable := make(map[NodeID]struct{})
	publicationSlots := uint64(len(publications)) +
		uint64(len(normalized)) +
		requestedCapacity
	for index, publication := range publications {
		root, rootNode, _ := publication.values()
		keep := hasCurrent && index == 0 || containsStorePublication(normalized, publication)
		_, loadErr := loadStoredSnapshotObserved(
			ctx, view, profile, root, rootNode, limits.Read,
			func(id NodeID) error {
				_, allPresent := allReachable[id]
				_, keptPresent := keptReachable[id]
				allCount := len(allReachable)
				keptCount := len(keptReachable)
				if !allPresent {
					actual := uint64(allCount) + 1
					if actual > uint64(limits.MaxInventoryNodes) {
						return &ResourceError{Resource: ResourceInventoryNodes, Limit: uint64(limits.MaxInventoryNodes), Actual: actual}
					}
					allCount++
				}
				if keep && !keptPresent {
					keptCount++
				}
				if err := checkAuditTemporary(
					limits, publicationSlots, uint64(allCount)+uint64(keptCount), 0, 0,
				); err != nil {
					return err
				}
				if !allPresent {
					allReachable[id] = struct{}{}
				}
				if keep && !keptPresent {
					keptReachable[id] = struct{}{}
				}

				return nil
			},
		)
		if loadErr != nil {
			return StoreMaintenance{}, wrapStorageMaintenanceError("verify publication", loadErr)
		}
	}
	allReachableSlots := len(allReachable)
	deleted, _, err := maintenanceInventory(
		ctx, view, limits, publicationSlots, allReachable, keptReachable,
	)
	if err != nil {
		return StoreMaintenance{}, err
	}

	if err := checkAuditTemporary(
		limits,
		publicationSlots+uint64(len(previousRetained)),
		uint64(allReachableSlots)+uint64(len(keptReachable)),
		uint64(cap(deleted)),
		0,
	); err != nil {
		return StoreMaintenance{}, err
	}

	return StoreMaintenance{
		profile:          profile,
		current:          current,
		previousRetained: cloneStorePublications(previousRetained),
		retained:         normalized,
		deleted:          deleted,
		hasCurrent:       hasCurrent,
		valid:            true,
	}, nil
}

func copyRequestedRetained(
	ctx context.Context,
	requested []StorePublication,
	limits StorageAuditLimits,
) ([]StorePublication, uint64, error) {
	if err := checkPublicContext(ctx); err != nil {
		return nil, 0, err
	}
	requestedCapacity := uint64(cap(requested))
	actual := max(uint64(len(requested)), requestedCapacity)
	if actual > uint64(limits.MaxPublications) {
		return nil, 0, &ResourceError{
			Resource: ResourcePublications,
			Limit:    uint64(limits.MaxPublications),
			Actual:   actual,
		}
	}
	if err := checkAuditTemporary(
		limits, requestedCapacity+uint64(len(requested)), 0, 0, 0,
	); err != nil {
		return nil, 0, err
	}
	owned := make([]StorePublication, len(requested))
	for index := range requested {
		if err := checkPublicContext(ctx); err != nil {
			return nil, 0, err
		}
		if _, _, err := requested[index].values(); err != nil {
			return nil, 0, ErrInvalidRetention
		}
		owned[index] = requested[index]
	}

	return owned, requestedCapacity, nil
}

func normalizeRetainedPublications(
	ctx context.Context,
	requested []StorePublication,
	requestedCapacity uint64,
	current StorePublication,
	hasCurrent bool,
	observed []StorePublication,
	limits StorageAuditLimits,
) ([]StorePublication, error) {
	if uint64(len(requested)) > uint64(limits.MaxPublications) ||
		requestedCapacity > uint64(limits.MaxPublications) {
		return nil, &ResourceError{Resource: ResourcePublications, Limit: uint64(limits.MaxPublications), Actual: max(uint64(len(requested)), requestedCapacity)}
	}
	publicationCount := uint64(len(observed))
	if hasCurrent {
		publicationCount++
	}
	if err := checkAuditTemporary(
		limits,
		publicationCount+2*uint64(len(requested))+requestedCapacity,
		0,
		0,
		0,
	); err != nil {
		return nil, err
	}
	normalized := requested
	for index := range normalized {
		if err := checkPublicContext(ctx); err != nil {
			return nil, err
		}
		if _, _, err := normalized[index].values(); err != nil {
			return nil, ErrInvalidRetention
		}
		if hasCurrent && compareStorePublication(normalized[index], current) == 0 {
			return nil, ErrInvalidRetention
		}
	}
	if err := sortStorePublications(ctx, normalized); err != nil {
		return nil, err
	}
	for index := range normalized {
		if err := checkPublicContext(ctx); err != nil {
			return nil, err
		}
		if !containsStorePublication(observed, normalized[index]) {
			return nil, ErrInvalidRetention
		}
	}

	return normalized, nil
}

func sortStorePublications(
	ctx context.Context,
	publications []StorePublication,
) error {
	if err := checkPublicContext(ctx); err != nil {
		return err
	}
	if len(publications) == 0 {
		return nil
	}
	scratch := make([]StorePublication, len(publications))

	return mergeSortStorePublications(
		ctx, publications, scratch, 0, len(publications),
	)
}

func mergeSortStorePublications(
	ctx context.Context,
	publications []StorePublication,
	scratch []StorePublication,
	start int,
	end int,
) error {
	if err := checkPublicContext(ctx); err != nil {
		return err
	}
	if end-start < 2 {
		return nil
	}
	middle := start + (end-start)/2
	if err := mergeSortStorePublications(
		ctx, publications, scratch, start, middle,
	); err != nil {
		return err
	}
	if err := mergeSortStorePublications(
		ctx, publications, scratch, middle, end,
	); err != nil {
		return err
	}
	left := start
	right := middle
	for output := start; output < end; output++ {
		if err := checkPublicContext(ctx); err != nil {
			return err
		}
		if right == end {
			scratch[output] = publications[left]
			left++
		} else if left == middle {
			scratch[output] = publications[right]
			right++
		} else {
			switch compared := compareStorePublication(
				publications[left], publications[right],
			); {
			case compared < 0:
				scratch[output] = publications[left]
				left++
			case compared > 0:
				scratch[output] = publications[right]
				right++
			default:
				return ErrInvalidRetention
			}
		}
	}
	for index := start; index < end; index++ {
		if err := checkPublicContext(ctx); err != nil {
			return err
		}
		publications[index] = scratch[index]
	}

	return nil
}

func maintenanceInventory(
	ctx context.Context,
	view NodeAuditSnapshot,
	limits StorageAuditLimits,
	publicationCount uint64,
	allReachable map[NodeID]struct{},
	keptReachable map[NodeID]struct{},
) ([]NodeID, int, error) {
	allReachableSlots := len(allReachable)
	deleted := make([]NodeID, 0)
	var after *NodeID
	var pages, inventory uint64
	for {
		if err := checkPublicContext(ctx); err != nil {
			return nil, 0, err
		}
		pages++
		if pages > uint64(limits.MaxInventoryPages) {
			return nil, 0, &ResourceError{Resource: ResourceInventoryPages, Limit: uint64(limits.MaxInventoryPages), Actual: pages}
		}
		remaining := uint64(limits.MaxInventoryNodes) - inventory
		if remaining == 0 {
			return nil, 0, &ResourceError{Resource: ResourceInventoryNodes, Limit: uint64(limits.MaxInventoryNodes), Actual: inventory + 1}
		}
		declared := uint32(min(remaining, uint64(limits.MaxNodeIDsPerPage)))
		maxIDs, limitErr := storageAuditPageLimit(
			limits,
			publicationCount,
			uint64(allReachableSlots)+uint64(len(keptReachable)),
			len(deleted),
			cap(deleted),
			declared,
		)
		if limitErr != nil {
			return nil, 0, limitErr
		}
		ids, more, readErr := view.NodeIDs(ctx, after, maxIDs)
		if readErr != nil {
			return nil, 0, wrapStorageMaintenanceError("read node inventory", readErr)
		}
		if err := checkPublicContext(ctx); err != nil {
			return nil, 0, err
		}
		if len(ids) > int(maxIDs) || cap(ids) > int(maxIDs) || more && len(ids) == 0 {
			return nil, 0, fmt.Errorf("read node inventory: %w", ErrStorageInventory)
		}
		for index, id := range ids {
			if err := checkPublicContext(ctx); err != nil {
				return nil, 0, err
			}
			if index > 0 && compareNodeID(ids[index-1], id) >= 0 || after != nil && compareNodeID(*after, id) >= 0 {
				return nil, 0, fmt.Errorf("read node inventory: %w", ErrStorageInventory)
			}
			inventory++
			delete(allReachable, id)
			if _, keep := keptReachable[id]; !keep {
				actual := uint64(len(deleted)) + 1
				if actual > uint64(limits.MaxUnreachableNodes) {
					return nil, 0, &ResourceError{Resource: ResourceUnreachableNodes, Limit: uint64(limits.MaxUnreachableNodes), Actual: actual}
				}
				deleted = appendStorageAuditNode(deleted, id, int(limits.MaxUnreachableNodes))
			}
		}
		if !more {
			break
		}
		last := ids[len(ids)-1]
		after = &last
	}
	if len(allReachable) != 0 {
		return nil, 0, fmt.Errorf("read node inventory: %w", ErrStorageInventory)
	}

	return deleted, int(inventory), nil
}

func compareNodeID(left NodeID, right NodeID) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}

	return 0
}

func containsStorePublication(values []StorePublication, target StorePublication) bool {
	index := sort.Search(len(values), func(index int) bool {
		return compareStorePublication(values[index], target) >= 0
	})

	return index < len(values) && compareStorePublication(values[index], target) == 0
}

func cloneStorePublications(values []StorePublication) []StorePublication {
	result := make([]StorePublication, len(values))
	copy(result, values)

	return result
}

func copyMaintenancePublications(
	ctx context.Context,
	values []StorePublication,
) ([]StorePublication, error) {
	if err := checkPublicContext(ctx); err != nil {
		return nil, err
	}
	result := make([]StorePublication, len(values))
	for index := range values {
		if err := checkPublicContext(ctx); err != nil {
			return nil, err
		}
		result[index] = values[index]
	}

	return result, nil
}

func copyMaintenanceNodes(ctx context.Context, values []NodeID) ([]NodeID, error) {
	if err := checkPublicContext(ctx); err != nil {
		return nil, err
	}
	result := make([]NodeID, len(values))
	for index := range values {
		if err := checkPublicContext(ctx); err != nil {
			return nil, err
		}
		result[index] = values[index]
	}

	return result, nil
}

func wrapStorageMaintenanceError(operation string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrCancelled) {
		return fmt.Errorf("%s: %w: %w: %w", operation, ErrStorageMaintenance, ErrCancelled, err)
	}

	return fmt.Errorf("%s: %w: %w", operation, ErrStorageMaintenance, err)
}

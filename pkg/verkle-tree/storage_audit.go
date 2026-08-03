package verkletree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/bits"
	"sort"
)

const (
	// Publication bytes conservatively cover both one adapter-owned returned
	// slot and one package-owned normalized copy, including allocator rounding.
	storageAuditPublicationBytes = uint64(512)
	// Reachable bytes conservatively cover one content-address map entry plus
	// the first allocation's map header, bucket, and allocator rounding. Later
	// entries amortize that fixed cost below this per-entry allowance.
	storageAuditReachableBytes = uint64(384)
	// Node-ID bytes cover the 32-byte payload plus allocator size-class
	// rounding and slice backing-storage overhead.
	storageAuditNodeIDBytes = uint64(64)
)

// NodeAuditStore opens one isolated view spanning the current publication,
// retained historical publications, and the complete immutable node
// inventory. Implementations must keep that view fixed until Close.
type NodeAuditStore interface {
	// Capabilities reports the storage guarantees asserted by the adapter.
	Capabilities() StoreCapabilities
	// OpenAudit opens one fixed publication, node namespace, and inventory view.
	OpenAudit(ctx context.Context) (NodeAuditSnapshot, error)
}

// NodeAuditSnapshot is one isolated maintenance view. Retained publications
// must exclude the current publication and be strictly ordered by canonical
// root bytes and then root-node identifier. NodeIDs must return identifiers in
// strictly increasing byte order, enforce maxIDs before allocation or I/O, and
// transfer ownership of a result whose length and capacity do not exceed
// maxIDs. RetainedPublications likewise transfers ownership of its result and
// must enforce maxPublications before allocation or I/O. A true more value
// requires a non-empty page. ReadNode follows NodeReadSnapshot.ReadNode's
// ownership and pre-I/O byte-bound contract. Close follows the same cleanup
// contract as NodeReadSnapshot.Close.
type NodeAuditSnapshot interface {
	// CurrentPublication returns the current root, or false when none exists.
	CurrentPublication(ctx context.Context) (StorePublication, bool, error)
	// RetainedPublications returns owned canonical publications within max.
	RetainedPublications(
		ctx context.Context,
		maxPublications uint32,
	) ([]StorePublication, error)
	// ReadNode returns owned canonical bytes after enforcing maxBytes.
	ReadNode(ctx context.Context, id NodeID, maxBytes uint64) ([]byte, error)
	// NodeIDs returns one strictly ascending inventory page after after.
	NodeIDs(
		ctx context.Context,
		after *NodeID,
		maxIDs uint32,
	) (ids []NodeID, more bool, err error)
	// Close releases the view exactly once, including after cancellation.
	Close(ctx context.Context) error
}

// StorageAuditLimits bounds publication verification and complete node
// inventory. Read applies independently to every publication; total read work
// is bounded by MaxPublications multiplied by each Read field.
type StorageAuditLimits struct {
	// MaxPublications bounds the current and retained roots audited together.
	MaxPublications uint32
	// MaxInventoryNodes bounds all content addresses returned by inventory.
	MaxInventoryNodes uint32
	// MaxNodeIDsPerPage bounds each inventory result length and capacity.
	MaxNodeIDsPerPage uint32
	// MaxInventoryPages bounds inventory calls, including the terminal page.
	MaxInventoryPages uint32
	// MaxUnreachableNodes bounds retained unreachable-node results.
	MaxUnreachableNodes uint32
	// MaxTemporaryBytes bounds conservatively accounted audit scratch.
	MaxTemporaryBytes uint64
	// Read independently bounds verification of each publication.
	Read StorageReadLimits
}

func (limits StorageAuditLimits) validate() error {
	if limits.MaxPublications == 0 || limits.MaxPublications > maxPublicCount ||
		limits.MaxInventoryNodes == 0 || limits.MaxInventoryNodes > maxPublicCount ||
		limits.MaxNodeIDsPerPage == 0 || limits.MaxNodeIDsPerPage > limits.MaxInventoryNodes ||
		limits.MaxInventoryPages == 0 || limits.MaxInventoryPages > maxPublicCount ||
		limits.MaxUnreachableNodes > limits.MaxInventoryNodes ||
		limits.MaxTemporaryBytes == 0 || limits.Read.validate() != nil {
		return ErrInvalidLimits
	}

	return nil
}

// StorageAudit is one immutable, deterministic inventory result. Unreachable
// nodes are ordered by content address. The report is not deletion authority;
// MaintainStorage independently revalidates a fresh atomic store view. The
// audit itself never mutates storage.
type StorageAudit struct {
	publications uint32
	reachable    uint32
	inventory    uint32
	unreachable  []NodeID
	valid        bool
}

// PublicationCount returns the number of current and retained roots verified.
func (audit StorageAudit) PublicationCount() uint32 {
	if !audit.valid {
		return 0
	}

	return audit.publications
}

// ReachableNodeCount returns the number of distinct nodes reachable from all
// verified publications.
func (audit StorageAudit) ReachableNodeCount() uint32 {
	if !audit.valid {
		return 0
	}

	return audit.reachable
}

// InventoryNodeCount returns the number of canonically inventoried node
// identifiers.
func (audit StorageAudit) InventoryNodeCount() uint32 {
	if !audit.valid {
		return 0
	}

	return audit.inventory
}

// UnreachableNodeCount returns the number of inventoried nodes outside every
// verified publication.
func (audit StorageAudit) UnreachableNodeCount() uint32 {
	if !audit.valid {
		return 0
	}

	return uint32(len(audit.unreachable))
}

// UnreachableNodes returns an owned ascending copy of every node outside all
// verified publications.
func (audit StorageAudit) UnreachableNodes(ctx context.Context) ([]NodeID, error) {
	if !audit.valid {
		return nil, ErrStorageAudit
	}
	if err := checkPublicContext(ctx); err != nil {
		return nil, err
	}
	result := make([]NodeID, len(audit.unreachable))
	for index := range audit.unreachable {
		if err := checkPublicContext(ctx); err != nil {
			return nil, err
		}
		result[index] = audit.unreachable[index]
	}

	return result, nil
}

// AuditStorage verifies every current and retained snapshot before comparing
// their reachable node set with the store's complete canonical inventory. It
// reports unpublished nodes without decoding them and returns no result when a
// reachable node, publication, inventory page, or view lifecycle is invalid.
func AuditStorage(
	ctx context.Context,
	profile Profile,
	store NodeAuditStore,
	limits StorageAuditLimits,
) (result StorageAudit, resultErr error) {
	if err := checkPublicContext(ctx); err != nil {
		return StorageAudit{}, err
	}
	if err := profile.Validate(); err != nil {
		return StorageAudit{}, ErrUnsupportedProfile
	}
	if !validStorageValue(store) {
		return StorageAudit{}, ErrInvalidStore
	}
	if err := limits.validate(); err != nil {
		return StorageAudit{}, err
	}
	available := store.Capabilities()
	if !available.Supports(RequiredAuditStoreCapabilities) {
		return StorageAudit{}, &StoreCapabilityError{
			Required:  RequiredAuditStoreCapabilities,
			Available: available,
			Missing:   RequiredAuditStoreCapabilities &^ available,
		}
	}
	if err := checkPublicContext(ctx); err != nil {
		return StorageAudit{}, err
	}
	view, err := store.OpenAudit(ctx)
	if err != nil {
		return StorageAudit{}, wrapStorageAuditError("open audit", err)
	}
	if !validStorageValue(view) {
		return StorageAudit{}, fmt.Errorf("open audit: %w", ErrStorageAudit)
	}
	defer func() {
		if closeErr := view.Close(ctx); closeErr != nil {
			result = StorageAudit{}
			wrapped := wrapStorageAuditError("close audit", closeErr)
			if resultErr == nil {
				resultErr = wrapped
			} else {
				resultErr = errors.Join(resultErr, wrapped)
			}
		}
	}()

	publications, err := auditPublications(ctx, view, limits)
	if err != nil {
		return StorageAudit{}, err
	}
	reachable := make(map[NodeID]struct{})
	for _, publication := range publications {
		// auditPublications already validated every opaque publication.
		root, rootNode, _ := publication.values()
		_, loadErr := loadStoredSnapshotObserved(
			ctx, view, profile, root, rootNode, limits.Read,
			func(id NodeID) error {
				if _, present := reachable[id]; present {
					return nil
				}
				actual := uint64(len(reachable)) + 1
				if actual > uint64(limits.MaxInventoryNodes) {
					return &ResourceError{
						Resource: ResourceInventoryNodes,
						Limit:    uint64(limits.MaxInventoryNodes),
						Actual:   actual,
					}
				}
				if err := checkAuditTemporary(
					limits, uint64(len(publications)), actual, 0, 0,
				); err != nil {
					return err
				}
				reachable[id] = struct{}{}

				return nil
			},
		)
		if loadErr != nil {
			return StorageAudit{}, wrapStorageAuditError("verify publication", loadErr)
		}
	}
	reachableCount := len(reachable)
	unreachable, inventory, err := auditInventory(
		ctx, view, limits, len(publications), reachableCount, reachable,
	)
	if err != nil {
		return StorageAudit{}, err
	}

	return StorageAudit{
		publications: uint32(len(publications)),
		reachable:    uint32(reachableCount),
		inventory:    uint32(inventory),
		unreachable:  unreachable,
		valid:        true,
	}, nil
}

func auditPublications(
	ctx context.Context,
	view NodeAuditSnapshot,
	limits StorageAuditLimits,
) ([]StorePublication, error) {
	publications, _, err := auditPublicationSet(ctx, view, limits)

	return publications, err
}

func auditPublicationSet(
	ctx context.Context,
	view NodeAuditSnapshot,
	limits StorageAuditLimits,
) ([]StorePublication, bool, error) {
	if err := checkPublicContext(ctx); err != nil {
		return nil, false, err
	}
	current, present, err := view.CurrentPublication(ctx)
	if err != nil {
		return nil, false, wrapStorageAuditError("read current publication", err)
	}
	count := uint32(0)
	if present {
		if _, _, err := current.values(); err != nil {
			return nil, false, fmt.Errorf("read current publication: %w", ErrStorageInventory)
		}
		count = 1
	}
	if err := checkPublicContext(ctx); err != nil {
		return nil, false, err
	}
	remainingPublications := limits.MaxPublications - count
	retained, err := view.RetainedPublications(ctx, remainingPublications)
	if err != nil {
		return nil, false, wrapStorageAuditError("read retained publications", err)
	}
	if cap(retained) > int(remainingPublications) {
		return nil, false, &ResourceError{
			Resource: ResourcePublications,
			Limit:    uint64(limits.MaxPublications),
			Actual:   uint64(count) + uint64(cap(retained)),
		}
	}
	actual := uint64(len(retained)) + uint64(count)
	// The adapter's full returned capacity remains live while the normalized
	// copy is allocated. Charging it as additional publication slots is
	// deliberately conservative and prevents hidden capacity from bypassing the
	// temporary-memory budget.
	workingPublications := actual + uint64(cap(retained))
	if err := checkAuditTemporary(limits, workingPublications, 0, 0, 0); err != nil {
		return nil, false, err
	}
	publications := make([]StorePublication, 0, int(actual))
	if present {
		publications = append(publications, current)
	}
	for index := range retained {
		if err := checkPublicContext(ctx); err != nil {
			return nil, false, err
		}
		if _, _, err := retained[index].values(); err != nil {
			return nil, false, fmt.Errorf("read retained publications: %w", ErrStorageInventory)
		}
		if index > 0 && compareStorePublication(retained[index-1], retained[index]) >= 0 {
			return nil, false, fmt.Errorf("read retained publications: %w", ErrStorageInventory)
		}
		if present && compareStorePublication(current, retained[index]) == 0 {
			return nil, false, fmt.Errorf("read retained publications: %w", ErrStorageInventory)
		}
		publications = append(publications, retained[index])
	}
	return publications, present, nil
}

func auditInventory(
	ctx context.Context,
	view NodeAuditSnapshot,
	limits StorageAuditLimits,
	publicationCount int,
	reachableCount int,
	reachable map[NodeID]struct{},
) ([]NodeID, int, error) {
	unreachable := make([]NodeID, 0)
	var after *NodeID
	pages := uint64(0)
	inventory := uint64(0)
	for {
		if err := checkPublicContext(ctx); err != nil {
			return nil, 0, err
		}
		pages++
		if pages > uint64(limits.MaxInventoryPages) {
			return nil, 0, &ResourceError{
				Resource: ResourceInventoryPages,
				Limit:    uint64(limits.MaxInventoryPages),
				Actual:   pages,
			}
		}
		remaining := uint64(limits.MaxInventoryNodes) - inventory
		if remaining == 0 {
			return nil, 0, &ResourceError{
				Resource: ResourceInventoryNodes,
				Limit:    uint64(limits.MaxInventoryNodes),
				Actual:   inventory + 1,
			}
		}
		declaredPageLimit := uint32(min(
			remaining,
			uint64(limits.MaxNodeIDsPerPage),
		))
		maxIDs, limitErr := storageAuditPageLimit(
			limits,
			uint64(publicationCount),
			uint64(reachableCount),
			len(unreachable),
			cap(unreachable),
			declaredPageLimit,
		)
		if limitErr != nil {
			return nil, 0, limitErr
		}
		ids, more, err := view.NodeIDs(ctx, after, maxIDs)
		if err != nil {
			return nil, 0, wrapStorageAuditError("read node inventory", err)
		}
		if err := checkPublicContext(ctx); err != nil {
			return nil, 0, err
		}
		if len(ids) > int(maxIDs) || cap(ids) > int(maxIDs) ||
			(more && len(ids) == 0) {
			return nil, 0, fmt.Errorf("read node inventory: %w", ErrStorageInventory)
		}
		for index, id := range ids {
			if err := checkPublicContext(ctx); err != nil {
				return nil, 0, err
			}
			if (index > 0 && bytes.Compare(ids[index-1][:], id[:]) >= 0) ||
				(after != nil && bytes.Compare(after[:], id[:]) >= 0) {
				return nil, 0, fmt.Errorf("read node inventory: %w", ErrStorageInventory)
			}
			inventory++
			if _, present := reachable[id]; present {
				delete(reachable, id)
			} else {
				actual := uint64(len(unreachable)) + 1
				if actual > uint64(limits.MaxUnreachableNodes) {
					return nil, 0, &ResourceError{
						Resource: ResourceUnreachableNodes,
						Limit:    uint64(limits.MaxUnreachableNodes),
						Actual:   actual,
					}
				}
				unreachable = appendStorageAuditNode(
					unreachable, id, int(limits.MaxUnreachableNodes),
				)
			}
		}
		if !more {
			break
		}
		last := ids[len(ids)-1]
		after = &last
	}
	if len(reachable) != 0 {
		return nil, 0, fmt.Errorf("read node inventory: %w", ErrStorageInventory)
	}

	return unreachable, int(inventory), nil
}

func appendStorageAuditNode(nodes []NodeID, id NodeID, maximum int) []NodeID {
	if len(nodes) == cap(nodes) {
		nextCapacity := min(max(1, 2*cap(nodes)), maximum)
		grown := make([]NodeID, len(nodes), nextCapacity)
		copy(grown, nodes)
		nodes = grown
	}

	return append(nodes, id)
}

func compareStorePublication(left StorePublication, right StorePublication) int {
	leftRoot, leftNode, _ := left.values()
	rightRoot, rightNode, _ := right.values()
	leftBytes, _ := leftRoot.Bytes()
	rightBytes, _ := rightRoot.Bytes()
	if compared := bytes.Compare(leftBytes[:], rightBytes[:]); compared != 0 {
		return compared
	}

	return bytes.Compare(leftNode[:], rightNode[:])
}

func storageAuditPageLimit(
	limits StorageAuditLimits,
	publications uint64,
	reachable uint64,
	unreachableLength int,
	unreachableCapacity int,
	declared uint32,
) (uint32, error) {
	fits := func(count uint32) (bool, uint64) {
		capacity := storageAuditResultCapacity(
			unreachableLength,
			unreachableCapacity,
			int(count),
			int(limits.MaxUnreachableNodes),
		)
		resultLength := min(
			unreachableLength+int(count),
			int(limits.MaxUnreachableNodes),
		)
		previousCapacity := storageAuditPreviousCapacity(
			unreachableCapacity,
			capacity,
		)
		workingSlots := uint64(capacity) + uint64(previousCapacity) + uint64(count)
		copySlots := uint64(capacity) + uint64(resultLength)
		actual := storageAuditTemporaryBytes(
			publications, reachable, max(workingSlots, copySlots), 0,
		)

		return actual <= limits.MaxTemporaryBytes, actual
	}
	if ok, actual := fits(1); !ok {
		return 0, &ResourceError{
			Resource: ResourceTemporaryBytes,
			Limit:    limits.MaxTemporaryBytes,
			Actual:   actual,
		}
	}
	firstRejected := sort.Search(int(declared), func(index int) bool {
		ok, _ := fits(uint32(index + 1))

		return !ok
	})
	if firstRejected == int(declared) {
		return declared, nil
	}

	return uint32(firstRejected), nil
}

func storageAuditPreviousCapacity(current int, final int) int {
	if final <= current || final <= 1 {
		return 0
	}
	base := max(1, current)
	ratio := (final - 1) / base
	factor := 1 << (bits.Len(uint(ratio)) - 1)

	return base * factor
}

func storageAuditResultCapacity(
	length int,
	capacity int,
	additional int,
	maximum int,
) int {
	required := min(length+additional, maximum)
	if required == 0 {
		return capacity
	}
	base := max(1, capacity)
	quotient := (required + base - 1) / base
	factor := 1 << bits.Len(uint(quotient-1))

	return min(base*factor, maximum)
}

func checkAuditTemporary(
	limits StorageAuditLimits,
	publications uint64,
	reachable uint64,
	unreachable uint64,
	page uint64,
) error {
	actual := storageAuditTemporaryBytes(
		publications, reachable, unreachable, page,
	)
	if actual > limits.MaxTemporaryBytes {
		return &ResourceError{
			Resource: ResourceTemporaryBytes,
			Limit:    limits.MaxTemporaryBytes,
			Actual:   actual,
		}
	}

	return nil
}

func storageAuditTemporaryBytes(
	publications uint64,
	reachable uint64,
	unreachable uint64,
	page uint64,
) uint64 {
	return publications*storageAuditPublicationBytes +
		reachable*storageAuditReachableBytes +
		(unreachable+page)*storageAuditNodeIDBytes
}

func wrapStorageAuditError(operation string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w: %w: %w", operation, ErrStorageAudit, ErrCancelled, err)
	}

	return fmt.Errorf("%s: %w: %w", operation, ErrStorageAudit, err)
}

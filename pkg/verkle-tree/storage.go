package verkletree

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
)

// NodeIDSize is the exact byte length of a stored-node content address.
const NodeIDSize = sha256.Size

// NodeID is the SHA-256 content address of one complete canonical,
// profile-bound stored node.
type NodeID [NodeIDSize]byte

// Bytes returns the content address by value.
func (id NodeID) Bytes() [NodeIDSize]byte {
	return id
}

// StoreCapabilities is a bit set of independently asserted store guarantees.
// Unknown bits are ignored by this package.
type StoreCapabilities uint8

const (
	// StoreCapabilityImmutableNodes means a content address can never resolve
	// to different bytes.
	StoreCapabilityImmutableNodes StoreCapabilities = 1 << iota

	// StoreCapabilityAtomicCommit means every supplied node and root
	// publication is one atomic operation.
	StoreCapabilityAtomicCommit

	// StoreCapabilityDurablePublication means a successful commit has made all
	// nodes durable before its root becomes observable.
	StoreCapabilityDurablePublication

	// StoreCapabilityCompareAndSwap means publication checks the exact
	// previous-root expectation.
	StoreCapabilityCompareAndSwap

	// StoreCapabilitySnapshotReads means OpenSnapshot returns one fixed root
	// publication whose content-addressed nodes remain readable until Close.
	StoreCapabilitySnapshotReads

	// StoreCapabilityNodeInventory means OpenAudit returns an isolated,
	// canonically ordered view of every retained publication and stored node.
	StoreCapabilityNodeInventory

	// StoreCapabilityAtomicMaintenance means ApplyMaintenance atomically
	// compares the complete observed publication set, installs the requested
	// retained subset, and deletes exactly the supplied unreachable nodes.
	StoreCapabilityAtomicMaintenance
)

// RequiredWriteStoreCapabilities is the complete guarantee set required by
// Snapshot.Commit.
const RequiredWriteStoreCapabilities = StoreCapabilityImmutableNodes |
	StoreCapabilityAtomicCommit |
	StoreCapabilityDurablePublication |
	StoreCapabilityCompareAndSwap

// RequiredReadStoreCapabilities is the complete guarantee set required by
// LoadSnapshot.
const RequiredReadStoreCapabilities = StoreCapabilityImmutableNodes |
	StoreCapabilitySnapshotReads

// RequiredAuditStoreCapabilities is the complete guarantee set required by
// AuditStorage.
const RequiredAuditStoreCapabilities = StoreCapabilityImmutableNodes |
	StoreCapabilitySnapshotReads |
	StoreCapabilityNodeInventory

// RequiredMaintenanceStoreCapabilities is the complete guarantee set required
// by MaintainStorage.
const RequiredMaintenanceStoreCapabilities = RequiredAuditStoreCapabilities |
	StoreCapabilityAtomicMaintenance

// Supports reports whether capabilities contains every required bit.
func (capabilities StoreCapabilities) Supports(
	required StoreCapabilities,
) bool {
	return capabilities&required == required
}

// StoreCapabilityError reports the exact guarantees a store did not assert.
type StoreCapabilityError struct {
	// Required is the complete capability set required by the operation.
	Required StoreCapabilities
	// Available is the capability set asserted by the adapter.
	Available StoreCapabilities
	// Missing is Required with every Available bit removed.
	Missing StoreCapabilities
}

// Error implements error.
func (err *StoreCapabilityError) Error() string {
	return fmt.Sprintf(
		"%v: required %d, available %d, missing %d",
		ErrStoreCapability,
		err.Required,
		err.Available,
		err.Missing,
	)
}

// Unwrap makes StoreCapabilityError match ErrStoreCapability.
func (err *StoreCapabilityError) Unwrap() error {
	return ErrStoreCapability
}

// NodeStore atomically makes a complete immutable node set durable and then
// publishes its root. Implementations must enforce StoreCommit's previous-root
// expectation. Each StoredNode encoding returned to an implementation is an
// owned copy that the implementation may retain.
type NodeStore interface {
	// Capabilities reports the storage guarantees asserted by the adapter.
	Capabilities() StoreCapabilities
	// CommitSnapshot atomically persists every node and publishes the root.
	CommitSnapshot(ctx context.Context, commit StoreCommit) error
}

// NodeReader opens one isolated immutable view of a published root and its
// content-addressed nodes. ReadNode explicitly transfers ownership of its
// returned byte slice to the loader; implementations must not retain or mutate
// that slice after returning it.
type NodeReader interface {
	// Capabilities reports the storage guarantees asserted by the adapter.
	Capabilities() StoreCapabilities
	// OpenSnapshot opens one fixed published-root and node-namespace view.
	OpenSnapshot(ctx context.Context) (NodeReadSnapshot, error)
}

// NodeReadSnapshot is one fixed publication and its immutable node namespace.
// LoadSnapshot closes every successfully opened value exactly once. Methods
// must be safe for sequential use by one loader; concurrent safety is not
// required. ReadNode must enforce maxBytes before allocating or reading the
// node and transfers ownership of a stable encoding on success. Close receives
// the operation context; implementations must release local resources even
// when that context is already cancelled and use it to bound external cleanup.
type NodeReadSnapshot interface {
	// Publication returns the root and canonical root-node address for the view.
	Publication(ctx context.Context) (StorePublication, error)
	// ReadNode returns owned canonical bytes after enforcing maxBytes.
	ReadNode(ctx context.Context, id NodeID, maxBytes uint64) ([]byte, error)
	// Close releases the view exactly once, including after cancellation.
	Close(ctx context.Context) error
}

// StoredNode is one immutable content-addressed canonical node.
type StoredNode struct {
	value committedtree.StorageNode
}

// ID returns the node's exact content address.
func (node StoredNode) ID() NodeID {
	return NodeID(node.value.ID())
}

// Encoded returns a caller-owned copy of the canonical profile-bound bytes.
func (node StoredNode) Encoded() []byte {
	return node.value.Encoded()
}

// StoreCommit is one immutable atomic node-write and root-publication request.
// Its zero value rejects use through every validating accessor.
type StoreCommit struct {
	previous    Root
	root        Root
	rootNode    NodeID
	nodes       []StoredNode
	hasPrevious bool
	valid       bool
}

// StorePublication is the immutable root pair adapters persist and return from
// NodeReadSnapshot.Publication. Its zero value rejects use.
type StorePublication struct {
	root     Root
	rootNode NodeID
	valid    bool
}

// NewStorePublication reconstructs the opaque publication pair from a decoded
// canonical root and its persisted root-node content address. It validates the
// root immediately; LoadSnapshot independently verifies that rootNode names the
// complete canonical state.
func NewStorePublication(root Root, rootNode NodeID) (StorePublication, error) {
	if _, err := root.Bytes(); err != nil {
		return StorePublication{}, ErrInvalidRoot
	}

	return StorePublication{root: root, rootNode: rootNode, valid: true}, nil
}

// Root returns the published profile-bound mathematical root.
func (publication StorePublication) Root() (Root, error) {
	if err := publication.validate(); err != nil {
		return Root{}, err
	}

	return publication.root, nil
}

// RootNode returns the content address of the published canonical root node.
func (publication StorePublication) RootNode() (NodeID, error) {
	if err := publication.validate(); err != nil {
		return NodeID{}, err
	}

	return publication.rootNode, nil
}

func (publication StorePublication) validate() error {
	if !publication.valid {
		return ErrStorageRead
	}
	if _, err := publication.root.Bytes(); err != nil {
		return ErrStorageRead
	}

	return nil
}

func (publication StorePublication) values() (Root, NodeID, error) {
	if err := publication.validate(); err != nil {
		return Root{}, NodeID{}, err
	}

	return publication.root, publication.rootNode, nil
}

// PreviousRoot returns the exact compare-and-swap expectation. A false present
// value means the store must require that no root is currently published.
func (commit StoreCommit) PreviousRoot() (root Root, present bool, err error) {
	if err := commit.validate(); err != nil {
		return Root{}, false, err
	}
	if !commit.hasPrevious {
		return Root{}, false, nil
	}

	return commit.previous, true, nil
}

// Root returns the root to publish only after every node is durable.
func (commit StoreCommit) Root() (Root, error) {
	if err := commit.validate(); err != nil {
		return Root{}, err
	}

	return commit.root, nil
}

// RootNode returns the content address of Root's canonical logical root node.
func (commit StoreCommit) RootNode() (NodeID, error) {
	if err := commit.validate(); err != nil {
		return NodeID{}, err
	}

	return commit.rootNode, nil
}

// Publication returns the exact immutable root pair for later snapshot reads.
func (commit StoreCommit) Publication() (StorePublication, error) {
	if err := commit.validate(); err != nil {
		return StorePublication{}, err
	}

	// commit.validate already proves the constructor precondition.
	publication, _ := NewStorePublication(commit.root, commit.rootNode)

	return publication, nil
}

// Nodes returns owned nodes in ascending content-address order.
func (commit StoreCommit) Nodes(ctx context.Context) ([]StoredNode, error) {
	if err := commit.validate(); err != nil {
		return nil, err
	}
	if err := checkPublicContext(ctx); err != nil {
		return nil, err
	}

	nodes := make([]StoredNode, len(commit.nodes))
	for index := range commit.nodes {
		if err := checkPublicContext(ctx); err != nil {
			return nil, err
		}
		nodes[index] = commit.nodes[index]
	}

	return nodes, nil
}

func (commit StoreCommit) validate() error {
	if !commit.valid || len(commit.nodes) == 0 {
		return ErrStorageCommit
	}
	if _, err := commit.root.Bytes(); err != nil {
		return ErrStorageCommit
	}
	if commit.hasPrevious {
		if _, err := commit.previous.Bytes(); err != nil {
			return ErrStorageCommit
		}
	}
	return nil
}

// StorageLimits bounds canonical node encoding before an adapter is invoked.
// Adapter-owned I/O and durability resources remain the adapter's explicit
// responsibility.
type StorageLimits struct {
	// MaxNodes bounds canonical nodes prepared for one atomic commit.
	MaxNodes uint32
	// MaxNodeBytes bounds each individual canonical node encoding.
	MaxNodeBytes uint64
	// MaxEncodedBytes bounds all canonical node bytes in one commit.
	MaxEncodedBytes uint64
	// MaxHashes bounds content-address calculations.
	MaxHashes uint32
	// MaxTemporaryBytes bounds conservatively accounted commit scratch.
	MaxTemporaryBytes uint64
}

func (limits StorageLimits) validate() error {
	if limits.MaxNodes == 0 ||
		limits.MaxNodes > maxPublicCount ||
		limits.MaxNodeBytes == 0 ||
		limits.MaxEncodedBytes == 0 ||
		limits.MaxHashes == 0 ||
		limits.MaxHashes > maxPublicCount ||
		limits.MaxTemporaryBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

// Commit canonically encodes every immutable logical node and asks store to
// atomically make the complete set durable before publishing the snapshot
// root. previous is copied when non-nil and is the required currently
// published root; nil requires that no root is currently published. Failure
// preserves the immutable snapshot.
func (snapshot Snapshot) Commit(
	ctx context.Context,
	store NodeStore,
	previous *Root,
	limits StorageLimits,
) error {
	if !snapshot.valid {
		return ErrInvalidSnapshot
	}
	if err := checkPublicContext(ctx); err != nil {
		return err
	}
	if !validNodeStore(store) {
		return ErrInvalidStore
	}
	if err := limits.validate(); err != nil {
		return err
	}

	var expected Root
	hasExpected := previous != nil
	if hasExpected {
		expected = *previous
		if _, err := expected.Bytes(); err != nil {
			return ErrInvalidRoot
		}
	}
	root, err := snapshot.Root()
	if err != nil {
		return err
	}
	available := store.Capabilities()
	if !available.Supports(RequiredWriteStoreCapabilities) {
		return &StoreCapabilityError{
			Required:  RequiredWriteStoreCapabilities,
			Available: available,
			Missing:   RequiredWriteStoreCapabilities &^ available,
		}
	}
	image, err := snapshot.value.StorageImage(
		ctx,
		committedtree.StorageEncodingLimits{
			MaxNodes:          limits.MaxNodes,
			MaxNodeBytes:      limits.MaxNodeBytes,
			MaxEncodedBytes:   limits.MaxEncodedBytes,
			MaxHashes:         limits.MaxHashes,
			MaxTemporaryBytes: limits.MaxTemporaryBytes,
		},
	)
	if err != nil {
		return translateStorageEncodingError(err)
	}
	// StorageImage returning nil error establishes this invariant.
	rootNode, _ := image.RootID()
	internalNodes, err := image.Nodes(ctx)
	if err != nil {
		return translateStorageEncodingError(err)
	}
	nodes := make([]StoredNode, len(internalNodes))
	for index := range internalNodes {
		if err := checkPublicContext(ctx); err != nil {
			return err
		}
		nodes[index] = StoredNode{value: internalNodes[index]}
	}
	commit := StoreCommit{
		previous:    expected,
		root:        root,
		rootNode:    NodeID(rootNode),
		nodes:       nodes,
		hasPrevious: hasExpected,
		valid:       true,
	}
	if err := store.CommitSnapshot(ctx, commit); err != nil {
		return fmt.Errorf("%w: %w", ErrStorageCommit, err)
	}

	return nil
}

func translateStorageEncodingError(err error) error {
	var resourceErr *committedtree.StorageEncodingResourceError
	if errors.As(err, &resourceErr) {
		resource := ResourceTemporaryBytes
		switch resourceErr.Resource {
		case committedtree.StorageEncodingResourceNodes:
			resource = ResourceNodes
		case committedtree.StorageEncodingResourceNodeBytes:
			resource = ResourceNodeBytes
		case committedtree.StorageEncodingResourceEncodedBytes:
			resource = ResourceEncodedNodeBytes
		case committedtree.StorageEncodingResourceHashes:
			resource = ResourceNodeHashes
		case committedtree.StorageEncodingResourceTemporaryBytes:
		}

		return &ResourceError{
			Resource: resource,
			Limit:    resourceErr.Limit,
			Actual:   resourceErr.Actual,
		}
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("encode snapshot nodes: %w: %w", ErrCancelled, err)
	}
	return fmt.Errorf("encode snapshot nodes: %w", ErrCryptographic)
}

func validNodeStore(store NodeStore) bool {
	if store == nil {
		return false
	}
	value := reflect.ValueOf(store)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

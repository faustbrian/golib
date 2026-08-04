// Package committedtree constructs immutable pre-v1 profile nodes and
// their vector-commitment root. It is an internal pre-v1 construction seam.
package committedtree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/leafvector"
)

const (
	maxSupportedCount = uint32(2_147_483_647)
	entryWorkingBytes = uint64(64)
	stemWorkingBytes  = uint64(64)
	edgeWorkingBytes  = uint64(16)
	// The retained node budget deliberately exceeds the current backend point
	// representation plus node metadata on every supported architecture.
	nodeWorkingBytes = uint64(768)
	vectorBytes      = uint64(backend.VectorWidth * 32)
	maxLiveVectors   = uint64(34)
)

var (
	errInvalidContext = errors.New("invalid committed-tree context")
	errInvalidLimits  = errors.New("invalid committed-tree limits")
	errInvalidBuilder = errors.New("invalid committed-tree builder")
	errInvalidTree    = errors.New("invalid committed tree")
	errDuplicateKey   = errors.New("duplicate committed-tree key")
	errResource       = errors.New("committed-tree resource limit exceeded")
)

// Key is one fixed-length raw key in the pre-v1 profile.
type Key [32]byte

// Value is one fixed-length raw value. Its zero value is present, not absent.
type Value [32]byte

// Entry is one present key/value pair. Array fields prevent caller aliasing.
type Entry struct {
	Key   Key
	Value Value
}

// Limits bounds all allocation-amplifying tree construction work. Zero fields
// are invalid and no field denotes an unbounded resource.
type Limits struct {
	MaxEntries         uint32
	MaxStems           uint32
	MaxNodes           uint32
	MaxEdges           uint32
	MaxCommitments     uint32
	MaxFieldMappings   uint64
	MaxCommitmentTerms uint64
	MaxTemporaryBytes  uint64
}

func (limits Limits) validate() error {
	if limits.MaxEntries == 0 ||
		limits.MaxStems == 0 ||
		limits.MaxNodes == 0 ||
		limits.MaxEdges == 0 ||
		limits.MaxCommitments == 0 ||
		limits.MaxFieldMappings == 0 ||
		limits.MaxCommitmentTerms == 0 ||
		limits.MaxTemporaryBytes == 0 ||
		limits.MaxEntries > maxSupportedCount ||
		limits.MaxStems > maxSupportedCount ||
		limits.MaxNodes > maxSupportedCount ||
		limits.MaxEdges > maxSupportedCount ||
		limits.MaxCommitments > maxSupportedCount {
		return errInvalidLimits
	}

	return nil
}

// Resource identifies one bounded construction resource.
type Resource uint8

const (
	// ResourceEntries counts present key/value pairs.
	ResourceEntries Resource = iota + 1

	// ResourceStems counts distinct 31-byte stems.
	ResourceStems

	// ResourceNodes counts logical internal and stem nodes.
	ResourceNodes

	// ResourceEdges counts retained internal-node child edges.
	ResourceEdges

	// ResourceCommitments counts all vector commitments.
	ResourceCommitments

	// ResourceFieldMappings counts commitment-to-field operations.
	ResourceFieldMappings

	// ResourceCommitmentTerms counts a conservative bound on all non-zero
	// scalar-multiplication terms across construction.
	ResourceCommitmentTerms

	// ResourceTemporaryBytes counts conservative deterministic scratch space.
	ResourceTemporaryBytes
)

// ResourceError reports a rejected resource declaration without disclosing
// keys or values.
type ResourceError struct {
	Resource Resource
	Limit    uint64
	Actual   uint64
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

type nodeKind uint8

const (
	nodeInternal nodeKind = iota + 1
	nodeStem
)

type node struct {
	kind       nodeKind
	depth      uint8
	stem       [31]byte
	entryStart uint32
	entryCount uint32
	firstEdge  uint32
	edgeCount  uint16
	commitment backend.VectorCommitment
	c1         backend.VectorCommitment
	c2         backend.VectorCommitment
}

type edge struct {
	index byte
	child uint32
}

// Tree is an immutable committed-node arena. Copies are safe for concurrent
// reads because construction owns the arena and never mutates it after return.
type Tree struct {
	entries []Entry
	nodes   []node
	edges   []edge
	root    uint32
	valid   bool
}

// Root returns the opaque root commitment. The empty tree returns the internal
// identity; no serialized empty-root representation is defined yet.
func (tree Tree) Root() (backend.VectorCommitment, error) {
	if !tree.valid || len(tree.nodes) == 0 || uint64(tree.root) >= uint64(len(tree.nodes)) {
		return backend.VectorCommitment{}, errInvalidTree
	}

	return tree.nodes[tree.root].commitment, nil
}

// NodeCount returns the number of retained logical nodes, including the root.
func (tree Tree) NodeCount() (uint32, error) {
	if !tree.valid || len(tree.nodes) == 0 || uint64(tree.root) >= uint64(len(tree.nodes)) {
		return 0, errInvalidTree
	}

	return uint32(len(tree.nodes)), nil
}

// EdgeCount returns the number of retained internal-node child edges.
func (tree Tree) EdgeCount() (uint32, error) {
	if !tree.valid || len(tree.nodes) == 0 || uint64(tree.root) >= uint64(len(tree.nodes)) {
		return 0, errInvalidTree
	}

	return uint32(len(tree.edges)), nil
}

type stemGroup struct {
	stem       [31]byte
	entryStart int
	entryEnd   int
}

type topologyCounts struct {
	stems         uint64
	internalNodes uint64
	edges         uint64
}

type commitmentEngine interface {
	Commit(context.Context, backend.Vector) (backend.VectorCommitment, error)
	UpdateCommitment(
		context.Context,
		backend.VectorCommitment,
		[]backend.VectorUpdate,
	) (backend.VectorCommitment, error)
	UpdateCapacity() uint16
}

// Builder owns one immutable commitment engine and fixed construction limits.
// It is safe for concurrent builds.
type Builder struct {
	limits Limits
	engine commitmentEngine
	valid  bool
}

// NewBuilder validates fixed construction limits and explicitly initializes
// the profile-bound commitment engine.
func NewBuilder(
	ctx context.Context,
	limits Limits,
	commitmentLimits backend.CommitmentLimits,
) (*Builder, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	engine, err := backend.NewCommitmentEngine(ctx, commitmentLimits)
	if err != nil {
		return nil, err
	}

	return &Builder{limits: limits, engine: engine, valid: true}, nil
}

// Build constructs an immutable tree while reusing the builder's fixed
// profile-bound commitment engine.
func (builder *Builder) Build(ctx context.Context, entries []Entry) (Tree, error) {
	if builder == nil || !builder.valid || builder.engine == nil || builder.limits.validate() != nil {
		return Tree{}, errInvalidBuilder
	}
	plan, err := prepareBuild(ctx, entries, builder.limits)
	if err != nil {
		return Tree{}, err
	}

	return buildPrepared(ctx, plan, builder.engine)
}

// Update constructs a new immutable tree while retaining unchanged stem
// commitments and sparsely updating existing ancestors. New topology nodes are
// committed in canonical order.
func (builder *Builder) Update(
	ctx context.Context,
	previous Tree,
	entries []Entry,
) (Tree, error) {
	if builder == nil || !builder.valid || builder.engine == nil || builder.limits.validate() != nil {
		return Tree{}, errInvalidBuilder
	}
	if err := previous.validateStorageTree(); err != nil {
		return Tree{}, err
	}
	if err := previous.validateStorageTopology(ctx); err != nil {
		return Tree{}, err
	}
	plan, err := prepareBuild(ctx, entries, builder.limits)
	if err != nil {
		return Tree{}, err
	}

	updated, compatible, err := updatePrepared(ctx, previous, plan, builder.engine)
	if err != nil {
		return Tree{}, err
	}
	if compatible {
		return updated, nil
	}

	return rebuildPrepared(ctx, previous, plan, builder.engine)
}

type buildPlan struct {
	entries   []Entry
	groups    []stemGroup
	nodeCount uint64
	edgeCount uint64
}

// Build copies and canonically orders entries before constructing every stem
// and internal commitment. Duplicate keys fail atomically.
func Build(
	ctx context.Context,
	entries []Entry,
	limits Limits,
	commitmentLimits backend.CommitmentLimits,
) (Tree, error) {
	plan, err := prepareBuild(ctx, entries, limits)
	if err != nil {
		return Tree{}, err
	}
	engine, err := backend.NewCommitmentEngine(ctx, commitmentLimits)
	if err != nil {
		return Tree{}, err
	}

	return buildPrepared(ctx, plan, engine)
}

func prepareBuild(
	ctx context.Context,
	entries []Entry,
	limits Limits,
) (buildPlan, error) {
	if err := checkContext(ctx); err != nil {
		return buildPlan{}, err
	}
	if err := limits.validate(); err != nil {
		return buildPlan{}, err
	}
	if err := checkResource(ResourceEntries, uint64(limits.MaxEntries), uint64(len(entries))); err != nil {
		return buildPlan{}, err
	}
	entryBytes := uint64(len(entries)) * 2 * entryWorkingBytes
	if err := checkResource(
		ResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		entryBytes,
	); err != nil {
		return buildPlan{}, err
	}

	owned := make([]Entry, len(entries))
	for index := range entries {
		if err := checkContext(ctx); err != nil {
			return buildPlan{}, err
		}
		owned[index] = entries[index]
	}
	if err := sortEntries(ctx, owned); err != nil {
		return buildPlan{}, err
	}
	for index := range owned {
		if err := checkContext(ctx); err != nil {
			return buildPlan{}, err
		}
		if index > 0 && owned[index-1].Key == owned[index].Key {
			return buildPlan{}, errDuplicateKey
		}
	}

	stemCount, err := countStems(ctx, owned)
	if err != nil {
		return buildPlan{}, err
	}
	if err := checkResource(ResourceStems, uint64(limits.MaxStems), stemCount); err != nil {
		return buildPlan{}, err
	}
	preGroupBytes := entryBytes + stemCount*stemWorkingBytes
	if err := checkResource(ResourceTemporaryBytes, limits.MaxTemporaryBytes, preGroupBytes); err != nil {
		return buildPlan{}, err
	}
	groups, err := groupEntries(ctx, owned, int(stemCount))
	if err != nil {
		return buildPlan{}, err
	}

	counts := topologyCounts{stems: stemCount, internalNodes: 1}
	if err := countInternalNodes(ctx, groups, 0, &counts); err != nil {
		return buildPlan{}, err
	}
	nodeCount := counts.stems + counts.internalNodes
	if err := checkResource(ResourceNodes, uint64(limits.MaxNodes), nodeCount); err != nil {
		return buildPlan{}, err
	}
	if err := checkResource(ResourceEdges, uint64(limits.MaxEdges), counts.edges); err != nil {
		return buildPlan{}, err
	}
	commitmentCount := 3*counts.stems + counts.internalNodes
	if err := checkResource(
		ResourceCommitments,
		uint64(limits.MaxCommitments),
		commitmentCount,
	); err != nil {
		return buildPlan{}, err
	}
	fieldMappings := 2*counts.stems + counts.edges
	if err := checkResource(
		ResourceFieldMappings,
		limits.MaxFieldMappings,
		fieldMappings,
	); err != nil {
		return buildPlan{}, err
	}
	commitmentTerms := 2*uint64(len(owned)) + 4*counts.stems + counts.edges
	if err := checkResource(
		ResourceCommitmentTerms,
		limits.MaxCommitmentTerms,
		commitmentTerms,
	); err != nil {
		return buildPlan{}, err
	}
	temporaryBytes := preGroupBytes +
		nodeCount*nodeWorkingBytes +
		counts.edges*edgeWorkingBytes +
		maxLiveVectors*vectorBytes
	if err := checkResource(
		ResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return buildPlan{}, err
	}

	return buildPlan{
		entries:   owned,
		groups:    groups,
		nodeCount: nodeCount,
		edgeCount: counts.edges,
	}, nil
}

func buildPrepared(
	ctx context.Context,
	plan buildPlan,
	engine commitmentEngine,
) (Tree, error) {
	builder := treeBuilder{
		ctx:     ctx,
		entries: plan.entries,
		groups:  plan.groups,
		engine:  engine,
		nodes:   make([]node, 0, int(plan.nodeCount)),
		edges:   make([]edge, 0, int(plan.edgeCount)),
	}
	_, err := builder.commitInternal(0, len(plan.groups), 0)
	if err != nil {
		return Tree{}, err
	}
	root := uint32(len(builder.nodes) - 1)

	tree, err := finalizeTree(
		builder.nodes,
		builder.edges,
		root,
		plan.nodeCount,
		plan.edgeCount,
	)
	if err != nil {
		return Tree{}, err
	}
	tree.entries = plan.entries

	return tree, nil
}

func rebuildPrepared(
	ctx context.Context,
	previous Tree,
	plan buildPlan,
	engine commitmentEngine,
) (Tree, error) {
	rebuilder := topologyRebuilder{
		ctx:      ctx,
		previous: previous,
		entries:  plan.entries,
		groups:   plan.groups,
		engine:   engine,
		nodes:    make([]node, 0, int(plan.nodeCount)),
		edges:    make([]edge, 0, int(plan.edgeCount)),
	}
	var prefix [31]byte
	_, err := rebuilder.commitInternal(0, len(plan.groups), 0, prefix)
	if err != nil {
		return Tree{}, err
	}
	root := uint32(len(rebuilder.nodes) - 1)
	tree, err := finalizeTree(
		rebuilder.nodes,
		rebuilder.edges,
		root,
		plan.nodeCount,
		plan.edgeCount,
	)
	if err != nil {
		return Tree{}, err
	}
	tree.entries = plan.entries

	return tree, nil
}

type topologyRebuilder struct {
	ctx      context.Context
	previous Tree
	entries  []Entry
	groups   []stemGroup
	engine   commitmentEngine
	nodes    []node
	edges    []edge
}

func (rebuilder *topologyRebuilder) commitInternal(
	start int,
	end int,
	depth uint8,
	prefix [31]byte,
) (backend.VectorCommitment, error) {
	if err := checkContext(rebuilder.ctx); err != nil {
		return backend.VectorCommitment{}, err
	}
	oldIndex, oldFound, err := rebuilder.previous.findInternalNode(
		rebuilder.ctx, prefix, depth,
	)
	if err != nil {
		return backend.VectorCommitment{}, err
	}
	groupCount := 0
	for groupStart := start; groupStart < end; {
		if err := checkContext(rebuilder.ctx); err != nil {
			return backend.VectorCommitment{}, err
		}
		groupCount++
		groupStart = stemGroupEnd(rebuilder.groups, groupStart, depth)
	}
	firstEdge := len(rebuilder.edges)
	rebuilder.edges = append(rebuilder.edges, make([]edge, groupCount)...)
	edgeIndex := 0
	var newVector backend.Vector
	for groupStart := start; groupStart < end; {
		if err := checkContext(rebuilder.ctx); err != nil {
			return backend.VectorCommitment{}, err
		}
		groupEnd := groupStart + 1
		index := rebuilder.groups[groupStart].stem[depth]
		for groupEnd < end && rebuilder.groups[groupEnd].stem[depth] == index {
			groupEnd++
		}
		childPrefix := prefix
		childPrefix[depth] = index

		var child backend.VectorCommitment
		if groupEnd-groupStart == 1 {
			child, err = rebuilder.commitStem(rebuilder.groups[groupStart], depth+1)
		} else {
			child, err = rebuilder.commitInternal(
				groupStart, groupEnd, depth+1, childPrefix,
			)
		}
		if err != nil {
			return backend.VectorCommitment{}, err
		}
		childIndex := uint32(len(rebuilder.nodes) - 1)
		rebuilder.edges[firstEdge+edgeIndex] = edge{index: index, child: childIndex}
		mapped, err := child.ScalarBytes()
		if err != nil {
			return backend.VectorCommitment{}, err
		}
		newVector[index] = mapped
		edgeIndex++
		groupStart = groupEnd
	}

	var committed backend.VectorCommitment
	if oldFound {
		oldVector, vectorErr := rebuilder.previous.internalNodeVector(
			rebuilder.ctx, oldIndex,
		)
		if vectorErr != nil {
			return backend.VectorCommitment{}, vectorErr
		}
		committed, err = updateVectorCommitment(
			rebuilder.ctx,
			rebuilder.engine,
			rebuilder.previous.nodes[oldIndex].commitment,
			oldVector,
			newVector,
		)
	} else {
		committed, err = rebuilder.engine.Commit(rebuilder.ctx, newVector)
	}
	if err != nil {
		return backend.VectorCommitment{}, err
	}
	rebuilder.nodes = append(rebuilder.nodes, node{
		kind:       nodeInternal,
		depth:      depth,
		firstEdge:  uint32(firstEdge),
		edgeCount:  uint16(groupCount),
		commitment: committed,
	})

	return committed, nil
}

func (rebuilder *topologyRebuilder) commitStem(
	group stemGroup,
	depth uint8,
) (backend.VectorCommitment, error) {
	if err := checkContext(rebuilder.ctx); err != nil {
		return backend.VectorCommitment{}, err
	}
	oldIndex, oldFound, err := rebuilder.previous.findStemNode(
		rebuilder.ctx, group.stem,
	)
	if err != nil {
		return backend.VectorCommitment{}, err
	}
	newEntries := rebuilder.entries[group.entryStart:group.entryEnd]
	var committed node
	if oldFound {
		old := rebuilder.previous.nodes[oldIndex]
		oldStart := int(old.entryStart)
		oldEnd := oldStart + int(old.entryCount)
		oldEntries := rebuilder.previous.entries[oldStart:oldEnd]
		committed = old
		if !entriesEqual(oldEntries, newEntries) {
			committed, err = updateStemEntries(
				rebuilder.ctx, old, oldEntries, newEntries, rebuilder.engine,
			)
		}
	} else {
		committed, err = commitStemEntries(
			rebuilder.ctx,
			newEntries,
			group.stem,
			depth,
			rebuilder.engine,
		)
	}
	if err != nil {
		return backend.VectorCommitment{}, err
	}
	committed.depth = depth
	committed.entryStart = uint32(group.entryStart)
	committed.entryCount = uint32(group.entryEnd - group.entryStart)
	rebuilder.nodes = append(rebuilder.nodes, committed)

	return committed.commitment, nil
}

func (tree Tree) findInternalNode(
	ctx context.Context,
	prefix [31]byte,
	depth uint8,
) (uint32, bool, error) {
	current := tree.root
	for level := uint8(0); level < depth; level++ {
		if err := checkContext(ctx); err != nil {
			return 0, false, err
		}
		node := tree.nodes[current]
		if node.kind != nodeInternal || node.depth != level {
			return 0, false, nil
		}
		first := int(node.firstEdge)
		end := first + int(node.edgeCount)
		edgeIndex, found := findProofPathChild(tree.edges[first:end], prefix[level])
		if !found {
			return 0, false, nil
		}
		current = tree.edges[first+edgeIndex].child
	}
	currentNode := tree.nodes[current]
	if currentNode.kind != nodeInternal || currentNode.depth != depth {
		return 0, false, nil
	}

	return current, true, nil
}

func (tree Tree) findStemNode(
	ctx context.Context,
	stem [31]byte,
) (uint32, bool, error) {
	current := tree.root
	for {
		if err := checkContext(ctx); err != nil {
			return 0, false, err
		}
		node := tree.nodes[current]
		if node.kind != nodeInternal || node.depth > 30 {
			return 0, false, nil
		}
		first := int(node.firstEdge)
		end := first + int(node.edgeCount)
		edgeIndex, found := findProofPathChild(tree.edges[first:end], stem[node.depth])
		if !found {
			return 0, false, nil
		}
		current = tree.edges[first+edgeIndex].child
		child := tree.nodes[current]
		switch child.kind {
		case nodeInternal:
			continue
		case nodeStem:
			return current, child.stem == stem, nil
		default:
			return 0, false, nil
		}
	}
}

func (tree Tree) internalNodeVector(
	ctx context.Context,
	index uint32,
) (backend.Vector, error) {
	current := tree.nodes[index]
	var vector backend.Vector
	first := int(current.firstEdge)
	end := first + int(current.edgeCount)
	for edgeIndex := first; edgeIndex < end; edgeIndex++ {
		if err := checkContext(ctx); err != nil {
			return backend.Vector{}, err
		}
		child := tree.edges[edgeIndex]
		mapped, err := tree.nodes[child.child].commitment.ScalarBytes()
		if err != nil {
			return backend.Vector{}, err
		}
		vector[child.index] = mapped
	}

	return vector, nil
}

func updatePrepared(
	ctx context.Context,
	previous Tree,
	plan buildPlan,
	engine commitmentEngine,
) (Tree, bool, error) {
	if entriesEqual(previous.entries, plan.entries) {
		return previous, true, nil
	}

	stemNodes := make([]int, 0, len(plan.groups))
	for index := range previous.nodes {
		if err := checkContext(ctx); err != nil {
			return Tree{}, false, err
		}
		if previous.nodes[index].kind == nodeStem {
			stemNodes = append(stemNodes, index)
		}
	}
	if len(stemNodes) != len(plan.groups) {
		return Tree{}, false, nil
	}
	for index := range stemNodes {
		if previous.nodes[stemNodes[index]].stem != plan.groups[index].stem {
			return Tree{}, false, nil
		}
	}

	nodes := slices.Clone(previous.nodes)
	changed := make([]bool, len(nodes))
	for groupIndex, nodeIndex := range stemNodes {
		if err := checkContext(ctx); err != nil {
			return Tree{}, false, err
		}
		group := plan.groups[groupIndex]
		current := nodes[nodeIndex]
		oldStart := uint64(current.entryStart)
		oldEnd := oldStart + uint64(current.entryCount)
		current.entryStart = uint32(group.entryStart)
		current.entryCount = uint32(group.entryEnd - group.entryStart)
		if !entriesEqual(
			previous.entries[int(oldStart):int(oldEnd)],
			plan.entries[group.entryStart:group.entryEnd],
		) {
			committed, err := updateStemEntries(
				ctx,
				current,
				previous.entries[int(oldStart):int(oldEnd)],
				plan.entries[group.entryStart:group.entryEnd],
				engine,
			)
			if err != nil {
				return Tree{}, false, err
			}
			current = committed
			changed[nodeIndex] = true
		}
		nodes[nodeIndex] = current
	}

	for nodeIndex := range nodes {
		if err := checkContext(ctx); err != nil {
			return Tree{}, false, err
		}
		current := nodes[nodeIndex]
		if current.kind != nodeInternal {
			continue
		}
		first := uint64(current.firstEdge)
		end := first + uint64(current.edgeCount)
		affected := false
		for edgeIndex := first; edgeIndex < end; edgeIndex++ {
			child := previous.edges[edgeIndex].child
			affected = affected || changed[child]
		}
		if !affected {
			continue
		}
		var oldVector backend.Vector
		var newVector backend.Vector
		for edgeIndex := first; edgeIndex < end; edgeIndex++ {
			child := previous.edges[edgeIndex].child
			oldMapped, err := previous.nodes[child].commitment.ScalarBytes()
			if err != nil {
				return Tree{}, false, err
			}
			newMapped, err := nodes[child].commitment.ScalarBytes()
			if err != nil {
				return Tree{}, false, err
			}
			position := previous.edges[edgeIndex].index
			oldVector[position] = oldMapped
			newVector[position] = newMapped
		}
		committed, err := updateVectorCommitment(
			ctx,
			engine,
			current.commitment,
			oldVector,
			newVector,
		)
		if err != nil {
			return Tree{}, false, err
		}
		current.commitment = committed
		nodes[nodeIndex] = current
		changed[nodeIndex] = true
	}
	return Tree{
		entries: plan.entries,
		nodes:   nodes,
		edges:   previous.edges,
		root:    previous.root,
		valid:   true,
	}, true, nil
}

func entriesEqual(left []Entry, right []Entry) bool {
	return slices.Equal(left, right)
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
			(left < middle && bytes.Compare(entries[left].Key[:], entries[right].Key[:]) <= 0) {
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

func countStems(ctx context.Context, entries []Entry) (uint64, error) {
	count := uint64(0)
	var previous [31]byte
	for index := range entries {
		if err := checkContext(ctx); err != nil {
			return 0, err
		}
		var stem [31]byte
		copy(stem[:], entries[index].Key[:31])
		if index == 0 || stem != previous {
			count++
			previous = stem
		}
	}

	return count, nil
}

func groupEntries(
	ctx context.Context,
	entries []Entry,
	capacity int,
) ([]stemGroup, error) {
	groups := make([]stemGroup, 0, capacity)
	for start := 0; start < len(entries); {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		var stem [31]byte
		copy(stem[:], entries[start].Key[:31])
		end := start + 1
		for end < len(entries) && bytes.Equal(entries[end].Key[:31], stem[:]) {
			if err := checkContext(ctx); err != nil {
				return nil, err
			}
			end++
		}
		groups = append(groups, stemGroup{stem: stem, entryStart: start, entryEnd: end})
		start = end
	}

	return groups, nil
}

func finalizeTree(
	nodes []node,
	edges []edge,
	root uint32,
	expectedNodes uint64,
	expectedEdges uint64,
) (Tree, error) {
	if uint64(len(nodes)) != expectedNodes ||
		uint64(len(edges)) != expectedEdges ||
		uint64(root) >= uint64(len(nodes)) {
		return Tree{}, errInvalidTree
	}

	return Tree{nodes: nodes, edges: edges, root: root, valid: true}, nil
}

func countInternalNodes(
	ctx context.Context,
	groups []stemGroup,
	depth uint8,
	counts *topologyCounts,
) error {
	for start := 0; start < len(groups); {
		if err := checkContext(ctx); err != nil {
			return err
		}
		end := stemGroupEnd(groups, start, depth)
		counts.edges++
		if end-start > 1 {
			if depth >= 30 {
				return errDuplicateKey
			}
			counts.internalNodes++
			if err := countInternalNodes(ctx, groups[start:end], depth+1, counts); err != nil {
				return err
			}
		}
		start = end
	}

	return nil
}

func stemGroupEnd(groups []stemGroup, start int, depth uint8) int {
	index := groups[start].stem[depth]
	end := start + 1
	for end < len(groups) && groups[end].stem[depth] == index {
		end++
	}

	return end
}

type treeBuilder struct {
	ctx     context.Context
	entries []Entry
	groups  []stemGroup
	engine  commitmentEngine
	nodes   []node
	edges   []edge
}

func (builder *treeBuilder) commitInternal(
	start int,
	end int,
	depth uint8,
) (backend.VectorCommitment, error) {
	var vector backend.Vector
	groupCount := 0
	for groupStart := start; groupStart < end; {
		if err := checkContext(builder.ctx); err != nil {
			return backend.VectorCommitment{}, err
		}
		groupCount++
		groupStart = stemGroupEnd(builder.groups, groupStart, depth)
	}
	firstEdge := len(builder.edges)
	builder.edges = append(builder.edges, make([]edge, groupCount)...)
	edgeIndex := 0
	for groupStart := start; groupStart < end; {
		if err := checkContext(builder.ctx); err != nil {
			return backend.VectorCommitment{}, err
		}
		groupEnd := groupStart + 1
		index := builder.groups[groupStart].stem[depth]
		for groupEnd < end && builder.groups[groupEnd].stem[depth] == index {
			groupEnd++
		}

		var child backend.VectorCommitment
		var err error
		if groupEnd-groupStart == 1 {
			child, err = builder.commitStem(builder.groups[groupStart], depth+1)
		} else {
			child, err = builder.commitInternal(groupStart, groupEnd, depth+1)
		}
		if err != nil {
			return backend.VectorCommitment{}, err
		}
		childIndex := uint32(len(builder.nodes) - 1)
		builder.edges[firstEdge+edgeIndex] = edge{index: index, child: childIndex}
		mapped, err := child.ScalarBytes()
		if err != nil {
			return backend.VectorCommitment{}, err
		}
		vector[index] = mapped
		edgeIndex++
		groupStart = groupEnd
	}

	committed, err := builder.engine.Commit(builder.ctx, vector)
	if err != nil {
		return backend.VectorCommitment{}, err
	}
	builder.nodes = append(builder.nodes, node{
		kind:       nodeInternal,
		depth:      depth,
		firstEdge:  uint32(firstEdge),
		edgeCount:  uint16(groupCount),
		commitment: committed,
	})

	return committed, nil
}

func (builder *treeBuilder) commitStem(
	group stemGroup,
	depth uint8,
) (backend.VectorCommitment, error) {
	committed, err := commitStemEntries(
		builder.ctx,
		builder.entries[group.entryStart:group.entryEnd],
		group.stem,
		depth,
		builder.engine,
	)
	if err != nil {
		return backend.VectorCommitment{}, err
	}
	committed.entryStart = uint32(group.entryStart)
	committed.entryCount = uint32(group.entryEnd - group.entryStart)
	builder.nodes = append(builder.nodes, committed)

	return committed.commitment, nil
}

func commitStemEntries(
	ctx context.Context,
	entries []Entry,
	stem [31]byte,
	depth uint8,
	engine commitmentEngine,
) (node, error) {
	c1, c2, err := encodeStemLeafVectors(ctx, entries)
	if err != nil {
		return node{}, err
	}
	c1Commitment, err := engine.Commit(ctx, c1)
	if err != nil {
		return node{}, err
	}
	c2Commitment, err := engine.Commit(ctx, c2)
	if err != nil {
		return node{}, err
	}
	vector, err := encodeStemCommitmentVector(stem, c1Commitment, c2Commitment)
	if err != nil {
		return node{}, err
	}
	committed, err := engine.Commit(ctx, vector)
	if err != nil {
		return node{}, err
	}

	return node{
		kind:       nodeStem,
		depth:      depth,
		stem:       stem,
		commitment: committed,
		c1:         c1Commitment,
		c2:         c2Commitment,
	}, nil
}

func updateStemEntries(
	ctx context.Context,
	current node,
	oldEntries []Entry,
	newEntries []Entry,
	engine commitmentEngine,
) (node, error) {
	oldC1, oldC2, err := encodeStemLeafVectors(ctx, oldEntries)
	if err != nil {
		return node{}, err
	}
	newC1, newC2, err := encodeStemLeafVectors(ctx, newEntries)
	if err != nil {
		return node{}, err
	}
	updatedC1, err := updateVectorCommitment(
		ctx, engine, current.c1, oldC1, newC1,
	)
	if err != nil {
		return node{}, err
	}
	updatedC2, err := updateVectorCommitment(
		ctx, engine, current.c2, oldC2, newC2,
	)
	if err != nil {
		return node{}, err
	}
	oldExtension, err := encodeStemCommitmentVector(current.stem, current.c1, current.c2)
	if err != nil {
		return node{}, err
	}
	newExtension, err := encodeStemCommitmentVector(current.stem, updatedC1, updatedC2)
	if err != nil {
		return node{}, err
	}
	updatedCommitment, err := updateVectorCommitment(
		ctx,
		engine,
		current.commitment,
		oldExtension,
		newExtension,
	)
	if err != nil {
		return node{}, err
	}
	current.c1 = updatedC1
	current.c2 = updatedC2
	current.commitment = updatedCommitment

	return current, nil
}

func encodeStemLeafVectors(
	ctx context.Context,
	entries []Entry,
) (backend.Vector, backend.Vector, error) {
	var c1 backend.Vector
	var c2 backend.Vector
	for index := range entries {
		if err := checkContext(ctx); err != nil {
			return backend.Vector{}, backend.Vector{}, err
		}
		entry := entries[index]
		opening := leafvector.EncodePresent(entry.Key[31], [32]byte(entry.Value))
		target := &c1
		if opening.Half == leafvector.C2 {
			target = &c2
		}
		target[opening.LowIndex] = [32]byte(opening.Low)
		target[opening.HighIndex] = [32]byte(opening.High)
	}

	return c1, c2, nil
}

func encodeStemCommitmentVector(
	stem [31]byte,
	c1 backend.VectorCommitment,
	c2 backend.VectorCommitment,
) (backend.Vector, error) {
	c1Scalar, err := c1.ScalarBytes()
	if err != nil {
		return backend.Vector{}, err
	}
	c2Scalar, err := c2.ScalarBytes()
	if err != nil {
		return backend.Vector{}, err
	}

	var vector backend.Vector
	vector[leafvector.ExtensionMarkerIndex] = [32]byte(leafvector.EncodeExtensionMarker())
	vector[leafvector.StemIndex] = [32]byte(leafvector.EncodeStem(stem))
	vector[leafvector.C1HashIndex] = c1Scalar
	vector[leafvector.C2HashIndex] = c2Scalar

	return vector, nil
}

func updateVectorCommitment(
	ctx context.Context,
	engine commitmentEngine,
	current backend.VectorCommitment,
	oldVector backend.Vector,
	newVector backend.Vector,
) (backend.VectorCommitment, error) {
	count := 0
	for index := range oldVector {
		if err := checkContext(ctx); err != nil {
			return backend.VectorCommitment{}, err
		}
		if oldVector[index] == newVector[index] {
			continue
		}
		count++
	}
	if count == 0 {
		return current, nil
	}
	changes := make([]backend.VectorUpdate, 0, count)
	for index := range oldVector {
		if err := checkContext(ctx); err != nil {
			return backend.VectorCommitment{}, err
		}
		if oldVector[index] == newVector[index] {
			continue
		}
		changes = append(changes, backend.VectorUpdate{
			Index: uint8(index),
			Old:   oldVector[index],
			New:   newVector[index],
		})
	}
	capacity := int(engine.UpdateCapacity())
	if capacity == 0 {
		return engine.Commit(ctx, newVector)
	}
	updated := current
	for start := 0; start < count; start += capacity {
		if err := checkContext(ctx); err != nil {
			return backend.VectorCommitment{}, err
		}
		end := min(start+capacity, count)
		next, err := engine.UpdateCommitment(ctx, updated, changes[start:end])
		if err != nil {
			return backend.VectorCommitment{}, err
		}
		updated = next
	}

	return updated, nil
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

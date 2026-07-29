// Package committedtree constructs immutable experimental-profile nodes and
// their vector-commitment root. It is an internal pre-v1 construction seam.
package committedtree

import (
	"bytes"
	"context"
	"errors"
	"fmt"

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
	nodeWorkingBytes = uint64(256)
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

// Key is one fixed-length raw key in the experimental profile.
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
	firstEdge  uint32
	edgeCount  uint16
	commitment backend.VectorCommitment
}

type edge struct {
	index byte
	child uint32
}

// Tree is an immutable committed-node arena. Copies are safe for concurrent
// reads because construction owns the arena and never mutates it after return.
type Tree struct {
	nodes []node
	edges []edge
	root  uint32
	valid bool
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

	return finalizeTree(
		builder.nodes,
		builder.edges,
		root,
		plan.nodeCount,
		plan.edgeCount,
	)
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
	var c1 backend.Vector
	var c2 backend.Vector
	for index := group.entryStart; index < group.entryEnd; index++ {
		if err := checkContext(builder.ctx); err != nil {
			return backend.VectorCommitment{}, err
		}
		entry := builder.entries[index]
		opening := leafvector.EncodePresent(entry.Key[31], [32]byte(entry.Value))
		target := &c1
		if opening.Half == leafvector.C2 {
			target = &c2
		}
		target[opening.LowIndex] = [32]byte(opening.Low)
		target[opening.HighIndex] = [32]byte(opening.High)
	}
	c1Commitment, err := builder.engine.Commit(builder.ctx, c1)
	if err != nil {
		return backend.VectorCommitment{}, err
	}
	c2Commitment, err := builder.engine.Commit(builder.ctx, c2)
	if err != nil {
		return backend.VectorCommitment{}, err
	}
	c1Scalar, err := c1Commitment.ScalarBytes()
	if err != nil {
		return backend.VectorCommitment{}, err
	}
	c2Scalar, err := c2Commitment.ScalarBytes()
	if err != nil {
		return backend.VectorCommitment{}, err
	}

	var vector backend.Vector
	vector[leafvector.ExtensionMarkerIndex] = [32]byte(leafvector.EncodeExtensionMarker())
	vector[leafvector.StemIndex] = [32]byte(leafvector.EncodeStem(group.stem))
	vector[leafvector.C1HashIndex] = c1Scalar
	vector[leafvector.C2HashIndex] = c2Scalar
	committed, err := builder.engine.Commit(builder.ctx, vector)
	if err != nil {
		return backend.VectorCommitment{}, err
	}
	builder.nodes = append(builder.nodes, node{
		kind:       nodeStem,
		depth:      depth,
		stem:       group.stem,
		commitment: committed,
	})

	return committed, nil
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

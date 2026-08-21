package treelayout

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"slices"
)

const (
	stemWorkingBytes      = uint64(62)
	nodeWorkingBytes      = uint64(64)
	edgeWorkingBytes      = uint64(8)
	maxSupportedStemCount = uint32(2_147_483_647)
)

// Stem is the fixed 31-byte path portion of a pre-v1 profile key.
type Stem [31]byte

// Kind identifies one logical committed-tree node kind.
type Kind uint8

const (
	// KindInternal is a width-256 node whose present edges select stem bytes.
	KindInternal Kind = iota + 1

	// KindStem is a terminal node containing all suffix values for one stem.
	KindStem
)

// Match classifies where a stem lookup terminates.
type Match uint8

const (
	// MatchPresentStem means the exact queried stem is attached at Depth.
	MatchPresentStem Match = iota + 1

	// MatchMissingChild means the selected internal-node edge is absent.
	MatchMissingChild

	// MatchDifferentStem means the selected edge terminates at another stem.
	MatchDifferentStem
)

// Result is the deterministic path outcome for one queried stem. Existing is
// set for present and different-stem outcomes and zero for a missing child.
type Result struct {
	Match    Match
	Depth    uint8
	Existing Stem
}

// Limits bounds every allocation-amplifying layout operation. Zero values are
// invalid and no field denotes an unbounded resource.
type Limits struct {
	MaxStems          uint32
	MaxNodes          uint32
	MaxEdges          uint32
	MaxTemporaryBytes uint64
}

func (limits Limits) validate() error {
	if limits.MaxStems == 0 ||
		limits.MaxNodes == 0 ||
		limits.MaxEdges == 0 ||
		limits.MaxTemporaryBytes == 0 ||
		limits.MaxStems > maxSupportedStemCount ||
		limits.MaxNodes > maxSupportedStemCount ||
		limits.MaxEdges > maxSupportedStemCount {
		return errInvalidLimits
	}

	return nil
}

type node struct {
	kind      Kind
	depth     uint8
	stem      Stem
	firstEdge uint32
	edgeCount uint16
}

type edge struct {
	index uint8
	child uint32
}

// Layout is an immutable canonical topology. Copies are safe for concurrent
// lookup because its owned arrays are never mutated after construction.
type Layout struct {
	limits         Limits
	stems          []Stem
	nodes          []node
	edges          []edge
	temporaryBytes uint64
	valid          bool
}

// Build copies, validates, and canonically orders stems before constructing
// the minimal radix topology. Duplicate stems fail the whole operation.
func Build(
	ctx context.Context,
	stems []Stem,
	limits Limits,
) (Layout, error) {
	if err := checkContext(ctx); err != nil {
		return Layout{}, err
	}
	if err := limits.validate(); err != nil {
		return Layout{}, err
	}
	if err := checkResource(
		ResourceStems,
		uint64(limits.MaxStems),
		uint64(len(stems)),
	); err != nil {
		return Layout{}, err
	}
	if err := checkInitialBytes(limits, len(stems)); err != nil {
		return Layout{}, err
	}

	owned := append([]Stem(nil), stems...)

	return buildOwned(ctx, owned, limits)
}

func buildOwned(
	ctx context.Context,
	stems []Stem,
	limits Limits,
) (Layout, error) {
	if len(stems) == 0 {
		stems = nil
	}
	if err := sortStems(ctx, stems); err != nil {
		return Layout{}, err
	}
	for index := 1; index < len(stems); index++ {
		if err := checkContext(ctx); err != nil {
			return Layout{}, err
		}
		if stems[index-1] == stems[index] {
			return Layout{}, errDuplicateStem
		}
	}

	counts := topologyCounts{nodes: 1}
	if err := countChildren(ctx, stems, 0, &counts); err != nil {
		return Layout{}, err
	}
	if err := checkResource(
		ResourceNodes,
		uint64(limits.MaxNodes),
		counts.nodes,
	); err != nil {
		return Layout{}, err
	}
	if err := checkResource(
		ResourceEdges,
		uint64(limits.MaxEdges),
		counts.edges,
	); err != nil {
		return Layout{}, err
	}

	// Limits cap each count at MaxInt32. These products and their sum are
	// therefore below MaxUint64 on every supported architecture.
	temporaryBytes := layoutBytes(
		uint64(len(stems)),
		counts.nodes,
		counts.edges,
	)
	if err := checkResource(
		ResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return Layout{}, err
	}

	layout := Layout{
		limits:         limits,
		stems:          stems,
		nodes:          make([]node, 1, int(counts.nodes)),
		edges:          make([]edge, 0, int(counts.edges)),
		temporaryBytes: temporaryBytes,
		valid:          true,
	}
	layout.nodes[0] = node{kind: KindInternal}
	if err := layout.buildChildren(ctx, 0, stems, 0); err != nil {
		return Layout{}, err
	}
	return finalizeLayout(layout, counts)
}

func finalizeLayout(
	layout Layout,
	counts topologyCounts,
) (Layout, error) {
	if uint64(len(layout.nodes)) != counts.nodes ||
		uint64(len(layout.edges)) != counts.edges {
		return Layout{}, errInvalidLayout
	}

	return layout, nil
}

type topologyCounts struct {
	nodes uint64
	edges uint64
}

func countChildren(
	ctx context.Context,
	stems []Stem,
	depth uint8,
	counts *topologyCounts,
) error {
	for start := 0; start < len(stems); {
		if err := checkContext(ctx); err != nil {
			return err
		}
		end := groupEnd(stems, start, depth)
		counts.nodes++
		counts.edges++
		if end-start > 1 {
			if depth >= 30 {
				return errDuplicateStem
			}
			if err := countChildren(
				ctx,
				stems[start:end],
				depth+1,
				counts,
			); err != nil {
				return err
			}
		}
		start = end
	}

	return nil
}

func (layout *Layout) buildChildren(
	ctx context.Context,
	parent uint32,
	stems []Stem,
	depth uint8,
) error {
	groupCount := 0
	for start := 0; start < len(stems); {
		if err := checkContext(ctx); err != nil {
			return err
		}
		groupCount++
		start = groupEnd(stems, start, depth)
	}

	firstEdge := len(layout.edges)
	layout.edges = append(layout.edges, make([]edge, groupCount)...)
	layout.nodes[parent].firstEdge = uint32(firstEdge)
	layout.nodes[parent].edgeCount = uint16(groupCount)

	groupIndex := 0
	for start := 0; start < len(stems); {
		if err := checkContext(ctx); err != nil {
			return err
		}
		end := groupEnd(stems, start, depth)
		childIndex := uint32(len(layout.nodes))
		child := node{depth: depth + 1}
		if end-start == 1 {
			child.kind = KindStem
			child.stem = stems[start]
		} else {
			child.kind = KindInternal
		}
		layout.nodes = append(layout.nodes, child)
		layout.edges[firstEdge+groupIndex] = edge{
			index: stems[start][depth],
			child: childIndex,
		}
		if child.kind == KindInternal {
			if err := layout.buildChildren(
				ctx,
				childIndex,
				stems[start:end],
				depth+1,
			); err != nil {
				return err
			}
		}
		groupIndex++
		start = end
	}

	return nil
}

func groupEnd(stems []Stem, start int, depth uint8) int {
	index := stems[start][depth]
	end := start + 1
	for end < len(stems) && stems[end][depth] == index {
		end++
	}

	return end
}

// Lookup classifies an exact stem against the immutable layout.
func (layout Layout) Lookup(ctx context.Context, stem Stem) (Result, error) {
	if err := layout.validate(); err != nil {
		return Result{}, err
	}
	if err := checkContext(ctx); err != nil {
		return Result{}, err
	}

	nodeIndex := uint32(0)
	for {
		if err := checkContext(ctx); err != nil {
			return Result{}, err
		}
		current := layout.nodes[nodeIndex]
		if current.kind != KindInternal || current.depth >= 31 {
			return Result{}, errInvalidLayout
		}
		childIndex, found, err := layout.findChild(
			ctx,
			current,
			stem[current.depth],
		)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Result{
				Match: MatchMissingChild,
				Depth: current.depth + 1,
			}, nil
		}

		child := layout.nodes[childIndex]
		switch child.kind {
		case KindInternal:
			nodeIndex = childIndex
		case KindStem:
			if child.stem == stem {
				return Result{
					Match:    MatchPresentStem,
					Depth:    child.depth,
					Existing: child.stem,
				}, nil
			}

			return Result{
				Match:    MatchDifferentStem,
				Depth:    child.depth,
				Existing: child.stem,
			}, nil
		default:
			return Result{}, errInvalidLayout
		}
	}
}

func (layout Layout) findChild(
	ctx context.Context,
	parent node,
	index uint8,
) (uint32, bool, error) {
	low, high, ok := checkedEdgeRange(
		parent.firstEdge,
		parent.edgeCount,
		len(layout.edges),
	)
	if !ok {
		return 0, false, errInvalidLayout
	}
	if err := checkContext(ctx); err != nil {
		return 0, false, err
	}
	position, found := slices.BinarySearchFunc(
		layout.edges[low:high],
		index,
		func(candidate edge, index uint8) int {
			return cmp.Compare(candidate.index, index)
		},
	)
	if !found {
		return 0, false, nil
	}
	candidate := layout.edges[low:high][position]
	if int(candidate.child) >= len(layout.nodes) {
		return 0, false, errInvalidLayout
	}

	return candidate.child, true, nil
}

func checkedEdgeRange(
	first uint32,
	count uint16,
	length int,
) (int, int, bool) {
	if length < 0 {
		return 0, 0, false
	}
	low := uint64(first)
	high := low + uint64(count)
	if high > uint64(length) {
		return 0, 0, false
	}

	return int(low), int(high), true
}

// Insert returns a canonical new layout and whether stem was newly inserted.
// The receiver is unchanged on every path.
func (layout Layout) Insert(
	ctx context.Context,
	stem Stem,
) (Layout, bool, error) {
	if err := layout.validate(); err != nil {
		return Layout{}, false, err
	}
	if err := checkContext(ctx); err != nil {
		return Layout{}, false, err
	}

	index, found := findStem(layout.stems, stem)
	if found {
		return layout, false, nil
	}
	count := len(layout.stems) + 1
	if err := checkResource(
		ResourceStems,
		uint64(layout.limits.MaxStems),
		uint64(count),
	); err != nil {
		return Layout{}, false, err
	}
	if err := checkInitialBytes(layout.limits, count); err != nil {
		return Layout{}, false, err
	}

	stems := make([]Stem, count)
	copy(stems, layout.stems[:index])
	stems[index] = stem
	copy(stems[index+1:], layout.stems[index:])
	result, err := buildOwned(ctx, stems, layout.limits)
	if err != nil {
		return Layout{}, false, err
	}

	return result, true, nil
}

// Delete returns a canonical new layout and whether stem was removed. Unary
// collision paths are collapsed because topology depends only on current state.
func (layout Layout) Delete(
	ctx context.Context,
	stem Stem,
) (Layout, bool, error) {
	if err := layout.validate(); err != nil {
		return Layout{}, false, err
	}
	if err := checkContext(ctx); err != nil {
		return Layout{}, false, err
	}

	index, found := findStem(layout.stems, stem)
	if !found {
		return layout, false, nil
	}
	count := len(layout.stems) - 1
	if err := checkInitialBytes(layout.limits, count); err != nil {
		return Layout{}, false, err
	}

	stems := make([]Stem, count)
	copy(stems, layout.stems[:index])
	copy(stems[index:], layout.stems[index+1:])
	result, err := buildOwned(ctx, stems, layout.limits)
	if err != nil {
		return Layout{}, false, err
	}

	return result, true, nil
}

func findStem(stems []Stem, stem Stem) (int, bool) {
	return slices.BinarySearchFunc(stems, stem, func(
		candidate Stem,
		stem Stem,
	) int {
		return bytes.Compare(candidate[:], stem[:])
	})
}

// StemCount returns the number of distinct retained stems.
func (layout Layout) StemCount() int {
	return len(layout.stems)
}

// NodeCount returns the root, internal, and stem node count.
func (layout Layout) NodeCount() int {
	return len(layout.nodes)
}

// EdgeCount returns the present parent-to-child edge count.
func (layout Layout) EdgeCount() int {
	return len(layout.edges)
}

// TemporaryBytes returns the deterministic construction-space upper bound.
func (layout Layout) TemporaryBytes() uint64 {
	return layout.temporaryBytes
}

func (layout Layout) validate() error {
	if !layout.valid ||
		layout.limits.validate() != nil ||
		len(layout.nodes) == 0 ||
		layout.nodes[0].kind != KindInternal ||
		layout.nodes[0].depth != 0 {
		return errInvalidLayout
	}

	return nil
}

func checkInitialBytes(limits Limits, stemCount int) error {
	return checkResource(
		ResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		uint64(stemCount)*stemWorkingBytes,
	)
}

func layoutBytes(stems, nodes, edges uint64) uint64 {
	return stems*stemWorkingBytes +
		nodes*nodeWorkingBytes +
		edges*edgeWorkingBytes
}

func checkResource(kind ResourceKind, limit, actual uint64) error {
	if actual <= limit {
		return nil
	}

	return &ResourceError{Kind: kind, Limit: limit, Actual: actual}
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return errInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(errCancelled, err)
	}

	return nil
}

func sortStems(ctx context.Context, stems []Stem) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if len(stems) < 2 {
		return nil
	}
	scratch := make([]Stem, len(stems))

	return mergeSortStems(ctx, stems, scratch, 0, len(stems))
}

func mergeSortStems(
	ctx context.Context,
	stems []Stem,
	scratch []Stem,
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
	if err := mergeSortStems(ctx, stems, scratch, start, middle); err != nil {
		return err
	}
	if err := mergeSortStems(ctx, stems, scratch, middle, end); err != nil {
		return err
	}
	left := start
	right := middle
	for output := start; output < end; output++ {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if right == end ||
			(left < middle && bytes.Compare(stems[left][:], stems[right][:]) != 1) {
			scratch[output] = stems[left]
			left++
		} else {
			scratch[output] = stems[right]
			right++
		}
	}
	for index := start; index < end; index++ {
		if err := checkContext(ctx); err != nil {
			return err
		}
		stems[index] = scratch[index]
	}

	return nil
}

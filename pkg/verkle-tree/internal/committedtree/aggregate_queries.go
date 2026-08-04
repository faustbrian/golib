package committedtree

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/leafvector"
)

const (
	maxAggregateProverQueries         = uint32(65_536)
	aggregateInternalQueriesFirstStem = uint64(31)
	aggregateQueriesPerStem           = uint64(2)
	aggregateQueriesPerStemHalf       = uint64(1)
	aggregateQueriesPerKey            = uint64(2)
	aggregateQueryKeyWorkingBytes     = uint64(64)
	aggregateQuerySortWorkingBytes    = uint64(16)
)

var (
	errInvalidAggregateProverQueryLimits = errors.New(
		"invalid committed-tree aggregate-query limits",
	)
	errInvalidAggregateProverQuery = errors.New(
		"invalid committed-tree aggregate query",
	)
	errAggregateProverQueryResource = errors.New(
		"committed-tree aggregate-query resource limit exceeded",
	)
)

// AggregateProverQueryLimits bounds deterministic opening-query extraction.
// Every field must be positive and no field denotes an unbounded resource.
type AggregateProverQueryLimits struct {
	MaxKeys           uint32
	MaxQueries        uint32
	MaxNodeReads      uint64
	MaxTemporaryBytes uint64
}

func (limits AggregateProverQueryLimits) validate() error {
	if limits.MaxKeys == 0 ||
		limits.MaxKeys > maxSupportedCount ||
		limits.MaxQueries == 0 ||
		limits.MaxQueries > maxAggregateProverQueries ||
		limits.MaxNodeReads == 0 ||
		limits.MaxTemporaryBytes == 0 {
		return errInvalidAggregateProverQueryLimits
	}

	return nil
}

// Validate rejects zero or unsupported aggregate-query limits before tree
// traversal begins.
func (limits AggregateProverQueryLimits) Validate() error {
	return limits.validate()
}

// AggregateProverQueryResource identifies one bounded extraction resource.
type AggregateProverQueryResource uint8

const (
	// AggregateProverQueryResourceKeys counts distinct caller keys.
	AggregateProverQueryResourceKeys AggregateProverQueryResource = iota + 1

	// AggregateProverQueryResourceQueries counts retained canonical openings.
	AggregateProverQueryResourceQueries

	// AggregateProverQueryResourceNodeReads counts immutable arena nodes.
	AggregateProverQueryResourceNodeReads

	// AggregateProverQueryResourceTemporaryBytes counts owned scratch/results.
	AggregateProverQueryResourceTemporaryBytes
)

// AggregateProverQueryResourceError reports one rejected bound without
// disclosing keys, values, vectors, or commitments.
type AggregateProverQueryResourceError struct {
	Resource AggregateProverQueryResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *AggregateProverQueryResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errAggregateProverQueryResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes AggregateProverQueryResourceError match the resource sentinel.
func (err *AggregateProverQueryResourceError) Unwrap() error {
	return errAggregateProverQueryResource
}

// AggregateProverQuery binds one canonical tree path to one complete vector
// opening. Path is empty for the root. The returned query set owns every
// immutable vector referenced by Opening.
type AggregateProverQuery struct {
	Path    [32]byte
	Length  uint8
	Opening backend.AggregateProverQuery
}

type aggregateQueryPath struct {
	path   [32]byte
	length uint8
}

type aggregateQueryIdentity struct {
	path  aggregateQueryPath
	index uint8
}

type aggregateQueryVector struct {
	commitment backend.VectorCommitment
	vector     *backend.Vector
	nodeIndex  uint32
}

// AggregateProverQueries derives every polynomial opening required for the
// unordered distinct keys and returns them in path/index transcript order.
func (tree Tree) AggregateProverQueries(
	ctx context.Context,
	keys []Key,
	limits AggregateProverQueryLimits,
) ([]AggregateProverQuery, error) {
	if !tree.valid ||
		len(tree.nodes) == 0 ||
		uint64(tree.root) >= uint64(len(tree.nodes)) {
		return nil, errInvalidTree
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, errInvalidTree
	}
	if err := checkAggregateProverQueryResource(
		AggregateProverQueryResourceKeys,
		uint64(limits.MaxKeys),
		uint64(len(keys)),
	); err != nil {
		return nil, err
	}
	keyTemporaryBytes := uint64(len(keys)) * 2 * aggregateQueryKeyWorkingBytes
	if err := checkAggregateProverQueryResource(
		AggregateProverQueryResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		keyTemporaryBytes,
	); err != nil {
		return nil, err
	}

	ordered := append([]Key(nil), keys...)
	if err := sortAggregateQueryKeys(ctx, ordered); err != nil {
		return nil, err
	}
	// An internal opening is identified by the selected prefix through its
	// next byte. The first stem can therefore visit 31 distinct internal
	// openings and every later sorted stem adds one opening after each byte
	// beyond its common prefix. A reached stem adds two metadata openings,
	// each distinct half adds one child-commitment opening, and every key adds
	// two suffix-field openings. Traversals that terminate early use less.
	var capacity uint64
	for index := range ordered {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if index > 0 && ordered[index-1] == ordered[index] {
			return nil, errDuplicateKey
		}
		sameStem := index > 0 && bytes.Equal(ordered[index-1][:31], ordered[index][:31])
		if !sameStem {
			capacity += aggregateQueriesPerStem
			if index == 0 {
				capacity += aggregateInternalQueriesFirstStem
			} else {
				commonPrefix := 0
				for commonPrefix < 31 &&
					ordered[index-1][commonPrefix] == ordered[index][commonPrefix] {
					commonPrefix++
				}
				capacity += uint64(31 - commonPrefix)
			}
		}
		if !sameStem || ordered[index-1][31]/128 != ordered[index][31]/128 {
			capacity += aggregateQueriesPerStemHalf
		}
		capacity += aggregateQueriesPerKey
	}
	capacity = min(uint64(limits.MaxQueries), capacity)
	temporaryBytes := keyTemporaryBytes +
		capacity*(aggregateQueryResultWorkingBytes()+aggregateQuerySortWorkingBytes)
	if err := checkAggregateProverQueryResource(
		AggregateProverQueryResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return nil, err
	}

	collector := aggregateQueryCollector{
		ctx:        ctx,
		tree:       &tree,
		limits:     limits,
		queries:    make([]AggregateProverQuery, 0, int(capacity)),
		queryByID:  make(map[aggregateQueryIdentity]int, int(capacity)),
		vectorByID: make(map[aggregateQueryPath]aggregateQueryVector),
	}
	for index := range ordered {
		if err := collector.collect(ordered[index]); err != nil {
			return nil, err
		}
	}
	if err := sortAggregateProverQueries(ctx, collector.queries); err != nil {
		return nil, err
	}
	queries, err := consolidateAggregateProverQueries(ctx, collector.queries)
	if err != nil {
		return nil, err
	}

	return queries, nil
}

func aggregateQueryResultWorkingBytes() uint64 {
	return uint64(backend.VectorWidth*32 + 256)
}

type aggregateQueryCollector struct {
	ctx        context.Context
	tree       *Tree
	limits     AggregateProverQueryLimits
	queries    []AggregateProverQuery
	queryByID  map[aggregateQueryIdentity]int
	vectorByID map[aggregateQueryPath]aggregateQueryVector
	zeroVector *backend.Vector
	nodeReads  uint64
}

func (collector *aggregateQueryCollector) collect(key Key) error {
	current := collector.tree.root
	for {
		if err := checkContext(collector.ctx); err != nil {
			return err
		}
		if uint64(current) >= uint64(len(collector.tree.nodes)) {
			return errInvalidTree
		}
		node := collector.tree.nodes[current]
		if node.kind != nodeInternal || node.depth > 30 {
			return errInvalidTree
		}
		path := aggregateQueryPath{length: node.depth}
		copy(path.path[:], key[:node.depth])
		vector, err := collector.internalVector(path, current, node)
		if err != nil {
			return err
		}
		selected := key[node.depth]
		if err := collector.append(path, vector, selected); err != nil {
			return err
		}
		child, found, err := collector.findChild(node, selected)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		childNode := collector.tree.nodes[child]
		switch childNode.kind {
		case nodeInternal:
			current = child
		case nodeStem:
			return collector.collectStem(key, child, childNode)
		default:
			return errInvalidTree
		}
	}
}

func (collector *aggregateQueryCollector) collectStem(
	key Key,
	nodeIndex uint32,
	node node,
) error {
	if node.depth == 0 || node.depth > 31 {
		return errInvalidTree
	}
	path := aggregateQueryPath{length: node.depth}
	copy(path.path[:], key[:node.depth])
	stemVector, c1, c2, err := collector.stemVectors(path, nodeIndex, node)
	if err != nil {
		return err
	}
	if err := collector.append(path, stemVector, leafvector.ExtensionMarkerIndex); err != nil {
		return err
	}
	if err := collector.append(path, stemVector, leafvector.StemIndex); err != nil {
		return err
	}
	var queriedStem [31]byte
	copy(queriedStem[:], key[:31])
	if node.stem != queriedStem {
		return nil
	}

	halfIndex := uint8(leafvector.C1HashIndex)
	half := c1
	if key[31] >= 128 {
		halfIndex = leafvector.C2HashIndex
		half = c2
	}
	if err := collector.append(path, stemVector, halfIndex); err != nil {
		return err
	}
	suffixPath := path
	suffixPath.path[suffixPath.length] = halfIndex
	suffixPath.length++
	opening := leafvector.EncodePresent(key[31], [32]byte{})
	if err := collector.append(suffixPath, half, opening.LowIndex); err != nil {
		return err
	}

	return collector.append(suffixPath, half, opening.HighIndex)
}

func (collector *aggregateQueryCollector) internalVector(
	path aggregateQueryPath,
	nodeIndex uint32,
	node node,
) (aggregateQueryVector, error) {
	if cached, ok := collector.vectorByID[path]; ok {
		if cached.nodeIndex != nodeIndex {
			return aggregateQueryVector{}, errInvalidTree
		}
		return cached, nil
	}
	if err := collector.readNodes(1); err != nil {
		return aggregateQueryVector{}, err
	}
	first := uint64(node.firstEdge)
	end := first + uint64(node.edgeCount)
	if end > uint64(len(collector.tree.edges)) {
		return aggregateQueryVector{}, errInvalidTree
	}
	vector := new(backend.Vector)
	var previous byte
	for offset := first; offset < end; offset++ {
		if err := checkContext(collector.ctx); err != nil {
			return aggregateQueryVector{}, err
		}
		edge := collector.tree.edges[offset]
		if offset > first && edge.index <= previous {
			return aggregateQueryVector{}, errInvalidTree
		}
		previous = edge.index
		if uint64(edge.child) >= uint64(len(collector.tree.nodes)) {
			return aggregateQueryVector{}, errInvalidTree
		}
		if err := collector.readNodes(1); err != nil {
			return aggregateQueryVector{}, err
		}
		child := collector.tree.nodes[edge.child]
		if child.depth != node.depth+1 {
			return aggregateQueryVector{}, errInvalidTree
		}
		mapped, err := child.commitment.ScalarBytes()
		if err != nil {
			return aggregateQueryVector{}, errInvalidTree
		}
		vector[edge.index] = mapped
	}
	result := aggregateQueryVector{
		commitment: node.commitment,
		vector:     vector,
		nodeIndex:  nodeIndex,
	}
	collector.vectorByID[path] = result

	return result, nil
}

func (collector *aggregateQueryCollector) stemVectors(
	path aggregateQueryPath,
	nodeIndex uint32,
	node node,
) (aggregateQueryVector, aggregateQueryVector, aggregateQueryVector, error) {
	c1Path := aggregateStemHalfPath(path, leafvector.C1HashIndex)
	c2Path := aggregateStemHalfPath(path, leafvector.C2HashIndex)
	if stem, ok := collector.vectorByID[path]; ok {
		c1, c1OK := collector.vectorByID[c1Path]
		c2, c2OK := collector.vectorByID[c2Path]
		if stem.nodeIndex != nodeIndex ||
			!c1OK || c1.nodeIndex != nodeIndex ||
			!c2OK || c2.nodeIndex != nodeIndex {
			return aggregateQueryVector{}, aggregateQueryVector{}, aggregateQueryVector{}, errInvalidTree
		}

		return stem, c1, c2, nil
	}
	if err := collector.readNodes(1); err != nil {
		return aggregateQueryVector{}, aggregateQueryVector{}, aggregateQueryVector{}, err
	}
	start := uint64(node.entryStart)
	end := start + uint64(node.entryCount)
	if node.entryCount == 0 || end > uint64(len(collector.tree.entries)) {
		return aggregateQueryVector{}, aggregateQueryVector{}, aggregateQueryVector{}, errInvalidTree
	}
	var c1Vector *backend.Vector
	var c2Vector *backend.Vector
	for index := start; index < end; index++ {
		if err := checkContext(collector.ctx); err != nil {
			return aggregateQueryVector{}, aggregateQueryVector{}, aggregateQueryVector{}, err
		}
		entry := collector.tree.entries[index]
		if !bytes.Equal(entry.Key[:31], node.stem[:]) {
			return aggregateQueryVector{}, aggregateQueryVector{}, aggregateQueryVector{}, errInvalidTree
		}
		opening := leafvector.EncodePresent(entry.Key[31], [32]byte(entry.Value))
		target := &c1Vector
		if opening.Half == leafvector.C2 {
			target = &c2Vector
		}
		if *target == nil {
			*target = new(backend.Vector)
		}
		(*target)[opening.LowIndex] = [32]byte(opening.Low)
		(*target)[opening.HighIndex] = [32]byte(opening.High)
	}
	if c1Vector == nil {
		c1Vector = collector.emptyAggregateQueryVector()
	}
	if c2Vector == nil {
		c2Vector = collector.emptyAggregateQueryVector()
	}
	c1Scalar, err := node.c1.ScalarBytes()
	if err != nil {
		return aggregateQueryVector{}, aggregateQueryVector{}, aggregateQueryVector{}, errInvalidTree
	}
	c2Scalar, err := node.c2.ScalarBytes()
	if err != nil {
		return aggregateQueryVector{}, aggregateQueryVector{}, aggregateQueryVector{}, errInvalidTree
	}
	stemVector := new(backend.Vector)
	stemVector[leafvector.ExtensionMarkerIndex] = [32]byte(leafvector.EncodeExtensionMarker())
	stemVector[leafvector.StemIndex] = [32]byte(leafvector.EncodeStem(node.stem))
	stemVector[leafvector.C1HashIndex] = c1Scalar
	stemVector[leafvector.C2HashIndex] = c2Scalar
	stem := aggregateQueryVector{commitment: node.commitment, vector: stemVector, nodeIndex: nodeIndex}
	collector.vectorByID[path] = stem
	c1 := aggregateQueryVector{commitment: node.c1, vector: c1Vector, nodeIndex: nodeIndex}
	c2 := aggregateQueryVector{commitment: node.c2, vector: c2Vector, nodeIndex: nodeIndex}
	collector.vectorByID[c1Path] = c1
	collector.vectorByID[c2Path] = c2

	return stem, c1, c2, nil
}

func aggregateStemHalfPath(path aggregateQueryPath, halfIndex uint8) aggregateQueryPath {
	path.path[path.length] = halfIndex
	path.length++

	return path
}

func (collector *aggregateQueryCollector) emptyAggregateQueryVector() *backend.Vector {
	if collector.zeroVector == nil {
		collector.zeroVector = new(backend.Vector)
	}

	return collector.zeroVector
}

func (collector *aggregateQueryCollector) findChild(
	node node,
	selected byte,
) (uint32, bool, error) {
	first := uint64(node.firstEdge)
	end := first + uint64(node.edgeCount)
	if end > uint64(len(collector.tree.edges)) {
		return 0, false, errInvalidTree
	}
	for index := first; index < end; index++ {
		edge := collector.tree.edges[index]
		if edge.index == selected {
			if uint64(edge.child) >= uint64(len(collector.tree.nodes)) {
				return 0, false, errInvalidTree
			}
			return edge.child, true, nil
		}
	}

	return 0, false, nil
}

func (collector *aggregateQueryCollector) append(
	path aggregateQueryPath,
	vector aggregateQueryVector,
	index uint8,
) error {
	if err := checkContext(collector.ctx); err != nil {
		return err
	}
	if vector.vector == nil {
		return errInvalidTree
	}
	incomingCommitment, incomingErr := vector.commitment.DeduplicationKey()
	if incomingErr != nil {
		return errInvalidTree
	}
	identity := aggregateQueryIdentity{path: path, index: index}
	if existingIndex, exists := collector.queryByID[identity]; exists {
		existing := collector.queries[existingIndex].Opening
		existingCommitment, existingErr := existing.Commitment.DeduplicationKey()
		if existingErr != nil ||
			existingCommitment != incomingCommitment ||
			existing.Vector == nil ||
			*existing.Vector != *vector.vector {
			return errInvalidTree
		}
		return nil
	}
	actual := uint64(len(collector.queries) + 1)
	if err := checkAggregateProverQueryResource(
		AggregateProverQueryResourceQueries,
		uint64(collector.limits.MaxQueries),
		actual,
	); err != nil {
		return err
	}
	query := AggregateProverQuery{
		Path:   path.path,
		Length: path.length,
		Opening: backend.AggregateProverQuery{
			Commitment: vector.commitment,
			Vector:     vector.vector,
			Index:      index,
		},
	}
	collector.queryByID[identity] = len(collector.queries)
	collector.queries = append(collector.queries, query)

	return nil
}

func (collector *aggregateQueryCollector) readNodes(count uint64) error {
	collector.nodeReads += count

	return checkAggregateProverQueryResource(
		AggregateProverQueryResourceNodeReads,
		collector.limits.MaxNodeReads,
		collector.nodeReads,
	)
}

func sortAggregateQueryKeys(ctx context.Context, keys []Key) error {
	if len(keys) < 2 {
		return checkContext(ctx)
	}
	scratch := make([]Key, len(keys))

	return mergeSortAggregateQueryKeys(ctx, keys, scratch, 0, len(keys))
}

func mergeSortAggregateQueryKeys(
	ctx context.Context,
	keys []Key,
	scratch []Key,
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
	if err := mergeSortAggregateQueryKeys(ctx, keys, scratch, start, middle); err != nil {
		return err
	}
	if err := mergeSortAggregateQueryKeys(ctx, keys, scratch, middle, end); err != nil {
		return err
	}
	left := start
	right := middle
	for output := start; output < end; output++ {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if right == end ||
			(left < middle && bytes.Compare(keys[left][:], keys[right][:]) != 1) {
			scratch[output] = keys[left]
			left++
		} else {
			scratch[output] = keys[right]
			right++
		}
	}
	copy(keys[start:end], scratch[start:end])

	return nil
}

func sortAggregateProverQueries(
	ctx context.Context,
	queries []AggregateProverQuery,
) error {
	if len(queries) < 2 {
		return checkContext(ctx)
	}
	order := make([]uint32, len(queries))
	scratch := make([]uint32, len(queries))
	for index := range order {
		if err := checkContext(ctx); err != nil {
			return err
		}
		order[index] = uint32(index)
	}
	if err := mergeSortAggregateProverQueryIndices(
		ctx,
		queries,
		order,
		scratch,
		0,
		len(order),
	); err != nil {
		return err
	}

	return applyAggregateProverQueryOrder(ctx, queries, order)
}

func mergeSortAggregateProverQueryIndices(
	ctx context.Context,
	queries []AggregateProverQuery,
	order []uint32,
	scratch []uint32,
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
	if err := mergeSortAggregateProverQueryIndices(
		ctx, queries, order, scratch, start, middle,
	); err != nil {
		return err
	}
	if err := mergeSortAggregateProverQueryIndices(
		ctx, queries, order, scratch, middle, end,
	); err != nil {
		return err
	}
	left := start
	right := middle
	for output := start; output < end; output++ {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if right == end ||
			(left < middle && compareAggregateProverQuery(
				queries[order[left]],
				queries[order[right]],
			) != 1) {
			scratch[output] = order[left]
			left++
		} else {
			scratch[output] = order[right]
			right++
		}
	}
	copy(order[start:end], scratch[start:end])

	return nil
}

func applyAggregateProverQueryOrder(
	ctx context.Context,
	queries []AggregateProverQuery,
	order []uint32,
) error {
	visited := make([]bool, len(queries))
	for start := range queries {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if visited[start] {
			continue
		}
		current := start
		displaced := queries[start]
		for {
			if err := checkContext(ctx); err != nil {
				return err
			}
			source := int(order[current])
			visited[current] = true
			if source == start {
				queries[current] = displaced

				break
			}
			queries[current] = queries[source]
			current = source
		}
	}

	return nil
}

func compareAggregateProverQuery(left, right AggregateProverQuery) int {
	if comparison := bytes.Compare(left.Path[:left.Length], right.Path[:right.Length]); comparison != 0 {
		return comparison
	}

	return cmp.Compare(left.Opening.Index, right.Opening.Index)
}

type aggregateOpeningIdentity struct {
	commitment [backend.CommitmentSize]byte
	index      uint8
}

func consolidateAggregateProverQueries(
	ctx context.Context,
	queries []AggregateProverQuery,
) ([]AggregateProverQuery, error) {
	retained := queries[:0]
	seen := make(map[aggregateOpeningIdentity]int, len(queries))
	for index := range queries {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		if queries[index].Opening.Vector == nil {
			return nil, errInvalidAggregateProverQuery
		}
		key, err := queries[index].Opening.Commitment.DeduplicationKey()
		if err != nil {
			return nil, errInvalidAggregateProverQuery
		}
		identity := aggregateOpeningIdentity{
			commitment: key,
			index:      queries[index].Opening.Index,
		}
		if existing, duplicate := seen[identity]; duplicate {
			if retained[existing].Opening.Vector == nil ||
				*retained[existing].Opening.Vector != *queries[index].Opening.Vector {
				return nil, errInvalidAggregateProverQuery
			}
			continue
		}
		seen[identity] = len(retained)
		retained = append(retained, queries[index])
	}

	return retained, nil
}

func checkAggregateProverQueryResource(
	resource AggregateProverQueryResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &AggregateProverQueryResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}

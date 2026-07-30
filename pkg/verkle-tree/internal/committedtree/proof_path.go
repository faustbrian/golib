package committedtree

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
)

const (
	maxProofPathCommitments = uint32(32)
	maxProofPathBytes       = uint64(528)
	proofPathWorkingBytes   = uint64(256)
)

var (
	errInvalidProofPathLimits = errors.New(
		"invalid committed-tree proof-path limits",
	)
	errProofPathResource = errors.New(
		"committed-tree proof-path resource limit exceeded",
	)
)

// ProofPathKind identifies how one queried stem terminates in the immutable
// committed topology.
type ProofPathKind uint8

const (
	// ProofPathPresent means the exact queried stem is committed at Depth.
	ProofPathPresent ProofPathKind = iota + 1

	// ProofPathMissing means the selected internal child is absent at Depth.
	ProofPathMissing

	// ProofPathDifferent means the selected child contains another stem.
	ProofPathDifferent
)

// ProofPathCommitment binds one non-root path to its immutable commitment.
type ProofPathCommitment struct {
	Path       [32]byte
	Length     uint8
	Commitment backend.VectorCommitment
}

// ProofPath is caller-owned topology and commitment material for one key. It
// does not contain or establish a cryptographic opening.
type ProofPath struct {
	Kind         ProofPathKind
	Depth        uint8
	ExistingStem [31]byte
	Commitments  []ProofPathCommitment
}

// ProofPathLimits bounds immutable proof-material extraction. Every field must
// be positive and no field denotes an unbounded resource.
type ProofPathLimits struct {
	MaxNodeReads      uint32
	MaxCommitments    uint32
	MaxPathBytes      uint64
	MaxTemporaryBytes uint64
}

func (limits ProofPathLimits) validate() error {
	if limits.MaxNodeReads == 0 ||
		limits.MaxNodeReads > maxProofPathCommitments ||
		limits.MaxCommitments == 0 ||
		limits.MaxCommitments > maxProofPathCommitments ||
		limits.MaxPathBytes == 0 ||
		limits.MaxPathBytes > maxProofPathBytes ||
		limits.MaxTemporaryBytes == 0 {
		return errInvalidProofPathLimits
	}

	return nil
}

// ProofPathResource identifies one bounded extraction resource.
type ProofPathResource uint8

const (
	// ProofPathResourceNodeReads counts immutable arena nodes inspected.
	ProofPathResourceNodeReads ProofPathResource = iota + 1

	// ProofPathResourceCommitments counts returned non-root commitments.
	ProofPathResourceCommitments

	// ProofPathResourcePathBytes counts returned canonical path bytes.
	ProofPathResourcePathBytes

	// ProofPathResourceTemporaryBytes counts the caller-owned result storage.
	ProofPathResourceTemporaryBytes
)

// ProofPathResourceError reports one rejected extraction bound without
// disclosing a key, path, value, or commitment.
type ProofPathResourceError struct {
	Resource ProofPathResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *ProofPathResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errProofPathResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes ProofPathResourceError match the extraction-resource sentinel.
func (err *ProofPathResourceError) Unwrap() error {
	return errProofPathResource
}

// ProofPath extracts exact non-root commitments for one key from an immutable
// tree. Returned storage is caller-owned and remains cryptographically
// unverified.
func (tree Tree) ProofPath(
	ctx context.Context,
	key Key,
	limits ProofPathLimits,
) (ProofPath, error) {
	if !tree.valid ||
		len(tree.nodes) == 0 ||
		uint64(tree.root) >= uint64(len(tree.nodes)) {
		return ProofPath{}, errInvalidTree
	}
	if err := checkContext(ctx); err != nil {
		return ProofPath{}, err
	}
	if err := limits.validate(); err != nil {
		return ProofPath{}, err
	}

	capacity := min(
		uint64(limits.MaxCommitments),
		limits.MaxTemporaryBytes/proofPathWorkingBytes,
	)
	result := ProofPath{
		Commitments: make([]ProofPathCommitment, 0, int(capacity)),
	}
	nodeReads := uint64(0)
	pathBytes := uint64(0)
	current, err := tree.readProofPathNode(
		ctx,
		tree.root,
		limits,
		&nodeReads,
	)
	if err != nil {
		return ProofPath{}, err
	}
	for {
		if err := checkContext(ctx); err != nil {
			return ProofPath{}, err
		}
		if current.kind != nodeInternal ||
			current.depth > 30 {
			return ProofPath{}, errInvalidTree
		}
		first := uint64(current.firstEdge)
		end := first + uint64(current.edgeCount)
		if end > uint64(len(tree.edges)) {
			return ProofPath{}, errInvalidTree
		}
		selected := key[current.depth]
		childIndex, found := findProofPathChild(
			tree.edges[int(first):int(end)],
			selected,
		)
		if !found {
			result.Kind = ProofPathMissing
			result.Depth = current.depth + 1
			return result, nil
		}
		child := tree.edges[int(first)+childIndex].child
		if uint64(child) >= uint64(len(tree.nodes)) {
			return ProofPath{}, errInvalidTree
		}
		childNode, err := tree.readProofPathNode(
			ctx,
			child,
			limits,
			&nodeReads,
		)
		if err != nil {
			return ProofPath{}, err
		}
		length := current.depth + 1
		if childNode.depth != length {
			return ProofPath{}, errInvalidTree
		}
		if err := appendProofPathCommitment(
			ctx,
			&result,
			key[:length],
			childNode.commitment,
			limits,
			&pathBytes,
		); err != nil {
			return ProofPath{}, err
		}
		switch childNode.kind {
		case nodeInternal:
			current = childNode
		case nodeStem:
			if childNode.stem != [31]byte(key[:31]) {
				result.Kind = ProofPathDifferent
				result.Depth = length
				result.ExistingStem = childNode.stem
				return result, nil
			}
			var suffixPath [32]byte
			copy(suffixPath[:], key[:length])
			suffixPath[length] = 2 + key[31]/128
			commitment := childNode.c1
			if key[31] >= 128 {
				commitment = childNode.c2
			}
			if err := appendProofPathCommitment(
				ctx,
				&result,
				suffixPath[:length+1],
				commitment,
				limits,
				&pathBytes,
			); err != nil {
				return ProofPath{}, err
			}
			result.Kind = ProofPathPresent
			result.Depth = length
			return result, nil
		default:
			return ProofPath{}, errInvalidTree
		}
	}
}

func (tree Tree) readProofPathNode(
	ctx context.Context,
	index uint32,
	limits ProofPathLimits,
	nodeReads *uint64,
) (node, error) {
	if err := checkContext(ctx); err != nil {
		return node{}, err
	}
	next := *nodeReads + 1
	if err := checkProofPathResource(
		ProofPathResourceNodeReads,
		uint64(limits.MaxNodeReads),
		next,
	); err != nil {
		return node{}, err
	}
	if uint64(index) >= uint64(len(tree.nodes)) {
		return node{}, errInvalidTree
	}
	*nodeReads = next

	return tree.nodes[index], nil
}

func findProofPathChild(edges []edge, selected byte) (int, bool) {
	return slices.BinarySearchFunc(
		edges,
		selected,
		func(edge edge, selected byte) int {
			return cmp.Compare(edge.index, selected)
		},
	)
}

func appendProofPathCommitment(
	ctx context.Context,
	result *ProofPath,
	path []byte,
	commitment backend.VectorCommitment,
	limits ProofPathLimits,
	pathBytes *uint64,
) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	count := uint64(len(result.Commitments)) + 1
	if err := checkProofPathResource(
		ProofPathResourceCommitments,
		uint64(limits.MaxCommitments),
		count,
	); err != nil {
		return err
	}
	nextPathBytes := *pathBytes + uint64(len(path))
	if err := checkProofPathResource(
		ProofPathResourcePathBytes,
		limits.MaxPathBytes,
		nextPathBytes,
	); err != nil {
		return err
	}
	if err := checkProofPathResource(
		ProofPathResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		count*proofPathWorkingBytes,
	); err != nil {
		return err
	}
	if _, err := commitment.IsIdentity(); err != nil {
		return errInvalidTree
	}

	value := ProofPathCommitment{
		Length:     uint8(len(path)),
		Commitment: commitment,
	}
	copy(value.Path[:], path)
	result.Commitments = append(result.Commitments, value)
	*pathBytes = nextPathBytes

	return nil
}

func checkProofPathResource(
	resource ProofPathResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &ProofPathResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}

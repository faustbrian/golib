package authstate

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/leafvector"
)

const (
	maxAggregateVerifierQueries       = uint32(65_536)
	aggregateVerifierQueriesPerClaim  = uint64(36)
	aggregateVerifierQueryWorkingByte = uint64(512)
)

var (
	errInvalidAggregateVerifierQueryLimits = errors.New(
		"invalid aggregate verifier-query limits",
	)
	errInvalidAggregateVerifierQuery = errors.New(
		"invalid aggregate verifier query",
	)
	errAggregateVerifierQueryResource = errors.New(
		"aggregate verifier-query resource limit exceeded",
	)
)

// AggregateVerifierQueryLimits bounds deterministic verifier-query
// reconstruction. Every field must be positive and no field is unbounded.
type AggregateVerifierQueryLimits struct {
	MaxQueries        uint32
	MaxTemporaryBytes uint64
}

func (limits AggregateVerifierQueryLimits) validate() error {
	if limits.MaxQueries == 0 ||
		limits.MaxQueries > maxAggregateVerifierQueries ||
		limits.MaxTemporaryBytes == 0 {
		return errInvalidAggregateVerifierQueryLimits
	}

	return nil
}

// AggregateVerifierQueryResource identifies one bounded reconstruction
// resource.
type AggregateVerifierQueryResource uint8

const (
	// AggregateVerifierQueryResourceQueries counts canonical openings.
	AggregateVerifierQueryResourceQueries AggregateVerifierQueryResource = iota + 1

	// AggregateVerifierQueryResourceTemporaryBytes counts owned scratch.
	AggregateVerifierQueryResourceTemporaryBytes
)

// AggregateVerifierQueryResourceError reports one rejected resource without
// disclosing claims, values, commitments, or paths.
type AggregateVerifierQueryResourceError struct {
	Resource AggregateVerifierQueryResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *AggregateVerifierQueryResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errAggregateVerifierQueryResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes AggregateVerifierQueryResourceError match the resource
// sentinel.
func (err *AggregateVerifierQueryResourceError) Unwrap() error {
	return errAggregateVerifierQueryResource
}

// AggregateVerifierQuery binds one canonical path to one public opening.
type AggregateVerifierQuery struct {
	Path    [maxProofPathLength]byte
	Length  uint8
	Opening backend.AggregateVerifierQuery
}

type aggregateVerifierPath struct {
	path   [maxProofPathLength]byte
	length uint8
}

type aggregateVerifierIdentity struct {
	path  aggregateVerifierPath
	index uint8
}

// AggregateVerifierQueries reconstructs the exact public transcript openings
// from immutable proof material without consulting mutable tree state.
func (material ProofMaterial) AggregateVerifierQueries(
	ctx context.Context,
	limits AggregateVerifierQueryLimits,
) ([]AggregateVerifierQuery, error) {
	if err := material.validate(); err != nil {
		return nil, err
	}
	if err := checkTreeProofContext(ctx); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	capacity := min(
		uint64(limits.MaxQueries),
		uint64(len(material.claims.claims))*aggregateVerifierQueriesPerClaim,
	)
	temporaryBytes := capacity * 2 * aggregateVerifierQueryWorkingByte
	if err := checkAggregateVerifierQueryResource(
		AggregateVerifierQueryResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return nil, err
	}
	markers, err := derivePathMarkers(
		ctx,
		material.claims.claims,
		material.stemPaths,
		int(capacity),
	)
	if err != nil {
		if errors.Is(err, errTreeProofCancelled) {
			return nil, err
		}
		return nil, errInvalidProofMaterial
	}
	if err := sortTreeProofValues(ctx, markers, comparePathMarkers); err != nil {
		return nil, err
	}
	if err := matchExpectedCommitments(
		ctx,
		markers,
		material.commitments,
	); err != nil {
		if errors.Is(err, errTreeProofCancelled) {
			return nil, err
		}
		return nil, errInvalidProofMaterial
	}

	// The material and exact commitment-set checks above establish that every
	// retained root and path commitment is valid and unique.
	rootCommitment, _ := material.root.Commitment()
	commitments := make(map[aggregateVerifierPath]backend.VectorCommitment)
	commitments[aggregateVerifierPath{}] = rootCommitment
	for index := range material.commitments {
		if err := checkTreeProofContext(ctx); err != nil {
			return nil, err
		}
		commitment := material.commitments[index]
		path := aggregateVerifierPath{path: commitment.path, length: commitment.length}
		commitments[path] = commitment.commitment
	}

	collector := aggregateVerifierCollector{
		ctx:         ctx,
		limits:      limits,
		commitments: commitments,
		queries:     make([]AggregateVerifierQuery, 0, int(capacity)),
		queryByID:   make(map[aggregateVerifierIdentity]int, int(capacity)),
	}
	claimIndex := 0
	for pathIndex := range material.stemPaths {
		if err := checkTreeProofContext(ctx); err != nil {
			return nil, err
		}
		path := material.stemPaths[pathIndex]
		stem := Stem(material.claims.claims[claimIndex].key[:31])
		claimEnd := claimIndex + 1
		for claimEnd < len(material.claims.claims) &&
			Stem(material.claims.claims[claimEnd].key[:31]) == stem {
			claimEnd++
		}
		if err := collector.collectStem(
			path,
			material.claims.claims[claimIndex:claimEnd],
		); err != nil {
			return nil, err
		}
		claimIndex = claimEnd
	}
	if err := sortTreeProofValues(ctx, collector.queries, compareAggregateVerifierQueries); err != nil {
		return nil, err
	}
	queries, err := consolidateAggregateVerifierQueries(ctx, collector.queries)
	if err != nil {
		return nil, err
	}

	return queries, nil
}

type aggregateVerifierCollector struct {
	ctx         context.Context
	limits      AggregateVerifierQueryLimits
	commitments map[aggregateVerifierPath]backend.VectorCommitment
	queries     []AggregateVerifierQuery
	queryByID   map[aggregateVerifierIdentity]int
}

func (collector *aggregateVerifierCollector) collectStem(
	path StemPath,
	claims []Claim,
) error {
	for depth := uint8(0); depth < path.depth; depth++ {
		parent := aggregateVerifierPath{length: depth}
		copy(parent.path[:], path.stem[:depth])
		if err := collector.appendChild(parent, path.stem[depth]); err != nil {
			return err
		}
	}
	if path.kind == StemPathMissing {
		return requireAbsenceClaims(collector.ctx, claims)
	}
	stemPath := aggregateVerifierPath{length: path.depth}
	copy(stemPath.path[:], path.stem[:path.depth])
	if err := collector.appendValue(stemPath, leafvector.ExtensionMarkerIndex, extensionMarkerScalar()); err != nil {
		return err
	}
	openedStem := path.stem
	if path.kind == StemPathDifferent {
		openedStem = path.existing
	}
	if err := collector.appendValue(
		stemPath,
		leafvector.StemIndex,
		[32]byte(leafvector.EncodeStem(openedStem)),
	); err != nil {
		return err
	}
	if path.kind != StemPathPresent {
		return requireAbsenceClaims(collector.ctx, claims)
	}
	for index := range claims {
		if err := checkTreeProofContext(collector.ctx); err != nil {
			return err
		}
		claim := claims[index]
		halfIndex := uint8(leafvector.C1HashIndex)
		if claim.key[31] >= 128 {
			halfIndex = leafvector.C2HashIndex
		}
		if err := collector.appendChild(stemPath, halfIndex); err != nil {
			return err
		}
		suffixPath := stemPath
		suffixPath.path[suffixPath.length] = halfIndex
		suffixPath.length++
		opening := leafvector.EncodePresent(claim.key[31], [32]byte(claim.value))
		low := [32]byte{}
		high := [32]byte{}
		if claim.kind == ClaimMembership {
			low = [32]byte(opening.Low)
			high = [32]byte(opening.High)
		}
		if err := collector.appendValue(suffixPath, opening.LowIndex, low); err != nil {
			return err
		}
		if err := collector.appendValue(suffixPath, opening.HighIndex, high); err != nil {
			return err
		}
	}

	return nil
}

func (collector *aggregateVerifierCollector) appendChild(
	path aggregateVerifierPath,
	index uint8,
) error {
	child := path
	child.path[child.length] = index
	child.length++
	value := [32]byte{}
	if commitment, exists := collector.commitments[child]; exists {
		mapped, err := commitment.ScalarBytes()
		if err != nil {
			return errInvalidProofMaterial
		}
		value = mapped
	}

	return collector.appendValue(path, index, value)
}

func (collector *aggregateVerifierCollector) appendValue(
	path aggregateVerifierPath,
	index uint8,
	value [32]byte,
) error {
	if err := checkTreeProofContext(collector.ctx); err != nil {
		return err
	}
	commitment, exists := collector.commitments[path]
	if !exists {
		return errInvalidProofMaterial
	}
	identity := aggregateVerifierIdentity{path: path, index: index}
	if existingIndex, duplicate := collector.queryByID[identity]; duplicate {
		existing := collector.queries[existingIndex]
		if existing.Opening.Value != value {
			return errInvalidAggregateVerifierQuery
		}
		return nil
	}
	actual := uint64(len(collector.queries) + 1)
	if err := checkAggregateVerifierQueryResource(
		AggregateVerifierQueryResourceQueries,
		uint64(collector.limits.MaxQueries),
		actual,
	); err != nil {
		return err
	}
	query := AggregateVerifierQuery{
		Path:   path.path,
		Length: path.length,
		Opening: backend.AggregateVerifierQuery{
			Commitment: commitment,
			Value:      value,
			Index:      index,
		},
	}
	collector.queryByID[identity] = len(collector.queries)
	collector.queries = append(collector.queries, query)

	return nil
}

func extensionMarkerScalar() [32]byte {
	return [32]byte(leafvector.EncodeExtensionMarker())
}

func compareAggregateVerifierQueries(
	left AggregateVerifierQuery,
	right AggregateVerifierQuery,
) int {
	if comparison := bytes.Compare(left.Path[:left.Length], right.Path[:right.Length]); comparison != 0 {
		return comparison
	}

	return int(left.Opening.Index) - int(right.Opening.Index)
}

type aggregateVerifierOpeningIdentity struct {
	commitment [backend.CommitmentSize]byte
	index      uint8
}

func consolidateAggregateVerifierQueries(
	ctx context.Context,
	queries []AggregateVerifierQuery,
) ([]AggregateVerifierQuery, error) {
	retained := queries[:0]
	seen := make(map[aggregateVerifierOpeningIdentity]int, len(queries))
	for index := range queries {
		if err := checkTreeProofContext(ctx); err != nil {
			return nil, err
		}
		key, err := queries[index].Opening.Commitment.DeduplicationKey()
		if err != nil {
			return nil, errInvalidAggregateVerifierQuery
		}
		identity := aggregateVerifierOpeningIdentity{
			commitment: key,
			index:      queries[index].Opening.Index,
		}
		if existing, duplicate := seen[identity]; duplicate {
			if retained[existing].Opening.Value != queries[index].Opening.Value {
				return nil, errInvalidAggregateVerifierQuery
			}
			continue
		}
		seen[identity] = len(retained)
		retained = append(retained, queries[index])
	}

	return retained, nil
}

func checkAggregateVerifierQueryResource(
	resource AggregateVerifierQueryResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &AggregateVerifierQueryResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}

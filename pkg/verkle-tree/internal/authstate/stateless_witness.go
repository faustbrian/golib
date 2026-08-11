package authstate

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	internalprofile "github.com/faustbrian/golib/pkg/verkle-tree/internal/profile"
)

const (
	statelessWitnessMagicBytes      = 4
	statelessWitnessProfileIDBytes  = 1
	statelessWitnessVersionBytes    = 2
	statelessWitnessEncodingBytes   = 2
	statelessWitnessLengthBytes     = 4
	statelessWitnessCountBytes      = 4
	statelessWitnessUpdateBytes     = 1 + 32 + 32
	statelessWitnessUpdateScratch   = uint64(192)
	statelessWitnessRelationScratch = uint64(256)

	statelessWitnessHeaderBytes = statelessWitnessMagicBytes +
		statelessWitnessProfileIDBytes +
		statelessWitnessVersionBytes +
		statelessWitnessEncodingBytes +
		statelessWitnessLengthBytes +
		statelessWitnessCountBytes +
		backend.RootSize
	maxStatelessWitnessEncodedBytes = statelessWitnessHeaderBytes +
		int(maxStatelessUpdates)*statelessWitnessUpdateBytes +
		maxTreeProofEncodedBytes
)

var (
	statelessWitnessMagic = [statelessWitnessMagicBytes]byte{'V', 'K', 'W', 'T'}

	errInvalidStatelessWitness       = errors.New("invalid stateless witness")
	errInvalidStatelessWitnessLimits = errors.New(
		"invalid stateless witness limits",
	)
	errInvalidStatelessWitnessEncodingLimits = errors.New(
		"invalid stateless witness encoding limits",
	)
	errInvalidStatelessWitnessDecodingLimits = errors.New(
		"invalid stateless witness decoding limits",
	)
	errInvalidStatelessWitnessEncoding = errors.New(
		"invalid canonical stateless witness encoding",
	)
	errStatelessPostRootMismatch = errors.New(
		"stateless witness post-state root mismatch",
	)
	errStatelessWitnessResource = errors.New(
		"stateless witness resource limit exceeded",
	)
)

// StatelessWitnessLimits bounds construction of one immutable update witness.
type StatelessWitnessLimits struct {
	MaxUpdates        uint32
	MaxTemporaryBytes uint64
}

func (limits StatelessWitnessLimits) validate() error {
	if limits.MaxUpdates == 0 ||
		limits.MaxUpdates > maxStatelessUpdates ||
		limits.MaxTemporaryBytes == 0 {
		return errInvalidStatelessWitnessLimits
	}

	return nil
}

// StatelessWitnessEncodingLimits bounds canonical witness serialization.
type StatelessWitnessEncodingLimits struct {
	MaxWitnessBytes   uint64
	MaxProofBytes     uint64
	MaxTemporaryBytes uint64
}

func (limits StatelessWitnessEncodingLimits) validate() error {
	if limits.MaxWitnessBytes == 0 ||
		limits.MaxWitnessBytes > uint64(maxStatelessWitnessEncodedBytes) ||
		limits.MaxProofBytes == 0 ||
		limits.MaxProofBytes > uint64(maxTreeProofEncodedBytes) ||
		limits.MaxTemporaryBytes == 0 {
		return errInvalidStatelessWitnessEncodingLimits
	}

	return nil
}

// StatelessWitnessDecodingLimits bounds hostile witness decoding. The
// post-root point-decode limit must equal one. Proof limits separately bound
// every embedded proof resource.
type StatelessWitnessDecodingLimits struct {
	MaxWitnessBytes         uint64
	MaxUpdates              uint32
	MaxPostRootPointDecodes uint32
	MaxTemporaryBytes       uint64
	Proof                   TreeProofDecodingLimits
}

func (limits StatelessWitnessDecodingLimits) validate() error {
	if limits.MaxWitnessBytes == 0 ||
		limits.MaxWitnessBytes > uint64(maxStatelessWitnessEncodedBytes) ||
		limits.MaxUpdates == 0 ||
		limits.MaxUpdates > maxStatelessUpdates ||
		limits.MaxPostRootPointDecodes != 1 ||
		limits.MaxTemporaryBytes == 0 ||
		limits.Proof.validate() != nil {
		return errInvalidStatelessWitnessDecodingLimits
	}

	return nil
}

// StatelessWitnessResource identifies one bounded witness resource.
type StatelessWitnessResource uint8

const (
	// StatelessWitnessResourceBytes counts canonical witness bytes.
	StatelessWitnessResourceBytes StatelessWitnessResource = iota + 1
	// StatelessWitnessResourceProofBytes counts the embedded canonical proof.
	StatelessWitnessResourceProofBytes
	// StatelessWitnessResourceUpdates counts bound update records.
	StatelessWitnessResourceUpdates
	// StatelessWitnessResourceTemporaryBytes counts conservative owned scratch.
	StatelessWitnessResourceTemporaryBytes
)

// StatelessWitnessResourceError reports one rejected witness budget without
// disclosing keys, values, roots, proofs, or witness bytes.
type StatelessWitnessResourceError struct {
	Resource StatelessWitnessResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *StatelessWitnessResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errStatelessWitnessResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes StatelessWitnessResourceError match the resource sentinel.
func (err *StatelessWitnessResourceError) Unwrap() error {
	return errStatelessWitnessResource
}

// StatelessWitness canonically binds one complete pre-state proof, a non-empty
// update batch, and one claimed post-state root. Construction does not verify
// the proof or establish that the claimed post-state root is correct.
type StatelessWitness struct {
	profile  internalprofile.Profile
	proof    TreeProof
	updates  []Update
	postRoot backend.Root
	valid    bool
}

// ApplyWitness verifies the complete pre-state proof, derives the post-state
// root from the bound updates, and rejects a different claimed post-state root.
func (updater *StatelessUpdater) ApplyWitness(
	ctx context.Context,
	witness StatelessWitness,
	verificationLimits ProofVerificationLimits,
	limits StatelessUpdateLimits,
) (backend.Root, error) {
	if err := witness.validate(ctx); err != nil {
		return backend.Root{}, err
	}
	root, err := updater.Apply(
		ctx,
		witness.proof,
		witness.updates,
		verificationLimits,
		limits,
	)
	if err != nil {
		return backend.Root{}, err
	}
	actual, _ := root.Bytes()
	expected, _ := witness.postRoot.Bytes()
	if actual != expected {
		return backend.Root{}, errStatelessPostRootMismatch
	}

	return root, nil
}

// NewStatelessWitness validates, canonicalizes, and owns one witness.
func NewStatelessWitness(
	ctx context.Context,
	proof TreeProof,
	updates []Update,
	postRoot backend.Root,
	limits StatelessWitnessLimits,
) (StatelessWitness, error) {
	if err := checkTreeProofContext(ctx); err != nil {
		return StatelessWitness{}, err
	}
	if err := limits.validate(); err != nil {
		return StatelessWitness{}, err
	}
	profile, err := proof.Profile()
	if err != nil {
		return StatelessWitness{}, errInvalidStatelessWitness
	}
	if _, err := postRoot.Profile(); err != nil {
		return StatelessWitness{}, errInvalidStatelessWitness
	}
	if len(updates) == 0 {
		return StatelessWitness{}, errInvalidStatelessWitness
	}
	if err := checkStatelessWitnessResource(
		StatelessWitnessResourceUpdates,
		uint64(limits.MaxUpdates),
		uint64(len(updates)),
	); err != nil {
		return StatelessWitness{}, err
	}
	temporaryBytes := uint64(len(updates))*2*statelessWitnessUpdateScratch +
		statelessWitnessRelationBytes(
			uint64(len(proof.claims.claims)), uint64(len(updates)),
		)
	if err := checkStatelessWitnessResource(
		StatelessWitnessResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return StatelessWitness{}, err
	}
	owned := append([]Update(nil), updates...)
	if err := sortUpdates(ctx, owned); err != nil {
		return StatelessWitness{}, err
	}
	for index := range owned {
		if err := checkTreeProofContext(ctx); err != nil {
			return StatelessWitness{}, err
		}
		if err := owned[index].validate(); err != nil {
			return StatelessWitness{}, errInvalidStatelessWitness
		}
		if index > 0 && owned[index-1].key == owned[index].key {
			return StatelessWitness{}, errDuplicateKey
		}
	}
	if err := validateStatelessWitnessClaims(
		ctx, proof.claims, proof.stemPaths, owned,
	); err != nil {
		return StatelessWitness{}, err
	}

	return StatelessWitness{
		profile: profile, proof: proof, updates: owned, postRoot: postRoot, valid: true,
	}, nil
}

// Proof returns the complete immutable pre-state proof.
func (witness StatelessWitness) Proof() (TreeProof, error) {
	if err := witness.validateContainer(); err != nil {
		return TreeProof{}, err
	}

	return witness.proof, nil
}

// Updates returns a caller-owned copy in canonical key order.
func (witness StatelessWitness) Updates(ctx context.Context) ([]Update, error) {
	if err := witness.validate(ctx); err != nil {
		return nil, err
	}
	updates := make([]Update, len(witness.updates))
	for index := range witness.updates {
		if err := checkTreeProofContext(ctx); err != nil {
			return nil, err
		}
		updates[index] = witness.updates[index]
	}

	return updates, nil
}

// PostRoot returns the claimed post-state root.
func (witness StatelessWitness) PostRoot() (backend.Root, error) {
	if err := witness.validateContainer(); err != nil {
		return backend.Root{}, err
	}

	return witness.postRoot, nil
}

// Bytes returns the exact canonical profile-bound witness encoding.
func (witness StatelessWitness) Bytes(
	ctx context.Context,
	limits StatelessWitnessEncodingLimits,
) ([]byte, error) {
	if err := checkTreeProofContext(ctx); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if err := witness.validateContainer(); err != nil {
		return nil, err
	}
	proofSize := treeProofEncodedSize(witness.proof)
	encodedSize := uint64(statelessWitnessHeaderBytes) +
		uint64(len(witness.updates))*statelessWitnessUpdateBytes + proofSize
	if err := checkStatelessWitnessResource(
		StatelessWitnessResourceBytes,
		limits.MaxWitnessBytes,
		encodedSize,
	); err != nil {
		return nil, err
	}
	if err := checkStatelessWitnessResource(
		StatelessWitnessResourceProofBytes,
		limits.MaxProofBytes,
		proofSize,
	); err != nil {
		return nil, err
	}
	temporaryBytes := encodedSize + proofSize + statelessWitnessRelationBytes(
		uint64(len(witness.proof.claims.claims)),
		uint64(len(witness.updates)),
	)
	if err := checkStatelessWitnessResource(
		StatelessWitnessResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return nil, err
	}
	if err := witness.validate(ctx); err != nil {
		return nil, err
	}
	proofBytes, err := witness.proof.Bytes(ctx, TreeProofEncodingLimits{
		MaxProofBytes: limits.MaxProofBytes, MaxTemporaryBytes: limits.MaxProofBytes,
	})
	if err != nil {
		return nil, err
	}
	postRootBytes, _ := witness.postRoot.Bytes()
	encoded := make([]byte, int(encodedSize))
	copy(encoded, statelessWitnessMagic[:])
	offset := statelessWitnessMagicBytes
	encoded[offset] = byte(witness.profile.ID())
	offset += statelessWitnessProfileIDBytes
	binary.BigEndian.PutUint16(encoded[offset:], witness.profile.Version())
	offset += statelessWitnessVersionBytes
	binary.BigEndian.PutUint16(encoded[offset:], witness.profile.EncodingVersion())
	offset += statelessWitnessEncodingBytes
	binary.BigEndian.PutUint32(encoded[offset:], uint32(proofSize))
	offset += statelessWitnessLengthBytes
	binary.BigEndian.PutUint32(encoded[offset:], uint32(len(witness.updates)))
	offset += statelessWitnessCountBytes
	copy(encoded[offset:], postRootBytes[:])
	offset += backend.RootSize
	for index := range witness.updates {
		if err := checkTreeProofContext(ctx); err != nil {
			return nil, err
		}
		update := witness.updates[index]
		encoded[offset] = byte(update.kind)
		offset++
		copy(encoded[offset:], update.key[:])
		offset += len(update.key)
		copy(encoded[offset:], update.value[:])
		offset += len(update.value)
	}
	copy(encoded[offset:], proofBytes)
	if err := checkTreeProofContext(ctx); err != nil {
		return nil, err
	}

	return encoded, nil
}

// DecodeStatelessWitness strictly decodes one canonical witness. It rejects
// trailing bytes and validates profile, counts, ordering, and aggregate limits
// before any point or scalar decoding.
func DecodeStatelessWitness(
	ctx context.Context,
	encoded []byte,
	limits StatelessWitnessDecodingLimits,
) (StatelessWitness, error) {
	if err := checkTreeProofContext(ctx); err != nil {
		return StatelessWitness{}, err
	}
	if err := limits.validate(); err != nil {
		return StatelessWitness{}, err
	}
	if err := checkStatelessWitnessResource(
		StatelessWitnessResourceBytes,
		limits.MaxWitnessBytes,
		uint64(len(encoded)),
	); err != nil {
		return StatelessWitness{}, err
	}
	var header [statelessWitnessHeaderBytes]byte
	if copy(header[:], encoded) != statelessWitnessHeaderBytes {
		return StatelessWitness{}, errInvalidStatelessWitnessEncoding
	}
	if [statelessWitnessMagicBytes]byte(encoded[:statelessWitnessMagicBytes]) !=
		statelessWitnessMagic {
		return StatelessWitness{}, errInvalidStatelessWitnessEncoding
	}
	profile := internalprofile.BandersnatchIPA256V0Profile()
	if encoded[statelessWitnessMagicBytes] != byte(profile.ID()) ||
		binary.BigEndian.Uint16(encoded[5:7]) != profile.Version() ||
		binary.BigEndian.Uint16(encoded[7:9]) != profile.EncodingVersion() {
		return StatelessWitness{}, internalprofile.ErrUnsupported
	}
	proofSize := binary.BigEndian.Uint32(encoded[9:13])
	updateCount := binary.BigEndian.Uint32(encoded[13:17])
	if updateCount == 0 {
		return StatelessWitness{}, errInvalidStatelessWitnessEncoding
	}
	if err := checkStatelessWitnessResource(
		StatelessWitnessResourceUpdates,
		uint64(limits.MaxUpdates),
		uint64(updateCount),
	); err != nil {
		return StatelessWitness{}, err
	}
	if err := checkStatelessWitnessResource(
		StatelessWitnessResourceProofBytes,
		limits.Proof.MaxProofBytes,
		uint64(proofSize),
	); err != nil {
		return StatelessWitness{}, err
	}
	encodedSize := uint64(statelessWitnessHeaderBytes) +
		uint64(updateCount)*statelessWitnessUpdateBytes + uint64(proofSize)
	if encodedSize != uint64(len(encoded)) {
		return StatelessWitness{}, errInvalidStatelessWitnessEncoding
	}
	if proofSize < treeProofHeaderBytes {
		return StatelessWitness{}, errInvalidStatelessWitnessEncoding
	}
	proofOffset := statelessWitnessHeaderBytes + int(updateCount)*statelessWitnessUpdateBytes
	claimCountOffset := proofOffset + treeProofMagicBytes +
		treeProofProfileIDBytes + treeProofVersionBytes +
		treeProofEncodingBytes + backend.RootSize
	claimCount := binary.BigEndian.Uint32(
		encoded[claimCountOffset : claimCountOffset+treeProofCountBytes],
	)
	temporaryBytes := encodedSize +
		uint64(updateCount)*2*statelessWitnessUpdateScratch +
		statelessWitnessRelationBytes(uint64(claimCount), uint64(updateCount))
	if err := checkStatelessWitnessResource(
		StatelessWitnessResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return StatelessWitness{}, err
	}
	updatesOffset := statelessWitnessHeaderBytes
	proofOffset = updatesOffset + int(updateCount)*statelessWitnessUpdateBytes
	updates := make([]Update, updateCount)
	for index := range updates {
		if err := checkTreeProofContext(ctx); err != nil {
			return StatelessWitness{}, err
		}
		offset := updatesOffset + index*statelessWitnessUpdateBytes
		record := encoded[offset : offset+statelessWitnessUpdateBytes]
		kind := UpdateKind(record[0])
		if kind != UpdateSet && kind != UpdateDelete {
			return StatelessWitness{}, errInvalidStatelessWitnessEncoding
		}
		copy(updates[index].key[:], record[1:33])
		copy(updates[index].value[:], record[33:])
		updates[index].kind = kind
		if updates[index].validate() != nil {
			return StatelessWitness{}, errInvalidStatelessWitnessEncoding
		}
		if index != 0 && !statelessWitnessUpdateKeysAreStrictlyIncreasing(
			updates[index-1].key, updates[index].key,
		) {
			return StatelessWitness{}, errInvalidStatelessWitnessEncoding
		}
	}
	postRootOffset := statelessWitnessMagicBytes +
		statelessWitnessProfileIDBytes + statelessWitnessVersionBytes +
		statelessWitnessEncodingBytes + statelessWitnessLengthBytes +
		statelessWitnessCountBytes
	postRoot, err := backend.DecodeRoot(
		ctx,
		encoded[postRootOffset:postRootOffset+backend.RootSize],
		backend.RootLimits{
			MaxRootBytes:    backend.RootSize,
			MaxPointDecodes: limits.MaxPostRootPointDecodes,
		},
	)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return StatelessWitness{}, errors.Join(errTreeProofCancelled, err)
		}
		return StatelessWitness{}, errInvalidStatelessWitnessEncoding
	}
	proof, err := DecodeTreeProof(ctx, encoded[proofOffset:], limits.Proof)
	if err != nil {
		return StatelessWitness{}, err
	}

	witness := StatelessWitness{
		profile: profile, proof: proof, updates: updates, postRoot: postRoot, valid: true,
	}
	if err := witness.validate(ctx); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return StatelessWitness{}, errors.Join(errTreeProofCancelled, err)
		}
		return StatelessWitness{}, errInvalidStatelessWitnessEncoding
	}

	return witness, nil
}

func (witness StatelessWitness) validateContainer() error {
	if !witness.valid {
		return errInvalidStatelessWitness
	}
	if witness.profile.Validate() != nil {
		return errInvalidStatelessWitness
	}
	if len(witness.updates) == 0 {
		return errInvalidStatelessWitness
	}
	if witness.proof.validate() != nil {
		return errInvalidStatelessWitness
	}
	if _, err := witness.postRoot.Profile(); err != nil {
		return errInvalidStatelessWitness
	}

	return nil
}

func (witness StatelessWitness) validate(ctx context.Context) error {
	if err := checkTreeProofContext(ctx); err != nil {
		return err
	}
	if err := witness.validateContainer(); err != nil {
		return err
	}
	for index := range witness.updates {
		if err := checkTreeProofContext(ctx); err != nil {
			return err
		}
		if witness.updates[index].validate() != nil {
			return errInvalidStatelessWitness
		}
	}
	if err := validateStatelessWitnessClaims(
		ctx,
		witness.proof.claims,
		witness.proof.stemPaths,
		witness.updates,
	); err != nil {
		return err
	}

	return nil
}

func validateStatelessWitnessClaims(
	ctx context.Context,
	claims ClaimSet,
	stemPaths []StemPath,
	updates []Update,
) error {
	if err := claims.validate(); err != nil {
		return err
	}
	expected := make(map[Key]struct{}, min(len(claims.claims), len(updates)))
	deletionStems := make(map[Stem]struct{})
	setStems := make(map[Stem]struct{})
	for index := range updates {
		if err := checkTreeProofContext(ctx); err != nil {
			return err
		}
		if index > 0 && !statelessWitnessUpdateKeysAreStrictlyIncreasing(
			updates[index-1].key, updates[index].key,
		) {
			return errInvalidStatelessWitness
		}
		stem := Stem(updates[index].key[:31])
		expected[updates[index].key] = struct{}{}
		if updates[index].kind == UpdateSet {
			setStems[stem] = struct{}{}

			continue
		}
		claim, found := statelessValidatedClaimLookup(
			claims.claims, updates[index].key,
		)
		if !found {
			return errInvalidStatelessWitness
		}
		if claim.kind == ClaimMembership {
			deletionStems[stem] = struct{}{}
		}
	}
	for stem := range deletionStems {
		if err := checkTreeProofContext(ctx); err != nil {
			return err
		}
		if err := statelessWitnessCompleteDeletionStem(
			ctx, claims.claims, stemPaths, expected, setStems, stem,
		); err != nil {
			return err
		}
	}
	if len(expected) != len(claims.claims) {
		return errInvalidStatelessWitness
	}
	for key := range expected {
		if err := checkTreeProofContext(ctx); err != nil {
			return err
		}
		if _, found := statelessValidatedClaimLookup(claims.claims, key); !found {
			return errInvalidStatelessWitness
		}
	}

	return nil
}

func statelessWitnessCompleteDeletionStem(
	ctx context.Context,
	claims []Claim,
	stemPaths []StemPath,
	expected map[Key]struct{},
	setStems map[Stem]struct{},
	stem Stem,
) error {
	if _, set := setStems[stem]; set {
		return nil
	}
	retained, disclosed, err := statelessWitnessStemAuxiliaries(
		ctx, claims, expected, stem,
	)
	if err != nil {
		return err
	}
	if len(retained) > 1 {
		return errInvalidStatelessWitness
	}
	if len(retained) == 1 {
		expected[retained[0]] = struct{}{}

		return nil
	}
	if !disclosed {
		return nil
	}
	path, found := statelessWitnessStemPath(stemPaths, stem)
	if !found || path.kind != StemPathPresent {
		return errInvalidStatelessWitness
	}
	for suffix := range backend.VectorWidth {
		if err := checkTreeProofContext(ctx); err != nil {
			return err
		}
		var key Key
		copy(key[:31], stem[:])
		key[31] = byte(suffix)
		expected[key] = struct{}{}
		if len(expected) > len(claims) {
			return errInvalidStatelessWitness
		}
	}
	for parentDepth := uint8(1); parentDepth < path.depth; parentDepth++ {
		parent := makeStatelessPath(stem[:parentDepth])
		for child := range backend.VectorWidth {
			if err := checkTreeProofContext(ctx); err != nil {
				return err
			}
			expected[statelessTopologyProbe(parent, byte(child))] = struct{}{}
			if len(expected) > len(claims) {
				return errInvalidStatelessWitness
			}
		}
	}

	return nil
}

func statelessWitnessUpdateKeysAreStrictlyIncreasing(
	previous Key,
	current Key,
) bool {
	return bytes.Compare(previous[:], current[:]) < 0
}

func statelessValidatedClaimLookup(claims []Claim, key Key) (Claim, bool) {
	index, found := slices.BinarySearchFunc(
		claims,
		key,
		func(claim Claim, candidate Key) int {
			return compareKey(claim.key, candidate)
		},
	)
	if !found {
		return Claim{}, false
	}

	return claims[index], true
}

func statelessWitnessStemAuxiliaries(
	ctx context.Context,
	claims []Claim,
	expected map[Key]struct{},
	stem Stem,
) ([]Key, bool, error) {
	start := sort.Search(len(claims), func(index int) bool {
		return bytes.Compare(claims[index].key[:31], stem[:]) >= 0
	})
	retained := make([]Key, 0, 2)
	disclosed := false
	for index := start; index < len(claims); index++ {
		if err := checkTreeProofContext(ctx); err != nil {
			return nil, false, err
		}
		claim := claims[index]
		if Stem(claim.key[:31]) != stem {
			break
		}
		if _, update := expected[claim.key]; update {
			continue
		}
		if claim.kind == ClaimMembership {
			retained = append(retained, claim.key)
		} else {
			disclosed = true
		}
	}

	return retained, disclosed, nil
}

func statelessWitnessRelationBytes(
	claimCount uint64,
	updateCount uint64,
) uint64 {
	relationCount := max(claimCount, updateCount) - updateCount

	return relationCount * 2 * statelessWitnessRelationScratch
}

func statelessWitnessStemPath(
	paths []StemPath,
	stem Stem,
) (StemPath, bool) {
	index := sort.Search(len(paths), func(index int) bool {
		return bytes.Compare(paths[index].stem[:], stem[:]) >= 0
	})
	if index == len(paths) || paths[index].stem != stem {
		return StemPath{}, false
	}

	return paths[index], true
}

func treeProofEncodedSize(proof TreeProof) uint64 {
	return uint64(treeProofFixedBytes) +
		uint64(len(proof.claims.claims))*claimEncodedBytes +
		uint64(len(proof.stemPaths))*stemPathEncodedBytes +
		uint64(len(proof.commitments))*pathCommitmentEncodedBytes
}

func checkStatelessWitnessResource(
	resource StatelessWitnessResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &StatelessWitnessResourceError{Resource: resource, Limit: limit, Actual: actual}
}

// IsInvalidStatelessWitnessError reports an unusable witness container.
func IsInvalidStatelessWitnessError(err error) bool {
	return errors.Is(err, errInvalidStatelessWitness)
}

// IsInvalidStatelessWitnessEncodingError reports malformed canonical bytes.
func IsInvalidStatelessWitnessEncodingError(err error) bool {
	return errors.Is(err, errInvalidStatelessWitnessEncoding)
}

// IsInvalidStatelessWitnessLimitsError reports invalid witness construction or
// codec limits, as distinct from malformed witness material.
func IsInvalidStatelessWitnessLimitsError(err error) bool {
	return errors.Is(err, errInvalidStatelessWitnessLimits) ||
		errors.Is(err, errInvalidStatelessWitnessEncodingLimits) ||
		errors.Is(err, errInvalidStatelessWitnessDecodingLimits)
}

// IsStatelessPostRootMismatchError reports a validly derived root that differs
// from the root claimed by the witness.
func IsStatelessPostRootMismatchError(err error) bool {
	return errors.Is(err, errStatelessPostRootMismatch)
}

// IsInvalidStatelessUpdaterError reports an unusable updater receiver.
func IsInvalidStatelessUpdaterError(err error) bool {
	return errors.Is(err, errInvalidStatelessUpdater)
}

// IsInvalidStatelessUpdateError reports a malformed update request.
func IsInvalidStatelessUpdateError(err error) bool {
	return errors.Is(err, errInvalidStatelessUpdate)
}

// IsIncompleteStatelessWitnessError reports omitted authenticated material.
func IsIncompleteStatelessWitnessError(err error) bool {
	return errors.Is(err, errIncompleteStatelessWitness)
}

// IsUnsupportedStatelessUpdateError reports a transition outside the
// currently implemented Set/Delete witness profile.
func IsUnsupportedStatelessUpdateError(err error) bool {
	return errors.Is(err, errUnsupportedStatelessUpdate)
}

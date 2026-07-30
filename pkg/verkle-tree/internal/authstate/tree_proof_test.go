package authstate

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
)

func TestTreeProofCanonicalizesAndBindsComponents(t *testing.T) {
	t.Parallel()

	keyPresent := testKey(0, 0)
	keyAbsentSuffix := testKey(0, 130)
	keyDifferent := testKey(1, 0)
	keyMissing := testKey(2, 0)
	claims, err := NewClaimSet(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]Claim{
			Absence(keyMissing),
			Absence(keyAbsentSuffix),
			Membership(keyPresent, Value{}),
			Absence(keyDifferent),
		},
		testClaimLimits(),
	)
	if err != nil {
		t.Fatalf("new claims: %v", err)
	}
	root := testProofRoot(t)
	opening := testRawOpeningProof(t)
	var differentExisting Stem
	differentExisting[0] = 1
	differentExisting[1] = 9
	stemPaths := []StemPath{
		MissingStemPath(stemFromKey(keyMissing), 1),
		DifferentStemPath(stemFromKey(keyDifferent), 1, differentExisting),
		PresentStemPath(stemFromKey(keyPresent), 1),
	}
	commitment := testProofCommitment(t)
	pathCommitments := []PathCommitment{
		mustPathCommitment(t, []byte{1}, commitment),
		mustPathCommitment(t, []byte{0, 3}, commitment),
		mustPathCommitment(t, []byte{0, 2}, commitment),
		mustPathCommitment(t, []byte{0}, commitment),
	}
	proof, err := NewTreeProof(
		context.Background(),
		root,
		claims,
		stemPaths,
		pathCommitments,
		opening,
		testTreeProofLimits(),
	)
	if err != nil {
		t.Fatalf("new tree proof: %v", err)
	}
	stemPaths[0] = PresentStemPath(Stem{9}, 1)
	pathCommitments[0] = mustPathCommitment(t, []byte{9}, commitment)

	if profile, profileErr := proof.Profile(); profileErr != nil ||
		profile != verkletree.ExperimentalBandersnatchIPA256V0() {
		t.Fatalf("proof profile = %#v, error = %v", profile, profileErr)
	}
	gotRoot, err := proof.Root()
	if err != nil {
		t.Fatalf("proof root: %v", err)
	}
	gotRootBytes, err := gotRoot.Bytes()
	if err != nil {
		t.Fatalf("proof root bytes: %v", err)
	}
	wantRootBytes, err := root.Bytes()
	if err != nil {
		t.Fatalf("root bytes: %v", err)
	}
	if gotRootBytes != wantRootBytes {
		t.Fatalf("proof root = %x, want %x", gotRootBytes, wantRootBytes)
	}
	gotClaims, err := proof.Claims()
	if err != nil {
		t.Fatalf("proof claims: %v", err)
	}
	if count, countErr := gotClaims.Count(); countErr != nil || count != 4 {
		t.Fatalf("proof claim count = %d, error = %v", count, countErr)
	}
	gotStemPaths, err := proof.StemPaths(context.Background())
	if err != nil {
		t.Fatalf("proof stem paths: %v", err)
	}
	if len(gotStemPaths) != 3 {
		t.Fatalf("stem path count = %d, want 3", len(gotStemPaths))
	}
	for index, first := range []byte{0, 1, 2} {
		stem, stemErr := gotStemPaths[index].Stem()
		if stemErr != nil || stem[0] != first {
			t.Fatalf("stem path %d = %x, error = %v", index, stem, stemErr)
		}
	}
	gotCommitments, err := proof.PathCommitments(context.Background())
	if err != nil {
		t.Fatalf("proof path commitments: %v", err)
	}
	wantPaths := [][]byte{{0}, {0, 2}, {0, 3}, {1}}
	for index, want := range wantPaths {
		got, pathErr := gotCommitments[index].Path()
		if pathErr != nil || !bytes.Equal(got, want) {
			t.Fatalf("path %d = %x, want %x, error = %v", index, got, want, pathErr)
		}
		got[0] = 0xff
	}
	repeated, err := proof.PathCommitments(context.Background())
	if err != nil {
		t.Fatalf("repeat path commitments: %v", err)
	}
	if got, pathErr := repeated[0].Path(); pathErr != nil || !bytes.Equal(got, []byte{0}) {
		t.Fatalf("retained path = %x, error = %v", got, pathErr)
	}
	gotOpening, err := proof.OpeningProof()
	if err != nil {
		t.Fatalf("proof opening: %v", err)
	}
	gotOpeningBytes, err := gotOpening.Bytes()
	if err != nil {
		t.Fatalf("proof opening bytes: %v", err)
	}
	wantOpeningBytes, err := opening.Bytes()
	if err != nil {
		t.Fatalf("opening bytes: %v", err)
	}
	if gotOpeningBytes != wantOpeningBytes {
		t.Fatal("proof opening payload changed")
	}
}

func TestTreeProofRequiresExactCanonicalPathSet(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	claims := mustClaimSet(t, []Claim{Membership(key, testValue(1))})
	root := testProofRoot(t)
	opening := testRawOpeningProof(t)
	commitment := testProofCommitment(t)
	stemPaths := []StemPath{PresentStemPath(stemFromKey(key), 1)}
	required := []PathCommitment{
		mustPathCommitment(t, []byte{0}, commitment),
		mustPathCommitment(t, []byte{0, 2}, commitment),
	}
	tests := map[string][]PathCommitment{
		"missing": required[:1],
		"surplus": append(
			slices.Clone(required),
			mustPathCommitment(t, []byte{9}, commitment),
		),
		"duplicate": {
			required[0],
			required[0],
			required[1],
		},
		"replaced": {
			required[0],
			mustPathCommitment(t, []byte{0, 3}, commitment),
		},
	}
	for name, commitments := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewTreeProof(
				context.Background(),
				root,
				claims,
				stemPaths,
				commitments,
				opening,
				testTreeProofLimits(),
			)
			if !errors.Is(err, errInvalidTreeProof) {
				t.Fatalf("tree proof error = %v, want %v", err, errInvalidTreeProof)
			}
		})
	}
}

func TestTreeProofRejectsEmptyRootUntilEmptyProofSemanticsAreFixed(t *testing.T) {
	t.Parallel()

	snapshot := newTestSnapshot(t, nil)
	root, err := snapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("empty root: %v", err)
	}
	key := testKey(0, 0)
	_, err = NewTreeProof(
		context.Background(),
		root,
		mustClaimSet(t, []Claim{Absence(key)}),
		[]StemPath{MissingStemPath(stemFromKey(key), 1)},
		nil,
		testRawOpeningProof(t),
		testTreeProofLimits(),
	)
	if !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("empty-root proof error = %v, want %v", err, errInvalidTreeProof)
	}
}

func TestTreeProofPathDerivationStopsDuringClaimGrouping(t *testing.T) {
	t.Parallel()

	keyLow := testKey(0, 0)
	keyHigh := testKey(0, 255)
	claims := mustClaimSet(t, []Claim{
		Membership(keyLow, testValue(1)),
		Membership(keyHigh, testValue(2)),
	})
	canonicalClaims, err := claims.Claims(context.Background())
	if err != nil {
		t.Fatalf("canonical claims: %v", err)
	}

	_, err = derivePathMarkers(
		&stepContext{successfulChecks: 2},
		canonicalClaims,
		[]StemPath{PresentStemPath(stemFromKey(keyLow), 1)},
		33,
	)
	if !errors.Is(err, errTreeProofCancelled) ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("path derivation cancellation error = %v", err)
	}
}

func TestStemPathRejectsInvalidMetadata(t *testing.T) {
	t.Parallel()

	var queried Stem
	queried[0] = 7
	queried[1] = 8
	var existing Stem
	existing[0] = 7
	existing[1] = 9

	for name, path := range map[string]StemPath{
		"present":   PresentStemPath(queried, 1),
		"missing":   MissingStemPath(queried, 1),
		"different": DifferentStemPath(queried, 1, existing),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got, err := path.Stem(); err != nil || got != queried {
				t.Fatalf("stem = %x, error = %v", got, err)
			}
			if got, err := path.Depth(); err != nil || got != 1 {
				t.Fatalf("depth = %d, error = %v", got, err)
			}
			kind, err := path.Kind()
			if err != nil {
				t.Fatalf("kind: %v", err)
			}
			gotExisting, present, err := path.ExistingStem()
			if err != nil {
				t.Fatalf("existing stem: %v", err)
			}
			if kind == StemPathDifferent {
				if !present || gotExisting != existing {
					t.Fatalf("existing stem = %x, present = %t", gotExisting, present)
				}
			} else if present || gotExisting != (Stem{}) {
				t.Fatalf("unexpected existing stem = %x, present = %t", gotExisting, present)
			}
		})
	}
	if err := PresentStemPath(queried, 31).validate(); err != nil {
		t.Fatalf("maximum depth: %v", err)
	}

	var wrongPrefix Stem
	wrongPrefix[0] = 6
	tests := map[string]StemPath{
		"zero":                   {},
		"zero depth":             {stem: queried, kind: StemPathPresent, valid: true},
		"excessive depth":        {stem: queried, depth: 32, kind: StemPathPresent, valid: true},
		"unknown kind":           {stem: queried, depth: 1, kind: 99, valid: true},
		"present existing stem":  {stem: queried, existing: existing, depth: 1, kind: StemPathPresent, valid: true},
		"missing existing stem":  {stem: queried, existing: existing, depth: 1, kind: StemPathMissing, valid: true},
		"different same stem":    DifferentStemPath(queried, 1, queried),
		"different wrong prefix": DifferentStemPath(queried, 1, wrongPrefix),
	}
	for name, path := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := path.validate(); !errors.Is(err, errInvalidStemPath) {
				t.Fatalf("validation error = %v, want %v", err, errInvalidStemPath)
			}
		})
	}

	var zero StemPath
	if _, err := zero.Stem(); !errors.Is(err, errInvalidStemPath) {
		t.Fatalf("zero stem error = %v", err)
	}
	if _, err := zero.Depth(); !errors.Is(err, errInvalidStemPath) {
		t.Fatalf("zero depth error = %v", err)
	}
	if _, err := zero.Kind(); !errors.Is(err, errInvalidStemPath) {
		t.Fatalf("zero kind error = %v", err)
	}
	if _, _, err := zero.ExistingStem(); !errors.Is(err, errInvalidStemPath) {
		t.Fatalf("zero existing stem error = %v", err)
	}
}

func TestPathCommitmentValidatesAndOwnsPath(t *testing.T) {
	t.Parallel()

	commitment := testProofCommitment(t)
	input := []byte{1, 2, 3}
	value, err := NewPathCommitment(input, commitment)
	if err != nil {
		t.Fatalf("new path commitment: %v", err)
	}
	input[0] = 9
	got, err := value.Path()
	if err != nil || !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("path = %x, error = %v", got, err)
	}
	gotCommitment, err := value.Commitment()
	if err != nil {
		t.Fatalf("commitment: %v", err)
	}
	gotBytes, err := gotCommitment.Bytes()
	if err != nil {
		t.Fatalf("commitment bytes: %v", err)
	}
	wantBytes, err := commitment.Bytes()
	if err != nil || gotBytes != wantBytes {
		t.Fatalf("commitment bytes differ, error = %v", err)
	}
	maximum, err := NewPathCommitment(
		make([]byte, maxProofPathLength),
		commitment,
	)
	if err != nil {
		t.Fatalf("maximum path: %v", err)
	}
	if got, pathErr := maximum.Path(); pathErr != nil ||
		len(got) != maxProofPathLength {
		t.Fatalf("maximum path length = %d, error = %v", len(got), pathErr)
	}

	identity, err := newTestSnapshot(t, nil).Root()
	if err != nil {
		t.Fatalf("identity root: %v", err)
	}
	empty, err := NewPathCommitment([]byte{1}, identity)
	if err != nil {
		t.Fatalf("empty path commitment: %v", err)
	}
	gotEmpty, err := empty.Commitment()
	if err != nil {
		t.Fatalf("empty path commitment value: %v", err)
	}
	if isIdentity, identityErr := gotEmpty.IsIdentity(); identityErr != nil ||
		!isIdentity {
		t.Fatalf(
			"empty path identity = %t, error %v",
			isIdentity,
			identityErr,
		)
	}
	for name, candidate := range map[string]struct {
		path       []byte
		commitment backend.VectorCommitment
	}{
		"empty path":      {path: nil, commitment: commitment},
		"excessive path":  {path: make([]byte, maxProofPathLength+1), commitment: commitment},
		"zero commitment": {path: []byte{1}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewPathCommitment(candidate.path, candidate.commitment)
			if !errors.Is(err, errInvalidPathCommitment) {
				t.Fatalf("constructor error = %v, want %v", err, errInvalidPathCommitment)
			}
		})
	}

	for name, corrupt := range map[string]PathCommitment{
		"zero":        {},
		"zero length": {commitment: commitment, valid: true},
		"long length": {commitment: commitment, length: 33, valid: true},
		"bad point":   {length: 1, valid: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := corrupt.validate(); !errors.Is(err, errInvalidPathCommitment) {
				t.Fatalf("validation error = %v", err)
			}
		})
	}
	var zero PathCommitment
	if _, err := zero.Path(); !errors.Is(err, errInvalidPathCommitment) {
		t.Fatalf("zero path error = %v", err)
	}
	if _, err := zero.Commitment(); !errors.Is(err, errInvalidPathCommitment) {
		t.Fatalf("zero commitment error = %v", err)
	}
}

func TestTreeProofRejectsInvalidComponentsAndTopology(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	claims := mustClaimSet(t, []Claim{Membership(key, testValue(1))})
	root := testProofRoot(t)
	opening := testRawOpeningProof(t)
	commitment := testProofCommitment(t)
	present := []StemPath{PresentStemPath(stemFromKey(key), 1)}
	required := []PathCommitment{
		mustPathCommitment(t, []byte{0}, commitment),
		mustPathCommitment(t, []byte{0, 2}, commitment),
	}

	type proofCase struct {
		root        backend.Root
		claims      ClaimSet
		stemPaths   []StemPath
		commitments []PathCommitment
		opening     backend.OpeningProof
		limits      TreeProofLimits
		expected    error
	}
	valid := proofCase{
		root:        root,
		claims:      claims,
		stemPaths:   present,
		commitments: required,
		opening:     opening,
		limits:      testTreeProofLimits(),
		expected:    errInvalidTreeProof,
	}
	tests := map[string]proofCase{
		"zero root": func() proofCase {
			value := valid
			value.root = backend.Root{}

			return value
		}(),
		"zero claims": func() proofCase {
			value := valid
			value.claims = ClaimSet{}

			return value
		}(),
		"zero opening": func() proofCase {
			value := valid
			value.opening = backend.OpeningProof{}

			return value
		}(),
		"invalid limits": func() proofCase {
			value := valid
			value.limits = TreeProofLimits{}
			value.expected = errInvalidTreeProofLimits

			return value
		}(),
		"invalid stem path": func() proofCase {
			value := valid
			value.stemPaths = []StemPath{{}}
			value.expected = errInvalidStemPath

			return value
		}(),
		"invalid commitment": func() proofCase {
			value := valid
			value.commitments = []PathCommitment{{}}
			value.expected = errInvalidPathCommitment

			return value
		}(),
		"missing stem metadata": func() proofCase {
			value := valid
			value.stemPaths = nil
			value.commitments = nil

			return value
		}(),
		"surplus stem metadata": func() proofCase {
			value := valid
			value.stemPaths = append(
				slices.Clone(present),
				MissingStemPath(Stem{1}, 1),
			)

			return value
		}(),
		"wrong stem metadata": func() proofCase {
			value := valid
			value.stemPaths = []StemPath{PresentStemPath(Stem{1}, 1)}

			return value
		}(),
		"duplicate stem metadata": func() proofCase {
			value := valid
			value.stemPaths = []StemPath{present[0], present[0]}

			return value
		}(),
		"membership missing child": func() proofCase {
			value := valid
			value.stemPaths = []StemPath{MissingStemPath(stemFromKey(key), 1)}
			value.commitments = nil

			return value
		}(),
		"membership different stem": func() proofCase {
			value := valid
			value.stemPaths = []StemPath{
				DifferentStemPath(stemFromKey(key), 1, Stem{0, 1}),
			}
			value.commitments = required[:1]

			return value
		}(),
	}
	for name, candidate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewTreeProof(
				context.Background(),
				candidate.root,
				candidate.claims,
				candidate.stemPaths,
				candidate.commitments,
				candidate.opening,
				candidate.limits,
			)
			if !errors.Is(err, candidate.expected) {
				t.Fatalf("tree proof error = %v, want %v", err, candidate.expected)
			}
		})
	}
}

func TestTreeProofRejectsConflictingSharedTopology(t *testing.T) {
	t.Parallel()

	root := testProofRoot(t)
	opening := testRawOpeningProof(t)
	commitment := testProofCommitment(t)
	var low Stem
	low[0] = 4
	var high Stem
	high[0] = 4
	high[1] = 1
	var lowKey Key
	copy(lowKey[:31], low[:])
	var highKey Key
	copy(highKey[:31], high[:])
	claims := mustClaimSet(t, []Claim{Absence(lowKey), Absence(highKey)})

	tests := map[string][]StemPath{
		"missing versus internal": {
			MissingStemPath(low, 1),
			PresentStemPath(high, 2),
		},
		"leaf versus internal": {
			PresentStemPath(low, 1),
			PresentStemPath(high, 2),
		},
		"different leaves": {
			DifferentStemPath(low, 1, Stem{4, 8}),
			DifferentStemPath(high, 1, Stem{4, 9}),
		},
	}
	allPotentialPaths := []PathCommitment{
		mustPathCommitment(t, []byte{4}, commitment),
		mustPathCommitment(t, []byte{4, 1}, commitment),
		mustPathCommitment(t, []byte{4, 1, 2}, commitment),
		mustPathCommitment(t, []byte{4, 2}, commitment),
	}
	for name, paths := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewTreeProof(
				context.Background(),
				root,
				claims,
				paths,
				allPotentialPaths,
				opening,
				testTreeProofLimits(),
			)
			if !errors.Is(err, errInvalidTreeProof) {
				t.Fatalf("conflicting topology error = %v", err)
			}
		})
	}
}

func TestTreeProofDeduplicatesSharedSuffixCommitmentPaths(t *testing.T) {
	t.Parallel()

	keyZero := testKey(0, 0)
	keyOne := testKey(0, 1)
	commitment := testProofCommitment(t)
	proof, err := NewTreeProof(
		context.Background(),
		testProofRoot(t),
		mustClaimSet(t, []Claim{
			Membership(keyZero, testValue(1)),
			Membership(keyOne, testValue(2)),
		}),
		[]StemPath{PresentStemPath(stemFromKey(keyZero), 1)},
		[]PathCommitment{
			mustPathCommitment(t, []byte{0}, commitment),
			mustPathCommitment(t, []byte{0, 2}, commitment),
		},
		testRawOpeningProof(t),
		testTreeProofLimits(),
	)
	if err != nil {
		t.Fatalf("new tree proof: %v", err)
	}
	got, err := proof.PathCommitments(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("commitments = %d, error = %v", len(got), err)
	}
}

func TestTreeProofInternalBoundariesFailClosed(t *testing.T) {
	t.Parallel()

	keyZero := testKey(0, 0)
	keyOne := testKey(1, 0)
	claims := mustClaimSet(t, []Claim{
		Absence(keyZero),
		Absence(keyOne),
	})
	canonical, err := claims.Claims(context.Background())
	if err != nil {
		t.Fatalf("canonical claims: %v", err)
	}
	if _, err := derivePathMarkers(
		context.Background(),
		canonical,
		[]StemPath{MissingStemPath(stemFromKey(keyZero), 1)},
		33,
	); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("omitted stem metadata error = %v", err)
	}
	if _, err := derivePathMarkers(
		context.Background(),
		canonical[:1],
		[]StemPath{{
			stem:  stemFromKey(keyZero),
			depth: 1,
			kind:  99,
			valid: true,
		}},
		32,
	); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("unknown path kind error = %v", err)
	}
	if err := requireAbsenceClaims(
		&stepContext{},
		canonical[:1],
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("absence cancellation error = %v", err)
	}
	if _, err := derivePathMarkers(
		&stepContext{successfulChecks: 2},
		canonical[:1],
		[]StemPath{MissingStemPath(stemFromKey(keyZero), 2)},
		63,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("internal-path cancellation error = %v", err)
	}
	deepMarkers, err := derivePathMarkers(
		&stepContext{successfulChecks: 50},
		canonical[:1],
		[]StemPath{MissingStemPath(stemFromKey(keyZero), 3)},
		94,
	)
	if err != nil {
		t.Fatalf("bounded internal path: %v", err)
	}
	if len(deepMarkers) != 3 ||
		deepMarkers[0].length != 1 ||
		deepMarkers[1].length != 2 ||
		deepMarkers[2].length != 3 {
		t.Fatalf("internal path markers = %#v", deepMarkers)
	}

	marker := newPathMarker([]byte{0}, pathMarkerStem, Stem{0})
	if err := matchExpectedCommitments(
		&stepContext{successfulChecks: 1},
		[]pathMarker{marker, marker},
		nil,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("marker grouping cancellation error = %v", err)
	}

	commitment := mustPathCommitment(t, []byte{0}, testProofCommitment(t))
	for name, markers := range map[string][]pathMarker{
		"kind conflict": {
			newPathMarker([]byte{0}, pathMarkerInternal, Stem{}),
			newPathMarker([]byte{0}, pathMarkerStem, Stem{0}),
		},
		"leaf conflict": {
			newPathMarker([]byte{0}, pathMarkerStem, Stem{0}),
			newPathMarker([]byte{0}, pathMarkerStem, Stem{0, 1}),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := matchExpectedCommitments(
				context.Background(),
				markers,
				[]PathCommitment{commitment},
			)
			if !errors.Is(err, errInvalidTreeProof) {
				t.Fatalf("marker conflict error = %v", err)
			}
		})
	}

	if !equalMarkerCommitmentPath(
		newPathMarker([]byte{0}, pathMarkerStem, Stem{}),
		commitment,
	) {
		t.Fatal("equal marker and commitment path differ")
	}
	if equalMarkerCommitmentPath(
		newPathMarker([]byte{0, 1}, pathMarkerStem, Stem{}),
		commitment,
	) {
		t.Fatal("different marker length compared equal")
	}
	if equalMarkerCommitmentPath(
		newPathMarker([]byte{1}, pathMarkerStem, Stem{}),
		commitment,
	) {
		t.Fatal("different marker bytes compared equal")
	}
}

func TestTreeProofEnforcesEveryResourceLimit(t *testing.T) {
	t.Parallel()

	keyLow := testKey(0, 0)
	keyHigh := testKey(0, 255)
	claims := mustClaimSet(t, []Claim{
		Membership(keyLow, testValue(1)),
		Membership(keyHigh, testValue(2)),
	})
	root := testProofRoot(t)
	opening := testRawOpeningProof(t)
	commitment := testProofCommitment(t)
	stemPaths := []StemPath{PresentStemPath(stemFromKey(keyLow), 1)}
	commitments := []PathCommitment{
		mustPathCommitment(t, []byte{0}, commitment),
		mustPathCommitment(t, []byte{0, 2}, commitment),
		mustPathCommitment(t, []byte{0, 3}, commitment),
	}

	for name, limits := range map[string]TreeProofLimits{
		"zero":                     {},
		"claims zero":              {MaxStemPaths: 1, MaxPathCommitments: 1, MaxPathDerivations: 1, MaxPathBytes: 1, MaxTemporaryBytes: 1},
		"claims maximum":           {MaxClaims: maxClaimCount + 1, MaxStemPaths: 1, MaxPathCommitments: 1, MaxPathDerivations: 1, MaxPathBytes: 1, MaxTemporaryBytes: 1},
		"stem paths zero":          {MaxClaims: 1, MaxPathCommitments: 1, MaxPathDerivations: 1, MaxPathBytes: 1, MaxTemporaryBytes: 1},
		"stem paths maximum":       {MaxClaims: 1, MaxStemPaths: maxTreeProofStemPaths + 1, MaxPathCommitments: 1, MaxPathDerivations: 1, MaxPathBytes: 1, MaxTemporaryBytes: 1},
		"path commitments zero":    {MaxClaims: 1, MaxStemPaths: 1, MaxPathDerivations: 1, MaxPathBytes: 1, MaxTemporaryBytes: 1},
		"path commitments maximum": {MaxClaims: 1, MaxStemPaths: 1, MaxPathCommitments: maxTreeProofPathCommitments + 1, MaxPathDerivations: 1, MaxPathBytes: 1, MaxTemporaryBytes: 1},
		"derivations zero":         {MaxClaims: 1, MaxStemPaths: 1, MaxPathCommitments: 1, MaxPathBytes: 1, MaxTemporaryBytes: 1},
		"derivations maximum":      {MaxClaims: 1, MaxStemPaths: 1, MaxPathCommitments: 1, MaxPathDerivations: maxTreeProofPathDerivations + 1, MaxPathBytes: 1, MaxTemporaryBytes: 1},
		"path bytes zero":          {MaxClaims: 1, MaxStemPaths: 1, MaxPathCommitments: 1, MaxPathDerivations: 1, MaxTemporaryBytes: 1},
		"path bytes maximum":       {MaxClaims: 1, MaxStemPaths: 1, MaxPathCommitments: 1, MaxPathDerivations: 1, MaxPathBytes: uint64(maxTreeProofPathCommitments)*maxProofPathLength + 1, MaxTemporaryBytes: 1},
		"temporary zero":           {MaxClaims: 1, MaxStemPaths: 1, MaxPathCommitments: 1, MaxPathDerivations: 1, MaxPathBytes: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := limits.validate(); !errors.Is(err, errInvalidTreeProofLimits) {
				t.Fatalf("limits error = %v", err)
			}
		})
	}
	maximumLimits := TreeProofLimits{
		MaxClaims:          maxClaimCount,
		MaxStemPaths:       maxTreeProofStemPaths,
		MaxPathCommitments: maxTreeProofPathCommitments,
		MaxPathDerivations: maxTreeProofPathDerivations,
		MaxPathBytes:       uint64(maxTreeProofPathCommitments) * maxProofPathLength,
		MaxTemporaryBytes:  1,
	}
	if err := maximumLimits.validate(); err != nil {
		t.Fatalf("exact maximum limits: %v", err)
	}

	type resourceCase struct {
		resource TreeProofResource
		limits   TreeProofLimits
		paths    []StemPath
		points   []PathCommitment
	}
	base := testTreeProofLimits()
	tests := map[string]resourceCase{
		"claims": {
			resource: TreeProofResourceClaims,
			limits: func() TreeProofLimits {
				value := base
				value.MaxClaims = 1

				return value
			}(),
			paths:  stemPaths,
			points: commitments,
		},
		"stem paths": {
			resource: TreeProofResourceStemPaths,
			limits: func() TreeProofLimits {
				value := base
				value.MaxStemPaths = 1

				return value
			}(),
			paths:  append(slices.Clone(stemPaths), MissingStemPath(Stem{1}, 1)),
			points: commitments,
		},
		"path commitments": {
			resource: TreeProofResourcePathCommitments,
			limits: func() TreeProofLimits {
				value := base
				value.MaxPathCommitments = 2

				return value
			}(),
			paths:  stemPaths,
			points: commitments,
		},
		"path derivations": {
			resource: TreeProofResourcePathDerivations,
			limits: func() TreeProofLimits {
				value := base
				value.MaxPathDerivations = 32

				return value
			}(),
			paths:  stemPaths,
			points: commitments,
		},
		"path bytes": {
			resource: TreeProofResourcePathBytes,
			limits: func() TreeProofLimits {
				value := base
				value.MaxPathBytes = 4

				return value
			}(),
			paths:  stemPaths,
			points: commitments,
		},
		"temporary bytes": {
			resource: TreeProofResourceTemporaryBytes,
			limits: func() TreeProofLimits {
				value := base
				value.MaxTemporaryBytes = 1

				return value
			}(),
			paths:  stemPaths,
			points: commitments,
		},
	}
	for name, candidate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewTreeProof(
				context.Background(),
				root,
				claims,
				candidate.paths,
				candidate.points,
				opening,
				candidate.limits,
			)
			if !errors.Is(err, errTreeProofResource) {
				t.Fatalf("resource error = %v", err)
			}
			var resourceErr *TreeProofResourceError
			if !errors.As(err, &resourceErr) ||
				resourceErr.Resource != candidate.resource ||
				resourceErr.Actual <= resourceErr.Limit {
				t.Fatalf("typed resource error = %#v", resourceErr)
			}
			if resourceErr.Error() == "" || resourceErr.Unwrap() != errTreeProofResource {
				t.Fatalf("resource error contract = %q / %v", resourceErr.Error(), resourceErr.Unwrap())
			}
		})
	}
}

func TestTreeProofTemporaryAccountingIsExact(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	claims := mustClaimSet(t, []Claim{Membership(key, testValue(1))})
	root := testProofRoot(t)
	opening := testRawOpeningProof(t)
	commitment := testProofCommitment(t)
	stemPaths := []StemPath{PresentStemPath(stemFromKey(key), 1)}
	commitments := []PathCommitment{
		mustPathCommitment(t, []byte{0}, commitment),
		mustPathCommitment(t, []byte{0, 2}, commitment),
	}
	const expectedTemporaryBytes = claimWorkingBytes +
		2*stemPathWorkingBytes +
		4*pathCommitmentWorkingBytes +
		64*pathMarkerWorkingBytes
	limits := testTreeProofLimits()
	limits.MaxTemporaryBytes = expectedTemporaryBytes - 1
	_, err := NewTreeProof(
		context.Background(),
		root,
		claims,
		stemPaths,
		commitments,
		opening,
		limits,
	)
	var resourceErr *TreeProofResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != TreeProofResourceTemporaryBytes ||
		resourceErr.Actual != expectedTemporaryBytes ||
		resourceErr.Limit != expectedTemporaryBytes-1 {
		t.Fatalf("temporary resource error = %#v", resourceErr)
	}

	limits.MaxTemporaryBytes = expectedTemporaryBytes
	if _, err := NewTreeProof(
		context.Background(),
		root,
		claims,
		stemPaths,
		commitments,
		opening,
		limits,
	); err != nil {
		t.Fatalf("exact temporary budget: %v", err)
	}
}

func TestTreeProofCancellationAndInvalidReceiverBehavior(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	claims := mustClaimSet(t, []Claim{Membership(key, testValue(1))})
	root := testProofRoot(t)
	opening := testRawOpeningProof(t)
	commitment := testProofCommitment(t)
	stemPaths := []StemPath{PresentStemPath(stemFromKey(key), 1)}
	commitments := []PathCommitment{
		mustPathCommitment(t, []byte{0}, commitment),
		mustPathCommitment(t, []byte{0, 2}, commitment),
	}
	var nilContext context.Context
	if _, err := NewTreeProof(
		nilContext,
		root,
		claims,
		stemPaths,
		commitments,
		opening,
		testTreeProofLimits(),
	); !errors.Is(err, errInvalidTreeProofContext) {
		t.Fatalf("nil context error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewTreeProof(
		cancelled,
		root,
		claims,
		stemPaths,
		commitments,
		opening,
		testTreeProofLimits(),
	); !errors.Is(err, errTreeProofCancelled) ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
	for successful := 0; successful <= 100; successful++ {
		_, err := NewTreeProof(
			&stepContext{successfulChecks: successful},
			root,
			claims,
			stemPaths,
			commitments,
			opening,
			testTreeProofLimits(),
		)
		if err != nil && !errors.Is(err, errTreeProofCancelled) {
			t.Fatalf("construction cancellation after %d checks = %v", successful, err)
		}
	}
	twoClaims := mustClaimSet(t, []Claim{
		Absence(testKey(0, 0)),
		Absence(testKey(1, 0)),
	})
	twoPaths := []StemPath{
		MissingStemPath(stemFromKey(testKey(0, 0)), 1),
		MissingStemPath(stemFromKey(testKey(1, 0)), 1),
	}
	for successful := 0; successful <= 100; successful++ {
		_, err := NewTreeProof(
			&stepContext{successfulChecks: successful},
			root,
			twoClaims,
			twoPaths,
			nil,
			opening,
			testTreeProofLimits(),
		)
		if err != nil && !errors.Is(err, errTreeProofCancelled) {
			t.Fatalf("multi-stem cancellation after %d checks = %v", successful, err)
		}
	}

	proof, err := NewTreeProof(
		context.Background(),
		root,
		claims,
		stemPaths,
		commitments,
		opening,
		testTreeProofLimits(),
	)
	if err != nil {
		t.Fatalf("new tree proof: %v", err)
	}
	if _, err := proof.StemPaths(nilContext); !errors.Is(err, errInvalidTreeProofContext) {
		t.Fatalf("nil stem-copy context error = %v", err)
	}
	if _, err := proof.PathCommitments(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled commitment-copy context error = %v", err)
	}
	if _, err := proof.StemPaths(&stepContext{successfulChecks: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("stem-copy cancellation error = %v", err)
	}
	if _, err := proof.PathCommitments(&stepContext{successfulChecks: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("commitment-copy cancellation error = %v", err)
	}

	var zero TreeProof
	if _, err := zero.Profile(); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("zero profile error = %v", err)
	}
	if _, err := zero.Root(); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("zero root error = %v", err)
	}
	if _, err := zero.Claims(); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("zero claims error = %v", err)
	}
	if _, err := zero.StemPaths(context.Background()); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("zero stem paths error = %v", err)
	}
	if _, err := zero.PathCommitments(context.Background()); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("zero commitments error = %v", err)
	}
	if _, err := zero.OpeningProof(); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("zero opening error = %v", err)
	}

	profile := verkletree.ExperimentalBandersnatchIPA256V0()
	validStem := PresentStemPath(stemFromKey(key), 1)
	emptyRoot, err := newTestSnapshot(t, nil).RootContainer(context.Background())
	if err != nil {
		t.Fatalf("empty root: %v", err)
	}
	corrupt := []TreeProof{
		{
			profile:     profile,
			root:        root,
			claims:      claims,
			stemPaths:   []StemPath{validStem},
			commitments: commitments,
			opening:     opening,
		},
		{
			root:        root,
			claims:      claims,
			stemPaths:   []StemPath{validStem},
			commitments: commitments,
			opening:     opening,
			valid:       true,
		},
		{
			profile:     profile,
			root:        root,
			claims:      claims,
			commitments: commitments,
			opening:     opening,
			valid:       true,
		},
		{
			profile:   profile,
			claims:    claims,
			stemPaths: []StemPath{validStem},
			opening:   opening,
			valid:     true,
		},
		{
			profile:   profile,
			root:      root,
			stemPaths: []StemPath{validStem},
			opening:   opening,
			valid:     true,
		},
		{
			profile:   profile,
			root:      root,
			claims:    claims,
			stemPaths: []StemPath{validStem},
			valid:     true,
		},
		{
			profile:   profile,
			root:      emptyRoot,
			claims:    claims,
			stemPaths: []StemPath{validStem},
			opening:   opening,
			valid:     true,
		},
	}
	for index := range corrupt {
		if _, err := corrupt[index].Profile(); !errors.Is(err, errInvalidTreeProof) {
			t.Fatalf("corrupt proof %d error = %v", index, err)
		}
	}
}

func TestTreeProofHelpersPreserveBoundariesAndStableOrder(t *testing.T) {
	t.Parallel()

	type ordered struct {
		group int
		order int
	}
	values := []ordered{
		{group: 1, order: 1},
		{group: 1, order: 2},
	}
	if err := sortTreeProofValues(
		context.Background(),
		values,
		func(left ordered, right ordered) int {
			return left.group - right.group
		},
	); err != nil {
		t.Fatalf("stable sort: %v", err)
	}
	if values[0].order != 1 || values[1].order != 2 {
		t.Fatalf("equal order changed: %#v", values)
	}

	reversed := []int{2, 1}
	if err := sortTreeProofValues(
		context.Background(),
		reversed,
		func(left int, right int) int {
			return left - right
		},
	); err != nil {
		t.Fatalf("two-value sort: %v", err)
	}
	if !slices.Equal(reversed, []int{1, 2}) {
		t.Fatalf("two-value order = %v", reversed)
	}

	if err := checkTreeProofResource(
		TreeProofResourceClaims,
		1,
		1,
	); err != nil {
		t.Fatalf("exact resource limit: %v", err)
	}
}

func TestTreeProofSupportsConcurrentImmutableReads(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	commitment := testProofCommitment(t)
	proof, err := NewTreeProof(
		context.Background(),
		testProofRoot(t),
		mustClaimSet(t, []Claim{Membership(key, testValue(1))}),
		[]StemPath{PresentStemPath(stemFromKey(key), 1)},
		[]PathCommitment{
			mustPathCommitment(t, []byte{0}, commitment),
			mustPathCommitment(t, []byte{0, 2}, commitment),
		},
		testRawOpeningProof(t),
		testTreeProofLimits(),
	)
	if err != nil {
		t.Fatalf("new tree proof: %v", err)
	}

	var wait sync.WaitGroup
	errs := make(chan error, 16*6)
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, readErr := proof.Profile(); readErr != nil {
				errs <- readErr
			}
			if _, readErr := proof.Root(); readErr != nil {
				errs <- readErr
			}
			if _, readErr := proof.Claims(); readErr != nil {
				errs <- readErr
			}
			if _, readErr := proof.StemPaths(context.Background()); readErr != nil {
				errs <- readErr
			}
			if _, readErr := proof.PathCommitments(context.Background()); readErr != nil {
				errs <- readErr
			}
			if _, readErr := proof.OpeningProof(); readErr != nil {
				errs <- readErr
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent read: %v", err)
	}
}

func mustClaimSet(t testing.TB, claims []Claim) ClaimSet {
	t.Helper()

	set, err := NewClaimSet(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		claims,
		testClaimLimits(),
	)
	if err != nil {
		t.Fatalf("new claim set: %v", err)
	}

	return set
}

func stemFromKey(key Key) Stem {
	return Stem(key[:31])
}

func testProofRoot(t testing.TB) backend.Root {
	t.Helper()

	snapshot := newTestSnapshot(t, []Entry{{
		Key:   testKey(7, 7),
		Value: testValue(7),
	}})
	root, err := snapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("root container: %v", err)
	}

	return root
}

func testProofCommitment(t testing.TB) backend.VectorCommitment {
	t.Helper()

	snapshot := newTestSnapshot(t, []Entry{{
		Key:   testKey(8, 8),
		Value: testValue(8),
	}})
	commitment, err := snapshot.Root()
	if err != nil {
		t.Fatalf("snapshot root: %v", err)
	}

	return commitment
}

func testRawOpeningProof(t testing.TB) backend.OpeningProof {
	t.Helper()

	contents, err := os.ReadFile("../backend/testdata/rust-verkle-multiproof.tsv")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("opening fixture rows = %d, want 2", len(lines))
	}
	fields := strings.Split(lines[1], "\t")
	if len(fields) != 3 {
		t.Fatalf("opening fixture fields = %d, want 3", len(fields))
	}
	encoded, err := hex.DecodeString(fields[2])
	if err != nil {
		t.Fatalf("decode opening fixture: %v", err)
	}
	proof, err := backend.DecodeOpeningProof(
		context.Background(),
		encoded,
		backend.OpeningProofLimits{
			MaxProofBytes:    backend.OpeningProofSize,
			MaxPointDecodes:  17,
			MaxScalarDecodes: 1,
		},
	)
	if err != nil {
		t.Fatalf("decode opening proof: %v", err)
	}

	return proof
}

func mustPathCommitment(
	t testing.TB,
	path []byte,
	commitment backend.VectorCommitment,
) PathCommitment {
	t.Helper()

	value, err := NewPathCommitment(path, commitment)
	if err != nil {
		t.Fatalf("new path commitment: %v", err)
	}

	return value
}

func testTreeProofLimits() TreeProofLimits {
	return TreeProofLimits{
		MaxClaims:          16,
		MaxStemPaths:       16,
		MaxPathCommitments: 64,
		MaxPathDerivations: 512,
		MaxPathBytes:       2_048,
		MaxTemporaryBytes:  64 * 1_024,
	}
}

package authstate

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	internalprofile "github.com/faustbrian/golib/pkg/verkle-tree/internal/profile"
)

func TestTreeProofBytesUsesCanonicalProfileBoundEncoding(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	value := testValue(1)
	commitment := testProofCommitment(t)
	proof, err := NewTreeProof(
		context.Background(),
		testProofRoot(t),
		mustClaimSet(t, []Claim{Membership(key, value)}),
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

	encoded, err := proof.Bytes(
		context.Background(),
		TreeProofEncodingLimits{
			MaxProofBytes:     1_024,
			MaxTemporaryBytes: 2_048,
		},
	)
	if err != nil {
		t.Fatalf("encode tree proof: %v", err)
	}

	rootBytes, err := proof.root.Bytes()
	if err != nil {
		t.Fatalf("root bytes: %v", err)
	}
	commitmentBytes, err := commitment.Bytes()
	if err != nil {
		t.Fatalf("commitment bytes: %v", err)
	}
	openingBytes, err := proof.opening.Bytes()
	if err != nil {
		t.Fatalf("opening bytes: %v", err)
	}
	want := make([]byte, 0, treeProofFixedBytes+claimEncodedBytes+
		stemPathEncodedBytes+2*pathCommitmentEncodedBytes)
	want = append(want, 'V', 'K', 'P', 'F')
	want = append(want, byte(proof.profile.ID()))
	want = binary.BigEndian.AppendUint16(want, proof.profile.Version())
	want = binary.BigEndian.AppendUint16(
		want,
		2,
	)
	want = append(want, rootBytes[:]...)
	want = binary.BigEndian.AppendUint32(want, 1)
	want = binary.BigEndian.AppendUint32(want, 1)
	want = binary.BigEndian.AppendUint32(want, 2)
	want = append(want, key[:]...)
	want = append(want, byte(ClaimMembership))
	want = append(want, value[:]...)
	want = append(want, key[:31]...)
	want = append(want, 1, byte(StemPathPresent))
	want = append(want, make([]byte, len(Stem{}))...)
	for _, path := range [][]byte{{0}, {0, 2}} {
		want = append(want, byte(len(path)))
		want = append(want, path...)
		want = append(
			want,
			make([]byte, maxProofPathLength-len(path))...,
		)
		want = append(want, commitmentBytes[:]...)
	}
	want = append(want, openingBytes[:]...)

	if !bytes.Equal(encoded, want) {
		t.Fatalf("encoded proof = %x, want %x", encoded, want)
	}
	encoded[0] ^= 0xff
	repeated, err := proof.Bytes(
		context.Background(),
		TreeProofEncodingLimits{
			MaxProofBytes:     1_024,
			MaxTemporaryBytes: 2_048,
		},
	)
	if err != nil {
		t.Fatalf("repeat encoding: %v", err)
	}
	if !bytes.Equal(repeated, want) {
		t.Fatal("returned bytes alias retained proof state")
	}
}

func TestTreeProofEncodingUsesExactExperimentalLayout(t *testing.T) {
	t.Parallel()

	if treeProofHeaderBytes != 63 ||
		treeProofFixedBytes != 639 ||
		claimEncodedBytes != 65 ||
		stemPathEncodedBytes != 64 ||
		pathCommitmentEncodedBytes != 65 ||
		maxTreeProofEncodedBytes != 144_769_663 {
		t.Fatalf(
			"layout = header %d, fixed %d, claim %d, stem %d, "+
				"commitment %d, maximum %d",
			treeProofHeaderBytes,
			treeProofFixedBytes,
			claimEncodedBytes,
			stemPathEncodedBytes,
			pathCommitmentEncodedBytes,
			maxTreeProofEncodedBytes,
		)
	}
}

func TestDecodeTreeProofRoundTripsCanonicalEncoding(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	commitment := testProofCommitment(t)
	original, err := NewTreeProof(
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
	encoded, err := original.Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode tree proof: %v", err)
	}

	decoded, err := DecodeTreeProof(
		context.Background(),
		encoded,
		testTreeProofDecodingLimits(),
	)
	if err != nil {
		t.Fatalf("decode tree proof: %v", err)
	}
	reencoded, err := decoded.Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("reencode tree proof: %v", err)
	}
	if !bytes.Equal(reencoded, encoded) {
		t.Fatalf("reencoded proof = %x, want %x", reencoded, encoded)
	}

	encoded[0] ^= 0xff
	repeated, err := decoded.Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("repeat encoding: %v", err)
	}
	if !bytes.Equal(repeated, reencoded) {
		t.Fatal("decoded proof aliases caller input")
	}
}

func TestTreeProofEncodingRoundTripsExplicitEmptyPathCommitment(
	t *testing.T,
) {
	t.Parallel()

	key := testKey(0, 130)
	identity, err := newTestSnapshot(t, nil).Root()
	if err != nil {
		t.Fatalf("identity commitment: %v", err)
	}
	emptyPath, err := NewPathCommitment([]byte{0, 3}, identity)
	if err != nil {
		t.Fatalf("new empty path commitment: %v", err)
	}
	proof, err := NewTreeProof(
		context.Background(),
		testProofRoot(t),
		mustClaimSet(t, []Claim{Absence(key)}),
		[]StemPath{PresentStemPath(stemFromKey(key), 1)},
		[]PathCommitment{
			mustPathCommitment(t, []byte{0}, testProofCommitment(t)),
			emptyPath,
		},
		testRawOpeningProof(t),
		testTreeProofLimits(),
	)
	if err != nil {
		t.Fatalf("new tree proof: %v", err)
	}
	encoded, err := proof.Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode tree proof: %v", err)
	}
	commitmentOffset := treeProofHeaderBytes + claimEncodedBytes +
		stemPathEncodedBytes + pathCommitmentEncodedBytes +
		1 + maxProofPathLength
	if got := [backend.CommitmentSize]byte(
		encoded[commitmentOffset : commitmentOffset+backend.CommitmentSize],
	); got != ([backend.CommitmentSize]byte{}) {
		t.Fatalf("empty path marker = %x, want zero", got)
	}

	limits := testTreeProofDecodingLimits()
	limits.MaxPointDecodes = 1 + backend.OpeningProofPointDecodes + 1
	decoded, err := DecodeTreeProof(context.Background(), encoded, limits)
	if err != nil {
		t.Fatalf("decode tree proof: %v", err)
	}
	limits.MaxPointDecodes--
	if _, err := DecodeTreeProof(
		context.Background(),
		encoded,
		limits,
	); !errors.Is(err, errTreeProofDecodingResource) {
		t.Fatalf("point decode limit error = %v", err)
	}
	commitments, err := decoded.PathCommitments(context.Background())
	if err != nil {
		t.Fatalf("decoded path commitments: %v", err)
	}
	got, err := commitments[1].Commitment()
	if err != nil {
		t.Fatalf("decoded empty commitment: %v", err)
	}
	if empty, emptyErr := got.IsIdentity(); emptyErr != nil || !empty {
		t.Fatalf("decoded identity = %t, error %v", empty, emptyErr)
	}
	reencoded, err := decoded.Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("reencode tree proof: %v", err)
	}
	if !bytes.Equal(reencoded, encoded) {
		t.Fatal("explicit empty path commitment did not round trip")
	}

	emptyStem, err := NewPathCommitment([]byte{0}, identity)
	if err != nil {
		t.Fatalf("new empty stem commitment: %v", err)
	}
	if _, err := NewTreeProof(
		context.Background(),
		testProofRoot(t),
		mustClaimSet(t, []Claim{Absence(key)}),
		[]StemPath{PresentStemPath(stemFromKey(key), 1)},
		[]PathCommitment{emptyStem, emptyPath},
		testRawOpeningProof(t),
		testTreeProofLimits(),
	); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("empty stem commitment error = %v", err)
	}
	if _, err := NewTreeProof(
		context.Background(),
		testProofRoot(t),
		mustClaimSet(t, []Claim{Membership(key, Value{})}),
		[]StemPath{PresentStemPath(stemFromKey(key), 1)},
		[]PathCommitment{
			mustPathCommitment(t, []byte{0}, testProofCommitment(t)),
			emptyPath,
		},
		testRawOpeningProof(t),
		testTreeProofLimits(),
	); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("empty membership commitment error = %v", err)
	}
}

func TestTreeProofEmptySuffixRequiresOnlyAbsenceClaims(t *testing.T) {
	t.Parallel()

	first := testKey(0, 130)
	second := testKey(0, 131)
	identity, err := newTestSnapshot(t, nil).Root()
	if err != nil {
		t.Fatalf("identity commitment: %v", err)
	}
	emptyPath, err := NewPathCommitment([]byte{0, 3}, identity)
	if err != nil {
		t.Fatalf("new empty path commitment: %v", err)
	}
	commitments := []PathCommitment{
		mustPathCommitment(t, []byte{0}, testProofCommitment(t)),
		emptyPath,
	}
	stemPaths := []StemPath{
		PresentStemPath(stemFromKey(first), 1),
	}
	if _, err := NewTreeProof(
		context.Background(),
		testProofRoot(t),
		mustClaimSet(t, []Claim{Absence(first), Absence(second)}),
		stemPaths,
		commitments,
		testRawOpeningProof(t),
		testTreeProofLimits(),
	); err != nil {
		t.Fatalf("two absent suffixes: %v", err)
	}
	if _, err := NewTreeProof(
		context.Background(),
		testProofRoot(t),
		mustClaimSet(t, []Claim{
			Absence(first),
			Membership(second, Value{}),
		}),
		stemPaths,
		commitments,
		testRawOpeningProof(t),
		testTreeProofLimits(),
	); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("mixed suffix claims error = %v", err)
	}
}

func TestTreeProofEncodingLimitBoundaries(t *testing.T) {
	t.Parallel()

	validDecoding := TreeProofDecodingLimits{
		MaxProofBytes:      144_769_663,
		MaxClaims:          65_536,
		MaxStemPaths:       65_536,
		MaxPathCommitments: 2_097_152,
		MaxPathDerivations: 2_097_152,
		MaxPathBytes:       67_108_864,
		MaxPointDecodes:    0,
		MaxScalarDecodes:   0,
		MaxTemporaryBytes:  1,
	}
	if err := validDecoding.validate(); err != nil {
		t.Fatalf("exact decoding limit boundaries: %v", err)
	}
	invalidDecoding := map[string]func(*TreeProofDecodingLimits){
		"zero proof bytes": func(limits *TreeProofDecodingLimits) {
			limits.MaxProofBytes = 0
		},
		"excessive proof bytes": func(limits *TreeProofDecodingLimits) {
			limits.MaxProofBytes = 144_769_664
		},
		"zero claims": func(limits *TreeProofDecodingLimits) {
			limits.MaxClaims = 0
		},
		"excessive claims": func(limits *TreeProofDecodingLimits) {
			limits.MaxClaims = 65_537
		},
		"zero stem paths": func(limits *TreeProofDecodingLimits) {
			limits.MaxStemPaths = 0
		},
		"excessive stem paths": func(limits *TreeProofDecodingLimits) {
			limits.MaxStemPaths = 65_537
		},
		"zero path commitments": func(limits *TreeProofDecodingLimits) {
			limits.MaxPathCommitments = 0
		},
		"excessive path commitments": func(limits *TreeProofDecodingLimits) {
			limits.MaxPathCommitments = 2_097_153
		},
		"zero path derivations": func(limits *TreeProofDecodingLimits) {
			limits.MaxPathDerivations = 0
		},
		"excessive path derivations": func(limits *TreeProofDecodingLimits) {
			limits.MaxPathDerivations = 2_097_153
		},
		"zero path bytes": func(limits *TreeProofDecodingLimits) {
			limits.MaxPathBytes = 0
		},
		"excessive path bytes": func(limits *TreeProofDecodingLimits) {
			limits.MaxPathBytes = 67_108_865
		},
		"zero temporary bytes": func(limits *TreeProofDecodingLimits) {
			limits.MaxTemporaryBytes = 0
		},
	}
	for name, change := range invalidDecoding {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			limits := validDecoding
			change(&limits)
			if err := limits.validate(); !errors.Is(
				err,
				errInvalidTreeProofDecodingLimits,
			) {
				t.Fatalf("invalid decoding limits error = %v", err)
			}
		})
	}

	validEncoding := TreeProofEncodingLimits{
		MaxProofBytes:     144_769_663,
		MaxTemporaryBytes: 1,
	}
	if err := validEncoding.validate(); err != nil {
		t.Fatalf("exact encoding limit boundaries: %v", err)
	}
	for name, limits := range map[string]TreeProofEncodingLimits{
		"zero proof bytes": {
			MaxProofBytes:     0,
			MaxTemporaryBytes: 1,
		},
		"excessive proof bytes": {
			MaxProofBytes:     144_769_664,
			MaxTemporaryBytes: 1,
		},
		"zero temporary bytes": {
			MaxProofBytes:     144_769_663,
			MaxTemporaryBytes: 0,
		},
	} {
		t.Run("encoding "+name, func(t *testing.T) {
			t.Parallel()

			if err := limits.validate(); !errors.Is(
				err,
				errInvalidTreeProofEncodingLimits,
			) {
				t.Fatalf("invalid encoding limits error = %v", err)
			}
		})
	}
}

func TestTreeProofEncodingRejectsInvalidStateContextAndResources(t *testing.T) {
	t.Parallel()

	proof, encoded := testCanonicalEncodedTreeProof(t)
	var missingContext context.Context
	if _, err := proof.Bytes(
		missingContext,
		testTreeProofEncodingLimits(),
	); !errors.Is(err, errInvalidTreeProofEncodingContext) {
		t.Fatalf("nil context error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := proof.Bytes(
		cancelled,
		testTreeProofEncodingLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
	if _, err := (TreeProof{}).Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("zero proof error = %v", err)
	}
	if _, err := proof.Bytes(
		context.Background(),
		TreeProofEncodingLimits{},
	); !errors.Is(err, errInvalidTreeProofEncodingLimits) {
		t.Fatalf("invalid limits error = %v", err)
	}
	excessive := testTreeProofEncodingLimits()
	excessive.MaxProofBytes = uint64(maxTreeProofEncodedBytes) + 1
	if _, err := proof.Bytes(
		context.Background(),
		excessive,
	); !errors.Is(err, errInvalidTreeProofEncodingLimits) {
		t.Fatalf("excessive limits error = %v", err)
	}

	for name, limits := range map[string]TreeProofEncodingLimits{
		"bytes": {
			MaxProofBytes:     uint64(len(encoded) - 1),
			MaxTemporaryBytes: uint64(len(encoded)),
		},
		"temporary": {
			MaxProofBytes:     uint64(len(encoded)),
			MaxTemporaryBytes: uint64(len(encoded) - 1),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := proof.Bytes(context.Background(), limits)
			var resourceErr *TreeProofEncodingResourceError
			if !errors.As(err, &resourceErr) ||
				!errors.Is(err, errTreeProofEncodingResource) ||
				resourceErr.Error() == "" {
				t.Fatalf("resource error = %#v, error = %v", resourceErr, err)
			}
		})
	}
	if _, err := proof.Bytes(
		context.Background(),
		TreeProofEncodingLimits{
			MaxProofBytes:     uint64(len(encoded)),
			MaxTemporaryBytes: uint64(len(encoded)),
		},
	); err != nil {
		t.Fatalf("exact encoding budgets: %v", err)
	}

	for successful := 0; successful < 6; successful++ {
		_, err := proof.Bytes(
			&stepContext{successfulChecks: successful},
			testTreeProofEncodingLimits(),
		)
		if !errors.Is(err, errTreeProofEncodingCancelled) ||
			!errors.Is(err, context.Canceled) {
			t.Fatalf("cancel after %d checks error = %v", successful, err)
		}
	}

	invalidCommitment := proof
	invalidCommitment.commitments = append(
		[]PathCommitment(nil),
		proof.commitments...,
	)
	invalidCommitment.commitments[0] = PathCommitment{
		path:   proof.commitments[0].path,
		length: proof.commitments[0].length,
		valid:  true,
	}
	if _, err := invalidCommitment.Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	); !errors.Is(err, errInvalidTreeProof) {
		t.Fatalf("invalid commitment error = %v", err)
	}
}

func TestDecodeTreeProofAcceptsEveryCanonicalClaimAndStemPathKind(
	t *testing.T,
) {
	t.Parallel()

	key := testKey(0, 0)
	commitment := testProofCommitment(t)
	var existing Stem
	existing[30] = 1
	tests := map[string]struct {
		path        StemPath
		commitments []PathCommitment
	}{
		"present absence": {
			path: PresentStemPath(stemFromKey(key), 1),
			commitments: []PathCommitment{
				mustPathCommitment(t, []byte{0}, commitment),
				mustPathCommitment(t, []byte{0, 2}, commitment),
			},
		},
		"missing": {
			path: MissingStemPath(stemFromKey(key), 1),
		},
		"different": {
			path: DifferentStemPath(stemFromKey(key), 1, existing),
			commitments: []PathCommitment{
				mustPathCommitment(t, []byte{0}, commitment),
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			proof, err := NewTreeProof(
				context.Background(),
				testProofRoot(t),
				mustClaimSet(t, []Claim{Absence(key)}),
				[]StemPath{test.path},
				test.commitments,
				testRawOpeningProof(t),
				testTreeProofLimits(),
			)
			if err != nil {
				t.Fatalf("new tree proof: %v", err)
			}
			encoded, err := proof.Bytes(
				context.Background(),
				testTreeProofEncodingLimits(),
			)
			if err != nil {
				t.Fatalf("encode tree proof: %v", err)
			}
			if _, err := DecodeTreeProof(
				context.Background(),
				encoded,
				testTreeProofDecodingLimits(),
			); err != nil {
				t.Fatalf("decode tree proof: %v", err)
			}
		})
	}
}

func TestDecodeTreeProofDistinguishesTruncatedHeaderFromDeclaredLength(
	t *testing.T,
) {
	t.Parallel()

	_, canonical := testCanonicalEncodedTreeProof(t)
	truncated := canonical[:treeProofFixedBytes-1]
	if _, err := DecodeTreeProof(
		context.Background(),
		truncated,
		testTreeProofDecodingLimits(),
	); err == nil || err.Error() !=
		"invalid canonical tree-proof encoding: minimum length" {
		t.Fatalf("truncated proof error = %v", err)
	}

	declaredLengthMismatch := append(
		bytes.Clone(canonical[:treeProofHeaderBytes]),
		canonical[len(canonical)-backend.OpeningProofSize:]...,
	)
	if len(declaredLengthMismatch) != treeProofFixedBytes {
		t.Fatalf(
			"declared-length fixture bytes = %d, want %d",
			len(declaredLengthMismatch),
			treeProofFixedBytes,
		)
	}
	if _, err := DecodeTreeProof(
		context.Background(),
		declaredLengthMismatch,
		testTreeProofDecodingLimits(),
	); err == nil || err.Error() !=
		"invalid canonical tree-proof encoding: declared length" {
		t.Fatalf("declared-length proof error = %v", err)
	}
}

func TestDecodeTreeProofPreservesMultipleRecordStrides(t *testing.T) {
	t.Parallel()

	decoded := testMultipleRecordTreeProofRoundTrip(t)
	firstKey := testKey(0, 0)
	secondKey := testKey(1, 128)
	firstValue := testValue(1)
	secondValue := testValue(101)
	for _, expected := range []struct {
		key   Key
		value Value
	}{
		{key: firstKey, value: firstValue},
		{key: secondKey, value: secondValue},
	} {
		claim, found, lookupErr := decoded.claims.Lookup(expected.key)
		if lookupErr != nil || !found || claim.value != expected.value {
			t.Fatalf(
				"claim %x = %#v, found %t, error %v",
				expected.key,
				claim,
				found,
				lookupErr,
			)
		}
	}
	if len(decoded.stemPaths) != 2 ||
		decoded.stemPaths[0].stem != stemFromKey(firstKey) ||
		decoded.stemPaths[1].stem != stemFromKey(secondKey) {
		t.Fatalf("decoded stem paths = %#v", decoded.stemPaths)
	}
	expectedPaths := [][]byte{{0}, {0, 2}, {1}, {1, 3}}
	if len(decoded.commitments) != len(expectedPaths) {
		t.Fatalf(
			"decoded commitment count = %d, want %d",
			len(decoded.commitments),
			len(expectedPaths),
		)
	}
	for index, expected := range expectedPaths {
		path, pathErr := decoded.commitments[index].Path()
		if pathErr != nil || !bytes.Equal(path, expected) {
			t.Fatalf(
				"path %d = %x, want %x, error %v",
				index,
				path,
				expected,
				pathErr,
			)
		}
	}
}

func TestDecodeTreeProofRejectsNonCanonicalRecordOrdering(t *testing.T) {
	t.Parallel()

	_, canonical := testMultipleRecordEncodedTreeProof(t)
	claimOffset := treeProofHeaderBytes
	stemOffset := claimOffset + 2*claimEncodedBytes
	commitmentOffset := stemOffset + 2*stemPathEncodedBytes
	tests := map[string]struct {
		encoded []byte
		detail  string
	}{
		"reordered claims": {
			encoded: swapTreeProofRecords(
				canonical,
				claimOffset,
				claimOffset+claimEncodedBytes,
				claimEncodedBytes,
			),
			detail: "invalid canonical tree-proof encoding: claim order",
		},
		"duplicate claims": {
			encoded: duplicateTreeProofRecord(
				canonical,
				claimOffset,
				claimOffset+claimEncodedBytes,
				claimEncodedBytes,
			),
			detail: "invalid canonical tree-proof encoding: claim order",
		},
		"reordered stem paths": {
			encoded: swapTreeProofRecords(
				canonical,
				stemOffset,
				stemOffset+stemPathEncodedBytes,
				stemPathEncodedBytes,
			),
			detail: "invalid canonical tree-proof encoding: stem-path order",
		},
		"duplicate stem paths": {
			encoded: duplicateTreeProofRecord(
				canonical,
				stemOffset,
				stemOffset+stemPathEncodedBytes,
				stemPathEncodedBytes,
			),
			detail: "invalid canonical tree-proof encoding: stem-path order",
		},
		"reordered first path commitments": {
			encoded: swapTreeProofRecords(
				canonical,
				commitmentOffset,
				commitmentOffset+pathCommitmentEncodedBytes,
				pathCommitmentEncodedBytes,
			),
			detail: "invalid canonical tree-proof encoding: " +
				"path-commitment order",
		},
		"reordered later path commitments": {
			encoded: swapTreeProofRecords(
				canonical,
				commitmentOffset+2*pathCommitmentEncodedBytes,
				commitmentOffset+3*pathCommitmentEncodedBytes,
				pathCommitmentEncodedBytes,
			),
			detail: "invalid canonical tree-proof encoding: " +
				"path-commitment order",
		},
		"duplicate path commitments": {
			encoded: duplicateTreeProofRecord(
				canonical,
				commitmentOffset,
				commitmentOffset+pathCommitmentEncodedBytes,
				pathCommitmentEncodedBytes,
			),
			detail: "invalid canonical tree-proof encoding: " +
				"path-commitment order",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := DecodeTreeProof(
				context.Background(),
				test.encoded,
				testTreeProofDecodingLimits(),
			); !errors.Is(err, errInvalidTreeProofEncoding) ||
				err.Error() != test.detail {
				t.Fatalf("reordered records error = %v", err)
			}
		})
	}
}

func testMultipleRecordTreeProofRoundTrip(t testing.TB) TreeProof {
	t.Helper()

	_, encoded := testMultipleRecordEncodedTreeProof(t)
	decoded, err := DecodeTreeProof(
		context.Background(),
		encoded,
		testTreeProofDecodingLimits(),
	)
	if err != nil {
		t.Fatalf("decode multi-record proof: %v", err)
	}

	return decoded
}

func testMultipleRecordEncodedTreeProof(
	t testing.TB,
) (TreeProof, []byte) {
	t.Helper()

	firstKey := testKey(0, 0)
	secondKey := testKey(1, 128)
	firstValue := testValue(1)
	secondValue := testValue(101)
	commitment := testProofCommitment(t)
	proof, err := NewTreeProof(
		context.Background(),
		testProofRoot(t),
		mustClaimSet(t, []Claim{
			Membership(secondKey, secondValue),
			Membership(firstKey, firstValue),
		}),
		[]StemPath{
			PresentStemPath(stemFromKey(secondKey), 1),
			PresentStemPath(stemFromKey(firstKey), 1),
		},
		[]PathCommitment{
			mustPathCommitment(t, []byte{1, 3}, commitment),
			mustPathCommitment(t, []byte{0, 2}, commitment),
			mustPathCommitment(t, []byte{1}, commitment),
			mustPathCommitment(t, []byte{0}, commitment),
		},
		testRawOpeningProof(t),
		testTreeProofLimits(),
	)
	if err != nil {
		t.Fatalf("new multi-record proof: %v", err)
	}
	encoded, err := proof.Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode multi-record proof: %v", err)
	}

	return proof, encoded
}

func TestDecodeTreeProofAcceptsMaximumPathLength(t *testing.T) {
	t.Parallel()

	key := testKey(0, 0)
	commitment := testProofCommitment(t)
	commitments := make([]PathCommitment, 0, 32)
	for length := 1; length <= 31; length++ {
		commitments = append(
			commitments,
			mustPathCommitment(t, make([]byte, length), commitment),
		)
	}
	suffixPath := make([]byte, 32)
	suffixPath[31] = 2
	commitments = append(
		commitments,
		mustPathCommitment(t, suffixPath, commitment),
	)
	proof, err := NewTreeProof(
		context.Background(),
		testProofRoot(t),
		mustClaimSet(t, []Claim{Membership(key, testValue(1))}),
		[]StemPath{PresentStemPath(stemFromKey(key), 31)},
		commitments,
		testRawOpeningProof(t),
		testTreeProofLimits(),
	)
	if err != nil {
		t.Fatalf("new maximum-path proof: %v", err)
	}
	encoded, err := proof.Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode maximum-path proof: %v", err)
	}
	if _, err := DecodeTreeProof(
		context.Background(),
		encoded,
		testTreeProofDecodingLimits(),
	); err != nil {
		t.Fatalf("decode maximum-path proof: %v", err)
	}
}

func TestDecodeTreeProofRejectsMalformedCanonicalEncoding(t *testing.T) {
	t.Parallel()

	_, canonical := testCanonicalEncodedTreeProof(t)
	claimOffset := treeProofHeaderBytes
	stemOffset := claimOffset + claimEncodedBytes
	commitmentOffset := stemOffset + stemPathEncodedBytes
	secondCommitmentOffset := commitmentOffset + pathCommitmentEncodedBytes
	rootOffset := treeProofMagicBytes + treeProofProfileIDBytes +
		treeProofVersionBytes + treeProofEncodingBytes

	tests := map[string][]byte{
		"short": canonical[:treeProofFixedBytes-1],
		"trailing": append(
			bytes.Clone(canonical),
			0,
		),
		"magic": mutateTreeProofEncoding(canonical, 0, 'X'),
		"profile id": mutateTreeProofEncoding(
			canonical,
			treeProofMagicBytes,
			0xff,
		),
		"profile version": mutateTreeProofEncoding(
			canonical,
			treeProofMagicBytes+treeProofProfileIDBytes+1,
			1,
		),
		"encoding version": mutateTreeProofEncoding(
			canonical,
			treeProofMagicBytes+treeProofProfileIDBytes+
				treeProofVersionBytes+1,
			1,
		),
		"root": zeroTreeProofEncoding(
			canonical,
			rootOffset+10,
			maxProofPathLength,
		),
		"claim kind": mutateTreeProofEncoding(
			canonical,
			claimOffset+32,
			0xff,
		),
		"absence value": mutateTreeProofEncoding(
			canonical,
			claimOffset+32,
			byte(ClaimAbsence),
		),
		"stem depth": mutateTreeProofEncoding(
			canonical,
			stemOffset+31,
			0,
		),
		"stem kind": mutateTreeProofEncoding(
			canonical,
			stemOffset+32,
			0xff,
		),
		"present existing stem": mutateTreeProofEncoding(
			canonical,
			stemOffset+33,
			1,
		),
		"path length": mutateTreeProofEncoding(
			canonical,
			commitmentOffset,
			0,
		),
		"path length above maximum": mutateTreeProofEncoding(
			canonical,
			commitmentOffset,
			33,
		),
		"path padding": mutateTreeProofEncoding(
			canonical,
			commitmentOffset+2,
			1,
		),
		"path commitment": zeroTreeProofEncoding(
			canonical,
			commitmentOffset+1+maxProofPathLength,
			32,
		),
		"wrong topology": mutateTreeProofEncoding(
			canonical,
			secondCommitmentOffset+2,
			3,
		),
	}
	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeTreeProof(
				context.Background(),
				encoded,
				testTreeProofDecodingLimits(),
			)
			if err == nil {
				t.Fatal("decode unexpectedly succeeded")
			}
			if name == "profile id" ||
				name == "profile version" ||
				name == "encoding version" {
				if !errors.Is(err, internalprofile.ErrUnsupported) {
					t.Fatalf("profile error = %v", err)
				}
			} else if !errors.Is(err, errInvalidTreeProofEncoding) {
				t.Fatalf("encoding error = %v", err)
			}
		})
	}
}

func TestTreeProofIdentityOpeningRequiresCryptographicVerification(t *testing.T) {
	t.Parallel()

	first := testKey(0x00, 0x00)
	second := testKey(0x01, 0xff)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: first, Value: testValue(0x11)},
		{Key: second, Value: testValue(0x22)},
	})
	engine := newTestProofEngine(t)
	proof, err := engine.Prove(
		context.Background(), snapshot, []Key{first, second},
		testProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("generate proof: %v", err)
	}
	encoded, err := proof.Bytes(context.Background(), testTreeProofEncodingLimits())
	if err != nil {
		t.Fatalf("encode proof: %v", err)
	}
	openingOffset := len(encoded) - (treeProofFixedBytes - treeProofHeaderBytes)
	if bytes.Equal(
		encoded[openingOffset:openingOffset+backend.CommitmentSize],
		make([]byte, backend.CommitmentSize),
	) {
		t.Fatal("test proof already contains an identity first point")
	}
	clear(encoded[openingOffset : openingOffset+backend.CommitmentSize])
	decoded, err := DecodeTreeProof(
		context.Background(), encoded, testTreeProofDecodingLimits(),
	)
	if err != nil {
		t.Fatalf("decode canonical identity proof element: %v", err)
	}
	if err := engine.Verify(
		context.Background(), decoded, testProofVerificationLimits(),
	); !IsProofVerificationError(err) {
		t.Fatalf("identity substitution verification error = %v", err)
	}
}

func TestDecodeTreeProofEnforcesResourcesBeforeCryptographicDecoding(
	t *testing.T,
) {
	t.Parallel()

	_, canonical := testCanonicalEncodedTreeProof(t)
	countOffset := treeProofMagicBytes + treeProofProfileIDBytes +
		treeProofVersionBytes + treeProofEncodingBytes + backend.RootSize
	tests := map[string]struct {
		encoded  []byte
		limits   TreeProofDecodingLimits
		resource TreeProofDecodingResource
	}{
		"bytes": {
			encoded: canonical,
			limits: withTreeProofDecodingLimits(func(
				limits *TreeProofDecodingLimits,
			) {
				limits.MaxProofBytes = uint64(len(canonical) - 1)
			}),
			resource: TreeProofDecodingResourceBytes,
		},
		"claims": {
			encoded: putTreeProofCount(canonical, countOffset, 2),
			limits: withTreeProofDecodingLimits(func(
				limits *TreeProofDecodingLimits,
			) {
				limits.MaxClaims = 1
			}),
			resource: TreeProofDecodingResourceClaims,
		},
		"stem paths": {
			encoded: putTreeProofCount(
				canonical,
				countOffset+treeProofCountBytes,
				2,
			),
			limits: withTreeProofDecodingLimits(func(
				limits *TreeProofDecodingLimits,
			) {
				limits.MaxStemPaths = 1
			}),
			resource: TreeProofDecodingResourceStemPaths,
		},
		"path commitments": {
			encoded: putTreeProofCount(
				canonical,
				countOffset+2*treeProofCountBytes,
				3,
			),
			limits: withTreeProofDecodingLimits(func(
				limits *TreeProofDecodingLimits,
			) {
				limits.MaxPathCommitments = 2
			}),
			resource: TreeProofDecodingResourcePathCommitments,
		},
		"path derivations": {
			encoded: canonical,
			limits: withTreeProofDecodingLimits(func(
				limits *TreeProofDecodingLimits,
			) {
				limits.MaxPathDerivations = 31
			}),
			resource: TreeProofDecodingResourcePathDerivations,
		},
		"path bytes": {
			encoded: canonical,
			limits: withTreeProofDecodingLimits(func(
				limits *TreeProofDecodingLimits,
			) {
				limits.MaxPathBytes = 2
			}),
			resource: TreeProofDecodingResourcePathBytes,
		},
		"point decodes": {
			encoded: canonical,
			limits: withTreeProofDecodingLimits(func(
				limits *TreeProofDecodingLimits,
			) {
				limits.MaxPointDecodes = 19
			}),
			resource: TreeProofDecodingResourcePointDecodes,
		},
		"scalar decodes": {
			encoded: canonical,
			limits: withTreeProofDecodingLimits(func(
				limits *TreeProofDecodingLimits,
			) {
				limits.MaxScalarDecodes = 0
			}),
			resource: TreeProofDecodingResourceScalarDecodes,
		},
		"temporary bytes": {
			encoded: canonical,
			limits: withTreeProofDecodingLimits(func(
				limits *TreeProofDecodingLimits,
			) {
				limits.MaxTemporaryBytes = 8_385
			}),
			resource: TreeProofDecodingResourceTemporaryBytes,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeTreeProof(
				context.Background(),
				test.encoded,
				test.limits,
			)
			var resourceErr *TreeProofDecodingResourceError
			if !errors.As(err, &resourceErr) ||
				!errors.Is(err, errTreeProofDecodingResource) ||
				resourceErr.Resource != test.resource ||
				resourceErr.Error() == "" {
				t.Fatalf("resource error = %#v, error = %v", resourceErr, err)
			}
			if name == "temporary bytes" &&
				(resourceErr.Actual != 8_386 ||
					resourceErr.Limit != 8_385) {
				t.Fatalf("temporary resource accounting = %#v", resourceErr)
			}
		})
	}
	exactTemporary := testTreeProofDecodingLimits()
	exactTemporary.MaxTemporaryBytes = 8_386
	if _, err := DecodeTreeProof(
		context.Background(),
		canonical,
		exactTemporary,
	); err != nil {
		t.Fatalf("exact temporary decoding budget: %v", err)
	}
}

func TestDecodeTreeProofRejectsInvalidLimitsAndHonorsCancellation(
	t *testing.T,
) {
	t.Parallel()

	_, canonical := testCanonicalEncodedTreeProof(t)
	var missingContext context.Context
	if _, err := DecodeTreeProof(
		missingContext,
		canonical,
		testTreeProofDecodingLimits(),
	); !errors.Is(err, errInvalidTreeProofEncodingContext) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := DecodeTreeProof(
		context.Background(),
		canonical,
		TreeProofDecodingLimits{},
	); !errors.Is(err, errInvalidTreeProofDecodingLimits) {
		t.Fatalf("invalid limits error = %v", err)
	}
	excessive := testTreeProofDecodingLimits()
	excessive.MaxPathBytes =
		uint64(maxTreeProofPathCommitments)*maxProofPathLength + 1
	if _, err := DecodeTreeProof(
		context.Background(),
		canonical,
		excessive,
	); !errors.Is(err, errInvalidTreeProofDecodingLimits) {
		t.Fatalf("excessive limits error = %v", err)
	}

	succeeded := false
	for successful := 0; successful < 256; successful++ {
		_, err := DecodeTreeProof(
			&stepContext{successfulChecks: successful},
			canonical,
			testTreeProofDecodingLimits(),
		)
		if err == nil {
			succeeded = true
			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel after %d checks error = %v", successful, err)
		}
	}
	if !succeeded {
		t.Fatal("decoder did not complete within cancellation audit bound")
	}
}

func testCanonicalEncodedTreeProof(t testing.TB) (TreeProof, []byte) {
	t.Helper()

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
	encoded, err := proof.Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode tree proof: %v", err)
	}

	return proof, encoded
}

func mutateTreeProofEncoding(encoded []byte, offset int, value byte) []byte {
	mutated := bytes.Clone(encoded)
	mutated[offset] = value

	return mutated
}

func zeroTreeProofEncoding(encoded []byte, offset int, length int) []byte {
	mutated := bytes.Clone(encoded)
	clear(mutated[offset : offset+length])

	return mutated
}

func putTreeProofCount(encoded []byte, offset int, count uint32) []byte {
	mutated := bytes.Clone(encoded)
	binary.BigEndian.PutUint32(
		mutated[offset:offset+treeProofCountBytes],
		count,
	)

	return mutated
}

func swapTreeProofRecords(
	encoded []byte,
	leftOffset int,
	rightOffset int,
	recordBytes int,
) []byte {
	mutated := bytes.Clone(encoded)
	left := bytes.Clone(mutated[leftOffset : leftOffset+recordBytes])
	copy(
		mutated[leftOffset:leftOffset+recordBytes],
		mutated[rightOffset:rightOffset+recordBytes],
	)
	copy(mutated[rightOffset:rightOffset+recordBytes], left)

	return mutated
}

func duplicateTreeProofRecord(
	encoded []byte,
	sourceOffset int,
	targetOffset int,
	recordBytes int,
) []byte {
	mutated := bytes.Clone(encoded)
	copy(
		mutated[targetOffset:targetOffset+recordBytes],
		mutated[sourceOffset:sourceOffset+recordBytes],
	)

	return mutated
}

func withTreeProofDecodingLimits(
	change func(*TreeProofDecodingLimits),
) TreeProofDecodingLimits {
	limits := testTreeProofDecodingLimits()
	change(&limits)

	return limits
}

func testTreeProofEncodingLimits() TreeProofEncodingLimits {
	return TreeProofEncodingLimits{
		MaxProofBytes:     16 * 1_024,
		MaxTemporaryBytes: 32 * 1_024,
	}
}

func testTreeProofDecodingLimits() TreeProofDecodingLimits {
	return TreeProofDecodingLimits{
		MaxProofBytes:      16 * 1_024,
		MaxClaims:          32,
		MaxStemPaths:       32,
		MaxPathCommitments: 64,
		MaxPathDerivations: 1_024,
		MaxPathBytes:       2 * 1_024,
		MaxPointDecodes:    128,
		MaxScalarDecodes:   1,
		MaxTemporaryBytes:  64 * 1_024,
	}
}

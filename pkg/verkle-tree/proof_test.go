package verkletree_test

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"testing"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

func TestPublicProofEngineGeneratesCanonicalVerifiableProofs(t *testing.T) {
	t.Parallel()

	present := publicKey(0, 0)
	absentSuffix := publicKey(0, 0x80)
	absentStem := publicKey(1, 0)
	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		[]verkletree.Entry{{Key: present, Value: publicValue(7)}},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	engine, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		publicOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new proof engine: %v", err)
	}
	keys := []verkletree.Key{absentStem, absentSuffix, present}
	proof, err := engine.Prove(
		context.Background(),
		snapshot,
		keys,
		publicProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove: %v", err)
	}
	if err := engine.Verify(
		context.Background(),
		proof,
		publicProofVerificationLimits(),
	); err != nil {
		t.Fatalf("verify: %v", err)
	}
	claims, err := proof.Claims(context.Background())
	if err != nil || len(claims) != 3 {
		t.Fatalf("claims = %#v, error = %v", claims, err)
	}
	firstKind, firstKindErr := claims[0].Kind()
	firstKey, firstKeyErr := claims[0].Key()
	firstValue, firstPresent, firstValueErr := claims[0].Value()
	secondKind, secondKindErr := claims[1].Kind()
	secondKey, secondKeyErr := claims[1].Key()
	_, secondPresent, secondValueErr := claims[1].Value()
	thirdKind, thirdKindErr := claims[2].Kind()
	thirdKey, thirdKeyErr := claims[2].Key()
	if firstKindErr != nil || firstKeyErr != nil || firstValueErr != nil ||
		secondKindErr != nil || secondKeyErr != nil || secondValueErr != nil ||
		thirdKindErr != nil || thirdKeyErr != nil ||
		firstKey != present ||
		firstKind != verkletree.ClaimMembership ||
		!firstPresent ||
		firstValue != publicValue(7) ||
		secondKey != absentSuffix ||
		secondKind != verkletree.ClaimAbsence ||
		secondPresent ||
		thirdKey != absentStem ||
		thirdKind != verkletree.ClaimAbsence {
		t.Fatalf("unexpected claims: %#v", claims)
	}

	encoded, err := proof.Bytes(
		context.Background(),
		publicProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode proof: %v", err)
	}
	decoded, err := verkletree.DecodeProof(
		context.Background(),
		encoded,
		publicProofDecodingLimits(),
	)
	if err != nil {
		t.Fatalf("decode proof: %v", err)
	}
	if err := engine.Verify(
		context.Background(),
		decoded,
		publicProofVerificationLimits(),
	); err != nil {
		t.Fatalf("verify decoded proof: %v", err)
	}

	reordered, err := engine.Prove(
		context.Background(),
		snapshot,
		[]verkletree.Key{present, absentSuffix, absentStem},
		publicProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove reordered: %v", err)
	}
	reorderedBytes, err := reordered.Bytes(
		context.Background(),
		publicProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode reordered proof: %v", err)
	}
	if !bytes.Equal(encoded, reorderedBytes) {
		t.Fatal("proof bytes depend on caller key order")
	}
}

func TestPublicProofEngineVerifiesTrustedRootAndKeySet(t *testing.T) {
	t.Parallel()

	present := publicKey(0, 0)
	absent := publicKey(1, 0)
	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		[]verkletree.Entry{{Key: present, Value: publicValue(7)}},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	trustedRoot, err := snapshot.Root()
	if err != nil {
		t.Fatalf("trusted root: %v", err)
	}
	engine, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		publicOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new proof engine: %v", err)
	}
	proof, err := engine.Prove(
		context.Background(),
		snapshot,
		[]verkletree.Key{present, absent},
		publicProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove: %v", err)
	}
	expectationLimits := verkletree.ProofExpectationLimits{
		MaxKeys:           4,
		MaxTemporaryBytes: 1 << 10,
	}
	if err := engine.VerifyForKeys(
		context.Background(),
		proof,
		trustedRoot,
		[]verkletree.Key{absent, present},
		expectationLimits,
		publicProofVerificationLimits(),
	); err != nil {
		t.Fatalf("verify trusted root and keys: %v", err)
	}

	otherSnapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		[]verkletree.Entry{{Key: present, Value: publicValue(8)}},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new replay snapshot: %v", err)
	}
	otherRoot, err := otherSnapshot.Root()
	if err != nil {
		t.Fatalf("replay root: %v", err)
	}
	if err := engine.VerifyForKeys(
		context.Background(),
		proof,
		otherRoot,
		[]verkletree.Key{present, absent},
		expectationLimits,
		publicProofVerificationLimits(),
	); !errors.Is(err, verkletree.ErrVerification) {
		t.Fatalf("cross-root replay error = %v", err)
	}
	if err := engine.VerifyForKeys(
		context.Background(),
		proof,
		trustedRoot,
		[]verkletree.Key{present, publicKey(2, 0)},
		expectationLimits,
		publicProofVerificationLimits(),
	); !errors.Is(err, verkletree.ErrVerification) {
		t.Fatalf("cross-key replay error = %v", err)
	}
	if err := engine.VerifyForKeys(
		context.Background(),
		proof,
		trustedRoot,
		[]verkletree.Key{present},
		expectationLimits,
		publicProofVerificationLimits(),
	); !errors.Is(err, verkletree.ErrVerification) {
		t.Fatalf("omitted key error = %v", err)
	}
	if err := engine.VerifyForKeys(
		context.Background(),
		proof,
		trustedRoot,
		[]verkletree.Key{present, absent, publicKey(2, 0)},
		expectationLimits,
		publicProofVerificationLimits(),
	); !errors.Is(err, verkletree.ErrVerification) {
		t.Fatalf("surplus key error = %v", err)
	}
	if err := engine.VerifyForKeys(
		context.Background(),
		proof,
		trustedRoot,
		[]verkletree.Key{present, present},
		expectationLimits,
		publicProofVerificationLimits(),
	); !errors.Is(err, verkletree.ErrDuplicateKey) {
		t.Fatalf("duplicate key error = %v", err)
	}
	if err := engine.VerifyForKeys(
		context.Background(),
		proof,
		verkletree.Root{},
		[]verkletree.Key{present, absent},
		expectationLimits,
		publicProofVerificationLimits(),
	); !errors.Is(err, verkletree.ErrInvalidRoot) {
		t.Fatalf("invalid trusted root error = %v", err)
	}
	if err := engine.VerifyForKeys(
		context.Background(),
		proof,
		trustedRoot,
		[]verkletree.Key{present, absent},
		verkletree.ProofExpectationLimits{},
		publicProofVerificationLimits(),
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("invalid expectation limits error = %v", err)
	}
	for _, test := range []struct {
		name   string
		limits verkletree.ProofExpectationLimits
	}{
		{
			name: "zero keys",
			limits: verkletree.ProofExpectationLimits{
				MaxTemporaryBytes: 1 << 10,
			},
		},
		{
			name: "excess keys",
			limits: verkletree.ProofExpectationLimits{
				MaxKeys:           65_537,
				MaxTemporaryBytes: 1 << 10,
			},
		},
		{
			name: "zero temporary bytes",
			limits: verkletree.ProofExpectationLimits{
				MaxKeys: 4,
			},
		},
	} {
		if err := engine.VerifyForKeys(
			context.Background(),
			proof,
			trustedRoot,
			[]verkletree.Key{present, absent},
			test.limits,
			publicProofVerificationLimits(),
		); !errors.Is(err, verkletree.ErrInvalidLimits) {
			t.Fatalf("%s expectation limits error = %v", test.name, err)
		}
	}
	boundaryLimits := expectationLimits
	boundaryLimits.MaxKeys = 65_536
	if err := engine.VerifyForKeys(
		context.Background(),
		proof,
		trustedRoot,
		[]verkletree.Key{present, absent},
		boundaryLimits,
		publicProofVerificationLimits(),
	); err != nil {
		t.Fatalf("exact maximum expectation limit: %v", err)
	}
	if err := engine.VerifyForKeys(
		context.Background(),
		proof,
		trustedRoot,
		[]verkletree.Key{present, absent},
		expectationLimits,
		verkletree.ProofVerificationLimits{},
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("invalid verification limits error = %v", err)
	}
	keyLimits := expectationLimits
	keyLimits.MaxKeys = 1
	if err := engine.VerifyForKeys(
		context.Background(),
		proof,
		trustedRoot,
		[]verkletree.Key{present, absent},
		keyLimits,
		publicProofVerificationLimits(),
	); resourceOf(err) != verkletree.ResourceKeys {
		t.Fatalf("expected-key resource error = %v", err)
	}
	if err := engine.VerifyForKeys(
		context.Background(),
		proof,
		trustedRoot,
		[]verkletree.Key{present},
		keyLimits,
		publicProofVerificationLimits(),
	); resourceOf(err) != verkletree.ResourceKeys {
		t.Fatalf("proof-claim resource error = %v", err)
	}
	temporaryLimits := expectationLimits
	temporaryLimits.MaxTemporaryBytes = 511
	if err := engine.VerifyForKeys(
		context.Background(),
		proof,
		trustedRoot,
		[]verkletree.Key{present, absent},
		temporaryLimits,
		publicProofVerificationLimits(),
	); resourceOf(err) != verkletree.ResourceTemporaryBytes {
		t.Fatalf("expectation temporary resource error = %v", err)
	}
	var nilContext context.Context
	if err := engine.VerifyForKeys(
		nilContext,
		proof,
		trustedRoot,
		[]verkletree.Key{present, absent},
		expectationLimits,
		publicProofVerificationLimits(),
	); !errors.Is(err, verkletree.ErrInvalidContext) {
		t.Fatalf("nil context error = %v", err)
	}
	if err := (verkletree.ProofEngine{}).VerifyForKeys(
		context.Background(),
		proof,
		trustedRoot,
		[]verkletree.Key{present, absent},
		expectationLimits,
		publicProofVerificationLimits(),
	); !errors.Is(err, verkletree.ErrInvalidProofEngine) {
		t.Fatalf("zero engine error = %v", err)
	}
	if err := engine.VerifyForKeys(
		context.Background(),
		verkletree.Proof{},
		trustedRoot,
		[]verkletree.Key{present, absent},
		expectationLimits,
		publicProofVerificationLimits(),
	); !errors.Is(err, verkletree.ErrInvalidProof) {
		t.Fatalf("zero proof error = %v", err)
	}
}

func TestPublicProofEngineProvesCanonicalEmptyRootNonMembership(t *testing.T) {
	t.Parallel()

	first := publicKey(0x20, 0x01)
	second := publicKey(0x10, 0x02)
	secondSuffix := second
	secondSuffix[31] = 0x80
	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		nil,
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new empty snapshot: %v", err)
	}
	engine, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		publicOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new proof engine: %v", err)
	}
	proof, err := engine.Prove(
		context.Background(),
		snapshot,
		[]verkletree.Key{first, secondSuffix, second},
		publicProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove empty-root non-membership: %v", err)
	}
	if err := engine.Verify(
		context.Background(), proof, publicProofVerificationLimits(),
	); err != nil {
		t.Fatalf("verify empty-root non-membership: %v", err)
	}
	root, err := proof.Root()
	if err != nil {
		t.Fatalf("proof root: %v", err)
	}
	empty, err := root.IsEmpty()
	if err != nil || !empty {
		t.Fatalf("proof root empty = %t, error %v", empty, err)
	}
	claims, err := proof.Claims(context.Background())
	if err != nil || len(claims) != 3 {
		t.Fatalf("claims = %#v, error %v", claims, err)
	}
	for index := range claims {
		kind, kindErr := claims[index].Kind()
		_, present, valueErr := claims[index].Value()
		if kindErr != nil || valueErr != nil ||
			kind != verkletree.ClaimAbsence || present {
			t.Fatalf(
				"claim %d = %d/%t, errors %v/%v",
				index, kind, present, kindErr, valueErr,
			)
		}
	}
	encoded, err := proof.Bytes(
		context.Background(), publicProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode empty-root proof: %v", err)
	}
	decoded, err := verkletree.DecodeProof(
		context.Background(), encoded, publicProofDecodingLimits(),
	)
	if err != nil {
		t.Fatalf("decode empty-root proof: %v", err)
	}
	if err := engine.Verify(
		context.Background(), decoded, publicProofVerificationLimits(),
	); err != nil {
		t.Fatalf("verify decoded empty-root proof: %v", err)
	}
	reordered, err := engine.Prove(
		context.Background(),
		snapshot,
		[]verkletree.Key{second, first, secondSuffix},
		publicProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove reordered empty-root non-membership: %v", err)
	}
	reorderedBytes, err := reordered.Bytes(
		context.Background(), publicProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode reordered empty-root proof: %v", err)
	}
	if !bytes.Equal(encoded, reorderedBytes) {
		t.Fatal("empty-root proof bytes depend on caller key order")
	}
}

func TestPublicProofEngineRejectsTamperingAndInvalidUse(t *testing.T) {
	t.Parallel()

	key := publicKey(0, 0)
	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		[]verkletree.Entry{{Key: key, Value: publicValue(1)}},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	engine, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		publicOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new proof engine: %v", err)
	}
	proof, err := engine.Prove(
		context.Background(),
		snapshot,
		[]verkletree.Key{key},
		publicProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove: %v", err)
	}
	encoded, err := proof.Bytes(
		context.Background(),
		publicProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode proof: %v", err)
	}
	tampered := append([]byte(nil), encoded...)
	tampered[len(tampered)-1] ^= 1
	decoded, decodeErr := verkletree.DecodeProof(
		context.Background(),
		tampered,
		publicProofDecodingLimits(),
	)
	if decodeErr == nil {
		if verifyErr := engine.Verify(
			context.Background(),
			decoded,
			publicProofVerificationLimits(),
		); !errors.Is(verifyErr, verkletree.ErrVerification) {
			t.Fatalf("tampered verification error = %v", verifyErr)
		}
	} else if !errors.Is(decodeErr, verkletree.ErrInvalidProof) {
		t.Fatalf("tampered decode error = %v", decodeErr)
	}
	excessiveEncodingLimits := publicProofEncodingLimits()
	excessiveEncodingLimits.MaxProofBytes = ^uint64(0)
	if _, err := proof.Bytes(
		context.Background(), excessiveEncodingLimits,
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("excessive proof encoding limits error = %v", err)
	}
	excessiveDecodingLimits := publicProofDecodingLimits()
	excessiveDecodingLimits.MaxProofBytes = ^uint64(0)
	if _, err := verkletree.DecodeProof(
		context.Background(), encoded, excessiveDecodingLimits,
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("excessive proof decoding limits error = %v", err)
	}

	if _, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.Profile{},
		publicOpeningLimits(),
	); !errors.Is(err, verkletree.ErrUnsupportedProfile) {
		t.Fatalf("unsupported profile error = %v", err)
	}
	if _, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		verkletree.OpeningLimits{},
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("invalid opening limits error = %v", err)
	}
	var zeroEngine verkletree.ProofEngine
	if _, err := zeroEngine.Prove(
		context.Background(),
		snapshot,
		[]verkletree.Key{key},
		publicProofGenerationLimits(),
	); !errors.Is(err, verkletree.ErrInvalidProofEngine) {
		t.Fatalf("zero engine error = %v", err)
	}
	if err := engine.Verify(
		context.Background(),
		verkletree.Proof{},
		publicProofVerificationLimits(),
	); !errors.Is(err, verkletree.ErrInvalidProof) {
		t.Fatalf("zero proof error = %v", err)
	}
	var zeroClaim verkletree.Claim
	if _, err := zeroClaim.Kind(); !errors.Is(err, verkletree.ErrInvalidProof) {
		t.Fatalf("zero claim kind error = %v", err)
	}
	if _, err := zeroClaim.Key(); !errors.Is(err, verkletree.ErrInvalidProof) {
		t.Fatalf("zero claim key error = %v", err)
	}
	if _, _, err := zeroClaim.Value(); !errors.Is(err, verkletree.ErrInvalidProof) {
		t.Fatalf("zero claim value error = %v", err)
	}
}

func publicOpeningLimits() verkletree.OpeningLimits {
	return verkletree.OpeningLimits{
		MaxGeneratorDerivations: 256,
		MaxPrecomputedPoints:    256,
		MaxQueries:              1_024,
		MaxScalarDecodes:        1_024 * 256,
		MaxMSMTerms:             2_048 * 256,
		MaxTemporaryBytes:       1 << 30,
		MaxWorkers:              uint32(runtime.NumCPU()),
		MaxQueuedOperations:     32,
	}
}

func publicProofGenerationLimits() verkletree.ProofGenerationLimits {
	return verkletree.ProofGenerationLimits{
		Material: verkletree.ProofMaterialLimits{
			MaxKeys:            64,
			MaxStemPaths:       64,
			MaxNodeReads:       2_048,
			MaxPathCommitments: 2_048,
			MaxPathBytes:       32_768,
			MaxTemporaryBytes:  1 << 20,
		},
		ProverQueries: verkletree.ProverQueryLimits{
			MaxKeys:           64,
			MaxQueries:        1_024,
			MaxNodeReads:      1_024,
			MaxTemporaryBytes: 16 << 20,
		},
		VerifierQueries: verkletree.VerifierQueryLimits{
			MaxQueries:        1_024,
			MaxTemporaryBytes: 16 << 20,
		},
		Proof: verkletree.ProofContainerLimits{
			MaxClaims:          64,
			MaxStemPaths:       64,
			MaxPathCommitments: 2_048,
			MaxPathDerivations: 2_048,
			MaxPathBytes:       32_768,
			MaxTemporaryBytes:  1 << 20,
		},
	}
}

func publicProofVerificationLimits() verkletree.ProofVerificationLimits {
	return verkletree.ProofVerificationLimits{
		VerifierQueries: verkletree.VerifierQueryLimits{
			MaxQueries:        1_024,
			MaxTemporaryBytes: 16 << 20,
		},
	}
}

func publicProofEncodingLimits() verkletree.ProofEncodingLimits {
	return verkletree.ProofEncodingLimits{
		MaxProofBytes:     64 << 10,
		MaxTemporaryBytes: 64 << 10,
	}
}

func publicProofDecodingLimits() verkletree.ProofDecodingLimits {
	return verkletree.ProofDecodingLimits{
		MaxProofBytes:      64 << 10,
		MaxClaims:          64,
		MaxStemPaths:       64,
		MaxPathCommitments: 2_048,
		MaxPathDerivations: 2_048,
		MaxPathBytes:       32_768,
		MaxPointDecodes:    2_048,
		MaxScalarDecodes:   1,
		MaxTemporaryBytes:  1 << 20,
	}
}

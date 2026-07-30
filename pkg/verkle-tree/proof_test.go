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
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{{Key: present, Value: publicValue(7)}},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	engine, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
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

func TestPublicProofEngineRejectsTamperingAndInvalidUse(t *testing.T) {
	t.Parallel()

	key := publicKey(0, 0)
	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{{Key: key, Value: publicValue(1)}},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	engine, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
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

	if _, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.Profile{},
		publicOpeningLimits(),
	); !errors.Is(err, verkletree.ErrUnsupportedProfile) {
		t.Fatalf("unsupported profile error = %v", err)
	}
	if _, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
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

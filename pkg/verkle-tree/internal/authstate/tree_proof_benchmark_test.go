package authstate

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
)

func BenchmarkNewTreeProofSixteen(b *testing.B) {
	root, claims, stemPaths, commitments, opening := benchmarkTreeProof(b)
	limits := benchmarkTreeProofLimits()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := NewTreeProof(
			context.Background(),
			root,
			claims,
			stemPaths,
			commitments,
			opening,
			limits,
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTreeProofCopySixteen(b *testing.B) {
	root, claims, stemPaths, commitments, opening := benchmarkTreeProof(b)
	proof, err := NewTreeProof(
		context.Background(),
		root,
		claims,
		stemPaths,
		commitments,
		opening,
		benchmarkTreeProofLimits(),
	)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, copyErr := proof.StemPaths(context.Background()); copyErr != nil {
			b.Fatal(copyErr)
		}
		if _, copyErr := proof.PathCommitments(context.Background()); copyErr != nil {
			b.Fatal(copyErr)
		}
	}
}

func benchmarkTreeProofLimits() TreeProofLimits {
	limits := testTreeProofLimits()
	limits.MaxTemporaryBytes = 128 * 1_024

	return limits
}

func benchmarkTreeProof(
	t testing.TB,
) (
	backendRoot backend.Root,
	claims ClaimSet,
	stemPaths []StemPath,
	commitments []PathCommitment,
	opening backend.OpeningProof,
) {
	t.Helper()

	backendRoot = testProofRoot(t)
	opening = testRawOpeningProof(t)
	commitment := testProofCommitment(t)
	rawClaims := make([]Claim, 16)
	stemPaths = make([]StemPath, 16)
	commitments = make([]PathCommitment, 0, 16)
	for index := range rawClaims {
		key := testKey(byte(15-index), byte(index))
		stem := stemFromKey(key)
		if index%2 == 0 {
			rawClaims[index] = Membership(key, testValue(byte(index)))
			stemPaths[index] = PresentStemPath(stem, 1)
			commitments = append(
				commitments,
				mustPathCommitment(t, []byte{key[0]}, commitment),
				mustPathCommitment(t, []byte{key[0], 2}, commitment),
			)
		} else {
			rawClaims[index] = Absence(key)
			stemPaths[index] = MissingStemPath(stem, 1)
		}
	}
	claims = mustClaimSet(t, rawClaims)

	return backendRoot, claims, stemPaths, commitments, opening
}

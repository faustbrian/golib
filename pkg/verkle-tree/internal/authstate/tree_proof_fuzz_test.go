package authstate

import (
	"bytes"
	"context"
	"slices"
	"testing"
)

const fuzzTreeProofClaimBytes = 34

func FuzzTreeProofCanonicalization(f *testing.F) {
	f.Add([]byte{1, 0})
	f.Add(bytes.Repeat([]byte{0xff}, fuzzTreeProofClaimBytes*4))
	f.Add(bytes.Repeat([]byte{0x55}, fuzzTreeProofClaimBytes*8))

	root := testProofRoot(f)
	opening := testRawOpeningProof(f)
	commitment := testProofCommitment(f)

	f.Fuzz(func(t *testing.T, encoded []byte) {
		count := len(encoded) / fuzzTreeProofClaimBytes
		if count == 0 || count > 8 {
			return
		}

		claims := make([]Claim, count)
		stemPaths := make([]StemPath, count)
		commitments := make([]PathCommitment, 0, count*2)
		for index := range count {
			offset := index * fuzzTreeProofClaimBytes
			var key Key
			key[0] = byte(index + 1)
			key[31] = encoded[offset+1]
			var value Value
			copy(value[:], encoded[offset+2:offset+fuzzTreeProofClaimBytes])
			stem := stemFromKey(key)
			if encoded[offset]&1 == 0 {
				claims[index] = Absence(key)
				stemPaths[index] = MissingStemPath(stem, 1)
				continue
			}
			claims[index] = Membership(key, value)
			stemPaths[index] = PresentStemPath(stem, 1)
			commitments = append(
				commitments,
				mustPathCommitment(t, []byte{key[0]}, commitment),
				mustPathCommitment(
					t,
					[]byte{key[0], 2 + key[31]/128},
					commitment,
				),
			)
		}

		leftClaims := mustClaimSet(t, claims)
		left, err := NewTreeProof(
			context.Background(),
			root,
			leftClaims,
			stemPaths,
			commitments,
			opening,
			testTreeProofLimits(),
		)
		if err != nil {
			t.Fatalf("canonical proof: %v", err)
		}

		slices.Reverse(claims)
		slices.Reverse(stemPaths)
		slices.Reverse(commitments)
		rightClaims := mustClaimSet(t, claims)
		right, err := NewTreeProof(
			context.Background(),
			root,
			rightClaims,
			stemPaths,
			commitments,
			opening,
			testTreeProofLimits(),
		)
		if err != nil {
			t.Fatalf("reordered proof: %v", err)
		}

		assertSameTreeProof(t, left, right)
	})
}

func assertSameTreeProof(t testing.TB, left TreeProof, right TreeProof) {
	t.Helper()

	leftClaims, err := left.Claims()
	if err != nil {
		t.Fatalf("left claims: %v", err)
	}
	rightClaims, err := right.Claims()
	if err != nil {
		t.Fatalf("right claims: %v", err)
	}
	leftClaimValues, err := leftClaims.Claims(context.Background())
	if err != nil {
		t.Fatalf("left claim values: %v", err)
	}
	rightClaimValues, err := rightClaims.Claims(context.Background())
	if err != nil {
		t.Fatalf("right claim values: %v", err)
	}
	if !slices.Equal(leftClaimValues, rightClaimValues) {
		t.Fatal("claim order depends on input order")
	}

	leftPaths, err := left.StemPaths(context.Background())
	if err != nil {
		t.Fatalf("left stem paths: %v", err)
	}
	rightPaths, err := right.StemPaths(context.Background())
	if err != nil {
		t.Fatalf("right stem paths: %v", err)
	}
	if !slices.Equal(leftPaths, rightPaths) {
		t.Fatal("stem-path order depends on input order")
	}

	leftCommitments, err := left.PathCommitments(context.Background())
	if err != nil {
		t.Fatalf("left path commitments: %v", err)
	}
	rightCommitments, err := right.PathCommitments(context.Background())
	if err != nil {
		t.Fatalf("right path commitments: %v", err)
	}
	if !slices.Equal(leftCommitments, rightCommitments) {
		t.Fatal("path-commitment order depends on input order")
	}
}

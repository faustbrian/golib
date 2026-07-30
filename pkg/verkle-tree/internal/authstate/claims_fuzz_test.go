package authstate

import (
	"context"
	"slices"
	"testing"

	internalprofile "github.com/faustbrian/golib/pkg/verkle-tree/internal/profile"
)

func FuzzClaimSetCanonicalization(f *testing.F) {
	f.Add([]byte(nil))
	f.Add(make([]byte, 65))
	f.Add([]byte{1, 1, 2, 3})

	f.Fuzz(func(t *testing.T, encoded []byte) {
		const maxClaims = 8
		if len(encoded) > maxClaims*65 {
			return
		}
		claims := fuzzClaims(encoded)
		reversed := slices.Clone(claims)
		slices.Reverse(reversed)
		limits := ClaimLimits{
			MaxClaims:         maxClaims,
			MaxTemporaryBytes: maxClaims * 2 * claimWorkingBytes,
		}
		left, leftErr := NewClaimSet(
			context.Background(),
			internalprofile.ExperimentalBandersnatchIPA256V0(),
			claims,
			limits,
		)
		right, rightErr := NewClaimSet(
			context.Background(),
			internalprofile.ExperimentalBandersnatchIPA256V0(),
			reversed,
			limits,
		)
		if (leftErr == nil) != (rightErr == nil) {
			t.Fatalf("reordering changed acceptance: %v / %v", leftErr, rightErr)
		}
		if leftErr != nil {
			return
		}
		leftClaims, err := left.Claims(context.Background())
		if err != nil {
			t.Fatalf("left claims: %v", err)
		}
		rightClaims, err := right.Claims(context.Background())
		if err != nil {
			t.Fatalf("right claims: %v", err)
		}
		if !slices.Equal(leftClaims, rightClaims) {
			t.Fatal("claim canonicalization depends on input order")
		}
		for index := 1; index < len(leftClaims); index++ {
			if compareKey(leftClaims[index-1].key, leftClaims[index].key) >= 0 {
				t.Fatal("accepted claims are not strictly ordered")
			}
		}
	})
}

func fuzzClaims(encoded []byte) []Claim {
	count := len(encoded) / 65
	claims := make([]Claim, count)
	for index := range claims {
		offset := index * 65
		var key Key
		copy(key[:], encoded[offset+1:offset+33])
		if encoded[offset]&1 == 0 {
			claims[index] = Absence(key)
			continue
		}
		var value Value
		copy(value[:], encoded[offset+33:offset+65])
		claims[index] = Membership(key, value)
	}

	return claims
}

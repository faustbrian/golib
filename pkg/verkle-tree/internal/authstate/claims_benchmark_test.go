package authstate

import (
	"context"
	"testing"

	internalprofile "github.com/faustbrian/golib/pkg/verkle-tree/internal/profile"
)

const benchmarkClaimBytes = 1 + len(Key{}) + len(Value{})

func BenchmarkNewClaimSetSixteen(b *testing.B) {
	claims := benchmarkClaims()
	limits := testClaimLimits()
	profile := internalprofile.ExperimentalBandersnatchIPA256V0()
	b.ReportAllocs()
	b.SetBytes(int64(len(claims) * benchmarkClaimBytes))
	b.ResetTimer()
	for range b.N {
		if _, err := NewClaimSet(
			context.Background(),
			profile,
			claims,
			limits,
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkClaimSetCopySixteen(b *testing.B) {
	set, err := NewClaimSet(
		context.Background(),
		internalprofile.ExperimentalBandersnatchIPA256V0(),
		benchmarkClaims(),
		testClaimLimits(),
	)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(16 * benchmarkClaimBytes))
	b.ResetTimer()
	for range b.N {
		if _, copyErr := set.Claims(context.Background()); copyErr != nil {
			b.Fatal(copyErr)
		}
	}
}

func benchmarkClaims() []Claim {
	claims := make([]Claim, 16)
	for index := range claims {
		key := testKey(byte(15-index), byte(index))
		if index%2 == 0 {
			claims[index] = Membership(key, testValue(byte(index)))
		} else {
			claims[index] = Absence(key)
		}
	}

	return claims
}

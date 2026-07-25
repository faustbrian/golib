package bip39_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/keyphrase/bip39"
)

func BenchmarkFromEntropy(b *testing.B) {
	entropy := make([]byte, 32)
	for b.Loop() {
		if _, err := bip39.FromEntropy(entropy, bip39.English); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseAndNormalize(b *testing.B) {
	phrase := "あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あおぞら"
	for b.Loop() {
		if _, err := bip39.ParseLanguage(phrase, bip39.Japanese); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSeedDerivation(b *testing.B) {
	mnemonic, err := bip39.FromEntropy(make([]byte, 16), bip39.English)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		seed, seedErr := bip39.Seed(context.Background(), mnemonic, "benchmark")
		if seedErr != nil {
			b.Fatal(seedErr)
		}
		clear(seed)
	}
}

package passphrase_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/keyphrase/passphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/wordlist/eff"
)

func BenchmarkGenerateEFFLarge(b *testing.B) {
	list, err := eff.Large()
	if err != nil {
		b.Fatal(err)
	}
	policy := passphrase.Policy{WordList: list, Words: 6, Separator: " "}
	generator := passphrase.DefaultGenerator()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		secret, generationErr := generator.Generate(ctx, policy)
		if generationErr != nil {
			b.Fatal(generationErr)
		}
		clear(secret)
	}
}

func BenchmarkParseEFFLarge(b *testing.B) {
	list, err := eff.Large()
	if err != nil {
		b.Fatal(err)
	}
	policy := passphrase.Policy{WordList: list, Words: 6, Separator: " "}
	encoded := []byte("abacus abdomen abdominal abide abiding ability")
	for b.Loop() {
		if _, parseErr := passphrase.Parse(encoded, policy); parseErr != nil {
			b.Fatal(parseErr)
		}
	}
}

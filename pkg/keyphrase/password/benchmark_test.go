package password_test

import (
	"context"
	"testing"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/keyphrasetest"
	"github.com/faustbrian/golib/pkg/keyphrase/password"
)

func BenchmarkGenerateConstrained(b *testing.B) {
	policy := password.Policy{
		Length:   24,
		Alphabet: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%",
		Required: []password.Class{
			{Name: "lower", Characters: "abcdefghijklmnopqrstuvwxyz"},
			{Name: "upper", Characters: "ABCDEFGHIJKLMNOPQRSTUVWXYZ"},
			{Name: "digits", Characters: "0123456789"},
			{Name: "symbols", Characters: "!@#$%"},
		},
	}
	generator := password.DefaultGenerator()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		secret, err := generator.Generate(ctx, policy)
		if err != nil {
			b.Fatal(err)
		}
		clear(secret)
	}
}

func BenchmarkAnalyzeLargePolicy(b *testing.B) {
	policy := password.Policy{Length: 1024, Alphabet: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"}
	for b.Loop() {
		if _, err := password.Analyze(policy); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRejectOversizedPolicy(b *testing.B) {
	policy := password.Policy{
		Length:   1024,
		Alphabet: "ab",
		Required: make([]password.Class, 10),
	}
	for b.Loop() {
		if _, err := password.Analyze(policy); err == nil {
			b.Fatal("oversized policy was accepted")
		}
	}
}

func BenchmarkGenerateSourceFailure(b *testing.B) {
	selector, err := keyphrase.NewSelector(keyphrasetest.NewSource(nil))
	if err != nil {
		b.Fatal(err)
	}
	generator, err := password.NewGenerator(selector)
	if err != nil {
		b.Fatal(err)
	}
	policy := password.Policy{Length: 24, Alphabet: "abcdefghijklmnopqrstuvwxyz"}
	b.ReportAllocs()
	for b.Loop() {
		if _, generationErr := generator.Generate(context.Background(), policy); generationErr == nil {
			b.Fatal("failing source generated a password")
		}
	}
}

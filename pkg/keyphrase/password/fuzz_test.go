package password_test

import (
	"context"
	"testing"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/keyphrasetest"
	"github.com/faustbrian/golib/pkg/keyphrase/password"
)

func FuzzAnalyzeAlphabet(f *testing.F) {
	f.Add("abcABC123", "abc", "ABC", 12)
	f.Add("ÅÅ", "Å", "Å", 2)
	f.Fuzz(func(t *testing.T, alphabet, firstClass, secondClass string, length int) {
		if len(alphabet) > 256 || len(firstClass) > 128 || len(secondClass) > 128 {
			t.Skip()
		}
		length = length%33 + 1
		policy := password.Policy{
			Length:   length,
			Alphabet: alphabet,
			Required: []password.Class{
				{Name: "first", Characters: firstClass},
				{Name: "second", Characters: secondClass},
			},
		}
		if _, err := password.Analyze(policy); err != nil {
			return
		}
		selector, err := keyphrase.NewSelector(keyphrasetest.NewSource(make([]byte, 512)))
		if err != nil {
			t.Fatalf("NewSelector() error = %v", err)
		}
		generator, err := password.NewGenerator(selector)
		if err != nil {
			t.Fatalf("NewGenerator() error = %v", err)
		}
		secret, err := generator.Generate(context.Background(), policy)
		if err != nil {
			t.Fatalf("Generate(valid policy) error = %v", err)
		}
		if err := password.Validate(policy, secret); err != nil {
			t.Fatalf("Validate(generated) error = %v", err)
		}
		clear(secret)
	})
}

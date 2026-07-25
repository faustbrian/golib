package password_test

import (
	"context"
	"math"
	"math/big"
	"testing"
	"unicode"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/keyphrasetest"
	"github.com/faustbrian/golib/pkg/keyphrase/password"
)

func TestRequiredClassesHaveNoDeterministicPosition(t *testing.T) {
	t.Parallel()

	policy := password.Policy{
		Length:   4,
		Alphabet: "abCD",
		Required: []password.Class{
			{Name: "lower", Characters: "ab"},
			{Name: "upper", Characters: "CD"},
		},
	}
	distribution, err := password.Analyze(policy)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	selector, err := keyphrase.NewSelector(keyphrasetest.NewCounterSource())
	if err != nil {
		t.Fatalf("NewSelector() error = %v", err)
	}
	generator, err := password.NewGenerator(selector)
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
	lowerAt := make([]bool, policy.Length)
	upperAt := make([]bool, policy.Length)
	for range distribution.Outcomes.Int64() {
		secret, generationErr := generator.Generate(context.Background(), policy)
		if generationErr != nil {
			t.Fatalf("Generate() error = %v", generationErr)
		}
		if validationErr := password.Validate(policy, secret); validationErr != nil {
			t.Fatalf("Validate() error = %v", validationErr)
		}
		for index, character := range string(secret) {
			lowerAt[index] = lowerAt[index] || unicode.IsLower(character)
			upperAt[index] = upperAt[index] || unicode.IsUpper(character)
		}
		clear(secret)
	}
	for position := range policy.Length {
		if !lowerAt[position] || !upperAt[position] {
			t.Fatalf("position %d class observations: lower=%t upper=%t", position, lowerAt[position], upperAt[position])
		}
	}
}

func TestAnalyzeMatchesBruteForceProperties(t *testing.T) {
	t.Parallel()

	policies := []password.Policy{
		{Length: 1, Alphabet: "ab"},
		{Length: 2, Alphabet: "ab", Required: []password.Class{{Name: "a", Characters: "a"}}},
		{Length: 3, Alphabet: "abc", Required: []password.Class{{Name: "left", Characters: "ab"}, {Name: "right", Characters: "bc"}}},
		{Length: 3, Alphabet: "abcd", Excluded: "d", Required: []password.Class{{Name: "edge", Characters: "ac"}}},
		{Length: 2, Alphabet: "aéΩ", Required: []password.Class{{Name: "accent", Characters: "é"}, {Name: "greek", Characters: "Ω"}}},
	}

	for _, policy := range policies {
		distribution, err := password.Analyze(policy)
		if err != nil {
			t.Fatalf("Analyze(%+v) error = %v", policy, err)
		}
		want := bruteForceOutcomes(policy)
		if distribution.Outcomes.Cmp(want) != 0 {
			t.Fatalf("Analyze(%+v) outcomes = %s, want %s", policy, distribution.Outcomes, want)
		}
		if math.Abs(distribution.Bits-math.Log2(float64(want.Int64()))) > 1e-12 {
			t.Fatalf("Analyze(%+v) bits = %.12f, want log2(%s)", policy, distribution.Bits, want)
		}
	}
}

func bruteForceOutcomes(policy password.Policy) *big.Int {
	excluded := make(map[rune]struct{})
	for _, symbol := range policy.Excluded {
		excluded[symbol] = struct{}{}
	}
	alphabet := make([]rune, 0, len([]rune(policy.Alphabet)))
	for _, symbol := range policy.Alphabet {
		if _, blocked := excluded[symbol]; !blocked {
			alphabet = append(alphabet, symbol)
		}
	}

	sequence := make([]rune, policy.Length)
	count := int64(0)
	var enumerate func(int)
	enumerate = func(position int) {
		if position < policy.Length {
			for _, symbol := range alphabet {
				sequence[position] = symbol
				enumerate(position + 1)
			}
			return
		}
		for _, class := range policy.Required {
			matched := false
			for _, symbol := range sequence {
				if containsRune(class.Characters, symbol) {
					matched = true
					break
				}
			}
			if !matched {
				return
			}
		}
		count++
	}
	enumerate(0)

	return big.NewInt(count)
}

func containsRune(value string, target rune) bool {
	for _, candidate := range value {
		if candidate == target {
			return true
		}
	}
	return false
}

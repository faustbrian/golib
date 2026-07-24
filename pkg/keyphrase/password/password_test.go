package password_test

import (
	"context"
	"errors"
	"math"
	"testing"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/password"
)

type repeatingSource struct {
	value byte
	err   error
}

func (s repeatingSource) ReadContext(_ context.Context, destination []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	for index := range destination {
		destination[index] = s.value
	}
	return len(destination), nil
}

func TestAnalyzeReportsExactConstrainedDistribution(t *testing.T) {
	t.Parallel()

	policy := password.Policy{
		Length:   2,
		Alphabet: "abCD",
		Required: []password.Class{
			{Name: "lowercase", Characters: "ab"},
			{Name: "uppercase", Characters: "CD"},
		},
	}

	distribution, err := password.Analyze(policy)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if distribution.Outcomes.String() != "8" {
		t.Fatalf("outcomes = %s, want 8", distribution.Outcomes)
	}
	if math.Abs(distribution.Bits-3) > 1e-12 {
		t.Fatalf("bits = %.12f, want 3", distribution.Bits)
	}
}

func TestGenerateUniformlySamplesValidPasswords(t *testing.T) {
	t.Parallel()

	selector, err := keyphrase.NewSelector(repeatingSource{value: 0})
	if err != nil {
		t.Fatalf("NewSelector() error = %v", err)
	}
	generator, err := password.NewGenerator(selector)
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
	policy := password.Policy{
		Length:   4,
		Alphabet: "abCD",
		Excluded: "b",
		Required: []password.Class{
			{Name: "lowercase", Characters: "ab"},
			{Name: "uppercase", Characters: "CD"},
		},
	}

	secret, err := generator.Generate(context.Background(), policy)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if string(secret) != "aaaC" {
		t.Fatalf("Generate() = %q, want deterministic aaaC", secret)
	}
}

func TestGenerateIntoNeverReturnsPartialSecrets(t *testing.T) {
	t.Parallel()

	sourceFailure := errors.New("source contains secret-looking diagnostic")
	selector, err := keyphrase.NewSelector(repeatingSource{err: sourceFailure})
	if err != nil {
		t.Fatalf("NewSelector() error = %v", err)
	}
	generator, err := password.NewGenerator(selector)
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
	destination := []byte("unchanged")

	n, err := generator.GenerateInto(context.Background(), destination, password.Policy{Length: 8, Alphabet: "ab"})
	if n != 0 {
		t.Fatalf("GenerateInto() count = %d, want 0", n)
	}
	if string(destination) != "unchanged" {
		t.Fatalf("destination = %q, want unchanged", destination)
	}
	if errors.Is(err, sourceFailure) && err.Error() == sourceFailure.Error() {
		t.Fatal("error disclosed the source diagnostic")
	}
}

func TestAnalyzeRejectsInvalidOrImpossiblePolicies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy password.Policy
		code   password.ErrorCode
	}{
		{name: "empty alphabet", policy: password.Policy{Length: 1}, code: password.CodeInvalidAlphabet},
		{name: "duplicate alphabet", policy: password.Policy{Length: 1, Alphabet: "aa"}, code: password.CodeDuplicateSymbol},
		{name: "normalization collision", policy: password.Policy{Length: 1, Alphabet: "ÅÅ"}, code: password.CodeNormalizationCollision},
		{name: "impossible requirements", policy: password.Policy{Length: 1, Alphabet: "ab", Required: []password.Class{{Name: "a", Characters: "a"}, {Name: "b", Characters: "b"}}}, code: password.CodeImpossible},
		{name: "minimum entropy", policy: password.Policy{Length: 2, Alphabet: "ab", MinimumEntropyBits: 3}, code: password.CodeInsufficientEntropy},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := password.Analyze(test.policy)
			var policyError *password.Error
			if !errors.As(err, &policyError) {
				t.Fatalf("Analyze() error = %v, want *password.Error", err)
			}
			if policyError.Code != test.code {
				t.Fatalf("error code = %q, want %q", policyError.Code, test.code)
			}
		})
	}
}

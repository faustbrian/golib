package passphrase_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/passphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/password"
	"github.com/faustbrian/golib/pkg/keyphrase/wordlist"
)

type fixedSource struct {
	value byte
	err   error
}

func (s fixedSource) ReadContext(_ context.Context, destination []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	for index := range destination {
		destination[index] = s.value
	}
	return len(destination), nil
}

func TestGenerateAnalyzeAndParseRoundTrip(t *testing.T) {
	t.Parallel()

	list := testList(t, []string{"alpha", "bravo", "charlie"})
	policy := passphrase.Policy{
		WordList:  list,
		Words:     3,
		Separator: "-",
		Casing:    passphrase.Upper,
		Prefix: &passphrase.Affix{
			Separator: ":",
			Policy:    password.Policy{Length: 2, Alphabet: "01"},
		},
		Suffix: &passphrase.Affix{
			Separator: ".",
			Policy:    password.Policy{Length: 2, Alphabet: "xy"},
		},
	}

	distribution, err := passphrase.Analyze(policy)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if distribution.Outcomes.String() != "432" || math.Abs(distribution.Bits-math.Log2(432)) > 1e-12 {
		t.Fatalf("distribution = %s, %.12f, want 432", distribution.Outcomes, distribution.Bits)
	}

	selector, err := keyphrase.NewSelector(fixedSource{value: 0})
	if err != nil {
		t.Fatalf("NewSelector() error = %v", err)
	}
	generator, err := passphrase.NewGenerator(selector)
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
	secret, err := generator.Generate(context.Background(), policy)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if string(secret) != "00:ALPHA-ALPHA-ALPHA.xx" {
		t.Fatalf("Generate() = %q", secret)
	}

	parsed, err := passphrase.Parse(secret, policy)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if string(parsed.Prefix) != "00" || string(parsed.Suffix) != "xx" || len(parsed.Words) != 3 || parsed.Words[1] != "ALPHA" {
		t.Fatalf("Parse() = %#v", parsed)
	}
}

func TestAnalyzeRejectsAmbiguousPolicies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy passphrase.Policy
		code   passphrase.ErrorCode
	}{
		{name: "casing collision", policy: passphrase.Policy{WordList: testList(t, []string{"word", "WORD"}), Words: 2, Separator: "-", Casing: passphrase.Lower}, code: passphrase.CodeCollision},
		{name: "separator in word", policy: passphrase.Policy{WordList: testList(t, []string{"two-words", "other"}), Words: 2, Separator: "-"}, code: passphrase.CodeAmbiguousSeparator},
		{name: "affix separator in alphabet", policy: passphrase.Policy{WordList: testList(t, []string{"one", "two"}), Words: 2, Separator: "-", Prefix: &passphrase.Affix{Separator: ":", Policy: password.Policy{Length: 2, Alphabet: "a:"}}}, code: passphrase.CodeAmbiguousSeparator},
		{name: "invalid affix separator", policy: passphrase.Policy{WordList: testList(t, []string{"one", "two"}), Words: 2, Separator: "-", Prefix: &passphrase.Affix{Separator: string([]byte{0xff}), Policy: password.Policy{Length: 2, Alphabet: "ab"}}}, code: passphrase.CodeAmbiguousSeparator},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := passphrase.Analyze(test.policy)
			var policyError *passphrase.Error
			if !errors.As(err, &policyError) {
				t.Fatalf("Analyze() error = %v, want *passphrase.Error", err)
			}
			if policyError.Code != test.code {
				t.Fatalf("error code = %q, want %q", policyError.Code, test.code)
			}
		})
	}
}

func TestAnalyzeBoundsCallerProvidedListWork(t *testing.T) {
	t.Parallel()

	words := make([]string, 32_769)
	for index := range words {
		words[index] = fmt.Sprintf("word%05d", index)
	}
	_, err := passphrase.Analyze(passphrase.Policy{WordList: testList(t, words), Words: 2, Separator: "-"})
	var policyError *passphrase.Error
	if !errors.As(err, &policyError) || policyError.Code != passphrase.CodeOversized {
		t.Fatalf("Analyze(large list) error = %v", err)
	}
}

func testList(t *testing.T, words []string) *wordlist.List {
	t.Helper()

	checksum := wordlist.Checksum(words)
	list, err := wordlist.New(wordlist.Metadata{
		ID:            "test",
		Language:      "en",
		Source:        "test",
		Version:       "1",
		License:       "CC0-1.0",
		ExpectedWords: len(words),
		SHA256:        checksum,
		SourceSHA256:  checksum,
	}, words)
	if err != nil {
		t.Fatalf("wordlist.New() error = %v", err)
	}
	return list
}

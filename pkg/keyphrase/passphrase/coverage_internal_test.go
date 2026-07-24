package passphrase

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/keyphrasetest"
	"github.com/faustbrian/golib/pkg/keyphrase/password"
	"github.com/faustbrian/golib/pkg/keyphrase/wordlist"
)

type errorSource struct{}

func (errorSource) ReadContext(context.Context, []byte) (int, error) {
	return 0, errors.New("source error")
}

func TestErrorAndGeneratorConstruction(t *testing.T) {
	t.Parallel()

	phraseError := &Error{Code: CodeRandomness, Cause: errors.New("cause")}
	if phraseError.Error() != "passphrase: operation failed (randomness)" || phraseError.Unwrap() == nil {
		t.Fatal("Error contract mismatch")
	}
	if _, err := NewGenerator(nil); errorCode(err) != CodeInvalidGenerator {
		t.Fatalf("NewGenerator(nil) code = %q", errorCode(err))
	}
	if DefaultGenerator() == nil {
		t.Fatal("DefaultGenerator() returned nil")
	}
	if applyCasing("Word", Casing(255)) != "Word" {
		t.Fatal("invalid casing did not preserve the word")
	}
	policy := Policy{WordList: internalList(t, []string{"alpha", "bravo"}), Words: 2, Separator: "-"}
	for _, generator := range []*Generator{nil, {}} {
		if secret, err := generator.Generate(context.Background(), policy); secret != nil || errorCode(err) != CodeInvalidGenerator {
			t.Fatalf("Generate(uninitialized) = %v, %v", secret, err)
		}
	}
}

func TestPolicyFailureMatrix(t *testing.T) {
	t.Parallel()

	list := internalList(t, []string{"alpha", "bravo"})
	tests := []struct {
		name   string
		policy Policy
		code   ErrorCode
	}{
		{name: "nil list", policy: Policy{Words: 2, Separator: "-"}, code: CodeInvalidPolicy},
		{name: "too few words", policy: Policy{WordList: list, Words: 1, Separator: "-"}, code: CodeInvalidPolicy},
		{name: "invalid casing", policy: Policy{WordList: list, Words: 2, Separator: "-", Casing: Upper + 1}, code: CodeInvalidPolicy},
		{name: "invalid entropy", policy: Policy{WordList: list, Words: 2, Separator: "-", MinimumEntropyBits: math.NaN()}, code: CodeInvalidPolicy},
		{name: "too many words", policy: Policy{WordList: list, Words: maxWords + 1, Separator: "-"}, code: CodeOversized},
		{name: "invalid affix policy", policy: Policy{WordList: list, Words: 2, Separator: "-", Prefix: &Affix{Separator: ":", Policy: password.Policy{}}}, code: CodeInvalidPolicy},
		{name: "minimum entropy", policy: Policy{WordList: list, Words: 2, Separator: "-", MinimumEntropyBits: 3}, code: CodeInsufficientEntropy},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Analyze(test.policy); errorCode(err) != test.code {
				t.Fatalf("Analyze() code = %q, want %q", errorCode(err), test.code)
			}
		})
	}

	longWord := strings.Repeat("x", 1024)
	longList := internalList(t, []string{longWord, strings.Repeat("y", 1024)})
	if _, err := Analyze(Policy{WordList: longList, Words: maxWords, Separator: strings.Repeat("-", maxSeparator)}); errorCode(err) != CodeOversized {
		t.Fatalf("Analyze(oversized output) code = %q", errorCode(err))
	}
	if distribution, err := Analyze(Policy{WordList: internalList(t, []string{"a", "b", "c"}), Words: maxWords, Separator: "-"}); err != nil || distribution.Bits <= 53 {
		t.Fatalf("Analyze(large entropy) = %#v, %v", distribution, err)
	}
}

func TestGenerationFailureStagesReturnNoPhrase(t *testing.T) {
	t.Parallel()

	list := internalList(t, []string{"alpha", "bravo"})
	base := Policy{WordList: list, Words: 2, Separator: "-"}
	failingSelector, _ := keyphrase.NewSelector(errorSource{})
	generator, _ := NewGenerator(failingSelector)
	if secret, err := generator.generateAffix(context.Background(), nil); secret != nil || err != nil {
		t.Fatalf("generateAffix(nil) = %v, %v", secret, err)
	}
	if secret, err := generator.Generate(context.Background(), base); secret != nil || errorCode(err) != CodeRandomness {
		t.Fatalf("Generate(word failure) = %v, %v", secret, err)
	}
	if secret, err := generator.Generate(context.Background(), Policy{}); secret != nil || errorCode(err) != CodeInvalidPolicy {
		t.Fatalf("Generate(policy failure) = %v, %v", secret, err)
	}

	affix := &Affix{Separator: ":", Policy: password.Policy{Length: 1, Alphabet: "01"}}
	prefixPolicy := base
	prefixPolicy.Prefix = affix
	selector, _ := keyphrase.NewSelector(keyphrasetest.NewSource([]byte{0, 0}))
	generator, _ = NewGenerator(selector)
	if secret, err := generator.Generate(context.Background(), prefixPolicy); secret != nil || errorCode(err) != CodeRandomness {
		t.Fatalf("Generate(prefix failure) = %v, %v", secret, err)
	}

	suffixPolicy := prefixPolicy
	suffixPolicy.Suffix = affix
	selector, _ = keyphrase.NewSelector(keyphrasetest.NewSource([]byte{0, 0, 0}))
	generator, _ = NewGenerator(selector)
	if secret, err := generator.Generate(context.Background(), suffixPolicy); secret != nil || errorCode(err) != CodeRandomness {
		t.Fatalf("Generate(suffix failure) = %v, %v", secret, err)
	}
}

func TestParsingFailureMatrix(t *testing.T) {
	t.Parallel()

	list := internalList(t, []string{"alpha", "bravo"})
	base := Policy{WordList: list, Words: 2, Separator: "-"}
	if _, err := Parse(nil, Policy{}); errorCode(err) != CodeInvalidPolicy {
		t.Fatalf("Parse(policy) code = %q", errorCode(err))
	}
	if _, err := Parse([]byte{0xff}, base); errorCode(err) != CodeInvalidPhrase {
		t.Fatalf("Parse(UTF-8) code = %q", errorCode(err))
	}
	if _, err := Parse([]byte("alpha"), base); errorCode(err) != CodeInvalidPhrase {
		t.Fatalf("Parse(count) code = %q", errorCode(err))
	}
	if _, err := Parse([]byte("alpha-unknown"), base); errorCode(err) != CodeInvalidPhrase {
		t.Fatalf("Parse(word) code = %q", errorCode(err))
	}

	affix := &Affix{Separator: ":", Policy: password.Policy{Length: 1, Alphabet: "01"}}
	withPrefix := base
	withPrefix.Prefix = affix
	if _, err := Parse([]byte("alpha-bravo"), withPrefix); errorCode(err) != CodeInvalidPhrase {
		t.Fatalf("Parse(prefix missing) code = %q", errorCode(err))
	}
	if _, err := Parse([]byte("x:alpha-bravo"), withPrefix); errorCode(err) != CodeInvalidPhrase {
		t.Fatalf("Parse(prefix invalid) code = %q", errorCode(err))
	}
	withSuffix := withPrefix
	withSuffix.Suffix = affix
	if _, err := Parse([]byte("0:alpha-bravo:x"), withSuffix); errorCode(err) != CodeInvalidPhrase {
		t.Fatalf("Parse(suffix invalid) code = %q", errorCode(err))
	}
	suffixOnly := base
	suffixOnly.Suffix = affix
	if _, err := Parse([]byte("0"), suffixOnly); errorCode(err) != CodeInvalidPhrase {
		t.Fatalf("Parse(suffix missing) code = %q", errorCode(err))
	}

	maximumWord := strings.Repeat("z", 1024)
	maximumList := internalList(t, []string{maximumWord, strings.Repeat("y", 1024)})
	maximumPolicy := Policy{WordList: maximumList, Words: maxWords, Separator: "-"}
	maximumPhrase := []byte(strings.Repeat(maximumWord+"-", maxWords-1) + maximumWord)
	if len(maximumPhrase) != maxOutputSize {
		t.Fatalf("maximum phrase length = %d, want %d", len(maximumPhrase), maxOutputSize)
	}
	if parsed, err := Parse(maximumPhrase, maximumPolicy); err != nil || len(parsed.Words) != maxWords {
		t.Fatalf("Parse(maximum phrase) words = %d, error = %v", len(parsed.Words), err)
	}
}

func internalList(t *testing.T, words []string) *wordlist.List {
	t.Helper()
	checksum := wordlist.Checksum(words)
	list, err := wordlist.New(wordlist.Metadata{
		ID: "internal", Language: "en", Source: "internal", Version: "1",
		License: "CC0-1.0", ExpectedWords: len(words), SHA256: checksum,
		SourceSHA256: checksum,
	}, words)
	if err != nil {
		t.Fatalf("wordlist.New() error = %v", err)
	}
	return list
}

func errorCode(err error) ErrorCode {
	var phraseError *Error
	if errors.As(err, &phraseError) {
		return phraseError.Code
	}
	return ""
}

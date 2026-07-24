// Package passphrase generates and validates uniformly selected word-list
// passphrases with optional independently generated password affixes.
package passphrase

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/big"
	"strings"
	"unicode/utf8"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/password"
	"github.com/faustbrian/golib/pkg/keyphrase/wordlist"
)

const (
	maxWords      = 128
	maxListWords  = 1 << 15
	maxSeparator  = 64
	maxOutputSize = 131_199
)

// ErrorCode identifies a passphrase policy, parsing, or generation failure.
type ErrorCode string

const (
	// CodeInvalidPolicy reports malformed passphrase configuration.
	CodeInvalidPolicy ErrorCode = "invalid_policy"
	// CodeCollision reports a casing transform that collapses list entries.
	CodeCollision ErrorCode = "collision"
	// CodeAmbiguousSeparator reports a separator that prevents exact parsing.
	CodeAmbiguousSeparator ErrorCode = "ambiguous_separator"
	// CodeInsufficientEntropy reports an unmet entropy floor.
	CodeInsufficientEntropy ErrorCode = "insufficient_entropy"
	// CodeOversized reports a policy or input above resource limits.
	CodeOversized ErrorCode = "oversized"
	// CodeRandomness reports a failure while sampling words or affixes.
	CodeRandomness ErrorCode = "randomness"
	// CodeInvalidPhrase reports a phrase that does not satisfy its policy.
	CodeInvalidPhrase ErrorCode = "invalid_phrase"
	// CodeInvalidGenerator reports a nil generator dependency.
	CodeInvalidGenerator ErrorCode = "invalid_generator"
)

// Error omits phrase contents and generated secret material.
type Error struct {
	Code  ErrorCode
	Cause error
}

func (e *Error) Error() string {
	return fmt.Sprintf("passphrase: operation failed (%s)", e.Code)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

// Format prevents wrapped diagnostics from appearing in debug output.
func (e *Error) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, e.Error())
}

// MarshalText omits wrapped diagnostics from encoded output.
func (e *Error) MarshalText() ([]byte, error) {
	return []byte(e.Error()), nil
}

// Casing controls a deterministic display transform.
type Casing uint8

const (
	// Preserve keeps list words unchanged.
	Preserve Casing = iota
	// Lower applies Unicode lowercase mapping.
	Lower
	// Upper applies Unicode uppercase mapping.
	Upper
)

// Affix is an independently generated password separated from the word
// phrase by Separator.
type Affix struct {
	Separator string
	Policy    password.Policy
}

// Policy defines a passphrase distribution.
type Policy struct {
	WordList           *wordlist.List
	Words              int
	Separator          string
	Casing             Casing
	Prefix             *Affix
	Suffix             *Affix
	MinimumEntropyBits float64
}

// Distribution describes the exact number of possible outputs.
type Distribution struct {
	Outcomes *big.Int
	Bits     float64
}

// Parsed contains caller-owned affix buffers and transformed list words.
type Parsed struct {
	Prefix keyphrase.Secret
	Words  []string
	Suffix keyphrase.Secret
}

// Format prevents parsed phrase disclosure through formatting verbs.
func (Parsed) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "passphrase.Parsed{redacted}")
}

// LogValue prevents parsed phrase disclosure through log/slog.
func (Parsed) LogValue() slog.Value {
	return slog.StringValue("passphrase.Parsed{redacted}")
}

// MarshalText prevents parsed phrase disclosure through standard encoders.
func (Parsed) MarshalText() ([]byte, error) {
	return []byte("passphrase.Parsed{redacted}"), nil
}

// Generator generates passphrases from a shared unbiased selector.
type Generator struct {
	selector  *keyphrase.Selector
	passwords *password.Generator
}

type preparedPolicy struct {
	words        []string
	distribution Distribution
}

// NewGenerator creates a passphrase generator.
func NewGenerator(selector *keyphrase.Selector) (*Generator, error) {
	if selector == nil {
		return nil, &Error{Code: CodeInvalidGenerator}
	}
	passwordGenerator, _ := password.NewGenerator(selector)
	return &Generator{selector: selector, passwords: passwordGenerator}, nil
}

// DefaultGenerator returns a generator backed by crypto/rand.
func DefaultGenerator() *Generator {
	selector := keyphrase.DefaultSelector()
	passwordGenerator, _ := password.NewGenerator(selector)
	return &Generator{selector: selector, passwords: passwordGenerator}
}

// Analyze validates policy and returns its exact output count and entropy.
func Analyze(policy Policy) (Distribution, error) {
	prepared, err := prepare(policy)
	if err != nil {
		return Distribution{}, err
	}

	return Distribution{
		Outcomes: new(big.Int).Set(prepared.distribution.Outcomes),
		Bits:     prepared.distribution.Bits,
	}, nil
}

// Generate returns a complete caller-owned UTF-8 buffer and never returns a
// partial phrase on failure.
func (g *Generator) Generate(ctx context.Context, policy Policy) (keyphrase.Secret, error) {
	if g == nil || g.selector == nil || g.passwords == nil {
		return nil, &Error{Code: CodeInvalidGenerator}
	}
	prepared, err := prepare(policy)
	if err != nil {
		return nil, err
	}

	indices := make([]int, policy.Words)
	for index := range indices {
		selection, selectionErr := g.selector.Index(ctx, uint64(policy.WordList.Len())) //nolint:gosec // prepare bounds the positive list length.
		if selectionErr != nil {
			clear(indices)
			return nil, &Error{Code: CodeRandomness, Cause: selectionErr}
		}
		indices[index] = int(selection) //nolint:gosec // selection is less than the bounded list length.
	}

	prefix, err := g.generateAffix(ctx, policy.Prefix)
	if err != nil {
		clear(indices)
		return nil, err
	}
	suffix, err := g.generateAffix(ctx, policy.Suffix)
	if err != nil {
		clear(prefix)
		clear(indices)
		return nil, err
	}
	defer clear(prefix)
	defer clear(suffix)
	defer clear(indices)

	result := make(keyphrase.Secret, 0, estimatedSize(policy, prepared.words))
	if policy.Prefix != nil {
		result = append(result, prefix...)
		result = append(result, policy.Prefix.Separator...)
	}
	for index, wordIndex := range indices {
		if index > 0 {
			result = append(result, policy.Separator...)
		}
		result = append(result, prepared.words[wordIndex]...)
	}
	if policy.Suffix != nil {
		result = append(result, policy.Suffix.Separator...)
		result = append(result, suffix...)
	}

	return result, nil
}

// Parse validates and separates a phrase without treating entropy as a
// password-strength assertion.
func Parse(encoded []byte, policy Policy) (Parsed, error) {
	prepared, err := prepare(policy)
	if err != nil {
		return Parsed{}, err
	}
	if len(encoded) > maxOutputSize || !utf8.Valid(encoded) {
		return Parsed{}, &Error{Code: CodeInvalidPhrase}
	}

	remaining := encoded
	parsed := Parsed{}
	if policy.Prefix != nil {
		separator := []byte(policy.Prefix.Separator)
		position := bytes.Index(remaining, separator)
		if position == -1 || password.Validate(policy.Prefix.Policy, remaining[:position]) != nil {
			return Parsed{}, &Error{Code: CodeInvalidPhrase}
		}
		parsed.Prefix = append(keyphrase.Secret(nil), remaining[:position]...)
		remaining = remaining[position+len(separator):]
	}
	if policy.Suffix != nil {
		separator := []byte(policy.Suffix.Separator)
		position := bytes.LastIndex(remaining, separator)
		if position == -1 || password.Validate(policy.Suffix.Policy, remaining[position+len(separator):]) != nil {
			clear(parsed.Prefix)
			return Parsed{}, &Error{Code: CodeInvalidPhrase}
		}
		parsed.Suffix = append(keyphrase.Secret(nil), remaining[position+len(separator):]...)
		remaining = remaining[:position]
	}

	parts := strings.Split(string(remaining), policy.Separator)
	if len(parts) != policy.Words {
		clear(parsed.Prefix)
		clear(parsed.Suffix)
		return Parsed{}, &Error{Code: CodeInvalidPhrase}
	}
	allowed := make(map[string]struct{}, len(prepared.words))
	for _, word := range prepared.words {
		allowed[word] = struct{}{}
	}
	for _, word := range parts {
		if _, exists := allowed[word]; !exists {
			clear(parsed.Prefix)
			clear(parsed.Suffix)
			return Parsed{}, &Error{Code: CodeInvalidPhrase}
		}
	}
	parsed.Words = append([]string(nil), parts...)

	return parsed, nil
}

func prepare(policy Policy) (*preparedPolicy, error) {
	if policy.WordList == nil || policy.Words < 2 || policy.Separator == "" ||
		len(policy.Separator) > maxSeparator || !utf8.ValidString(policy.Separator) ||
		policy.Casing > Upper || math.IsNaN(policy.MinimumEntropyBits) ||
		math.IsInf(policy.MinimumEntropyBits, 0) || policy.MinimumEntropyBits < 0 {
		return nil, &Error{Code: CodeInvalidPolicy}
	}
	if policy.Words > maxWords {
		return nil, &Error{Code: CodeOversized}
	}
	if policy.WordList.Len() > maxListWords {
		return nil, &Error{Code: CodeOversized}
	}

	words := policy.WordList.Words()
	transformed := make([]string, len(words))
	seen := make(map[string]struct{}, len(words))
	for index, word := range words {
		transformedWord := applyCasing(word, policy.Casing)
		if strings.Contains(transformedWord, policy.Separator) {
			return nil, &Error{Code: CodeAmbiguousSeparator}
		}
		if _, exists := seen[transformedWord]; exists {
			return nil, &Error{Code: CodeCollision}
		}
		seen[transformedWord] = struct{}{}
		transformed[index] = transformedWord
	}

	outcomes := new(big.Int).Exp(big.NewInt(int64(len(words))), big.NewInt(int64(policy.Words)), nil)
	for _, affix := range []*Affix{policy.Prefix, policy.Suffix} {
		if affix == nil {
			continue
		}
		if affix.Separator == "" || len(affix.Separator) > maxSeparator ||
			!utf8.ValidString(affix.Separator) ||
			strings.ContainsAny(affix.Policy.Alphabet, affix.Separator) {
			return nil, &Error{Code: CodeAmbiguousSeparator}
		}
		affixDistribution, err := password.Analyze(affix.Policy)
		if err != nil {
			return nil, &Error{Code: CodeInvalidPolicy, Cause: err}
		}
		outcomes.Mul(outcomes, affixDistribution.Outcomes)
	}

	bits := log2(outcomes)
	if policy.MinimumEntropyBits > bits {
		return nil, &Error{Code: CodeInsufficientEntropy}
	}
	if estimatedSize(policy, transformed) > maxOutputSize {
		return nil, &Error{Code: CodeOversized}
	}

	return &preparedPolicy{words: transformed, distribution: Distribution{Outcomes: outcomes, Bits: bits}}, nil
}

func (g *Generator) generateAffix(ctx context.Context, affix *Affix) (keyphrase.Secret, error) {
	if affix == nil {
		return nil, nil
	}
	secret, err := g.passwords.Generate(ctx, affix.Policy)
	if err != nil {
		return nil, &Error{Code: CodeRandomness, Cause: err}
	}
	return secret, nil
}

func applyCasing(word string, casing Casing) string {
	switch casing {
	case Lower:
		return strings.ToLower(word)
	case Upper:
		return strings.ToUpper(word)
	case Preserve:
		return word
	default:
		return word
	}
}

func estimatedSize(policy Policy, words []string) int {
	maximumWord := 0
	for _, word := range words {
		maximumWord = max(maximumWord, len(word))
	}
	size := policy.Words*maximumWord + (policy.Words-1)*len(policy.Separator)
	if policy.Prefix != nil {
		size += policy.Prefix.Policy.Length*utf8.UTFMax + len(policy.Prefix.Separator)
	}
	if policy.Suffix != nil {
		size += policy.Suffix.Policy.Length*utf8.UTFMax + len(policy.Suffix.Separator)
	}
	return size
}

func log2(value *big.Int) float64 {
	bitLength := value.BitLen()
	if bitLength <= 53 {
		asFloat, _ := new(big.Float).SetInt(value).Float64()
		return math.Log2(asFloat)
	}
	top := new(big.Int).Rsh(new(big.Int).Set(value), uint(bitLength-53))
	return float64(bitLength-53) + math.Log2(float64(top.Uint64()))
}

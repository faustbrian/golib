// Package password generates uniformly distributed passwords under explicit
// character and required-class policies.
package password

import (
	"context"
	"fmt"
	"io"
	"math"
	"math/big"
	"unicode/utf8"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
	"golang.org/x/text/unicode/norm"
)

const (
	maxLength            = 1024
	maxAlphabet          = 4096
	maxEncodedAlphabet   = 16_384
	maxByteAlphabet      = 256
	maxClasses           = 16
	maxDynamicCells      = 1 << 20
	maxDynamicOperations = 8 << 20
)

// ErrorCode identifies a password policy or generation failure.
type ErrorCode string

const (
	// CodeInvalidLength reports an unsupported password length.
	CodeInvalidLength ErrorCode = "invalid_length"
	// CodeInvalidAlphabet reports an empty or unusable alphabet.
	CodeInvalidAlphabet ErrorCode = "invalid_alphabet"
	// CodeDuplicateSymbol reports a repeated alphabet symbol.
	CodeDuplicateSymbol ErrorCode = "duplicate_symbol"
	// CodeNormalizationCollision reports canonically colliding symbols.
	CodeNormalizationCollision ErrorCode = "normalization_collision"
	// CodeInvalidClass reports malformed required-class configuration.
	CodeInvalidClass ErrorCode = "invalid_class"
	// CodeImpossible reports a policy with no valid outputs.
	CodeImpossible ErrorCode = "impossible"
	// CodeInsufficientEntropy reports an unmet entropy floor.
	CodeInsufficientEntropy ErrorCode = "insufficient_entropy"
	// CodeOversized reports a policy above resource limits.
	CodeOversized ErrorCode = "oversized"
	// CodeInvalidGenerator reports a nil generator dependency.
	CodeInvalidGenerator ErrorCode = "invalid_generator"
	// CodeBufferTooSmall reports insufficient caller-owned output space.
	CodeBufferTooSmall ErrorCode = "buffer_too_small"
	// CodeRandomness reports a failure while sampling.
	CodeRandomness ErrorCode = "randomness"
	// CodePolicyViolation reports a value that does not satisfy its policy.
	CodePolicyViolation ErrorCode = "policy_violation"
)

// Error deliberately excludes policy symbols and generated secret material.
type Error struct {
	Code  ErrorCode
	Cause error
}

func (e *Error) Error() string {
	return fmt.Sprintf("password: operation failed (%s)", e.Code)
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

// Class is a required set of characters. Classes may overlap.
type Class struct {
	Name       string
	Characters string
}

// Policy defines a Unicode password distribution. Length is measured in
// Unicode code points, not encoded bytes.
type Policy struct {
	Length             int
	Alphabet           string
	Required           []Class
	Excluded           string
	MinimumEntropyBits float64
}

// Distribution describes the exact number of possible outputs and its base-2
// logarithm. Outcomes is an owned value and may be modified by the caller.
type Distribution struct {
	Outcomes *big.Int
	Bits     float64
}

// Generator is safe for concurrent use when its randomness source is safe for
// concurrent use.
type Generator struct {
	selector *keyphrase.Selector
}

type preparedPolicy struct {
	symbols []rune
	masks   []uint64
	ways    [][]big.Int
	all     uint64
}

// NewGenerator creates a generator using selector.
func NewGenerator(selector *keyphrase.Selector) (*Generator, error) {
	if selector == nil {
		return nil, &Error{Code: CodeInvalidGenerator}
	}

	return &Generator{selector: selector}, nil
}

// DefaultGenerator returns a generator backed by crypto/rand.
func DefaultGenerator() *Generator {
	return &Generator{selector: keyphrase.DefaultSelector()}
}

// Analyze validates policy and reports its exact constrained distribution.
func Analyze(policy Policy) (Distribution, error) {
	prepared, err := prepare(policy)
	if err != nil {
		return Distribution{}, err
	}

	outcomes := new(big.Int).Set(&prepared.ways[policy.Length][0])
	bits := log2(outcomes)
	if policy.MinimumEntropyBits > bits {
		return Distribution{}, &Error{Code: CodeInsufficientEntropy}
	}

	return Distribution{Outcomes: outcomes, Bits: bits}, nil
}

// Validate reports whether encoded is a complete UTF-8 password satisfying
// policy. The returned error never includes encoded.
func Validate(policy Policy, encoded []byte) error {
	prepared, err := prepare(policy)
	if err != nil {
		return err
	}
	if !utf8.Valid(encoded) {
		return &Error{Code: CodePolicyViolation}
	}
	symbols := []rune(string(encoded))
	if len(symbols) != policy.Length {
		return &Error{Code: CodePolicyViolation}
	}
	state := uint64(0)
	for _, symbol := range symbols {
		index := -1
		for candidate, allowed := range prepared.symbols {
			if symbol == allowed {
				index = candidate
				break
			}
		}
		if index < 0 {
			return &Error{Code: CodePolicyViolation}
		}
		state |= prepared.masks[index]
	}
	if state != prepared.all {
		return &Error{Code: CodePolicyViolation}
	}

	return nil
}

// Generate returns a caller-owned UTF-8 buffer. No partial secret is returned
// on failure. Callers may clear the buffer after use as a best-effort measure,
// but Go cannot guarantee complete erasure of compiler and runtime copies.
func (g *Generator) Generate(ctx context.Context, policy Policy) (keyphrase.Secret, error) {
	if g == nil || g.selector == nil {
		return nil, &Error{Code: CodeInvalidGenerator}
	}
	prepared, err := prepare(policy)
	if err != nil {
		return nil, err
	}

	total := &prepared.ways[policy.Length][0]
	bits := log2(total)
	if policy.MinimumEntropyBits > bits {
		return nil, &Error{Code: CodeInsufficientEntropy}
	}

	rank, err := g.selector.BigInt(ctx, total)
	if err != nil {
		return nil, &Error{Code: CodeRandomness, Cause: err}
	}
	return unrankRunes(prepared, policy.Length, rank)
}

func unrankRunes(prepared *preparedPolicy, length int, rank *big.Int) (keyphrase.Secret, error) {
	result := make([]rune, 0, length)
	state := uint64(0)
	for position := range length {
		remaining := length - position - 1
		selected := false
		for index, symbol := range prepared.symbols {
			weight := &prepared.ways[remaining][state|prepared.masks[index]]
			if weight.Sign() == 0 {
				continue
			}
			if rank.Cmp(weight) < 0 {
				result = append(result, symbol)
				state |= prepared.masks[index]
				selected = true
				break
			}
			rank.Sub(rank, weight)
		}
		if !selected {
			clear(result)
			return nil, &Error{Code: CodeImpossible}
		}
	}

	encoded := keyphrase.Secret(string(result))
	clear(result)
	return encoded, nil
}

// GenerateInto copies a complete generated password into destination. It
// leaves destination unchanged when generation fails or the buffer is too
// small.
func (g *Generator) GenerateInto(ctx context.Context, destination []byte, policy Policy) (int, error) {
	secret, err := g.Generate(ctx, policy)
	if err != nil {
		return 0, err
	}
	defer clear(secret)

	if len(destination) < len(secret) {
		return 0, &Error{Code: CodeBufferTooSmall}
	}
	copy(destination, secret)

	return len(secret), nil
}

func prepare(policy Policy) (*preparedPolicy, error) {
	if policy.Length <= 0 {
		return nil, &Error{Code: CodeInvalidLength}
	}
	if policy.Length > maxLength {
		return nil, &Error{Code: CodeOversized}
	}
	if math.IsNaN(policy.MinimumEntropyBits) || math.IsInf(policy.MinimumEntropyBits, 0) || policy.MinimumEntropyBits < 0 {
		return nil, &Error{Code: CodeInsufficientEntropy}
	}
	if len(policy.Alphabet) > maxEncodedAlphabet || len(policy.Excluded) > maxEncodedAlphabet ||
		len(policy.Required) > maxClasses {
		return nil, &Error{Code: CodeOversized}
	}
	for _, class := range policy.Required {
		if len(class.Characters) > maxEncodedAlphabet {
			return nil, &Error{Code: CodeOversized}
		}
	}

	alphabet, alphabetSet, err := validateSymbols(policy.Alphabet)
	if err != nil {
		return nil, err
	}
	if len(alphabet) > maxAlphabet {
		return nil, &Error{Code: CodeOversized}
	}
	excluded, _, err := validateOptionalSymbols(policy.Excluded)
	if err != nil {
		return nil, err
	}
	excludedSet := make(map[rune]struct{}, len(excluded))
	for _, symbol := range excluded {
		excludedSet[symbol] = struct{}{}
	}

	effective := make([]rune, 0, len(alphabet))
	for _, symbol := range alphabet {
		if _, excluded := excludedSet[symbol]; !excluded {
			effective = append(effective, symbol)
		}
	}
	if len(effective) < 2 {
		return nil, &Error{Code: CodeInvalidAlphabet}
	}
	states := 1 << len(policy.Required)
	if (policy.Length+1)*states > maxDynamicCells {
		return nil, &Error{Code: CodeOversized}
	}

	masks := make([]uint64, len(effective))
	for classIndex, class := range policy.Required {
		if class.Name == "" {
			return nil, &Error{Code: CodeInvalidClass}
		}
		characters, _, classErr := validateSymbols(class.Characters)
		if classErr != nil {
			return nil, &Error{Code: CodeInvalidClass, Cause: classErr}
		}
		matched := false
		classSet := make(map[rune]struct{}, len(characters))
		for _, character := range characters {
			if _, exists := alphabetSet[character]; !exists {
				return nil, &Error{Code: CodeInvalidClass}
			}
			classSet[character] = struct{}{}
		}
		for symbolIndex, symbol := range effective {
			if _, exists := classSet[symbol]; exists {
				masks[symbolIndex] |= uint64(1) << classIndex
				matched = true
			}
		}
		if !matched {
			return nil, &Error{Code: CodeImpossible}
		}
	}

	all := uint64(1)<<len(policy.Required) - 1
	if int64(policy.Length)*int64(states)*int64(maskGroups(masks)) > maxDynamicOperations {
		return nil, &Error{Code: CodeOversized}
	}
	ways := countWays(policy.Length, states, all, masks)
	if ways[policy.Length][0].Sign() == 0 {
		return nil, &Error{Code: CodeImpossible}
	}

	return &preparedPolicy{symbols: effective, masks: masks, ways: ways, all: all}, nil
}

func validateSymbols(value string) ([]rune, map[rune]struct{}, error) {
	if value == "" || !utf8.ValidString(value) {
		return nil, nil, &Error{Code: CodeInvalidAlphabet}
	}

	symbols := []rune(value)
	seen := make(map[rune]struct{}, len(symbols))
	normalized := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		if _, exists := seen[symbol]; exists {
			return nil, nil, &Error{Code: CodeDuplicateSymbol}
		}
		normalizedSymbol := norm.NFKD.String(string(symbol))
		if _, exists := normalized[normalizedSymbol]; exists {
			return nil, nil, &Error{Code: CodeNormalizationCollision}
		}
		seen[symbol] = struct{}{}
		normalized[normalizedSymbol] = struct{}{}
	}

	return symbols, seen, nil
}

func validateOptionalSymbols(value string) ([]rune, map[rune]struct{}, error) {
	if value == "" {
		return nil, map[rune]struct{}{}, nil
	}
	return validateSymbols(value)
}

func countWays(length, states int, all uint64, masks []uint64) [][]big.Int {
	ways := make([][]big.Int, length+1)
	for remaining := range ways {
		ways[remaining] = make([]big.Int, states)
	}
	ways[0][all].SetInt64(1)

	groups := make(map[uint64]int)
	for _, mask := range masks {
		groups[mask]++
	}
	for remaining := 1; remaining <= length; remaining++ {
		for state := range states {
			for mask, multiplicity := range groups {
				var term big.Int
				term.Mul(&ways[remaining-1][uint64(state)|mask], big.NewInt(int64(multiplicity)))
				ways[remaining][state].Add(&ways[remaining][state], &term)
			}
		}
	}

	return ways
}

func maskGroups(masks []uint64) int {
	groups := make(map[uint64]struct{}, len(masks))
	for _, mask := range masks {
		groups[mask] = struct{}{}
	}
	return len(groups)
}

func log2(value *big.Int) float64 {
	bitLength := value.BitLen()
	if bitLength <= 53 {
		return math.Log2(float64(value.Uint64()))
	}
	top := new(big.Int).Rsh(new(big.Int).Set(value), uint(bitLength-53))
	return float64(bitLength-53) + math.Log2(float64(top.Uint64()))
}

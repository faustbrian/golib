package password

import (
	"context"
	"math"
	"math/big"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
)

// ByteClass is a required set in a byte-alphabet policy.
type ByteClass struct {
	Name  string
	Bytes []byte
}

// BytePolicy defines a distribution over arbitrary byte values.
type BytePolicy struct {
	Length             int
	Alphabet           []byte
	Required           []ByteClass
	Excluded           []byte
	MinimumEntropyBits float64
}

type preparedBytePolicy struct {
	symbols []byte
	masks   []uint64
	ways    [][]big.Int
}

// AnalyzeBytes validates policy and reports its exact constrained
// distribution.
func AnalyzeBytes(policy BytePolicy) (Distribution, error) {
	prepared, err := prepareBytes(policy)
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

// GenerateBytes returns a complete caller-owned byte password.
func (g *Generator) GenerateBytes(ctx context.Context, policy BytePolicy) (keyphrase.Secret, error) {
	if g == nil || g.selector == nil {
		return nil, &Error{Code: CodeInvalidGenerator}
	}
	prepared, err := prepareBytes(policy)
	if err != nil {
		return nil, err
	}

	total := &prepared.ways[policy.Length][0]
	if policy.MinimumEntropyBits > log2(total) {
		return nil, &Error{Code: CodeInsufficientEntropy}
	}
	rank, err := g.selector.BigInt(ctx, total)
	if err != nil {
		return nil, &Error{Code: CodeRandomness, Cause: err}
	}
	return unrankBytes(prepared, policy.Length, rank)
}

func unrankBytes(prepared *preparedBytePolicy, length int, rank *big.Int) (keyphrase.Secret, error) {
	result := make(keyphrase.Secret, 0, length)
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

	return result, nil
}

// GenerateBytesInto fills destination only when it can hold the complete
// fixed-length output.
func (g *Generator) GenerateBytesInto(ctx context.Context, destination []byte, policy BytePolicy) (int, error) {
	if g == nil || g.selector == nil {
		return 0, &Error{Code: CodeInvalidGenerator}
	}
	if policy.Length > len(destination) {
		return 0, &Error{Code: CodeBufferTooSmall}
	}
	secret, err := g.GenerateBytes(ctx, policy)
	if err != nil {
		return 0, err
	}
	defer clear(secret)
	copy(destination, secret)

	return len(secret), nil
}

func prepareBytes(policy BytePolicy) (*preparedBytePolicy, error) {
	if policy.Length <= 0 {
		return nil, &Error{Code: CodeInvalidLength}
	}
	if policy.Length > maxLength {
		return nil, &Error{Code: CodeOversized}
	}
	if math.IsNaN(policy.MinimumEntropyBits) || math.IsInf(policy.MinimumEntropyBits, 0) || policy.MinimumEntropyBits < 0 {
		return nil, &Error{Code: CodeInsufficientEntropy}
	}
	if len(policy.Alphabet) > maxByteAlphabet || len(policy.Excluded) > maxByteAlphabet ||
		len(policy.Required) > maxClasses {
		return nil, &Error{Code: CodeOversized}
	}
	for _, class := range policy.Required {
		if len(class.Bytes) > maxByteAlphabet {
			return nil, &Error{Code: CodeOversized}
		}
	}

	alphabet, alphabetSet, err := validateByteSymbols(policy.Alphabet, false)
	if err != nil {
		return nil, err
	}
	excluded, _, err := validateByteSymbols(policy.Excluded, true)
	if err != nil {
		return nil, err
	}
	excludedSet := make(map[byte]struct{}, len(excluded))
	for _, symbol := range excluded {
		excludedSet[symbol] = struct{}{}
	}
	effective := make([]byte, 0, len(alphabet))
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
		characters, _, classErr := validateByteSymbols(class.Bytes, false)
		if classErr != nil {
			return nil, &Error{Code: CodeInvalidClass, Cause: classErr}
		}
		classSet := make(map[byte]struct{}, len(characters))
		for _, character := range characters {
			if _, exists := alphabetSet[character]; !exists {
				return nil, &Error{Code: CodeInvalidClass}
			}
			classSet[character] = struct{}{}
		}
		matched := false
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

	return &preparedBytePolicy{symbols: effective, masks: masks, ways: ways}, nil
}

func validateByteSymbols(value []byte, optional bool) ([]byte, map[byte]struct{}, error) {
	if len(value) == 0 {
		if optional {
			return nil, map[byte]struct{}{}, nil
		}
		return nil, nil, &Error{Code: CodeInvalidAlphabet}
	}
	seen := make(map[byte]struct{}, len(value))
	for _, symbol := range value {
		if _, exists := seen[symbol]; exists {
			return nil, nil, &Error{Code: CodeDuplicateSymbol}
		}
		seen[symbol] = struct{}{}
	}

	return append([]byte(nil), value...), seen, nil
}

package password

import (
	"context"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
	"unicode/utf8"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
)

type failingSource struct{}

func (failingSource) ReadContext(context.Context, []byte) (int, error) {
	return 0, errors.New("source failure")
}

func TestUnicodePolicyFailureMatrix(t *testing.T) {
	t.Parallel()

	if _, err := NewGenerator(nil); policyErrorCode(err) != CodeInvalidGenerator {
		t.Fatalf("NewGenerator(nil) code = %q", policyErrorCode(err))
	}

	oversizedAlphabet := make([]rune, maxAlphabet+1)
	for index := range oversizedAlphabet {
		oversizedAlphabet[index] = rune(0x4e00 + index)
	}
	tooManyClasses := make([]Class, maxClasses+1)
	oversizedEncoded := strings.Repeat("a", maxAlphabet*utf8.UTFMax+1)
	tests := []struct {
		name   string
		policy Policy
		code   ErrorCode
	}{
		{name: "zero length", policy: Policy{Alphabet: "ab"}, code: CodeInvalidLength},
		{name: "oversized length", policy: Policy{Length: maxLength + 1, Alphabet: "ab"}, code: CodeOversized},
		{name: "invalid entropy floor", policy: Policy{Length: 2, Alphabet: "ab", MinimumEntropyBits: math.NaN()}, code: CodeInsufficientEntropy},
		{name: "oversized alphabet", policy: Policy{Length: 2, Alphabet: string(oversizedAlphabet)}, code: CodeOversized},
		{name: "oversized encoded alphabet", policy: Policy{Length: 2, Alphabet: oversizedEncoded}, code: CodeOversized},
		{name: "oversized exclusion", policy: Policy{Length: 2, Alphabet: "ab", Excluded: oversizedEncoded}, code: CodeOversized},
		{name: "oversized class", policy: Policy{Length: 2, Alphabet: "ab", Required: []Class{{Name: "large", Characters: oversizedEncoded}}}, code: CodeOversized},
		{name: "invalid exclusion", policy: Policy{Length: 2, Alphabet: "abc", Excluded: "aa"}, code: CodeDuplicateSymbol},
		{name: "excluded alphabet", policy: Policy{Length: 2, Alphabet: "ab", Excluded: "a"}, code: CodeInvalidAlphabet},
		{name: "too many classes", policy: Policy{Length: 2, Alphabet: "ab", Required: tooManyClasses}, code: CodeOversized},
		{name: "too many cells", policy: Policy{Length: maxLength, Alphabet: "ab", Required: make([]Class, 10)}, code: CodeOversized},
		{name: "empty class name", policy: Policy{Length: 2, Alphabet: "ab", Required: []Class{{Characters: "a"}}}, code: CodeInvalidClass},
		{name: "invalid class alphabet", policy: Policy{Length: 2, Alphabet: "ab", Required: []Class{{Name: "bad", Characters: "aa"}}}, code: CodeInvalidClass},
		{name: "class outside alphabet", policy: Policy{Length: 2, Alphabet: "ab", Required: []Class{{Name: "bad", Characters: "c"}}}, code: CodeInvalidClass},
		{name: "class fully excluded", policy: Policy{Length: 2, Alphabet: "abc", Excluded: "c", Required: []Class{{Name: "excluded", Characters: "c"}}}, code: CodeImpossible},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Analyze(test.policy); policyErrorCode(err) != test.code {
				t.Fatalf("Analyze() code = %q, want %q", policyErrorCode(err), test.code)
			}
		})
	}
}

func TestUninitializedGeneratorsReturnTypedErrors(t *testing.T) {
	t.Parallel()

	policy := Policy{Length: 2, Alphabet: "ab"}
	bytePolicy := BytePolicy{Length: 2, Alphabet: []byte{1, 2}}
	for _, generator := range []*Generator{nil, {}} {
		if secret, err := generator.Generate(context.Background(), policy); secret != nil || policyErrorCode(err) != CodeInvalidGenerator {
			t.Fatalf("Generate(uninitialized) = %v, %v", secret, err)
		}
		if secret, err := generator.GenerateBytes(context.Background(), bytePolicy); secret != nil || policyErrorCode(err) != CodeInvalidGenerator {
			t.Fatalf("GenerateBytes(uninitialized) = %v, %v", secret, err)
		}
		if count, err := generator.GenerateBytesInto(context.Background(), make([]byte, 2), bytePolicy); count != 0 || policyErrorCode(err) != CodeInvalidGenerator {
			t.Fatalf("GenerateBytesInto(uninitialized) = %d, %v", count, err)
		}
	}
}

func TestUnicodeValidationAndBufferFailureMatrix(t *testing.T) {
	t.Parallel()

	policy := Policy{Length: 2, Alphabet: "abCD", Required: []Class{{Name: "upper", Characters: "CD"}}}
	if err := Validate(Policy{}, []byte("ab")); policyErrorCode(err) != CodeInvalidLength {
		t.Fatalf("Validate(policy) code = %q", policyErrorCode(err))
	}
	for name, encoded := range map[string][]byte{
		"invalid UTF-8":  {0xff},
		"wrong length":   []byte("a"),
		"unknown symbol": []byte("aX"),
		"missing class":  []byte("ab"),
	} {
		if err := Validate(policy, encoded); policyErrorCode(err) != CodePolicyViolation {
			t.Fatalf("Validate(%s) code = %q", name, policyErrorCode(err))
		}
	}
	if err := Validate(policy, []byte("aC")); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}

	selector, _ := keyphrase.NewSelector(failingSource{})
	generator, _ := NewGenerator(selector)
	if _, err := generator.Generate(context.Background(), Policy{}); policyErrorCode(err) != CodeInvalidLength {
		t.Fatalf("Generate(policy) code = %q", policyErrorCode(err))
	}
	if _, err := generator.Generate(context.Background(), Policy{Length: 2, Alphabet: "ab", MinimumEntropyBits: 3}); policyErrorCode(err) != CodeInsufficientEntropy {
		t.Fatalf("Generate(entropy) code = %q", policyErrorCode(err))
	}
	if _, err := generator.Generate(context.Background(), Policy{Length: 2, Alphabet: "ab"}); policyErrorCode(err) != CodeRandomness {
		t.Fatalf("Generate(randomness) code = %q", policyErrorCode(err))
	}
	if count, err := generator.GenerateInto(context.Background(), make([]byte, 2), Policy{Length: 2, Alphabet: "ab"}); count != 0 || policyErrorCode(err) != CodeRandomness {
		t.Fatalf("GenerateInto(randomness) = %d, %v", count, err)
	}

	deterministic, _ := keyphrase.NewSelector(&zeroSource{})
	generator, _ = NewGenerator(deterministic)
	if count, err := generator.GenerateInto(context.Background(), make([]byte, 1), Policy{Length: 2, Alphabet: "ab"}); count != 0 || policyErrorCode(err) != CodeBufferTooSmall {
		t.Fatalf("GenerateInto(small) = %d, %v", count, err)
	}
	destination := make([]byte, 2)
	if count, err := generator.GenerateInto(context.Background(), destination, Policy{Length: 2, Alphabet: "ab"}); count != 2 || err != nil || string(destination) != "aa" {
		t.Fatalf("GenerateInto(valid) = %q, %d, %v", destination, count, err)
	}
}

func TestBytePolicyFailureMatrix(t *testing.T) {
	t.Parallel()

	tooManyClasses := make([]ByteClass, maxClasses+1)
	oversizedBytes := make([]byte, 257)
	tests := []struct {
		name   string
		policy BytePolicy
		code   ErrorCode
	}{
		{name: "zero length", policy: BytePolicy{Alphabet: []byte{1, 2}}, code: CodeInvalidLength},
		{name: "oversized length", policy: BytePolicy{Length: maxLength + 1, Alphabet: []byte{1, 2}}, code: CodeOversized},
		{name: "invalid entropy", policy: BytePolicy{Length: 2, Alphabet: []byte{1, 2}, MinimumEntropyBits: math.Inf(1)}, code: CodeInsufficientEntropy},
		{name: "empty alphabet", policy: BytePolicy{Length: 2}, code: CodeInvalidAlphabet},
		{name: "oversized alphabet", policy: BytePolicy{Length: 2, Alphabet: oversizedBytes}, code: CodeOversized},
		{name: "oversized exclusion", policy: BytePolicy{Length: 2, Alphabet: []byte{1, 2}, Excluded: oversizedBytes}, code: CodeOversized},
		{name: "oversized class", policy: BytePolicy{Length: 2, Alphabet: []byte{1, 2}, Required: []ByteClass{{Name: "large", Bytes: oversizedBytes}}}, code: CodeOversized},
		{name: "invalid exclusion", policy: BytePolicy{Length: 2, Alphabet: []byte{1, 2, 3}, Excluded: []byte{1, 1}}, code: CodeDuplicateSymbol},
		{name: "excluded alphabet", policy: BytePolicy{Length: 2, Alphabet: []byte{1, 2}, Excluded: []byte{1}}, code: CodeInvalidAlphabet},
		{name: "too many classes", policy: BytePolicy{Length: 2, Alphabet: []byte{1, 2}, Required: tooManyClasses}, code: CodeOversized},
		{name: "too many cells", policy: BytePolicy{Length: maxLength, Alphabet: []byte{1, 2}, Required: make([]ByteClass, 10)}, code: CodeOversized},
		{name: "empty class name", policy: BytePolicy{Length: 2, Alphabet: []byte{1, 2}, Required: []ByteClass{{Bytes: []byte{1}}}}, code: CodeInvalidClass},
		{name: "invalid class", policy: BytePolicy{Length: 2, Alphabet: []byte{1, 2}, Required: []ByteClass{{Name: "bad", Bytes: []byte{1, 1}}}}, code: CodeInvalidClass},
		{name: "outside alphabet", policy: BytePolicy{Length: 2, Alphabet: []byte{1, 2}, Required: []ByteClass{{Name: "bad", Bytes: []byte{3}}}}, code: CodeInvalidClass},
		{name: "fully excluded", policy: BytePolicy{Length: 2, Alphabet: []byte{1, 2, 3}, Excluded: []byte{3}, Required: []ByteClass{{Name: "bad", Bytes: []byte{3}}}}, code: CodeImpossible},
		{name: "impossible", policy: BytePolicy{Length: 1, Alphabet: []byte{1, 2}, Required: []ByteClass{{Name: "one", Bytes: []byte{1}}, {Name: "two", Bytes: []byte{2}}}}, code: CodeImpossible},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := AnalyzeBytes(test.policy); policyErrorCode(err) != test.code {
				t.Fatalf("AnalyzeBytes() code = %q, want %q", policyErrorCode(err), test.code)
			}
		})
	}
}

func TestByteGenerationFailureMatrix(t *testing.T) {
	t.Parallel()

	selector, _ := keyphrase.NewSelector(failingSource{})
	generator, _ := NewGenerator(selector)
	if _, err := generator.GenerateBytes(context.Background(), BytePolicy{}); policyErrorCode(err) != CodeInvalidLength {
		t.Fatalf("GenerateBytes(policy) code = %q", policyErrorCode(err))
	}
	if _, err := generator.GenerateBytes(context.Background(), BytePolicy{Length: 2, Alphabet: []byte{1, 2}, MinimumEntropyBits: 3}); policyErrorCode(err) != CodeInsufficientEntropy {
		t.Fatalf("GenerateBytes(entropy) code = %q", policyErrorCode(err))
	}
	if _, err := generator.GenerateBytes(context.Background(), BytePolicy{Length: 2, Alphabet: []byte{1, 2}}); policyErrorCode(err) != CodeRandomness {
		t.Fatalf("GenerateBytes(randomness) code = %q", policyErrorCode(err))
	}
	if count, err := generator.GenerateBytesInto(context.Background(), make([]byte, 2), BytePolicy{Length: 2, Alphabet: []byte{1, 2}}); count != 0 || policyErrorCode(err) != CodeRandomness {
		t.Fatalf("GenerateBytesInto(randomness) = %d, %v", count, err)
	}
	if _, _, err := validateByteSymbols(nil, false); policyErrorCode(err) != CodeInvalidAlphabet {
		t.Fatalf("validateByteSymbols(empty) code = %q", policyErrorCode(err))
	}
	if _, err := AnalyzeBytes(BytePolicy{Length: 2, Alphabet: []byte{1, 2}, MinimumEntropyBits: 3}); policyErrorCode(err) != CodeInsufficientEntropy {
		t.Fatalf("AnalyzeBytes(entropy) code = %q", policyErrorCode(err))
	}

	invalidWays := [][]big.Int{make([]big.Int, 1), make([]big.Int, 1)}
	if _, err := unrankRunes(&preparedPolicy{symbols: []rune{'a'}, masks: []uint64{0}, ways: invalidWays}, 1, big.NewInt(0)); policyErrorCode(err) != CodeImpossible {
		t.Fatalf("unrankRunes(invalid) code = %q", policyErrorCode(err))
	}
	if _, err := unrankBytes(&preparedBytePolicy{symbols: []byte{1}, masks: []uint64{0}, ways: invalidWays}, 1, big.NewInt(0)); policyErrorCode(err) != CodeImpossible {
		t.Fatalf("unrankBytes(invalid) code = %q", policyErrorCode(err))
	}
	prepared, err := prepareBytes(BytePolicy{Length: 1, Alphabet: []byte{1, 2}})
	if err != nil {
		t.Fatalf("prepareBytes() error = %v", err)
	}
	if result, err := unrankBytes(prepared, 1, big.NewInt(1)); err != nil || len(result) != 1 || result[0] != 2 {
		t.Fatalf("unrankBytes(rank one) = %v, %v", result, err)
	}
}

type zeroSource struct{}

func (*zeroSource) ReadContext(_ context.Context, destination []byte) (int, error) {
	clear(destination)
	return len(destination), nil
}

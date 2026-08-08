package password

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
)

func TestAnalyzeBoundsDynamicProgrammingWork(t *testing.T) {
	t.Parallel()

	symbols := make([]rune, 128)
	for index := range symbols {
		symbols[index] = rune(0x1000 + index)
	}
	classes := make([]Class, 7)
	for bit := range classes {
		var characters strings.Builder
		for index, symbol := range symbols {
			if index&(1<<bit) != 0 {
				characters.WriteRune(symbol)
			}
		}
		classes[bit] = Class{Name: string(rune('a' + bit)), Characters: characters.String()}
	}

	_, err := Analyze(Policy{Length: 600, Alphabet: string(symbols), Required: classes})
	if policyErrorCode(err) != CodeOversized {
		t.Fatalf("Analyze(large state space) code = %q", policyErrorCode(err))
	}
}

func TestAnalyzeBytesBoundsDynamicProgrammingWork(t *testing.T) {
	t.Parallel()

	alphabet := make([]byte, 128)
	for index := range alphabet {
		alphabet[index] = byte(index)
	}
	classes := make([]ByteClass, 7)
	for bit := range classes {
		characters := make([]byte, 0, 64)
		for index, symbol := range alphabet {
			if index&(1<<bit) != 0 {
				characters = append(characters, symbol)
			}
		}
		classes[bit] = ByteClass{Name: string(rune('a' + bit)), Bytes: characters}
	}

	_, err := AnalyzeBytes(BytePolicy{Length: 600, Alphabet: alphabet, Required: classes})
	if policyErrorCode(err) != CodeOversized {
		t.Fatalf("AnalyzeBytes(large state space) code = %q", policyErrorCode(err))
	}
}

func TestAnalyzeAcceptsExactResourceBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := Analyze(Policy{Length: maxLength, Alphabet: "ab"}); err != nil {
		t.Fatalf("Analyze(maximum length) error = %v", err)
	}

	symbols := make([]rune, maxAlphabet)
	for index := range symbols {
		symbols[index] = rune(0x10000 + index)
	}
	alphabet := string(symbols)
	if len(alphabet) != maxEncodedAlphabet {
		t.Fatalf("encoded alphabet bytes = %d, want %d", len(alphabet), maxEncodedAlphabet)
	}
	if _, err := Analyze(Policy{Length: 1, Alphabet: alphabet}); err != nil {
		t.Fatalf("Analyze(maximum alphabet) error = %v", err)
	}
	if _, err := Analyze(Policy{Length: 1, Alphabet: alphabet, Excluded: alphabet}); policyErrorCode(err) != CodeInvalidAlphabet {
		t.Fatalf("Analyze(maximum exclusion) code = %q", policyErrorCode(err))
	}
	classes := make([]Class, maxClasses)
	for index := range classes {
		classes[index] = Class{Name: string(rune('a' + index)), Characters: alphabet}
	}
	if _, err := Analyze(Policy{Length: 1, Alphabet: alphabet, Required: classes}); err != nil {
		t.Fatalf("Analyze(maximum classes) error = %v", err)
	}

	byteAlphabet := make([]byte, maxByteAlphabet)
	for index := range byteAlphabet {
		byteAlphabet[index] = byte(index)
	}
	if _, err := AnalyzeBytes(BytePolicy{Length: 1, Alphabet: byteAlphabet}); err != nil {
		t.Fatalf("AnalyzeBytes(maximum alphabet) error = %v", err)
	}
	if _, err := AnalyzeBytes(BytePolicy{Length: maxLength, Alphabet: []byte{0, 1}}); err != nil {
		t.Fatalf("AnalyzeBytes(maximum length) error = %v", err)
	}
	if _, err := AnalyzeBytes(BytePolicy{Length: 1, Alphabet: byteAlphabet, Excluded: byteAlphabet}); policyErrorCode(err) != CodeInvalidAlphabet {
		t.Fatalf("AnalyzeBytes(maximum exclusion) code = %q", policyErrorCode(err))
	}
	byteClasses := make([]ByteClass, maxClasses)
	for index := range byteClasses {
		byteClasses[index] = ByteClass{Name: string(rune('a' + index)), Bytes: byteAlphabet}
	}
	if _, err := AnalyzeBytes(BytePolicy{Length: 1, Alphabet: byteAlphabet, Required: byteClasses}); err != nil {
		t.Fatalf("AnalyzeBytes(maximum classes) error = %v", err)
	}

	if exceedsDynamicCells(1023, 1024) || !exceedsDynamicCells(1024, 1024) {
		t.Fatal("dynamic cell boundary mismatch")
	}
	if exceedsDynamicOperations(1024, 512, 16) || !exceedsDynamicOperations(1024, 512, 17) {
		t.Fatal("dynamic operation boundary mismatch")
	}
	for _, value := range []uint64{1, 3, 1 << 52, (1 << 53) + 1, 1 << 63} {
		got := log2(new(big.Int).SetUint64(value))
		want := math.Log2(float64(value))
		if math.Abs(got-want) > 1e-12 {
			t.Fatalf("log2(%d) = %.16f, want %.16f", value, got, want)
		}
	}
}

func policyErrorCode(err error) ErrorCode {
	var policyError *Error
	if errors.As(err, &policyError) {
		return policyError.Code
	}
	return ""
}

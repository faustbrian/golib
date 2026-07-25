package encoder

import (
	"bytes"
	"testing"

	"github.com/ericlevine/zxinggo/bitutil"
)

func TestEncodeExercisesAutomaticCompactAndFullLayers(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		layers  int
		compact bool
	}{
		{name: "automatic", payload: []byte("AZTEC")},
		{name: "compact one", payload: []byte("A"), layers: -1, compact: true},
		{name: "compact four", payload: bytes.Repeat([]byte("A"), 40), layers: -4, compact: true},
		{name: "full five", payload: bytes.Repeat([]byte("A"), 40), layers: 5},
		{name: "full ten", payload: bytes.Repeat([]byte("A"), 150), layers: 10},
		{name: "full twenty three", payload: bytes.Repeat([]byte("A"), 500), layers: 23},
		{name: "full thirty two", payload: bytes.Repeat([]byte("A"), 1000), layers: 32},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, err := EncodeWithControls(test.payload, 25, test.layers, true, 26)
			if err != nil {
				t.Fatalf("EncodeWithControls() error = %v", err)
			}
			if code.Matrix == nil || code.Size <= 0 || code.Layers <= 0 || code.CodeWords <= 0 {
				t.Fatalf("invalid code metadata = %+v", code)
			}
			if test.layers != 0 && code.Compact != test.compact {
				t.Fatalf("Compact = %t, want %t", code.Compact, test.compact)
			}
		})
	}
}

func TestEncodeRejectsInvalidLayersAndCapacity(t *testing.T) {
	for _, test := range []struct {
		payload []byte
		layers  int
	}{
		{payload: []byte("A"), layers: 33},
		{payload: []byte("A"), layers: -5},
		{payload: bytes.Repeat([]byte("A"), 100), layers: -1},
		{payload: bytes.Repeat([]byte{0xff}, 3000)},
	} {
		if _, err := Encode(test.payload, 33, test.layers); err == nil {
			t.Fatalf("Encode(length %d, layers %d) succeeded", len(test.payload), test.layers)
		}
	}
	if _, err := Encode(nil, 33, 0); err == nil {
		t.Fatal("Encode(nil) succeeded")
	}
	if _, err := Encode(bytes.Repeat([]byte{0xff}, 70), -100, -4); err == nil {
		t.Fatal("compact codeword limit succeeded")
	}
	if _, err := Encode(bytes.Repeat([]byte{0xff}, 70), -100, 0); err != nil {
		t.Fatalf("automatic compact fallback error = %v", err)
	}
}

func TestHighLevelEncodingExercisesModesPairsAndBinaryShift(t *testing.T) {
	inputs := [][]byte{
		[]byte("UPPER lower 0123 @\\^_`|~ !\"#$%&'()*+,-./:;<=>?[]{}"),
		[]byte("\r\n. , : "),
		{1, 2, 13, 27, 28, 29, 30, 31, 127},
		bytes.Repeat([]byte{0xff}, 31),
		bytes.Repeat([]byte{0xff}, 32),
		bytes.Repeat([]byte{0xff}, 2079),
		[]byte("aAa"),
		{'a', '1', 0xff},
		{'.', ' ', 0xff},
	}
	for _, input := range inputs {
		bits, err := highLevelEncode(input, true, 999_999)
		if err != nil {
			t.Fatalf("highLevelEncode(length %d) error = %v", len(input), err)
		}
		if bits.Size() == 0 {
			t.Fatalf("highLevelEncode(length %d) returned no bits", len(input))
		}
	}
}

func TestInternalModeAndStuffingEdges(t *testing.T) {
	if got := getLatchSequence(modeUpper, 99); got != nil {
		t.Fatalf("invalid latch sequence = %v", got)
	}
	if findBestMode('A', modeUpper) != modeUpper {
		t.Fatal("findBestMode did not preserve current mode")
	}
	for _, mode := range []int{modeLower, modeDigit} {
		bits := bitutil.NewBitArray(0)
		emitShiftCode(bits, mode, modeUpper)
		if bits.Size() == 0 {
			t.Fatalf("emitShiftCode(%d) emitted no bits", mode)
		}
	}
	bits := bitutil.NewBitArray(0)
	if end := emitBinaryShift(bits, []byte("A"), 0, modeUpper); end != 1 || bits.Size() == 0 {
		t.Fatalf("emitBinaryShift() = (%d, %d bits)", end, bits.Size())
	}
	if !inAnyMode('A') || inAnyMode(0xff) {
		t.Fatal("inAnyMode returned an invalid result")
	}

	ones := bitutil.NewBitArray(0)
	ones.AppendBits(0x3f, 6)
	stuffed := stuffBits(ones, 6)
	if stuffed.Size() != 12 {
		t.Fatalf("stuffBits(all ones) size = %d, want 12", stuffed.Size())
	}
}

func TestUnsupportedWordSizePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("gfForWordSize(5) did not panic")
		}
	}()
	_ = gfForWordSize(5)
}

func TestEveryLatchSequenceAndShiftDecision(t *testing.T) {
	for from := modeUpper; from <= modePunct; from++ {
		for to := modeUpper; to <= modePunct; to++ {
			sequence := getLatchSequence(from, to)
			if from == to && len(sequence) != 0 {
				t.Fatalf("getLatchSequence(%d, %d) = %v", from, to, sequence)
			}
			if from != to && len(sequence) == 0 {
				t.Fatalf("getLatchSequence(%d, %d) is empty", from, to)
			}
		}
	}
	if !canShift(modeLower, modeUpper) || !canShift(modeDigit, modeUpper) ||
		canShift(modeUpper, modeLower) {
		t.Fatal("canShift() returned an invalid decision")
	}
	if !shouldShift([]byte("aA"), 1, modeLower) || shouldShift([]byte("Aa"), 0, modeUpper) {
		t.Fatal("shouldShift() returned an invalid decision")
	}
}

package encoder

import (
	"bytes"
	"testing"

	upstream "github.com/ericlevine/zxinggo/aztec/encoder"
	"github.com/ericlevine/zxinggo/bitutil"
)

func TestEncodeMatchesPinnedZXingImplementation(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload []byte
		ecc     int
		layers  int
	}{
		{name: "automatic text", payload: []byte("Aztec 0123, punctuation. "), ecc: 33},
		{name: "automatic binary 31", payload: bytes.Repeat([]byte{0xff}, 31), ecc: 25},
		{name: "automatic binary 32", payload: bytes.Repeat([]byte{0xff}, 32), ecc: 25},
		{name: "compact one", payload: []byte("A"), ecc: 0, layers: -1},
		{name: "compact four", payload: bytes.Repeat([]byte("A"), 40), ecc: 25, layers: -4},
		{name: "full five", payload: bytes.Repeat([]byte("A"), 40), ecc: 25, layers: 5},
		{name: "full ten", payload: bytes.Repeat([]byte("A"), 150), ecc: 25, layers: 10},
		{name: "full twelve", payload: bytes.Repeat([]byte("A"), 200), ecc: 25, layers: 12},
		{name: "full twenty three", payload: bytes.Repeat([]byte("A"), 500), ecc: 25, layers: 23},
		{name: "full thirty two", payload: bytes.Repeat([]byte("A"), 1000), ecc: 25, layers: 32},
	} {
		t.Run(test.name, func(t *testing.T) {
			want, wantErr := upstream.Encode(test.payload, test.ecc, test.layers)
			got, gotErr := Encode(test.payload, test.ecc, test.layers)
			if (gotErr != nil) != (wantErr != nil) {
				t.Fatalf("Encode() error = %v, upstream error = %v", gotErr, wantErr)
			}
			if gotErr != nil {
				return
			}
			if got.Compact != want.Compact || got.Size != want.Size || got.Layers != want.Layers ||
				got.CodeWords != want.CodeWords || !got.Matrix.Equals(want.Matrix) {
				t.Fatalf("Encode() = compact=%t size=%d layers=%d words=%d, upstream = compact=%t size=%d layers=%d words=%d",
					got.Compact, got.Size, got.Layers, got.CodeWords,
					want.Compact, want.Size, want.Layers, want.CodeWords)
			}
		})
	}
}

func TestEncodeMatchesPinnedZXingCapacityBoundaries(t *testing.T) {
	for _, test := range []struct {
		name   string
		ecc    int
		layers int
	}{
		{name: "automatic", ecc: 33},
		{name: "compact one", ecc: 33, layers: -1},
		{name: "compact four", ecc: 33, layers: -4},
		{name: "compact codeword cap", ecc: -100, layers: -4},
		{name: "full one", ecc: 33, layers: 1},
		{name: "full twelve", ecc: 33, layers: 12},
		{name: "full thirty two", ecc: 33, layers: 32},
	} {
		t.Run(test.name, func(t *testing.T) {
			low, high := 1, 5000
			for low < high {
				middle := low + (high-low+1)/2
				_, err := upstream.Encode(bytes.Repeat([]byte{0xff}, middle), test.ecc, test.layers)
				if err == nil {
					low = middle
				} else {
					high = middle - 1
				}
			}
			payload := bytes.Repeat([]byte{0xff}, low)
			want, wantErr := upstream.Encode(payload, test.ecc, test.layers)
			got, gotErr := Encode(payload, test.ecc, test.layers)
			if wantErr != nil || gotErr != nil {
				t.Fatalf("maximum payload %d errors = (%v, %v)", low, gotErr, wantErr)
			}
			if got.Size != want.Size || got.Layers != want.Layers || got.CodeWords != want.CodeWords ||
				got.Compact != want.Compact || !got.Matrix.Equals(want.Matrix) {
				t.Fatalf("maximum payload %d did not match upstream", low)
			}
			next := bytes.Repeat([]byte{0xff}, low+1)
			_, wantErr = upstream.Encode(next, test.ecc, test.layers)
			_, gotErr = Encode(next, test.ecc, test.layers)
			if (gotErr != nil) != (wantErr != nil) {
				t.Fatalf("payload %d error = %v, upstream error = %v", low+1, gotErr, wantErr)
			}
		})
	}
}

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

func TestHighLevelExactTablesFlagsAndBinaryLimits(t *testing.T) {
	for value := byte(1); value <= 13; value++ {
		if got := charMap[value][modeMixed]; got != int(value)+1 {
			t.Fatalf("mixed charMap[%d] = %d", value, got)
		}
	}
	if charMap[0][modeMixed] != -1 || charMap[14][modeMixed] != -1 {
		t.Fatal("mixed control boundaries are incorrect")
	}
	for value := byte('0'); value <= '9'; value++ {
		if got := charMap[value][modeDigit]; got != int(value-'0')+2 {
			t.Fatalf("digit charMap[%q] = %d", value, got)
		}
	}
	if charMap['/'][modeDigit] != -1 || charMap[':'][modeDigit] != -1 {
		t.Fatal("digit boundaries are incorrect")
	}

	flag := bitutil.NewBitArray(0)
	appendFlag(flag, "09")
	if got, want := bitString(flag), "00000"+"00000"+"010"+"0010"+"1011"; got != want {
		t.Fatalf("appendFlag() = %s, want %s", got, want)
	}

	for _, length := range []int{31, 32, 2078, 2079} {
		bits := bitutil.NewBitArray(0)
		data := bytes.Repeat([]byte{0xff}, length)
		end := emitBinaryShift(bits, data, 0, modeUpper)
		used := length
		if used > 2078 {
			used = 2078
		}
		lengthBits := 5
		if used > 31 {
			lengthBits = 16
		}
		if end != used || bits.Size() != 5+lengthBits+used*8 {
			t.Fatalf("emitBinaryShift(length %d) = end %d, bits %d", length, end, bits.Size())
		}
	}
}

func TestLayerFitAndFullMatrixExactBoundaries(t *testing.T) {
	if !fitsLayer(90, 6, 100, 6, false) || fitsLayer(91, 6, 100, 6, false) {
		t.Fatal("usable word boundary is incorrect")
	}
	if !fitsLayer(384, 0, 500, 6, true) || fitsLayer(385, 0, 500, 6, true) {
		t.Fatal("compact codeword boundary is incorrect")
	}
	for base, want := range map[int]int{15: 16, 60: 63, 62: 67, 122: 131} {
		if got := fullMatrixSize(base); got != want {
			t.Fatalf("fullMatrixSize(%d) = %d, want %d", base, got, want)
		}
	}
}

func bitString(bits *bitutil.BitArray) string {
	result := make([]byte, bits.Size())
	for index := range result {
		result[index] = '0'
		if bits.Get(index) {
			result[index] = '1'
		}
	}
	return string(result)
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

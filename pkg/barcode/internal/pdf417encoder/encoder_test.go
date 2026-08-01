package pdf417encoder

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	upstream "github.com/ericlevine/zxinggo/pdf417/encoder"
)

func TestEncoderMatchesPinnedZXingImplementation(t *testing.T) {
	for _, test := range []struct {
		name       string
		message    string
		compaction Compaction
		level      int
		compact    bool
		maxCols    int
		minCols    int
		maxRows    int
		minRows    int
	}{
		{name: "text classes", message: "ABC abc 0123456789!?;:\t[]{}~", compaction: CompactionText, level: 0, maxCols: 30, minCols: 1, maxRows: 90, minRows: 3},
		{name: "binary five", message: string([]byte{0, 1, 2, 3, 4}), compaction: CompactionByte, level: 1, compact: true, maxCols: 10, minCols: 2, maxRows: 30, minRows: 3},
		{name: "binary six", message: string([]byte{0, 1, 2, 3, 4, 5}), compaction: CompactionByte, level: 2, maxCols: 15, minCols: 2, maxRows: 40, minRows: 3},
		{name: "binary seven", message: string([]byte{0, 1, 2, 3, 4, 5, 6}), compaction: CompactionByte, level: 3, compact: true, maxCols: 20, minCols: 2, maxRows: 50, minRows: 3},
		{name: "automatic single byte", message: "\x00", compaction: CompactionAuto, level: 0, maxCols: 10, minCols: 1, maxRows: 30, minRows: 3},
		{name: "automatic shifted byte", message: "ABCDE\x00", compaction: CompactionAuto, level: 1, maxCols: 10, minCols: 1, maxRows: 30, minRows: 3},
		{name: "text transitions", message: "aA1a!A", compaction: CompactionText, level: 2, maxCols: 15, minCols: 1, maxRows: 40, minRows: 3},
		{name: "text punctuation latch", message: "1!!A", compaction: CompactionText, level: 2, maxCols: 15, minCols: 1, maxRows: 40, minRows: 3},
		{name: "numeric forty three", message: strings.Repeat("1", 43), compaction: CompactionNumeric, level: 3, maxCols: 20, minCols: 2, maxRows: 50, minRows: 3},
		{name: "numeric forty four", message: strings.Repeat("12345678901", 4), compaction: CompactionNumeric, level: 4, maxCols: 20, minCols: 2, maxRows: 60, minRows: 3},
		{name: "numeric forty five", message: strings.Repeat("1", 45), compaction: CompactionNumeric, level: 4, maxCols: 20, minCols: 2, maxRows: 60, minRows: 3},
		{name: "automatic twelve digits", message: "AB123456789012Z", compaction: CompactionAuto, level: 4, maxCols: 20, minCols: 2, maxRows: 60, minRows: 3},
		{name: "automatic thirteen digits", message: "AB1234567890123Z", compaction: CompactionAuto, level: 4, maxCols: 20, minCols: 2, maxRows: 60, minRows: 3},
		{name: "automatic transitions", message: "HELLO1234567890123world-END", compaction: CompactionAuto, level: 5, compact: true, maxCols: 25, minCols: 2, maxRows: 70, minRows: 3},
		{name: "automatic long binary", message: string(bytes.Repeat([]byte{0x7f}, 13)), compaction: CompactionAuto, level: 8, maxCols: 30, minCols: 1, maxRows: 90, minRows: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			gotHigh, gotErr := EncodeHighLevel(test.message, test.compaction)
			wantHigh, wantErr := upstream.EncodeHighLevel(test.message, upstream.Compaction(test.compaction))
			if gotErr != nil || wantErr != nil || gotHigh != wantHigh {
				t.Fatalf("EncodeHighLevel() = (%v, %v), upstream = (%v, %v)", []rune(gotHigh), gotErr, []rune(wantHigh), wantErr)
			}

			got := NewPDF417Encoder()
			want := upstream.NewPDF417Encoder()
			got.SetDimensions(test.maxCols, test.minCols, test.maxRows, test.minRows)
			want.SetDimensions(test.maxCols, test.minCols, test.maxRows, test.minRows)
			got.SetCompaction(test.compaction)
			want.SetCompaction(upstream.Compaction(test.compaction))
			got.SetCompact(test.compact)
			want.SetCompact(test.compact)
			gotErr = got.GenerateBarcodeLogic(test.message, test.level)
			wantErr = want.GenerateBarcodeLogic(test.message, test.level)
			if gotErr != nil || wantErr != nil {
				t.Fatalf("GenerateBarcodeLogic() errors = (%v, %v)", gotErr, wantErr)
			}
			gotMatrix, wantMatrix := got.BarcodeMatrix().Matrix(), want.BarcodeMatrix().Matrix()
			gotHeight, wantHeight := len(gotMatrix), len(wantMatrix)
			gotWidth, wantWidth := 0, 0
			if gotHeight > 0 {
				gotWidth = len(gotMatrix[0])
			}
			if wantHeight > 0 {
				wantWidth = len(wantMatrix[0])
			}
			if gotHeight != wantHeight || gotWidth != wantWidth {
				t.Fatalf("GenerateBarcodeLogic() dimensions = %dx%d, upstream = %dx%d",
					gotWidth, gotHeight, wantWidth, wantHeight)
			}
		})
	}
}

func TestBarcodeRowsAndMatrices(t *testing.T) {
	row := NewBarcodeRow(5)
	row.Set(0, 1)
	row.currentLocation = 1
	row.AddBar(false, 2)
	row.AddBar(true, 2)
	if got, want := row.GetScaledRow(2), []byte{1, 1, 0, 0, 0, 0, 1, 1, 1, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scaled row = %v, want %v", got, want)
	}

	matrix := NewBarcodeMatrix(2, 1)
	matrix.Set(0, 0, 1)
	matrix.StartRow()
	if matrix.CurrentRow() != matrix.matrix[0] {
		t.Fatal("CurrentRow() did not return the active row")
	}
	if got := matrix.Matrix(); len(got) != 2 || got[1][0] != 1 {
		t.Fatalf("Matrix() = %v", got)
	}
	if got := matrix.ScaledMatrix(2, 3); len(got) != 6 || len(got[0]) != 172 {
		t.Fatalf("ScaledMatrix() dimensions = %dx%d", len(got[0]), len(got))
	}
}

func TestErrorCorrectionContracts(t *testing.T) {
	for level := 0; level <= 8; level++ {
		count, err := GetErrorCorrectionCodewordCount(level)
		if err != nil || count != 1<<(level+1) {
			t.Fatalf("GetErrorCorrectionCodewordCount(%d) = (%d, %v)", level, count, err)
		}
		encoded, err := GenerateErrorCorrection(string([]rune{10, 900, 928}), level)
		if err != nil || len([]rune(encoded)) != count {
			t.Fatalf("GenerateErrorCorrection(%d) = (%d codewords, %v)", level, len([]rune(encoded)), err)
		}
	}
	for _, level := range []int{-1, 9} {
		if _, err := GetErrorCorrectionCodewordCount(level); err == nil {
			t.Fatalf("GetErrorCorrectionCodewordCount(%d) succeeded", level)
		}
		if _, err := GenerateErrorCorrection("A", level); err == nil {
			t.Fatalf("GenerateErrorCorrection(%d) succeeded", level)
		}
	}

	for _, test := range []struct {
		size int
		want int
	}{
		{size: 1, want: 2}, {size: 40, want: 2}, {size: 41, want: 3},
		{size: 160, want: 3}, {size: 161, want: 4}, {size: 320, want: 4},
		{size: 321, want: 5}, {size: 863, want: 5},
	} {
		got, err := GetRecommendedMinimumErrorCorrectionLevel(test.size)
		if err != nil || got != test.want {
			t.Fatalf("recommendation(%d) = (%d, %v)", test.size, got, err)
		}
	}
	for _, size := range []int{0, 864} {
		if _, err := GetRecommendedMinimumErrorCorrectionLevel(size); err == nil {
			t.Fatalf("recommendation(%d) succeeded", size)
		}
	}
}

func TestHighLevelCompactionPaths(t *testing.T) {
	for _, test := range []struct {
		message    string
		compaction Compaction
	}{
		{message: "ABC abc 12!?;:\tZ", compaction: CompactionText},
		{message: "\x00\x01\x02\x03\x04", compaction: CompactionByte},
		{message: "\x00\x01\x02\x03\x04\x05", compaction: CompactionByte},
		{message: "\x00\x01\x02\x03\x04\x05\x06", compaction: CompactionByte},
		{message: strings.Repeat("1234567890", 5), compaction: CompactionNumeric},
		{message: "HELLO1234567890123world", compaction: CompactionAuto},
		{message: "\x80\x81HELLO", compaction: CompactionAuto},
		{message: "A\x80", compaction: CompactionAuto},
	} {
		if encoded, err := EncodeHighLevel(test.message, test.compaction); err != nil || encoded == "" {
			t.Fatalf("EncodeHighLevel(%q, %d) = (%q, %v)", test.message, test.compaction, encoded, err)
		}
	}
	if _, err := EncodeHighLevel("", CompactionAuto); err == nil {
		t.Fatal("empty message succeeded")
	}
	for _, message := range []string{"\x00", "\x7f", "\u0080"} {
		if _, err := EncodeHighLevel(message, CompactionText); err == nil {
			t.Fatalf("non-text character %q succeeded", message)
		}
	}
	if _, err := EncodeHighLevel("12A", CompactionNumeric); err == nil {
		t.Fatal("non-numeric numeric compaction succeeded")
	}

	var encoded strings.Builder
	for _, test := range []struct {
		message string
		mode    int
	}{
		{message: " A", mode: submodeAlpha},
		{message: "!", mode: submodeAlpha},
		{message: "a A1!", mode: submodeLower},
		{message: "1Aa!!A", mode: submodeMixed},
		{message: "1a", mode: submodeMixed},
		{message: "!A", mode: submodePunctuation},
	} {
		encoded.Reset()
		encodeText(test.message, 0, len(test.message), &encoded, test.mode)
		if encoded.Len() == 0 {
			t.Fatalf("encodeText(%q, %d) was empty", test.message, test.mode)
		}
	}

	if determineConsecutiveDigitCount("123A", 0) != 3 || determineConsecutiveDigitCount("A", 1) != 0 {
		t.Fatal("digit run detection failed")
	}
	if determineConsecutiveTextCount("ABC1234567890123", 0) != 3 ||
		determineConsecutiveTextCount("123ABC", 0) != 6 ||
		determineConsecutiveTextCount("\x80", 0) != 0 {
		t.Fatal("text run detection failed")
	}
	if determineConsecutiveBinaryCount("AB1234567890123", 0) != 2 ||
		determineConsecutiveBinaryCount("ABC", 0) != 3 {
		t.Fatal("binary run detection failed")
	}
}

func TestHighLevelCharacterAndRunBoundaries(t *testing.T) {
	for _, test := range []struct {
		name string
		got  bool
		want bool
	}{
		{name: "digit below", got: isDigit('/'), want: false},
		{name: "digit lower", got: isDigit('0'), want: true},
		{name: "digit upper", got: isDigit('9'), want: true},
		{name: "digit above", got: isDigit(':'), want: false},
		{name: "upper space", got: isAlphaUpper(' '), want: true},
		{name: "upper below", got: isAlphaUpper('@'), want: false},
		{name: "upper lower", got: isAlphaUpper('A'), want: true},
		{name: "upper upper", got: isAlphaUpper('Z'), want: true},
		{name: "upper above", got: isAlphaUpper('['), want: false},
		{name: "lower space", got: isAlphaLower(' '), want: true},
		{name: "lower below", got: isAlphaLower('`'), want: false},
		{name: "lower lower", got: isAlphaLower('a'), want: true},
		{name: "lower upper", got: isAlphaLower('z'), want: true},
		{name: "lower above", got: isAlphaLower('{'), want: false},
		{name: "mixed NUL", got: isMixed(0), want: false},
		{name: "mixed digit", got: isMixed('0'), want: true},
		{name: "punctuation NUL", got: isPunctuation(0), want: false},
		{name: "punctuation mark", got: isPunctuation('!'), want: true},
		{name: "text below tab", got: isText(8), want: false},
		{name: "text tab", got: isText('\t'), want: true},
		{name: "text line feed", got: isText('\n'), want: true},
		{name: "text carriage return", got: isText('\r'), want: true},
		{name: "text below printable", got: isText(31), want: false},
		{name: "text printable lower", got: isText(32), want: true},
		{name: "text printable upper", got: isText(126), want: true},
		{name: "text above printable", got: isText(127), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("classification = %t, want %t", test.got, test.want)
			}
		})
	}

	for _, test := range []struct {
		name string
		got  int
		want int
	}{
		{name: "digit offset", got: determineConsecutiveDigitCount("xx123Z", 2), want: 3},
		{name: "digit at end", got: determineConsecutiveDigitCount("xx", 2), want: 0},
		{name: "text offset before numeric latch", got: determineConsecutiveTextCount("xxABC1234567890123", 2), want: 3},
		{name: "text offset below numeric latch", got: determineConsecutiveTextCount("xxABC123456789012Z", 2), want: 16},
		{name: "binary offset before numeric latch", got: determineConsecutiveBinaryCount("xxAB1234567890123", 2), want: 2},
		{name: "binary offset below numeric latch", got: determineConsecutiveBinaryCount("xxAB123456789012Z", 2), want: 15},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("run length = %d, want %d", test.got, test.want)
			}
		})
	}
}

func TestBinaryCompactionCodewordBoundaries(t *testing.T) {
	for _, test := range []struct {
		name      string
		bytes     []byte
		startMode int
		want      []rune
	}{
		{name: "single text shift", bytes: []byte{7}, startMode: textCompaction, want: []rune{shiftToByte, 7}},
		{name: "single byte latch", bytes: []byte{7}, startMode: byteCompaction, want: []rune{latchToBytePadded, 7}},
		{name: "six byte latch", bytes: make([]byte, 6), startMode: byteCompaction, want: []rune{latchToByte, 0, 0, 0, 0, 0}},
		{name: "seven byte padded latch", bytes: make([]byte, 7), startMode: byteCompaction, want: []rune{latchToBytePadded, 0, 0, 0, 0, 0, 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var encoded strings.Builder
			encodeBinary(test.bytes, test.startMode, &encoded)
			if got := []rune(encoded.String()); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("codewords = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSymbolCodewordCapacityBoundaries(t *testing.T) {
	encoder := NewPDF417Encoder()
	encoder.SetCompaction(CompactionByte)
	encoder.SetDimensions(30, 1, 90, 3)
	if err := encoder.GenerateBarcodeLogic(strings.Repeat("A", 1_108), 0); err != nil {
		t.Fatalf("maximum-capacity symbol error = %v", err)
	}
	if err := encoder.GenerateBarcodeLogic(strings.Repeat("A", 1_109), 0); err == nil ||
		!strings.Contains(err.Error(), "too many code words") {
		t.Fatalf("over-capacity symbol error = %v", err)
	}
}

func TestEncodeCharPatterns(t *testing.T) {
	for _, test := range []struct {
		name    string
		pattern int
		length  int
		want    []byte
	}{
		{name: "starts black", pattern: 0b101, length: 3, want: []byte{1, 0, 1}},
		{name: "starts white", pattern: 0b010, length: 3, want: []byte{0, 1, 0}},
		{name: "single black", pattern: 1, length: 1, want: []byte{1}},
		{name: "single white", pattern: 0, length: 1, want: []byte{0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := NewBarcodeRow(test.length)
			encodeChar(test.pattern, test.length, row)
			if !reflect.DeepEqual(row.row, test.want) {
				t.Fatalf("row = %v, want %v", row.row, test.want)
			}
		})
	}
}

func TestEncoderLayoutAndFailurePaths(t *testing.T) {
	for level := 0; level <= 8; level++ {
		encoder := NewPDF417Encoder()
		encoder.SetDimensions(30, 1, 90, 3)
		encoder.SetCompaction(Compaction(level % 4))
		encoder.SetCompact(level%2 == 0)
		message := "PDF417 LAYOUT"
		if encoder.compaction == CompactionNumeric {
			message = "12345678901234567890"
		}
		if err := encoder.GenerateBarcodeLogic(message, level); err != nil {
			t.Fatalf("GenerateBarcodeLogic(level %d) error = %v", level, err)
		}
		if encoder.BarcodeMatrix() == nil || len(encoder.BarcodeMatrix().Matrix()) < 3 {
			t.Fatalf("level %d did not produce a matrix", level)
		}
	}

	encoder := NewPDF417Encoder()
	if err := encoder.GenerateBarcodeLogic("", 2); err == nil {
		t.Fatal("empty message succeeded")
	}
	if err := encoder.GenerateBarcodeLogic("A", 9); err == nil {
		t.Fatal("invalid correction level succeeded")
	}
	if err := encoder.GenerateBarcodeLogicWithControls("A", 2, 811_800, nil); err == nil {
		t.Fatal("invalid ECI succeeded")
	}
	if err := encoder.GenerateBarcodeLogicWithControls("A", 2, 26, nil); err != nil {
		t.Fatalf("valid ECI error = %v", err)
	}
	encoder.SetCompaction(CompactionByte)
	encoder.SetDimensions(30, 1, 90, 3)
	if err := encoder.GenerateBarcodeLogic(strings.Repeat("A", 2_000), 0); err == nil {
		t.Fatal("oversized message succeeded")
	}
	if err := encoder.GenerateBarcodeLogicWithControls("A", 2, 0, &Macro{}); err == nil {
		t.Fatal("invalid macro succeeded")
	}
	encoder.SetDimensions(1, 1, 1, 1)
	if err := encoder.GenerateBarcodeLogic(strings.Repeat("A", 100), 2); err == nil {
		t.Fatal("impossible dimensions succeeded")
	}

	if calculateNumberOfRows(1, 2, 2) != 2 || getNumberOfPadCodewords(1, 2, 2, 2) != 0 ||
		getNumberOfPadCodewords(1, 2, 3, 2) != 2 {
		t.Fatal("row or padding calculation failed")
	}
	for _, test := range []struct {
		name                                         string
		minCols, maxCols, minRows, maxRows, data, ec int
		want                                         []int
	}{
		{name: "preferred ratio", minCols: 1, maxCols: 30, minRows: 3, maxRows: 90, data: 20, ec: 8, want: []int{4, 8}},
		{name: "only maximum column", minCols: 1, maxCols: 1, minRows: 2, maxRows: 4, data: 1, ec: 2, want: []int{1, 4}},
		{name: "rows equal minimum and maximum", minCols: 1, maxCols: 1, minRows: 4, maxRows: 4, data: 1, ec: 2, want: []int{1, 4}},
		{name: "larger payload", minCols: 2, maxCols: 30, minRows: 2, maxRows: 90, data: 100, ec: 16, want: []int{9, 13}},
		{name: "minimum fallback", minCols: 10, maxCols: 10, minRows: 20, maxRows: 20, data: 1, ec: 2, want: []int{10, 20}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dimensions, err := determineDimensions(test.minCols, test.maxCols, test.minRows, test.maxRows, test.data, test.ec)
			if err != nil || !reflect.DeepEqual(dimensions, test.want) {
				t.Fatalf("determineDimensions() = (%v, %v), want %v", dimensions, err, test.want)
			}
		})
	}
	if _, err := determineDimensions(1, 1, 1, 1, 100, 8); err == nil {
		t.Fatal("impossible dimensions succeeded")
	}
}

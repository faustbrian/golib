package pdf417encoder

import (
	"reflect"
	"strings"
	"testing"
)

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
	if _, err := EncodeHighLevel("\u0080", CompactionText); err == nil {
		t.Fatal("non-ASCII text compaction succeeded")
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
	if dimensions, err := determineDimensions(1, 30, 3, 90, 20, 8); err != nil || len(dimensions) != 2 {
		t.Fatalf("determineDimensions() = (%v, %v)", dimensions, err)
	}
	if dimensions, err := determineDimensions(10, 10, 20, 20, 1, 2); err != nil || !reflect.DeepEqual(dimensions, []int{10, 20}) {
		t.Fatalf("minimum dimensions = (%v, %v)", dimensions, err)
	}
	if _, err := determineDimensions(1, 1, 1, 1, 100, 8); err == nil {
		t.Fatal("impossible dimensions succeeded")
	}
}

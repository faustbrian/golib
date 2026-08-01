package imagedecode

import (
	"context"
	"errors"
	"image"
	"image/color"
	"testing"
	"time"

	zxinggo "github.com/ericlevine/zxinggo"
	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/qr"
	"github.com/faustbrian/golib/pkg/barcode/render"
	"github.com/makiuchi-d/gozxing"
)

func TestTwoDReaderDecodeWithoutHints(t *testing.T) {
	symbol, err := qr.Encode([]byte("WITHOUT-HINTS"), qr.Options{})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	input, err := render.Image(symbol.Logical(), render.Options{Scale: 4})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	bitmap, err := gozxing.NewBinaryBitmap(
		gozxing.NewHybridBinarizer(gozxing.NewLuminanceSourceFromImage(input)),
	)
	if err != nil {
		t.Fatalf("NewBinaryBitmap() error = %v", err)
	}
	result, err := newQRCodeReader().DecodeWithoutHints(bitmap)
	if err != nil {
		t.Fatalf("DecodeWithoutHints() error = %v", err)
	}
	if result.GetText() != "WITHOUT-HINTS" {
		t.Fatalf("GetText() = %q, want WITHOUT-HINTS", result.GetText())
	}
}

func TestTwoDReaderContainsDependencyPanics(t *testing.T) {
	symbol, err := qr.Encode([]byte("PANIC-BOUNDARY"), qr.Options{})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	input, err := render.Image(symbol.Logical(), render.Options{Scale: 4})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	bitmap, err := gozxing.NewBinaryBitmap(gozxing.NewHybridBinarizer(
		gozxing.NewLuminanceSourceFromImage(input),
	))
	if err != nil {
		t.Fatalf("NewBinaryBitmap() error = %v", err)
	}
	reader := twoDReader{reader: panickingZXingReader{}, format: gozxing.BarcodeFormat_DATA_MATRIX}
	if result, decodeErr := reader.Decode(bitmap, nil); result != nil || decodeErr == nil {
		t.Fatalf("Decode() = (%v, %v), want nil classified failure", result, decodeErr)
	}
}

func TestControlledDataMatrixECIWidths(t *testing.T) {
	tests := []struct {
		name   string
		prefix []byte
	}{
		{name: "one byte", prefix: []byte{241, 2}},
		{name: "two bytes", prefix: []byte{241, 128, 1}},
		{name: "three bytes", prefix: []byte{241, 192, 1, 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			codewords := controlledCodewords(test.prefix, []byte("A"))
			got, ok := decodeControlledDataMatrix(codewords)
			if !ok || got != "A" {
				t.Fatalf("decodeControlledDataMatrix() = (%q, %t)", got, ok)
			}
		})
	}
}

func TestDataMatrixECIExactBoundaries(t *testing.T) {
	for _, test := range []struct {
		codewords  []byte
		assignment int
		next       int
	}{
		{codewords: []byte{1}, assignment: 0, next: 1},
		{codewords: []byte{127}, assignment: 126, next: 1},
		{codewords: []byte{128, 1}, assignment: 127, next: 2},
		{codewords: []byte{191, 255}, assignment: 16_383, next: 2},
		{codewords: []byte{192, 1, 1}, assignment: 16_383, next: 3},
		{codewords: []byte{254, 255, 255}, assignment: 4_081_145, next: 3},
	} {
		assignment, next, ok := decodeDataMatrixECI(test.codewords, 0)
		if !ok || assignment != test.assignment || next != test.next {
			t.Fatalf("decodeDataMatrixECI(%v) = (%d, %d, %t), want (%d, %d, true)",
				test.codewords, assignment, next, ok, test.assignment, test.next)
		}
	}
	for _, codewords := range [][]byte{{0}, {192, 1, 0}} {
		if _, _, ok := decodeDataMatrixECI(codewords, 0); ok {
			t.Fatalf("decodeDataMatrixECI(%v) succeeded", codewords)
		}
	}
}

func TestControlledDataMatrixSequenceMacroAndFallbackECI(t *testing.T) {
	for _, test := range []struct {
		prefix  []byte
		payload []byte
		want    string
	}{
		{prefix: []byte{233, 15, 1}, payload: []byte("A"), want: "A"},
		{prefix: []byte{236, 232}, payload: []byte("A"), want: "[)>\x1e05\x1dA\x1e\x04"},
		{prefix: []byte{241, 1}, payload: []byte{0x80}, want: string([]byte{0x80})},
	} {
		got, ok := decodeControlledDataMatrix(controlledCodewords(test.prefix, test.payload))
		if !ok || got != test.want {
			t.Fatalf("decodeControlledDataMatrix(%v) = (%q, %t), want %q", test.prefix, got, ok, test.want)
		}
	}
	if _, ok := decodeControlledDataMatrix([]byte{233}); ok {
		t.Fatal("short structured append header succeeded")
	}
}

func TestTwoDReaderFallbackControls(t *testing.T) {
	symbol, err := qr.Encode([]byte("FALLBACK"), qr.Options{})
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	input, err := render.Image(symbol.Logical(), render.Options{Scale: 4})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	bitmap, err := gozxing.NewBinaryBitmap(gozxing.NewHybridBinarizer(
		gozxing.NewLuminanceSourceFromImage(input),
	))
	if err != nil {
		t.Fatalf("NewBinaryBitmap() error = %v", err)
	}
	for _, test := range []struct {
		text string
		raw  []byte
		want string
	}{
		{text: "EMPTY", want: "EMPTY"},
		{text: "\x1dDATA", raw: []byte{232, 230}, want: "DATA"},
		{text: "[)>\x1e05\x1dDATA", raw: []byte{236, 230}, want: "[)>\x1e05\x1dDATA\x1e\x04"},
		{text: "[)>\x1e06\x1dDATA\x1e\x04", raw: []byte{237, 230}, want: "[)>\x1e06\x1dDATA\x1e\x04"},
	} {
		decoded := zxinggo.NewResult(test.text, test.raw, nil, zxinggo.FormatDataMatrix)
		reader := twoDReader{reader: &staticZXingReader{result: decoded}, format: gozxing.BarcodeFormat_DATA_MATRIX}
		result, decodeErr := reader.Decode(bitmap, nil)
		if decodeErr != nil {
			t.Fatalf("Decode(%v) error = %v", test.raw, decodeErr)
		}
		if result == nil {
			t.Fatalf("Decode(%v) returned a nil result", test.raw)
		}
		if result.GetText() != test.want {
			t.Fatalf("Decode(%v) text = %q, want %q", test.raw, result.GetText(), test.want)
		}
	}
}

func TestReadersPropagateBlackMatrixErrors(t *testing.T) {
	bitmap, err := gozxing.NewBinaryBitmap(errorBinarizer{
		source: gozxing.NewLuminanceSourceFromImage(image.NewGray(image.Rect(0, 0, 1, 1))),
	})
	if err != nil {
		t.Fatalf("NewBinaryBitmap() error = %v", err)
	}
	if _, err = newQRCodeReader().DecodeWithoutHints(bitmap); err == nil {
		t.Fatal("twoDReader accepted a failed black matrix")
	}
	if _, err = (pdf417Reader{}).DecodeWithoutHints(bitmap); err == nil {
		t.Fatal("pdf417Reader accepted a failed black matrix")
	}
	(pdf417Reader{}).Reset()
}

func TestTwoDMetadataCopiesSequenceAndByteSegments(t *testing.T) {
	source := zxinggo.NewResult("A", []byte("A"), nil, zxinggo.FormatQRCode)
	source.PutMetadata(zxinggo.MetadataByteSegments, [][]byte{{'A'}})
	source.PutMetadata(zxinggo.MetadataStructuredAppendSequence, 2)
	source.PutMetadata(zxinggo.MetadataStructuredAppendParity, 7)
	target := gozxing.NewResult("A", []byte("A"), nil, gozxing.BarcodeFormat_QR_CODE)
	copyTwoDMetadata(target, source)
	metadata := target.GetResultMetadata()
	if metadata[gozxing.ResultMetadataType_STRUCTURED_APPEND_SEQUENCE] != 2 ||
		metadata[gozxing.ResultMetadataType_STRUCTURED_APPEND_PARITY] != 7 ||
		metadata[gozxing.ResultMetadataType_BYTE_SEGMENTS] == nil {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestDecodeChecksDeadlineBetweenAttempts(t *testing.T) {
	_, err := Decode(context.Background(), slowImage{}, Options{
		Formats: []barcode.Format{barcode.QRCode},
		Limits:  Limits{MaxDuration: 5 * time.Millisecond},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Decode() error = %v, want deadline exceeded", err)
	}
	if err := contextError(context.Background(), time.Now().Add(-time.Second), time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contextError() error = %v", err)
	}
	if err := decodeFailure(context.Background(), time.Now(), 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("decodeFailure() error = %v, want not found", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := decodeFailure(canceled, time.Now(), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("decodeFailure() error = %v, want canceled", err)
	}
}

func TestDecodeRotatesInvertedInput(t *testing.T) {
	_, err := Decode(context.Background(), image.NewGray(image.Rect(0, 0, 64, 64)), Options{
		Formats:       []barcode.Format{barcode.QRCode},
		AllowInverted: true,
		Limits:        Limits{MaxRotations: 2},
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Decode() error = %v, want not found", err)
	}
}

func TestControlledDataMatrixRejectsMalformedCodewords(t *testing.T) {
	tests := [][]byte{
		nil,
		{233, 1},
		{232},
		{236},
		{237},
		{241},
		{241, 0},
		{241, 128},
		{241, 128, 0},
		{241, 192, 1},
		{241, 192, 0, 1},
		{241, 255},
		{241, 2, 230},
		{241, 2, 231},
		{231, randomize255(2, 2), randomize255('A', 3)},
	}
	for _, codewords := range tests {
		if got, ok := decodeControlledDataMatrix(codewords); ok {
			t.Fatalf("decodeControlledDataMatrix(%v) = %q, true", codewords, got)
		}
	}
}

func TestBase256LengthFormsAndFailures(t *testing.T) {
	zero := []byte{randomize255(0, 1), 'A', 'B'}
	if length, next, ok := decodeBase256Length(zero, 0); !ok || length != 2 || next != 1 {
		t.Fatalf("zero length form = (%d, %d, %t)", length, next, ok)
	}
	large := make([]byte, 2)
	large[0] = randomize255(250, 1)
	large[1] = randomize255(0, 2)
	if length, next, ok := decodeBase256Length(large, 0); !ok || length != 250 || next != 2 {
		t.Fatalf("large length form = (%d, %d, %t)", length, next, ok)
	}
	for _, test := range []struct {
		first, second byte
		want          int
	}{
		{first: 249, want: 249},
		{first: 250, second: 249, want: 499},
		{first: 255, second: 249, want: 1_749},
	} {
		codewords := []byte{randomize255(test.first, 1)}
		if test.first > 249 {
			codewords = append(codewords, randomize255(test.second, 2))
		}
		length, _, ok := decodeBase256Length(codewords, 0)
		if !ok || length != test.want {
			t.Fatalf("decodeBase256Length(%d, %d) = (%d, %t), want %d", test.first, test.second, length, ok, test.want)
		}
	}
	for _, test := range []struct {
		value    byte
		position int
		want     byte
	}{
		{value: 150, position: 1, want: 0},
		{value: 149, position: 1, want: 255},
		{value: 151, position: 1, want: 1},
	} {
		if got := unrandomize255(test.value, test.position); got != test.want {
			t.Fatalf("unrandomize255(%d, %d) = %d, want %d", test.value, test.position, got, test.want)
		}
	}
	for _, test := range []struct {
		codewords []byte
		index     int
	}{
		{index: 0},
		{codewords: []byte{randomize255(250, 1)}, index: 0},
		{codewords: []byte{1}, index: 1},
	} {
		if _, _, ok := decodeBase256Length(test.codewords, test.index); ok {
			t.Fatalf("decodeBase256Length(%v, %d) succeeded", test.codewords, test.index)
		}
	}
}

func TestOrientationChecksumAndFormatMappings(t *testing.T) {
	for degrees, want := range map[int]barcode.Orientation{
		0: barcode.Orientation0, 90: barcode.Orientation90,
		180: barcode.Orientation180, 270: barcode.Orientation270,
	} {
		result := gozxing.NewResult("A", nil, nil, gozxing.BarcodeFormat_CODE_128)
		result.PutMetadata(gozxing.ResultMetadataType_ORIENTATION, degrees)
		if got := orientationFor(result, 0); got != want {
			t.Fatalf("orientationFor(%d) = %d, want %d", degrees, got, want)
		}
	}
	points := func(x0, y0, x1, y1 float64) []gozxing.ResultPoint {
		return []gozxing.ResultPoint{
			gozxing.NewResultPoint(x0, y0), gozxing.NewResultPoint(x1, y1),
		}
	}
	for _, test := range []struct {
		points []gozxing.ResultPoint
		want   int
	}{
		{want: 0},
		{points: points(2, 0, 1, 0), want: 90},
		{points: points(1, 0, 2, 0), want: 270},
		{points: points(0, 1, 0, 2), want: 180},
		{points: points(0, 2, 0, 1), want: 0},
		{points: points(0, 0, 1, 1), want: 180},
		{points: points(1, 1, 0, 0), want: 0},
		{points: points(1, 1, 1, 1), want: 0},
	} {
		if got := qrOrientation(test.points); got != test.want {
			t.Fatalf("qrOrientation(%v) = %d, want %d", test.points, got, test.want)
		}
	}
	for format, want := range map[barcode.Format]barcode.ChecksumStatus{
		barcode.QRCode:            barcode.ChecksumNotApplicable,
		barcode.Code39:            barcode.ChecksumUnknown,
		barcode.Code128:           barcode.ChecksumValid,
		barcode.Format("unknown"): barcode.ChecksumValid,
	} {
		if got := checksumStatus(format); got != want {
			t.Fatalf("checksumStatus(%q) = %v, want %v", format, got, want)
		}
	}
	combined := gozxing.NewResult("A", nil, nil, gozxing.BarcodeFormat_CODE_128)
	combined.PutMetadata(gozxing.ResultMetadataType_ORIENTATION, 90)
	if got := orientationFor(combined, 1); got != barcode.Orientation0 {
		t.Fatalf("orientationFor(combined) = %d, want 0", got)
	}
	corrected := gozxing.NewResult("A", nil, nil, gozxing.BarcodeFormat_QR_CODE)
	corrected.PutMetadata(gozxing.ResultMetadataType_OTHER, 1)
	if got := metadataDiagnostics(corrected); len(got) != 1 || got[0] != "ERRORS_CORRECTED=1" {
		t.Fatalf("metadataDiagnostics() = %q", got)
	}
	uncorrected := gozxing.NewResult("A", nil, nil, gozxing.BarcodeFormat_QR_CODE)
	if got := metadataDiagnostics(uncorrected); len(got) != 0 {
		t.Fatalf("metadataDiagnostics(uncorrected) = %q", got)
	}
	if exceedsCorrectionBudget(1, 1) || !exceedsCorrectionBudget(2, 1) || exceedsCorrectionBudget(1, 0) {
		t.Fatal("correction budget boundary is incorrect")
	}
	payload := gozxing.NewResult("AB", []byte("AB"), nil, gozxing.BarcodeFormat_QR_CODE)
	if exceedsPayloadBudget(payload, 2) || !exceedsPayloadBudget(payload, 1) {
		t.Fatal("payload budget boundary is incorrect")
	}
	if err := contextError(context.Background(), timeNow(), 0); err != nil {
		t.Fatalf("contextError() error = %v", err)
	}
}

func TestImageAndLimitExactBoundaries(t *testing.T) {
	limits := Limits{
		MaxWidth: 4, MaxHeight: 3, MaxPixels: 12, MaxMemoryBytes: 48,
	}
	if err := validateImageSize(4, 3, limits); err != nil {
		t.Fatalf("validateImageSize(exact limits) error = %v", err)
	}
	memoryLimits := limits
	memoryLimits.MaxHeight = 4
	memoryLimits.MaxPixels = 100
	if err := validateImageSize(4, 4, memoryLimits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("validateImageSize(memory limit) error = %v", err)
	}
	for _, size := range [][2]int{{0, 1}, {1, 0}, {5, 1}, {1, 4}, {4, 4}} {
		if err := validateImageSize(size[0], size[1], limits); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("validateImageSize(%d, %d) error = %v", size[0], size[1], err)
		}
	}
	for _, limits := range []Limits{
		{MaxWidth: -1}, {MaxHeight: -1}, {MaxPixels: -1},
		{MaxCandidates: -1}, {MaxPayloadBytes: -1}, {MaxRotations: -1},
		{MaxMemoryBytes: -1}, {MaxEncodedBytes: -1},
		{MaxCorrections: -1}, {MaxDuration: -1},
	} {
		if _, err := normalizeLimits(limits); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("normalizeLimits(%+v) error = %v", limits, err)
		}
	}
}

func controlledCodewords(prefix, payload []byte) []byte {
	codewords := append([]byte(nil), prefix...)
	codewords = append(codewords, 231)
	// #nosec G115 -- test payloads are deliberately bounded below 256 bytes.
	codewords = append(codewords, randomize255(byte(len(payload)), len(codewords)+1))
	for _, value := range payload {
		codewords = append(codewords, randomize255(value, len(codewords)+1))
	}
	return codewords
}

func randomize255(value byte, position int) byte {
	randomized := int(value) + (149*position)%255 + 1
	if randomized > 255 {
		randomized -= 256
	}
	// #nosec G115 -- modulo arithmetic bounds randomized to one byte.
	return byte(randomized)
}

func timeNow() time.Time { return time.Now() }

type staticZXingReader struct{ result *zxinggo.Result }

type panickingZXingReader struct{}

func (panickingZXingReader) Decode(*zxinggo.BinaryBitmap, *zxinggo.DecodeOptions) (*zxinggo.Result, error) {
	panic("dependency panic")
}

func (panickingZXingReader) Reset() {}

func (reader *staticZXingReader) Decode(*zxinggo.BinaryBitmap, *zxinggo.DecodeOptions) (*zxinggo.Result, error) {
	return reader.result, nil
}

func (*staticZXingReader) Reset() {}

type errorBinarizer struct{ source gozxing.LuminanceSource }

func (b errorBinarizer) GetLuminanceSource() gozxing.LuminanceSource { return b.source }
func (errorBinarizer) GetBlackRow(int, *gozxing.BitArray) (*gozxing.BitArray, error) {
	return nil, errors.New("black row failure")
}
func (errorBinarizer) GetBlackMatrix() (*gozxing.BitMatrix, error) {
	return nil, errors.New("black matrix failure")
}
func (b errorBinarizer) CreateBinarizer(source gozxing.LuminanceSource) gozxing.Binarizer {
	return errorBinarizer{source: source}
}
func (b errorBinarizer) GetWidth() int  { return b.source.GetWidth() }
func (b errorBinarizer) GetHeight() int { return b.source.GetHeight() }

type slowImage struct{}

func (slowImage) ColorModel() color.Model { return color.GrayModel }
func (slowImage) Bounds() image.Rectangle { return image.Rect(0, 0, 4, 4) }
func (slowImage) At(int, int) color.Color {
	time.Sleep(time.Millisecond)
	return color.White
}

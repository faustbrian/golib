package imagedecode

import (
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/pdf417"
	"github.com/faustbrian/golib/pkg/barcode/render"
	"github.com/makiuchi-d/gozxing"
)

func TestPDF417ReaderImplementsReaderLifecycle(t *testing.T) {
	symbol, err := pdf417.Encode([]byte("PDF417-READER"), pdf417.Options{
		Macro: &pdf417.Macro{SegmentIndex: 0, FileID: "001", LastSegment: true},
	})
	if err != nil {
		t.Fatalf("pdf417.Encode() error = %v", err)
	}
	input, err := render.Image(symbol.Logical(), render.Options{Scale: 6})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	bitmap, err := gozxing.NewBinaryBitmap(
		gozxing.NewHybridBinarizer(gozxing.NewLuminanceSourceFromImage(input)),
	)
	if err != nil {
		t.Fatalf("gozxing.NewBinaryBitmap() error = %v", err)
	}

	reader := pdf417Reader{}
	result, err := reader.DecodeWithoutHints(bitmap)
	if err != nil {
		t.Fatalf("DecodeWithoutHints() error = %v", err)
	}
	if result.GetText() != "PDF417-READER" {
		t.Fatalf("GetText() = %q, want PDF417-READER", result.GetText())
	}
	metadata := result.GetResultMetadata()
	if metadata[gozxing.ResultMetadataType_PDF417_EXTRA_METADATA] == nil ||
		metadata[gozxing.ResultMetadataType_SYMBOLOGY_IDENTIFIER] == nil {
		t.Fatalf("metadata = %+v", metadata)
	}
	reader.Reset()
}

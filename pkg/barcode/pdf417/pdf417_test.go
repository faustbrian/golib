package pdf417_test

import (
	"context"
	"errors"
	"testing"

	zxinggo "github.com/ericlevine/zxinggo"
	"github.com/ericlevine/zxinggo/binarizer"
	zxingpdf417 "github.com/ericlevine/zxinggo/pdf417"
	"github.com/ericlevine/zxinggo/pdf417/decoder"
	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/imagedecode"
	"github.com/faustbrian/golib/pkg/barcode/pdf417"
	"github.com/faustbrian/golib/pkg/barcode/render"
)

func TestEncodeSupportsErrorCorrection(t *testing.T) {
	symbol, err := pdf417.Encode([]byte("123456789012345678901234567890"), pdf417.Options{
		ErrorCorrection: pdf417.Level4,
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if symbol.Logical().Format() != barcode.PDF417 || symbol.Compaction() != pdf417.Automatic ||
		symbol.ErrorCorrection() != pdf417.Level4 || symbol.Compact() {
		t.Fatalf("metadata = (%q, %v, %v, %t)", symbol.Logical().Format(), symbol.Compaction(), symbol.ErrorCorrection(), symbol.Compact())
	}
	if matrix := symbol.Logical().Matrix(); matrix.Width() <= matrix.Height() {
		t.Fatalf("matrix = %dx%d", matrix.Width(), matrix.Height())
	}
	if _, ok := symbol.Macro(); ok {
		t.Fatal("Macro() reported absent metadata")
	}
}

func TestEncodeSupportsECIAndMacroControlBlocks(t *testing.T) {
	segmentCount := 2
	timestamp := int64(1_700_000_000)
	fileSize := int64(7)
	checksum := 42
	options := pdf417.Options{ECI: 26, Macro: &pdf417.Macro{
		SegmentIndex: 1, FileID: "129899", LastSegment: true,
		FileName: "FILE.TXT", SegmentCount: &segmentCount, Timestamp: &timestamp,
		Sender: "ALICE", Addressee: "BOB", FileSize: &fileSize, Checksum: &checksum,
	}}
	symbol, err := pdf417.Encode([]byte("Helsinki \xe2\x98\x83"), options)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if symbol.ECI() != 26 {
		t.Fatalf("ECI() = %d, want 26", symbol.ECI())
	}
	macro, ok := symbol.Macro()
	if !ok || macro.FileID != "129899" || macro.SegmentIndex != 1 || !macro.LastSegment {
		t.Fatalf("Macro() = (%+v, %t)", macro, ok)
	}
	*macro.SegmentCount = 99
	macroAgain, _ := symbol.Macro()
	if *macroAgain.SegmentCount != 2 {
		t.Fatal("Macro() returned aliased metadata")
	}

	raster, err := render.Image(symbol.Logical(), render.Options{Scale: 3})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	input := zxinggo.NewBinaryBitmap(binarizer.NewGlobalHistogram(
		zxinggo.NewImageLuminanceSource(raster),
	))
	decoded, err := zxingpdf417.NewPDF417Reader().Decode(input, &zxinggo.DecodeOptions{})
	if err != nil {
		t.Fatalf("independent Decode() error = %v", err)
	}
	if decoded.Text != "Helsinki \xe2\x98\x83" {
		t.Fatalf("payload = %q", decoded.Text)
	}
	metadata, ok := decoded.Metadata[zxinggo.MetadataPDF417ExtraMetadata].(*decoder.PDF417ResultMetadata)
	if !ok {
		t.Fatalf("macro metadata = %T", decoded.Metadata[zxinggo.MetadataPDF417ExtraMetadata])
	}
	if metadata.SegmentIndex != 1 || metadata.FileID != "129899" || !metadata.LastSegment ||
		metadata.FileName != "FILE.TXT" || metadata.SegmentCount != 2 ||
		metadata.Timestamp != timestamp || metadata.Sender != "ALICE" ||
		metadata.Addressee != "BOB" || metadata.FileSize != fileSize ||
		metadata.Checksum != checksum {
		t.Fatalf("macro metadata = %+v", metadata)
	}
}

func TestEncodeSupportsAllCorrectionLevelsAndQuietZones(t *testing.T) {
	levels := []pdf417.ErrorCorrection{
		pdf417.Level0, pdf417.Level1, pdf417.Level2, pdf417.Level3, pdf417.Level4,
		pdf417.Level5, pdf417.Level6, pdf417.Level7, pdf417.Level8,
	}
	for _, level := range levels {
		symbol, err := pdf417.Encode([]byte("PDF417 CORRECTION"), pdf417.Options{
			ErrorCorrection: level, QuietZone: 5,
		})
		if err != nil {
			t.Fatalf("Encode(%v) error = %v", level, err)
		}
		if symbol.ErrorCorrection() != level {
			t.Fatalf("ErrorCorrection() = %v, want %v", symbol.ErrorCorrection(), level)
		}
	}
}

func TestEncodeSupportsCompactionAndLayoutControls(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		options pdf417.Options
	}{
		{name: "text", payload: "PDF417 TEXT", options: pdf417.Options{Compaction: pdf417.Text}},
		{name: "byte", payload: "\x00\x7fPDF417", options: pdf417.Options{Compaction: pdf417.Byte}},
		{name: "numeric", payload: "12345678901234567890", options: pdf417.Options{Compaction: pdf417.Numeric}},
		{name: "compact", payload: "PDF417 COMPACT", options: pdf417.Options{Compact: true}},
		{name: "dimensions", payload: "PDF417 DIMENSIONS", options: pdf417.Options{
			MinRows: 4, MaxRows: 20, MinColumns: 2, MaxColumns: 8,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			symbol, err := pdf417.Encode([]byte(test.payload), test.options)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			if symbol.Compaction() != test.options.Compaction || symbol.Compact() != test.options.Compact {
				t.Fatalf("metadata = (%v, %t)", symbol.Compaction(), symbol.Compact())
			}
			raster, err := render.Image(symbol.Logical(), render.Options{Scale: 3})
			if err != nil {
				t.Fatalf("render.Image() error = %v", err)
			}
			decoded, err := imagedecode.Decode(context.Background(), raster, imagedecode.Options{
				Formats: []barcode.Format{barcode.PDF417},
			})
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if got := string(decoded.Payload()); got != test.payload {
				t.Fatalf("payload = %q, want %q", got, test.payload)
			}
		})
	}
}

func TestEncodeRoundTripsThroughImageDecoder(t *testing.T) {
	symbol, err := pdf417.Encode([]byte("PDF417-INTEROP"), pdf417.Options{})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	raster, err := render.Image(symbol.Logical(), render.Options{Scale: 3})
	if err != nil {
		t.Fatalf("render.Image() error = %v", err)
	}
	decoded, err := imagedecode.Decode(context.Background(), raster, imagedecode.Options{
		Formats: []barcode.Format{barcode.PDF417},
	})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got := string(decoded.Payload()); got != "PDF417-INTEROP" {
		t.Fatalf("payload = %q", got)
	}
}

func TestEncodeRejectsUnsafeOptions(t *testing.T) {
	for _, test := range []struct {
		payload []byte
		options pdf417.Options
	}{
		{},
		{payload: []byte("A"), options: pdf417.Options{QuietZone: -1}},
		{payload: []byte("A"), options: pdf417.Options{QuietZone: 257}},
		{payload: []byte("A"), options: pdf417.Options{Compaction: pdf417.Compaction(99)}},
		{payload: []byte("A"), options: pdf417.Options{ErrorCorrection: pdf417.ErrorCorrection(99)}},
		{payload: []byte("A"), options: pdf417.Options{MinRows: 10, MaxRows: 5}},
		{payload: []byte("A"), options: pdf417.Options{MinColumns: 10, MaxColumns: 5}},
		{payload: []byte("A"), options: pdf417.Options{MinRows: -1}},
		{payload: []byte("A"), options: pdf417.Options{MaxRows: 91}},
		{payload: []byte("A"), options: pdf417.Options{MinColumns: -1}},
		{payload: []byte("A"), options: pdf417.Options{MaxColumns: 31}},
		{payload: []byte("A"), options: pdf417.Options{ECI: -1}},
		{payload: []byte("A"), options: pdf417.Options{ECI: 811_800}},
		{payload: []byte("12A"), options: pdf417.Options{Compaction: pdf417.Numeric}},
		{payload: []byte{0x80}, options: pdf417.Options{Compaction: pdf417.Text}},
		{payload: make([]byte, 2_000), options: pdf417.Options{Compaction: pdf417.Byte}},
		{payload: []byte("A"), options: pdf417.Options{Macro: &pdf417.Macro{FileID: ""}}},
		{payload: []byte("A"), options: pdf417.Options{Macro: &pdf417.Macro{SegmentIndex: -1, FileID: "001"}}},
		{payload: []byte("A"), options: pdf417.Options{Macro: &pdf417.Macro{SegmentIndex: 99_999, FileID: "001"}}},
		{payload: []byte("A"), options: pdf417.Options{Macro: &pdf417.Macro{FileID: "01"}}},
		{payload: []byte("A"), options: pdf417.Options{Macro: &pdf417.Macro{FileID: "ABC"}}},
		{payload: []byte("A"), options: pdf417.Options{Macro: &pdf417.Macro{FileID: "900"}}},
		{payload: make([]byte, 4097)},
	} {
		if _, err := pdf417.Encode(test.payload, test.options); !errors.Is(err, pdf417.ErrInvalidInput) {
			t.Fatalf("Encode(%q, %+v) error = %v", test.payload, test.options, err)
		}
	}
}

func TestEncodeRejectsNegativeMacroOptionalNumbers(t *testing.T) {
	negative := -1
	negative64 := int64(-1)
	for _, macro := range []*pdf417.Macro{
		{FileID: "001", SegmentCount: &negative},
		{FileID: "001", Timestamp: &negative64},
		{FileID: "001", FileSize: &negative64},
		{FileID: "001", Checksum: &negative},
	} {
		if _, err := pdf417.Encode([]byte("A"), pdf417.Options{Macro: macro}); !errors.Is(err, pdf417.ErrInvalidInput) {
			t.Fatalf("Encode(%+v) error = %v", macro, err)
		}
	}

	symbol, err := pdf417.Encode([]byte("A"), pdf417.Options{Macro: &pdf417.Macro{FileID: "001"}})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	macro, ok := symbol.Macro()
	if !ok || macro.SegmentCount != nil || macro.Timestamp != nil || macro.FileSize != nil || macro.Checksum != nil {
		t.Fatalf("Macro() = (%+v, %t)", macro, ok)
	}
}

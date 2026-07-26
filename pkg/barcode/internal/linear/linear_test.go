package linear

import (
	"errors"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
)

type failingWriter struct{}

func (failingWriter) EncodeWithoutHint(string, gozxing.BarcodeFormat, int, int) (*gozxing.BitMatrix, error) {
	return nil, errors.New("encode failed")
}

func (failingWriter) Encode(string, gozxing.BarcodeFormat, int, int, map[gozxing.EncodeHintType]interface{}) (*gozxing.BitMatrix, error) {
	return nil, errors.New("encode failed")
}

type trailingLightWriter struct{}

func (trailingLightWriter) EncodeWithoutHint(string, gozxing.BarcodeFormat, int, int) (*gozxing.BitMatrix, error) {
	return nil, errors.New("unused")
}

func (trailingLightWriter) Encode(string, gozxing.BarcodeFormat, int, int, map[gozxing.EncodeHintType]interface{}) (*gozxing.BitMatrix, error) {
	matrix, err := gozxing.NewBitMatrix(3, 1)
	if err != nil {
		return nil, err
	}
	matrix.Set(0, 0)

	return matrix, nil
}

func TestEncodeBuildsExactAlternatingRuns(t *testing.T) {
	bars, err := Encode(oned.NewCode128Writer(), gozxing.BarcodeFormat_CODE_128, "1234", 10, 4, 5, nil)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	runs := bars.Runs()
	if bars.Height() != 10 || runs[0].Dark || runs[0].Width != 4 ||
		runs[len(runs)-1].Dark || runs[len(runs)-1].Width < 5 {
		t.Fatalf("bars = height %d, first %+v, last %+v", bars.Height(), runs[0], runs[len(runs)-1])
	}
	for index := 1; index < len(runs); index++ {
		if runs[index-1].Dark == runs[index].Dark {
			t.Fatalf("runs %d and %d do not alternate", index-1, index)
		}
	}
}

func TestEncodePropagatesWriterAndBarValidationErrors(t *testing.T) {
	if _, err := Encode(failingWriter{}, gozxing.BarcodeFormat_CODE_128, "A", 1, 1, 1, nil); err == nil {
		t.Fatal("Encode(failing writer) error = nil")
	}
	if _, err := Encode(oned.NewCode128Writer(), gozxing.BarcodeFormat_CODE_128, "A", 0, 1, 1, nil); err == nil {
		t.Fatal("Encode(zero height) error = nil")
	}
}

func TestEncodeMergesTrailingLightRunWithQuietZone(t *testing.T) {
	bars, err := Encode(trailingLightWriter{}, gozxing.BarcodeFormat_CODE_128, "A", 1, 2, 3, nil)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	runs := bars.Runs()
	last := runs[len(runs)-1]
	if last.Dark || last.Width != 5 {
		t.Fatalf("last run = %+v, want five light modules", last)
	}
}

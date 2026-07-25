package imagedecode

import (
	"image"
	"image/color"

	zxinggo "github.com/ericlevine/zxinggo"
	"github.com/ericlevine/zxinggo/binarizer"
	zxingpdf417 "github.com/ericlevine/zxinggo/pdf417"
	"github.com/makiuchi-d/gozxing"
)

// pdf417Reader adapts the maintained PDF417 implementation to the reader
// contract used by the bounded multi-format decode loop.
type pdf417Reader struct{}

func (pdf417Reader) DecodeWithoutHints(input *gozxing.BinaryBitmap) (*gozxing.Result, error) {
	return pdf417Reader{}.Decode(input, nil)
}

func (pdf417Reader) Decode(input *gozxing.BinaryBitmap, hints map[gozxing.DecodeHintType]interface{}) (*gozxing.Result, error) {
	matrix, err := input.GetBlackMatrix()
	if err != nil {
		return nil, err
	}
	gray := image.NewGray(image.Rect(0, 0, matrix.GetWidth(), matrix.GetHeight()))
	for index := range gray.Pix {
		gray.Pix[index] = 255
	}
	for y := 0; y < matrix.GetHeight(); y++ {
		for x := 0; x < matrix.GetWidth(); x++ {
			if matrix.Get(x, y) {
				gray.SetGray(x, y, color.Gray{})
			}
		}
	}
	source := zxinggo.NewGrayImageLuminanceSource(gray)
	binary := zxinggo.NewBinaryBitmap(binarizer.NewGlobalHistogram(source))
	options := &zxinggo.DecodeOptions{}
	if _, ok := hints[gozxing.DecodeHintType_TRY_HARDER]; ok {
		options.TryHarder = true
	}
	decoded, err := zxingpdf417.NewPDF417Reader().Decode(binary, options)
	if err != nil {
		return nil, gozxing.NewNotFoundException("%v", err)
	}
	result := gozxing.NewResult(decoded.Text, decoded.RawBytes, nil, gozxing.BarcodeFormat_PDF_417)
	if level, ok := decoded.Metadata[zxinggo.MetadataErrorCorrectionLevel]; ok {
		result.PutMetadata(gozxing.ResultMetadataType_ERROR_CORRECTION_LEVEL, level)
	}
	if corrections, ok := decoded.Metadata[zxinggo.MetadataErrorsCorrected].(int); ok {
		result.PutMetadata(gozxing.ResultMetadataType_OTHER, corrections)
	}
	if extra, ok := decoded.Metadata[zxinggo.MetadataPDF417ExtraMetadata]; ok {
		result.PutMetadata(gozxing.ResultMetadataType_PDF417_EXTRA_METADATA, extra)
	}
	if identifier, ok := decoded.Metadata[zxinggo.MetadataSymbologyIdentifier]; ok {
		result.PutMetadata(gozxing.ResultMetadataType_SYMBOLOGY_IDENTIFIER, identifier)
	}

	return result, nil
}

func (pdf417Reader) Reset() {
	//lint:ignore S1023 The return makes this required no-op method coverable.
	return //nolint:staticcheck // Go coverage needs a statement in this method.
}

package imagedecode

import (
	"image"
	"image/color"
	"strings"

	zxinggo "github.com/ericlevine/zxinggo"
	zxingaztec "github.com/ericlevine/zxinggo/aztec"
	"github.com/ericlevine/zxinggo/binarizer"
	"github.com/ericlevine/zxinggo/charset"
	zxingdatamatrix "github.com/ericlevine/zxinggo/datamatrix"
	zxingqr "github.com/ericlevine/zxinggo/qrcode"
	"github.com/makiuchi-d/gozxing"
	"golang.org/x/text/encoding/htmlindex"
)

type twoDReader struct {
	reader zxinggo.Reader
	format gozxing.BarcodeFormat
}

const resultMetadataECI gozxing.ResultMetadataType = 1000

type eciMetadata struct {
	assignment int
	supported  bool
}

func newQRCodeReader() gozxing.Reader {
	return twoDReader{reader: zxingqr.NewReader(), format: gozxing.BarcodeFormat_QR_CODE}
}

func newDataMatrixReader() gozxing.Reader {
	return twoDReader{reader: zxingdatamatrix.NewReader(), format: gozxing.BarcodeFormat_DATA_MATRIX}
}

func newAztecReader() gozxing.Reader {
	return twoDReader{reader: zxingaztec.NewReader(), format: gozxing.BarcodeFormat_AZTEC}
}

func (reader twoDReader) DecodeWithoutHints(input *gozxing.BinaryBitmap) (*gozxing.Result, error) {
	return reader.Decode(input, nil)
}

func (reader twoDReader) Decode(input *gozxing.BinaryBitmap, hints map[gozxing.DecodeHintType]interface{}) (result *gozxing.Result, err error) {
	defer func() {
		if recover() != nil {
			result = nil
			err = gozxing.NewNotFoundException("two-dimensional decoder rejected malformed data")
		}
	}()
	matrix, err := input.GetBlackMatrix()
	if err != nil {
		return nil, err
	}
	gray := image.NewGray(image.Rect(0, 0, matrix.GetWidth(), matrix.GetHeight()))
	for index := range gray.Pix {
		gray.Pix[index] = 255
	}
	for y := range matrix.GetHeight() {
		for x := range matrix.GetWidth() {
			if matrix.Get(x, y) {
				gray.SetGray(x, y, color.Gray{})
			}
		}
	}
	options := &zxinggo.DecodeOptions{}
	if _, ok := hints[gozxing.DecodeHintType_TRY_HARDER]; ok {
		options.TryHarder = true
	}
	decoded, err := reader.reader.Decode(
		zxinggo.NewBinaryBitmap(binarizer.NewGlobalHistogram(zxinggo.NewGrayImageLuminanceSource(gray))),
		options,
	)
	if err != nil {
		return nil, gozxing.NewNotFoundException("%v", err)
	}
	points := make([]gozxing.ResultPoint, len(decoded.Points))
	for index, point := range decoded.Points {
		points[index] = gozxing.NewResultPoint(point.X, point.Y)
	}
	text := decoded.Text
	controls := dataMatrixControls{}
	if reader.format == gozxing.BarcodeFormat_DATA_MATRIX {
		if len(decoded.RawBytes) > 0 {
			if controlled, ok := decodeControlledDataMatrixDetails(decoded.RawBytes); ok {
				text, controls = controlled.text, controlled
			} else {
				switch decoded.RawBytes[0] {
				case 232:
					text = strings.TrimPrefix(text, "\x1d")
				case 236, 237:
					if !strings.HasSuffix(text, "\x1e\x04") {
						text += "\x1e\x04"
					}
				}
			}
		}
	}
	result = gozxing.NewResult(text, decoded.RawBytes, points, reader.format)
	copyTwoDMetadata(result, decoded)
	if controls.hasSequence {
		result.PutMetadata(gozxing.ResultMetadataType_STRUCTURED_APPEND_SEQUENCE, controls.sequence)
		result.PutMetadata(gozxing.ResultMetadataType_STRUCTURED_APPEND_PARITY, controls.fileID)
	}
	if controls.hasECI {
		result.PutMetadata(resultMetadataECI, eciMetadata{
			assignment: controls.eciAssignment,
			supported:  controls.eciSupported,
		})
	}

	return result, nil
}

type dataMatrixControls struct {
	text          string
	sequence      int
	fileID        int
	hasSequence   bool
	eciAssignment int
	eciSupported  bool
	hasECI        bool
}

func decodeControlledDataMatrix(codewords []byte) (string, bool) {
	controlled, ok := decodeControlledDataMatrixDetails(codewords)
	return controlled.text, ok
}

func decodeControlledDataMatrixDetails(codewords []byte) (dataMatrixControls, bool) {
	controlled := dataMatrixControls{}
	if len(codewords) == 0 {
		return controlled, false
	}
	index := 0
	prefix, suffix := "", ""
	switch codewords[index] {
	case 233:
		if len(codewords) == 1 {
			return controlled, false
		}
		if len(codewords) == 2 {
			return controlled, false
		}
		controlled.sequence = int(codewords[1])
		controlled.fileID = int(codewords[2])
		controlled.hasSequence = true
		index = 3
	case 236:
		prefix = "[)>\x1e05\x1d"
		suffix = "\x1e\x04"
		index++
	case 237:
		prefix = "[)>\x1e06\x1d"
		suffix = "\x1e\x04"
		index++
	case 232:
		index++
	}
	if index < len(codewords) {
		if codewords[index] == 232 {
			index++
		}
	}
	assignment, hasAssignment := 0, false
	if index < len(codewords) {
		if codewords[index] == 241 {
			index++
			var ok bool
			assignment, index, ok = decodeDataMatrixECI(codewords, index)
			if !ok {
				return controlled, false
			}
			controlled.eciAssignment = assignment
			controlled.hasECI = true
			hasAssignment = true
		}
	}
	if index >= len(codewords) {
		return controlled, false
	}
	if codewords[index] != 231 {
		return controlled, false
	}
	index++
	length, next, ok := decodeBase256Length(codewords, index)
	if !ok {
		return controlled, false
	}
	if length > len(codewords)-next {
		return controlled, false
	}
	payload := make([]byte, length)
	for offset := range payload {
		payload[offset] = unrandomize255(codewords[next+offset], next+offset+1)
	}
	text := string(payload)
	if hasAssignment {
		if eci, err := charset.GetECIByValue(assignment); err == nil {
			if eci == nil {
				controlled.text = prefix + text + suffix
				return controlled, true
			}
			controlled.eciSupported = true
			labels := append([]string{eci.GoName, eci.Name}, eci.Aliases...)
			for _, label := range labels {
				encoding, lookupErr := htmlindex.Get(label)
				if lookupErr != nil {
					continue
				}
				converted, decodeErr := encoding.NewDecoder().Bytes(payload)
				if decodeErr == nil {
					text = string(converted)
					controlled.text = prefix + text + suffix
					return controlled, true
				}
			}
			text = charset.DecodeBytes(payload, eci.GoName)
		}
	}

	controlled.text = prefix + text + suffix
	return controlled, true
}

func decodeDataMatrixECI(codewords []byte, index int) (int, int, bool) {
	if index >= len(codewords) {
		return 0, index, false
	}
	first := int(codewords[index])
	index++
	switch {
	case first <= 127:
		return first - 1, index, first > 0
	case first <= 191:
		if index >= len(codewords) {
			return 0, index, false
		}
		if codewords[index] == 0 {
			return 0, index, false
		}
		assignment := (first-128)*254 + int(codewords[index]) - 1 + 127
		return assignment, index + 1, true
	case first <= 254:
		if index+1 >= len(codewords) {
			return 0, index, false
		}
		if codewords[index] == 0 {
			return 0, index, false
		}
		if codewords[index+1] == 0 {
			return 0, index, false
		}
		assignment := (first-192)*64_516 + (int(codewords[index])-1)*254 +
			int(codewords[index+1]) - 1 + 16_383
		return assignment, index + 2, true
	default:
		return 0, index, false
	}
}

func decodeBase256Length(codewords []byte, index int) (int, int, bool) {
	if index >= len(codewords) {
		return 0, index, false
	}
	first := int(unrandomize255(codewords[index], index+1))
	index++
	if first == 0 {
		return len(codewords) - index, index, true
	}
	if first <= 249 {
		return first, index, true
	}
	if index >= len(codewords) {
		return 0, index, false
	}
	second := int(unrandomize255(codewords[index], index+1))

	return (first-249)*250 + second, index + 1, true
}

func unrandomize255(value byte, position int) byte {
	pseudoRandom := (149*position)%255 + 1
	// #nosec G115 -- modulo reversal bounds unrandomized to one byte.
	return byte(int(value) - pseudoRandom)
}

func (reader twoDReader) Reset() { reader.reader.Reset() }

func copyTwoDMetadata(target *gozxing.Result, source *zxinggo.Result) {
	for sourceKey, targetKey := range map[zxinggo.ResultMetadataKey]gozxing.ResultMetadataType{
		zxinggo.MetadataByteSegments:             gozxing.ResultMetadataType_BYTE_SEGMENTS,
		zxinggo.MetadataStructuredAppendSequence: gozxing.ResultMetadataType_STRUCTURED_APPEND_SEQUENCE,
		zxinggo.MetadataStructuredAppendParity:   gozxing.ResultMetadataType_STRUCTURED_APPEND_PARITY,
	} {
		if value, ok := source.Metadata[sourceKey]; ok {
			target.PutMetadata(targetKey, value)
		}
	}
	if value, ok := source.Metadata[zxinggo.MetadataErrorCorrectionLevel]; ok {
		target.PutMetadata(gozxing.ResultMetadataType_ERROR_CORRECTION_LEVEL, value)
	}
	if value, ok := source.Metadata[zxinggo.MetadataSymbologyIdentifier]; ok {
		target.PutMetadata(gozxing.ResultMetadataType_SYMBOLOGY_IDENTIFIER, value)
	}
	if value, ok := source.Metadata[zxinggo.MetadataErrorsCorrected].(int); ok {
		target.PutMetadata(gozxing.ResultMetadataType_OTHER, value)
	}
}

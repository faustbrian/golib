// Package imagedecode provides additive, bounded image barcode decoding.
package imagedecode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  //nolint:revive // Register GIF for the generic image decoder.
	_ "image/jpeg" //nolint:revive // Register JPEG for the generic image decoder.
	_ "image/png"  //nolint:revive // Register PNG for the generic image decoder.
	"io"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
)

const (
	defaultMaxDimension    = 8192
	defaultMaxPixels       = 16 * 1024 * 1024
	defaultMaxCandidates   = 64
	defaultMaxPayload      = 4096
	defaultMaxRotations    = 4
	defaultMaxMemoryBytes  = 64 * 1024 * 1024
	defaultMaxEncodedBytes = 16 * 1024 * 1024
)

var (
	// ErrLimitExceeded reports a caller-provided decode budget violation.
	ErrLimitExceeded = errors.New("imagedecode: limit exceeded")
	// ErrUnsupportedFormat reports a format absent from the image decoder.
	ErrUnsupportedFormat = errors.New("imagedecode: unsupported format")
	// ErrInvalidImage reports an image that cannot be converted for decoding.
	ErrInvalidImage = errors.New("imagedecode: invalid image")
	// ErrNotFound reports that no requested barcode was detected.
	ErrNotFound = errors.New("imagedecode: barcode not found")
)

// Limits are enforced before image conversion and between decoder attempts.
// MaxRotations includes the original orientation and is limited to four.
type Limits struct {
	MaxWidth        int
	MaxHeight       int
	MaxPixels       int
	MaxCandidates   int
	MaxPayloadBytes int
	MaxRotations    int
	MaxMemoryBytes  int
	MaxEncodedBytes int
	MaxCorrections  int
	MaxDuration     time.Duration
}

// DecodeEncoded bounds and decodes a PNG, JPEG, or GIF stream before scanning
// it for a barcode. The compressed byte and decoded image limits are enforced
// before the full image allocation.
func DecodeEncoded(ctx context.Context, input io.Reader, options Options) (barcode.DecodeResult, error) {
	if err := ctx.Err(); err != nil {
		return barcode.DecodeResult{}, err
	}
	if input == nil {
		return barcode.DecodeResult{}, ErrInvalidImage
	}
	limits, err := normalizeLimits(options.Limits)
	if err != nil {
		return barcode.DecodeResult{}, err
	}
	if limits.MaxDuration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limits.MaxDuration)
		defer cancel()
	}
	encoded, err := io.ReadAll(io.LimitReader(input, int64(limits.MaxEncodedBytes)+1))
	if err != nil {
		return barcode.DecodeResult{}, fmt.Errorf("%w: %w", ErrInvalidImage, err)
	}
	if len(encoded) > limits.MaxEncodedBytes {
		return barcode.DecodeResult{}, ErrLimitExceeded
	}
	if err := ctx.Err(); err != nil {
		return barcode.DecodeResult{}, err
	}
	configuration, _, err := image.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		return barcode.DecodeResult{}, fmt.Errorf("%w: %w", ErrInvalidImage, err)
	}
	if err := validateImageSize(configuration.Width, configuration.Height, limits); err != nil {
		return barcode.DecodeResult{}, err
	}
	decoded, _, err := image.Decode(bytes.NewReader(encoded))
	if err != nil {
		return barcode.DecodeResult{}, fmt.Errorf("%w: %w", ErrInvalidImage, err)
	}
	if err := ctx.Err(); err != nil {
		return barcode.DecodeResult{}, err
	}
	options.Limits = limits

	return Decode(ctx, decoded, options)
}

// Options controls requested formats, scan behavior, and resource limits.
type Options struct {
	Formats              []barcode.Format
	AllowInverted        bool
	TryHarder            bool
	AssumeCode39Checksum bool
	Limits               Limits
}

type candidate struct {
	format barcode.Format
	reader gozxing.Reader
	gs1    bool
}

var defaultFormats = [...]barcode.Format{
	barcode.QRCode, barcode.Code128, barcode.GS1128, barcode.Code39,
	barcode.Code93, barcode.EAN8, barcode.EAN13, barcode.UPCA,
	barcode.UPCE, barcode.ITF, barcode.Codabar, barcode.DataMatrix,
	barcode.Aztec, barcode.PDF417,
}

// Decode returns the first requested format found within the configured
// bounds. Decoded content is data only and is never executed or followed.
func Decode(ctx context.Context, input image.Image, options Options) (barcode.DecodeResult, error) {
	started := time.Now()
	if err := ctx.Err(); err != nil {
		return barcode.DecodeResult{}, err
	}
	if input == nil {
		return barcode.DecodeResult{}, ErrInvalidImage
	}
	limits, err := normalizeLimits(options.Limits)
	if err != nil {
		return barcode.DecodeResult{}, err
	}
	if limits.MaxDuration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limits.MaxDuration)
		defer cancel()
		if err := contextError(ctx, started, limits.MaxDuration); err != nil {
			return barcode.DecodeResult{}, err
		}
	}
	bounds := input.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if err := validateImageSize(width, height, limits); err != nil {
		return barcode.DecodeResult{}, err
	}
	inversionAttempts := 1
	if options.AllowInverted {
		inversionAttempts = 2
	}
	candidateCount := len(options.Formats)
	if candidateCount == 0 {
		candidateCount = len(defaultFormats)
	}
	attemptsPerCandidate := limits.MaxRotations * inversionAttempts
	if candidateCount > limits.MaxCandidates/attemptsPerCandidate {
		return barcode.DecodeResult{}, ErrLimitExceeded
	}
	candidates, err := candidatesFor(options.Formats, options.AssumeCode39Checksum)
	if err != nil {
		return barcode.DecodeResult{}, err
	}

	source := gozxing.NewLuminanceSourceFromImage(input)
	bitmap, _ := gozxing.NewBinaryBitmap(gozxing.NewHybridBinarizer(source))
	var inverted *gozxing.BinaryBitmap
	if options.AllowInverted {
		inverted, _ = gozxing.NewBinaryBitmap(gozxing.NewHybridBinarizer(source.Invert()))
	}
	hints := map[gozxing.DecodeHintType]interface{}{}
	if options.AllowInverted {
		hints[gozxing.DecodeHintType_ALSO_INVERTED] = true
	}
	if options.TryHarder {
		hints[gozxing.DecodeHintType_TRY_HARDER] = true
	}

	current := bitmap
	currentInverted := inverted
	for rotation := 0; rotation < limits.MaxRotations; rotation++ {
		for _, item := range candidates {
			variants := []*gozxing.BinaryBitmap{current}
			if currentInverted != nil {
				variants = append(variants, currentInverted)
			}
			for _, variant := range variants {
				if err := contextError(ctx, started, limits.MaxDuration); err != nil {
					return barcode.DecodeResult{}, err
				}
				attemptHints := cloneHints(hints)
				if item.gs1 {
					attemptHints[gozxing.DecodeHintType_ASSUME_GS1] = true
				}
				result, decodeErr := item.reader.Decode(variant, attemptHints)
				item.reader.Reset()
				if decodeErr != nil {
					continue
				}
				if corrections := correctionsFor(result); limits.MaxCorrections > 0 && corrections > limits.MaxCorrections {
					return barcode.DecodeResult{}, ErrLimitExceeded
				}
				if len(result.GetText()) > limits.MaxPayloadBytes || len(result.GetRawBytes()) > limits.MaxPayloadBytes {
					return barcode.DecodeResult{}, ErrLimitExceeded
				}
				payload := result.GetText()
				if item.gs1 && strings.HasPrefix(payload, "]C1") {
					payload = strings.TrimPrefix(payload, "]C1")
					result.PutMetadata(gozxing.ResultMetadataType_SYMBOLOGY_IDENTIFIER, "]C1")
				}
				// Preserve the requested semantic format. Some symbologies share a
				// physical reader, notably ITF and GS1 ITF-14, so the reader's broad
				// format cannot always represent the caller's requested contract.
				format := item.format
				orientation := orientationFor(result, rotation)
				decoded, _ := barcode.NewDecodeResult(barcode.DecodeResultOptions{
					Format:      format,
					Payload:     []byte(payload),
					RawBytes:    result.GetRawBytes(),
					Orientation: orientation,
					Checksum:    checksumStatus(format),
					Diagnostics: metadataDiagnostics(result),
				})
				return decoded, nil
			}
		}
		if rotation+1 < limits.MaxRotations {
			current, _ = current.RotateCounterClockwise()
			if currentInverted != nil {
				currentInverted, _ = currentInverted.RotateCounterClockwise()
			}
		}
	}
	return barcode.DecodeResult{}, decodeFailure(ctx, started, limits.MaxDuration)
}

func validateImageSize(width, height int, limits Limits) error {
	if width <= 0 || height <= 0 || width > limits.MaxWidth || height > limits.MaxHeight ||
		width > int(^uint(0)>>1)/height || width*height > limits.MaxPixels ||
		width*height > limits.MaxMemoryBytes/4 {
		return ErrLimitExceeded
	}

	return nil
}

func decodeFailure(ctx context.Context, started time.Time, maximum time.Duration) error {
	if err := contextError(ctx, started, maximum); err != nil {
		return err
	}

	return ErrNotFound
}

func contextError(ctx context.Context, started time.Time, maximum time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if maximum > 0 && time.Since(started) >= maximum {
		return context.DeadlineExceeded
	}

	return nil
}

func normalizeLimits(limits Limits) (Limits, error) {
	if limits.MaxWidth < 0 || limits.MaxHeight < 0 || limits.MaxPixels < 0 ||
		limits.MaxCandidates < 0 || limits.MaxPayloadBytes < 0 || limits.MaxRotations < 0 ||
		limits.MaxMemoryBytes < 0 || limits.MaxEncodedBytes < 0 ||
		limits.MaxCorrections < 0 || limits.MaxDuration < 0 {
		return Limits{}, ErrLimitExceeded
	}
	if limits.MaxWidth == 0 {
		limits.MaxWidth = defaultMaxDimension
	}
	if limits.MaxHeight == 0 {
		limits.MaxHeight = defaultMaxDimension
	}
	if limits.MaxPixels == 0 {
		limits.MaxPixels = defaultMaxPixels
	}
	if limits.MaxCandidates == 0 {
		limits.MaxCandidates = defaultMaxCandidates
	}
	if limits.MaxPayloadBytes == 0 {
		limits.MaxPayloadBytes = defaultMaxPayload
	}
	if limits.MaxRotations == 0 {
		limits.MaxRotations = defaultMaxRotations
	}
	if limits.MaxMemoryBytes == 0 {
		limits.MaxMemoryBytes = defaultMaxMemoryBytes
	}
	if limits.MaxEncodedBytes == 0 {
		limits.MaxEncodedBytes = defaultMaxEncodedBytes
	}
	if limits.MaxRotations > 4 {
		return Limits{}, ErrLimitExceeded
	}

	return limits, nil
}

func candidatesFor(formats []barcode.Format, assumeCode39Checksum bool) ([]candidate, error) {
	if len(formats) == 0 {
		formats = defaultFormats[:]
	}
	result := make([]candidate, 0, len(formats))
	for _, format := range formats {
		var item candidate
		switch format {
		case barcode.QRCode:
			item = candidate{format: format, reader: newQRCodeReader()}
		case barcode.DataMatrix:
			item = candidate{format: format, reader: newDataMatrixReader()}
		case barcode.Aztec:
			item = candidate{format: format, reader: newAztecReader()}
		case barcode.PDF417:
			item = candidate{format: format, reader: pdf417Reader{}}
		case barcode.Code128:
			item = candidate{format: format, reader: oned.NewCode128Reader()}
		case barcode.GS1128:
			item = candidate{format: format, reader: oned.NewCode128Reader(), gs1: true}
		case barcode.Code39:
			item = candidate{format: format, reader: oned.NewCode39ReaderWithFlags(assumeCode39Checksum, true)}
		case barcode.Code93:
			item = candidate{format: format, reader: oned.NewCode93Reader()}
		case barcode.EAN8:
			item = candidate{format: format, reader: oned.NewEAN8Reader()}
		case barcode.EAN13:
			item = candidate{format: format, reader: oned.NewEAN13Reader()}
		case barcode.UPCA:
			item = candidate{format: format, reader: oned.NewUPCAReader()}
		case barcode.UPCE:
			item = candidate{format: format, reader: oned.NewUPCEReader()}
		case barcode.ITF, barcode.ITF14:
			item = candidate{format: format, reader: oned.NewITFReader()}
		case barcode.Codabar:
			item = candidate{format: format, reader: oned.NewCodaBarReader()}
		default:
			return nil, fmt.Errorf("%w: %q", ErrUnsupportedFormat, format)
		}
		result = append(result, item)
	}

	return result, nil
}

func cloneHints(source map[gozxing.DecodeHintType]interface{}) map[gozxing.DecodeHintType]interface{} {
	result := make(map[gozxing.DecodeHintType]interface{}, len(source)+1)
	for key, value := range source {
		result[key] = value
	}

	return result
}

func orientationFor(result *gozxing.Result, rotation int) barcode.Orientation {
	degrees := (360 - rotation*90) % 360
	if value, ok := result.GetResultMetadata()[gozxing.ResultMetadataType_ORIENTATION].(int); ok {
		degrees = (degrees + value) % 360
	} else if result.GetBarcodeFormat() == gozxing.BarcodeFormat_QR_CODE {
		degrees = (degrees + qrOrientation(result.GetResultPoints())) % 360
	}
	switch degrees {
	case 90:
		return barcode.Orientation90
	case 180:
		return barcode.Orientation180
	case 270:
		return barcode.Orientation270
	default:
		return barcode.Orientation0
	}
}

func qrOrientation(points []gozxing.ResultPoint) int {
	if len(points) < 2 {
		return 0
	}
	dx := points[1].GetX() - points[0].GetX()
	dy := points[1].GetY() - points[0].GetY()
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		if points[1].GetX() > points[0].GetX() {
			return 270
		}

		return 90
	}
	if points[1].GetY() > points[0].GetY() {
		return 180
	}

	return 0
}

func checksumStatus(format barcode.Format) barcode.ChecksumStatus {
	switch format {
	case barcode.QRCode, barcode.Codabar:
		return barcode.ChecksumNotApplicable
	case barcode.Code39:
		return barcode.ChecksumUnknown
	case barcode.Code128, barcode.GS1128, barcode.Code93, barcode.EAN8,
		barcode.EAN13, barcode.UPCA, barcode.UPCE, barcode.ITF, barcode.ITF14,
		barcode.DataMatrix, barcode.PDF417, barcode.Aztec:
		return barcode.ChecksumValid
	default:
		return barcode.ChecksumValid
	}
}

func metadataDiagnostics(result *gozxing.Result) []string {
	metadata := result.GetResultMetadata()
	diagnostics := make([]string, 0, 3)
	for _, key := range []gozxing.ResultMetadataType{
		gozxing.ResultMetadataType_ERROR_CORRECTION_LEVEL,
		gozxing.ResultMetadataType_UPC_EAN_EXTENSION,
		gozxing.ResultMetadataType_SYMBOLOGY_IDENTIFIER,
		gozxing.ResultMetadataType_STRUCTURED_APPEND_SEQUENCE,
		gozxing.ResultMetadataType_STRUCTURED_APPEND_PARITY,
		gozxing.ResultMetadataType_PDF417_EXTRA_METADATA,
	} {
		if value, ok := metadata[key]; ok {
			diagnostics = append(diagnostics, key.String()+"="+fmt.Sprint(value))
		}
	}
	if corrections := correctionsFor(result); corrections > 0 {
		diagnostics = append(diagnostics, fmt.Sprintf("ERRORS_CORRECTED=%d", corrections))
	}
	if eci, ok := metadata[resultMetadataECI].(eciMetadata); ok {
		diagnostics = append(diagnostics, fmt.Sprintf("ECI_ASSIGNMENT=%d", eci.assignment))
		if !eci.supported {
			diagnostics = append(diagnostics, fmt.Sprintf("ECI_UNSUPPORTED=%d", eci.assignment))
		}
	}

	return diagnostics
}

func correctionsFor(result *gozxing.Result) int {
	corrections, _ := result.GetResultMetadata()[gozxing.ResultMetadataType_OTHER].(int)

	return corrections
}

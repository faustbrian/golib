// Package qr encodes ISO/IEC 18004:2024 QR Code logical symbols.
package qr

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/gs1"
	unixcoding "github.com/unixdj/qr/coding"
	unixsplit "github.com/unixdj/qr/split"
)

const (
	defaultQuietZone = 4
	maxPayloadBytes  = 4096
)

var (
	// ErrInvalidInput reports invalid QR payloads and option combinations.
	ErrInvalidInput = errors.New("qr: invalid input")
	// ErrUnsupported reports a valid QR feature unavailable to the encoder.
	ErrUnsupported = errors.New("qr: unsupported option")
)

// Mode selects a QR data encoding mode.
type Mode uint8

const (
	// Auto chooses an optimized mix of supported QR modes.
	Auto Mode = iota
	// Numeric forces numeric mode.
	Numeric
	// Alphanumeric forces QR alphanumeric mode.
	Alphanumeric
	// Byte forces byte mode.
	Byte
	// Kanji forces QR Kanji mode.
	Kanji
)

// ErrorCorrection selects a QR Reed-Solomon correction level.
type ErrorCorrection uint8

const (
	// DefaultErrorCorrection selects Medium correction.
	DefaultErrorCorrection ErrorCorrection = iota
	// Low selects about seven percent correction.
	Low
	// Medium selects about fifteen percent correction.
	Medium
	// Quartile selects about twenty-five percent correction.
	Quartile
	// High selects about thirty percent correction.
	High
)

// FNC1Mode selects QR FNC1 interpretation.
type FNC1Mode uint8

const (
	// FNC1None disables FNC1 headers.
	FNC1None FNC1Mode = iota
	// FNC1First selects first-position GS1 FNC1.
	FNC1First
	// FNC1Second selects second-position FNC1.
	FNC1Second
)

// StructuredAppend identifies one symbol in a sequence of 2 through 16 QR
// symbols. Encoding is rejected until this header is supported end to end.
type StructuredAppend struct {
	Index  int
	Total  int
	Parity byte
}

// Options controls QR mode, correction, layout, ECI, FNC1, and sequencing.
type Options struct {
	Mode                     Mode
	ErrorCorrection          ErrorCorrection
	Version                  int
	Mask                     int
	MaskSet                  bool
	QuietZone                int
	ECI                      int
	FNC1                     FNC1Mode
	FNC1ApplicationIndicator byte
	StructuredAppend         *StructuredAppend
}

// Symbol combines the immutable logical matrix with QR-specific metadata.
type Symbol struct {
	logical                  barcode.Symbol
	mode                     Mode
	errorCorrection          ErrorCorrection
	version                  int
	mask                     int
	eci                      int
	fnc1                     FNC1Mode
	fnc1ApplicationIndicator byte
	structuredAppend         *StructuredAppend
}

// Logical returns the immutable format-neutral symbol.
func (symbol Symbol) Logical() barcode.Symbol { return symbol.logical }

// Mode returns the effective QR data mode.
func (symbol Symbol) Mode() Mode { return symbol.mode }

// ErrorCorrection returns the effective correction level.
func (symbol Symbol) ErrorCorrection() ErrorCorrection { return symbol.errorCorrection }

// Version returns the QR version from 1 through 40.
func (symbol Symbol) Version() int { return symbol.version }

// Mask returns the selected mask pattern from 0 through 7.
func (symbol Symbol) Mask() int { return symbol.mask }

// ECI returns the requested ECI assignment, or zero when absent.
func (symbol Symbol) ECI() int { return symbol.eci }

// FNC1 returns the requested FNC1 mode.
func (symbol Symbol) FNC1() FNC1Mode { return symbol.fnc1 }

// FNC1ApplicationIndicator returns the second-position application indicator.
func (symbol Symbol) FNC1ApplicationIndicator() byte {
	return symbol.fnc1ApplicationIndicator
}

// StructuredAppend returns the sequence header when present.
func (symbol Symbol) StructuredAppend() (StructuredAppend, bool) {
	if symbol.structuredAppend == nil {
		return StructuredAppend{}, false
	}

	return *symbol.structuredAppend, true
}

// Encode returns an exact logical QR matrix including its quiet zone.
func Encode(payload []byte, options Options) (Symbol, error) {
	if len(payload) == 0 || len(payload) > maxPayloadBytes || options.Mode > Kanji ||
		options.ErrorCorrection > High || options.Version < 0 || options.Version > 40 ||
		options.QuietZone < 0 || options.FNC1 > FNC1Second {
		return Symbol{}, ErrInvalidInput
	}
	if options.StructuredAppend != nil && (options.StructuredAppend.Total < 2 ||
		options.StructuredAppend.Total > 16 || options.StructuredAppend.Index < 0 ||
		options.StructuredAppend.Index >= options.StructuredAppend.Total) {
		return Symbol{}, ErrInvalidInput
	}
	if options.FNC1 == FNC1First {
		if _, err := gs1.ParseRaw(string(payload), gs1.ParseLimits{MaxInputBytes: maxPayloadBytes}); err != nil {
			return Symbol{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
	}
	maskSet := options.MaskSet || options.Mask != 0
	if maskSet && (options.Mask < 0 || options.Mask > 7) {
		return Symbol{}, ErrInvalidInput
	}
	quietZone := options.QuietZone
	if quietZone == 0 {
		quietZone = defaultQuietZone
	}
	if quietZone < defaultQuietZone || quietZone > 256 {
		return Symbol{}, ErrInvalidInput
	}
	if err := validateMode(payload, options.Mode); err != nil {
		return Symbol{}, err
	}
	if options.Mode == Kanji && options.ECI != 0 && options.ECI != unixsplit.ShiftJISECI {
		return Symbol{}, ErrInvalidInput
	}
	if options.Mode == Auto && options.StructuredAppend == nil {
		return encodeOptimized(payload, options, quietZone, maskSet)
	}

	return encodeExtended(payload, options, quietZone, maskSet)
}

// EncodeStructured splits an oversized automatic-mode payload across two to
// sixteen QR symbols. Payloads that fit in one symbol are returned unchanged.
func EncodeStructured(payload []byte, options Options) ([]Symbol, error) {
	if options.StructuredAppend != nil {
		return nil, ErrInvalidInput
	}
	if symbol, err := Encode(payload, options); err == nil {
		return []Symbol{symbol}, nil
	}
	if len(payload) == 0 || len(payload) > maxPayloadBytes || options.Mode != Auto ||
		options.ErrorCorrection > High || options.Version < 0 || options.Version > 40 ||
		options.QuietZone < 0 || options.FNC1 > FNC1Second {
		return nil, ErrInvalidInput
	}
	quietZone := options.QuietZone
	if quietZone == 0 {
		quietZone = defaultQuietZone
	}
	if quietZone < defaultQuietZone || quietZone > 256 {
		return nil, ErrInvalidInput
	}
	maskSet := options.MaskSet || options.Mask != 0
	if maskSet && (options.Mask < 0 || options.Mask > 7) {
		return nil, ErrInvalidInput
	}
	level := options.ErrorCorrection
	if level == DefaultErrorCorrection {
		level = Medium
	}
	text, charset, eci, err := splitInput(payload, options.ECI)
	if err != nil {
		return nil, err
	}
	var data unixsplit.Data
	switch options.FNC1 {
	case FNC1None:
		data = unixsplit.Text(text, charset, eci)
	case FNC1First:
		if _, parseErr := gs1.ParseRaw(string(payload), gs1.ParseLimits{MaxInputBytes: maxPayloadBytes}); parseErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidInput, parseErr)
		}
		data = unixsplit.FNC1Text(text, charset, eci, -1)
	case FNC1Second:
		data = unixsplit.FNC1Text(text, charset, eci, int(options.FNC1ApplicationIndicator))
	}
	version := options.Version
	if version == 0 {
		version = 1
	}
	var parts [][]unixcoding.Segment
	for ; version <= 40; version++ {
		parts, err = unixsplit.SplitMulti(data, unixcoding.Version(version), codingLevel(level))
		if err == nil || options.Version != 0 {
			break
		}
	}
	if err != nil || len(parts) < 2 || len(parts) > 16 {
		return nil, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	symbols := make([]Symbol, len(parts))
	for index, part := range parts {
		code, mask, _ := encodeSegments(part, version, level, options.Mask, maskSet)
		matrix := logicalCodingMatrix(code, quietZone)
		partPayload := structuredPayload(part)
		logical, _ := barcode.NewSymbol(barcode.SymbolOptions{
			Format: barcode.QRCode, Payload: partPayload, Matrix: matrix,
		})
		headerBytes := []byte(part[0].Text)
		header := &StructuredAppend{
			Index: int(headerBytes[0] >> 4), Total: int(headerBytes[0]&0x0f) + 1,
			Parity: headerBytes[1],
		}
		symbols[index] = Symbol{
			logical: logical, mode: optimizedMode(part), errorCorrection: level,
			version: version, mask: mask, eci: options.ECI, fnc1: options.FNC1,
			fnc1ApplicationIndicator: options.FNC1ApplicationIndicator,
			structuredAppend:         header,
		}
	}

	return symbols, nil
}

func structuredPayload(segments []unixcoding.Segment) []byte {
	length := 0
	for _, segment := range segments {
		if segment.Mode < unixcoding.ECI {
			length += len(segment.Text)
		}
	}
	payload := make([]byte, 0, length)
	for _, segment := range segments {
		if segment.Mode < unixcoding.ECI {
			payload = append(payload, segment.Text...)
		}
	}

	return payload
}

func encodeOptimized(payload []byte, options Options, quietZone int, maskSet bool) (Symbol, error) {
	level := options.ErrorCorrection
	if level == DefaultErrorCorrection {
		level = Medium
	}
	text, charset, eci, err := splitInput(payload, options.ECI)
	if err != nil {
		return Symbol{}, err
	}
	var data unixsplit.Data
	switch options.FNC1 {
	case FNC1None:
		data = unixsplit.Text(text, charset, eci)
	case FNC1First:
		data = unixsplit.FNC1Text(text, charset, eci, -1)
	case FNC1Second:
		data = unixsplit.FNC1Text(text, charset, eci, int(options.FNC1ApplicationIndicator))
	}
	segments, minimumVersion, err := unixsplit.Split(data, codingLevel(level), unixsplit.QR)
	if err != nil {
		return Symbol{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	version := int(minimumVersion)
	if options.Version != 0 {
		if options.Version < version {
			return Symbol{}, ErrInvalidInput
		}
		version = options.Version
	}
	code, mask, _ := encodeSegments(segments, version, level, options.Mask, maskSet)
	matrix := logicalCodingMatrix(code, quietZone)
	logical, _ := barcode.NewSymbol(barcode.SymbolOptions{
		Format: barcode.QRCode, Payload: payload, Matrix: matrix,
	})

	return Symbol{
		logical: logical, mode: optimizedMode(segments), errorCorrection: level,
		version: version, mask: mask, eci: options.ECI, fnc1: options.FNC1,
		fnc1ApplicationIndicator: options.FNC1ApplicationIndicator,
	}, nil
}

func splitInput(payload []byte, assignment int) (string, unixsplit.Charset, int, error) {
	if assignment == 0 {
		for _, value := range payload {
			if value >= 128 {
				return "", nil, 0, ErrInvalidInput
			}
		}
		return string(payload), unixsplit.ASCIICompat, -1, nil
	}
	if _, err := encodeECI(assignment); err != nil {
		return "", nil, 0, err
	}
	switch assignment {
	case unixsplit.Latin1ECI:
		runes := make([]rune, len(payload))
		for index, value := range payload {
			runes[index] = rune(value)
		}
		return string(runes), unixsplit.UTF8AsLatin1, assignment, nil
	case unixsplit.ShiftJISECI:
		if !utf8.Valid(payload) {
			return "", nil, 0, ErrInvalidInput
		}
		return string(payload), unixsplit.ShiftJIS, assignment, nil
	case unixsplit.UTF8ECI:
		if !utf8.Valid(payload) {
			return "", nil, 0, ErrInvalidInput
		}
		return string(payload), unixsplit.UTF8, assignment, nil
	default:
		return string(payload), unixsplit.ASCIICompat, assignment, nil
	}
}

func optimizedMode(segments []unixcoding.Segment) Mode {
	mode, set := Auto, false
	for _, segment := range segments {
		var current Mode
		switch segment.Mode {
		case unixcoding.Numeric:
			current = Numeric
		case unixcoding.Alphanumeric, unixcoding.FNC1Alpha:
			current = Alphanumeric
		case unixcoding.Kanji, unixcoding.ShiftJISKanji:
			current = Kanji
		case unixcoding.Byte, unixcoding.Latin1:
			current = Byte
		case unixcoding.ECI, unixcoding.StructAppend, unixcoding.FNC1First, unixcoding.FNC1Second:
			continue
		}
		if set && mode != current {
			return Auto
		}
		mode, set = current, true
	}
	if !set {
		return Auto
	}

	return mode
}

func encodeExtended(payload []byte, options Options, quietZone int, maskSet bool) (Symbol, error) {
	level := options.ErrorCorrection
	if level == DefaultErrorCorrection {
		level = Medium
	}
	segments := make([]unixcoding.Segment, 0, 4)
	if options.StructuredAppend != nil {
		header := options.StructuredAppend
		segments = append(segments, unixcoding.Segment{
			Mode: unixcoding.StructAppend,
			// #nosec G115 -- index and total were bounded to four-bit fields.
			Text: string([]byte{byte(header.Index<<4 | (header.Total - 1)), header.Parity}),
		})
	}
	if options.FNC1 == FNC1First {
		segments = append(segments, unixcoding.Segment{Mode: unixcoding.FNC1First})
	}
	if options.FNC1 == FNC1Second {
		segments = append(segments, unixcoding.Segment{
			Mode: unixcoding.FNC1Second,
			Text: string([]byte{options.FNC1ApplicationIndicator}),
		})
	}
	if options.ECI != 0 {
		eci, _ := encodeECI(options.ECI)
		segments = append(segments, unixcoding.Segment{Mode: unixcoding.ECI, Text: eci})
	}
	segments = append(segments, unixcoding.Segment{Mode: codingMode(options.Mode), Text: string(payload)})

	version := options.Version
	if version == 0 {
		version = 1
	}
	var code *unixcoding.Code
	var mask int
	var err error
	for ; version <= 40; version++ {
		code, mask, err = encodeSegments(segments, version, level, options.Mask, maskSet)
		if err == nil || options.Version != 0 {
			break
		}
	}
	if err != nil {
		return Symbol{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	matrix := logicalCodingMatrix(code, quietZone)
	logical, _ := barcode.NewSymbol(barcode.SymbolOptions{
		Format: barcode.QRCode, Payload: payload, Matrix: matrix,
	})
	var header *StructuredAppend
	if options.StructuredAppend != nil {
		headerCopy := *options.StructuredAppend
		header = &headerCopy
	}

	return Symbol{
		logical: logical, mode: options.Mode, errorCorrection: level,
		version: version, mask: mask, eci: options.ECI, fnc1: options.FNC1,
		fnc1ApplicationIndicator: options.FNC1ApplicationIndicator,
		structuredAppend:         header,
	}, nil
}

func encodeSegments(segments []unixcoding.Segment, version int, level ErrorCorrection, requestedMask int, maskSet bool) (*unixcoding.Code, int, error) {
	v, l := unixcoding.Version(version), codingLevel(level)
	plan, err := unixcoding.NewPlan(v, l)
	if err != nil {
		return nil, 0, err
	}
	bits := unixcoding.NewBits(v, l)
	for _, segment := range segments {
		if err := segment.Encode(bits, v.SizeClass()); err != nil {
			return nil, 0, err
		}
	}
	if bits.Bits() > plan.DataBits {
		return nil, 0, fmt.Errorf("payload requires %d bits; version has %d", bits.Bits(), plan.DataBits)
	}
	bits.AddCheckBytes(v, l)
	stream := bits.Permute(v, l)
	data := make([]byte, plan.Size*((plan.Size+7)/8))
	plan.Serialise(data, stream)

	bestMask, bestPenalty := 0, int(^uint(0)>>1)
	best := make([]byte, len(data))
	first, last := 0, 7
	if maskSet {
		first, last = requestedMask, requestedMask
	}
	for candidate := first; candidate <= last; candidate++ {
		bitmap := make([]byte, len(data))
		for index := range bitmap {
			bitmap[index] = data[index] ^ plan.Pattern[candidate][index]
		}
		code := &unixcoding.Code{Bitmap: bitmap, Size: plan.Size, Stride: (plan.Size + 7) / 8}
		if penalty := code.Penalty(); penalty < bestPenalty {
			bestPenalty, bestMask, best = penalty, candidate, bitmap
		}
	}

	return &unixcoding.Code{Bitmap: best, Size: plan.Size, Stride: (plan.Size + 7) / 8}, bestMask, nil
}

func codingMode(mode Mode) unixcoding.Mode {
	switch mode {
	case Auto, Byte:
		return unixcoding.Byte
	case Numeric:
		return unixcoding.Numeric
	case Alphanumeric:
		return unixcoding.Alphanumeric
	case Kanji:
		return unixcoding.Kanji
	}
	return unixcoding.Byte
}

func codingLevel(level ErrorCorrection) unixcoding.Level {
	switch level {
	case DefaultErrorCorrection, Medium:
		return unixcoding.M
	case Low:
		return unixcoding.L
	case Quartile:
		return unixcoding.Q
	case High:
		return unixcoding.H
	}
	return unixcoding.M
}

func encodeECI(assignment int) (string, error) {
	switch {
	case assignment < 0 || assignment >= 1_000_000:
		return "", ErrUnsupported
	case assignment < 128:
		return string([]byte{byte(assignment)}), nil
	case assignment < 16_384:
		// #nosec G115 -- the branch bounds the assignment to two ECI bytes.
		return string([]byte{byte(assignment>>8) | 0x80, byte(assignment)}), nil
	default:
		// #nosec G115 -- validation bounds the assignment to three ECI bytes.
		return string([]byte{byte(assignment>>16) | 0xc0, byte(assignment >> 8), byte(assignment)}), nil
	}
}

func logicalCodingMatrix(source *unixcoding.Code, quietZone int) barcode.Matrix {
	width := source.Size + 2*quietZone
	modules := make([]bool, width*width)
	for y := 0; y < source.Size; y++ {
		for x := 0; x < source.Size; x++ {
			modules[(y+quietZone)*width+x+quietZone] = source.Black(x, y)
		}
	}
	matrix, _ := barcode.NewMatrix(width, width, modules)

	return matrix
}

// EncodeGS1 serializes validated elements and adds first-position FNC1.
func EncodeGS1(elements gs1.ElementString, options Options) (Symbol, error) {
	options.FNC1 = FNC1First

	return Encode([]byte(elements.Raw()), options)
}

func validateMode(payload []byte, mode Mode) error {
	switch mode {
	case Auto, Byte:
		return nil
	case Numeric:
		for _, value := range payload {
			if value < '0' || value > '9' {
				return ErrInvalidInput
			}
		}
	case Alphanumeric:
		const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ $%*+-./:"
		for _, value := range payload {
			if !strings.ContainsRune(alphabet, rune(value)) {
				return ErrInvalidInput
			}
		}
	case Kanji:
		if !utf8.Valid(payload) {
			return ErrInvalidInput
		}
	}

	return nil
}

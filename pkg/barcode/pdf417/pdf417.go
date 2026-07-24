// Package pdf417 encodes ISO/IEC 15438:2015 PDF417 logical symbols.
package pdf417

import (
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/barcode/barcode"
	"github.com/faustbrian/golib/pkg/barcode/internal/pdf417encoder"
)

const (
	defaultQuietZone = 2
	maxPayloadBytes  = 4096
)

var (
	// ErrInvalidInput and ErrUnsupported classify PDF417 option failures.
	ErrInvalidInput = errors.New("pdf417: invalid input")
	// ErrUnsupported reports a valid option not implemented by the encoder.
	ErrUnsupported = errors.New("pdf417: unsupported option")
)

// Compaction selects a PDF417 high-level encoding strategy.
type Compaction uint8

const (
	// Automatic and the other constants select PDF417 compaction modes.
	Automatic Compaction = iota
	// Text forces text compaction.
	Text
	// Byte forces byte compaction.
	Byte
	// Numeric forces numeric compaction.
	Numeric
)

// ErrorCorrection selects one of the nine PDF417 correction levels.
type ErrorCorrection uint8

const (
	// DefaultErrorCorrection and the other constants select correction levels.
	DefaultErrorCorrection ErrorCorrection = iota
	// Level0 adds two correction codewords.
	Level0
	// Level1 adds four correction codewords.
	Level1
	// Level2 adds eight correction codewords.
	Level2
	// Level3 adds sixteen correction codewords.
	Level3
	// Level4 adds thirty-two correction codewords.
	Level4
	// Level5 adds sixty-four correction codewords.
	Level5
	// Level6 adds 128 correction codewords.
	Level6
	// Level7 adds 256 correction codewords.
	Level7
	// Level8 adds 512 correction codewords.
	Level8
)

// Options controls PDF417 compaction, correction, layout, and quiet zone.
type Options struct {
	Compaction      Compaction
	ErrorCorrection ErrorCorrection
	Compact         bool
	MinRows         int
	MaxRows         int
	MinColumns      int
	MaxColumns      int
	QuietZone       int
	ECI             int
	Macro           *Macro
}

// Macro describes a Macro PDF417 control block. FileID contains one or more
// three-digit codewords in the range 000 through 899.
type Macro struct {
	SegmentIndex int
	FileID       string
	LastSegment  bool
	FileName     string
	SegmentCount *int
	Timestamp    *int64
	Sender       string
	Addressee    string
	FileSize     *int64
	Checksum     *int
}

// Symbol combines an immutable logical symbol with PDF417 metadata.
type Symbol struct {
	logical         barcode.Symbol
	compaction      Compaction
	errorCorrection ErrorCorrection
	compact         bool
	eci             int
	macro           *Macro
}

// Logical returns the immutable format-neutral symbol.
func (symbol Symbol) Logical() barcode.Symbol { return symbol.logical }

// Compaction returns the requested high-level encoding strategy.
func (symbol Symbol) Compaction() Compaction { return symbol.compaction }

// ErrorCorrection returns the selected correction level.
func (symbol Symbol) ErrorCorrection() ErrorCorrection { return symbol.errorCorrection }

// Compact reports whether compact PDF417 layout was selected.
func (symbol Symbol) Compact() bool { return symbol.compact }

// ECI returns the requested ECI assignment, or zero when absent.
func (symbol Symbol) ECI() int { return symbol.eci }

// Macro returns a defensive copy of the Macro PDF417 control block.
func (symbol Symbol) Macro() (Macro, bool) {
	if symbol.macro == nil {
		return Macro{}, false
	}

	return cloneMacro(*symbol.macro), true
}

// Encode returns a PDF417 matrix with explicit compaction, correction, and
// row/column bounds. Each codeword row is rendered four modules high.
func Encode(payload []byte, options Options) (Symbol, error) {
	if len(payload) == 0 || len(payload) > maxPayloadBytes || options.Compaction > Numeric ||
		options.ErrorCorrection > Level8 || options.QuietZone < 0 ||
		options.ECI < 0 || options.ECI >= 811_800 || !validMacro(options.Macro) ||
		options.MinRows < 0 || options.MaxRows < 0 || options.MinColumns < 0 || options.MaxColumns < 0 ||
		options.MinRows > 90 || options.MaxRows > 90 || options.MinColumns > 30 || options.MaxColumns > 30 ||
		options.MaxRows > 0 && options.MinRows > options.MaxRows ||
		options.MaxColumns > 0 && options.MinColumns > options.MaxColumns {
		return Symbol{}, ErrInvalidInput
	}
	quietZone := options.QuietZone
	if quietZone == 0 {
		quietZone = defaultQuietZone
	}
	if quietZone > 256 {
		return Symbol{}, ErrInvalidInput
	}
	errorCorrection := options.ErrorCorrection
	if errorCorrection == DefaultErrorCorrection {
		errorCorrection = Level2
	}
	if options.Compaction == Numeric {
		for _, value := range payload {
			if value < '0' || value > '9' {
				return Symbol{}, ErrInvalidInput
			}
		}
	}
	encoder := pdf417encoder.NewPDF417Encoder()
	encoder.SetCompaction(pdf417encoder.Compaction(options.Compaction))
	encoder.SetCompact(options.Compact)
	minRows, maxRows := dimensions(options.MinRows, options.MaxRows, 3, 90)
	minColumns, maxColumns := dimensions(options.MinColumns, options.MaxColumns, 1, 30)
	encoder.SetDimensions(maxColumns, minColumns, maxRows, minRows)
	if err := encoder.GenerateBarcodeLogicWithControls(
		string(payload), int(errorCorrection-1), options.ECI, internalMacro(options.Macro),
	); err != nil {
		return Symbol{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	source := encoder.BarcodeMatrix().ScaledMatrix(1, 4)
	width, height := len(source[0])+2*quietZone, len(source)+2*quietZone
	modules := make([]bool, width*height)
	for y, row := range source {
		outputY := len(source) - y - 1 + quietZone
		for x, value := range row {
			modules[outputY*width+x+quietZone] = value == 1
		}
	}
	matrix, _ := barcode.NewMatrix(width, height, modules)
	logical, _ := barcode.NewSymbol(barcode.SymbolOptions{
		Format: barcode.PDF417, Payload: payload, Matrix: matrix,
	})

	return Symbol{
		logical: logical, compaction: options.Compaction,
		errorCorrection: errorCorrection, compact: options.Compact, eci: options.ECI,
		macro: cloneMacroPointer(options.Macro),
	}, nil
}

func dimensions(minimum, maximum, defaultMinimum, defaultMaximum int) (int, int) {
	if minimum == 0 {
		minimum = defaultMinimum
	}
	if maximum == 0 {
		maximum = defaultMaximum
	}
	return minimum, maximum
}

func validMacro(macro *Macro) bool {
	if macro == nil {
		return true
	}
	if macro.SegmentIndex < 0 || macro.SegmentIndex > 99_998 || macro.FileID == "" ||
		len(macro.FileID)%3 != 0 || negative(macro.SegmentCount) || negative(macro.Timestamp) ||
		negative(macro.FileSize) || negative(macro.Checksum) {
		return false
	}
	for index, value := range macro.FileID {
		if value < '0' || value > '9' || index%3 == 2 && macro.FileID[index-2:index+1] > "899" {
			return false
		}
	}
	return true
}

func negative[T ~int | ~int64](value *T) bool { return value != nil && *value < 0 }

func internalMacro(macro *Macro) *pdf417encoder.Macro {
	if macro == nil {
		return nil
	}
	return &pdf417encoder.Macro{
		SegmentIndex: macro.SegmentIndex, FileID: macro.FileID, LastSegment: macro.LastSegment,
		FileName: macro.FileName, SegmentCount: macro.SegmentCount, Timestamp: macro.Timestamp,
		Sender: macro.Sender, Addressee: macro.Addressee, FileSize: macro.FileSize,
		Checksum: macro.Checksum,
	}
}

func cloneMacroPointer(macro *Macro) *Macro {
	if macro == nil {
		return nil
	}
	cloned := cloneMacro(*macro)
	return &cloned
}

func cloneMacro(macro Macro) Macro {
	macro.SegmentCount = clonePointer(macro.SegmentCount)
	macro.Timestamp = clonePointer(macro.Timestamp)
	macro.FileSize = clonePointer(macro.FileSize)
	macro.Checksum = clonePointer(macro.Checksum)
	return macro
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

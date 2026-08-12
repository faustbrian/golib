package tabular

import (
	"errors"
	"io"
	"slices"

	internalxls "github.com/faustbrian/golib/pkg/tabular/internal/xls"
)

const defaultMaxWorkbookBytes int64 = 64 * 1024 * 1024

// SpreadsheetFormat identifies an explicitly selected workbook format.
type SpreadsheetFormat string

const (
	// FormatXLS selects legacy OLE2/BIFF8 workbooks.
	FormatXLS SpreadsheetFormat = "xls"
	// FormatXLSX selects OOXML workbooks.
	FormatXLSX SpreadsheetFormat = "xlsx"
)

// SpreadsheetConfig controls workbook selection and row semantics.
type SpreadsheetConfig struct {
	Format              SpreadsheetFormat
	Sheet               string
	Header              *HeaderConfig
	Normalize           NormalizationConfig
	FieldsPerRecord     int
	AllowVariableFields bool
	PreserveCellErrors  bool
	// PreserveCellPresence enables ReadCells and its absent-versus-stored-empty
	// distinction. Zero preserves the optimized string-only Read behavior.
	PreserveCellPresence bool
	MaxWorkbookBytes     int64
	// MaxRecordBytes bounds one parsed worksheet row before normalization.
	// Zero preserves the unbounded legacy behavior.
	MaxRecordBytes int
	// MaxFieldBytes bounds one parsed worksheet cell before normalization.
	// Zero preserves the unbounded legacy behavior.
	MaxFieldBytes int
	// MaxSheets bounds the number of worksheets in an XLSX workbook.
	// Zero preserves the unbounded legacy behavior.
	MaxSheets int
	ZIP       ZIPConfig
}

// SpreadsheetCell is one immutable decoded worksheet cell.
type SpreadsheetCell struct {
	value   string
	present bool
}

// Value returns the decoded and normalized cell value.
func (cell SpreadsheetCell) Value() string {
	return cell.value
}

// Present reports whether the workbook stored a cell at this position.
func (cell SpreadsheetCell) Present() bool {
	return cell.present
}

// SpreadsheetRow preserves workbook cell presence alongside decoded values.
type SpreadsheetRow []SpreadsheetCell

// SpreadsheetReader presents format-independent workbook rows.
type SpreadsheetReader struct {
	source           spreadsheetRowSource
	index            int
	format           SpreadsheetFormat
	headerConfig     *HeaderConfig
	header           Row
	headerRead       bool
	headerErr        error
	normalize        NormalizationConfig
	fields           int
	variable         bool
	preserveErrors   bool
	preservePresence bool
	maxRecordBytes   int
	maxFieldBytes    int
	closed           bool
}

type spreadsheetCell struct {
	value string
	err   string
}

type spreadsheetSourceRow struct {
	cells    []spreadsheetCell
	presence []bool
}

type spreadsheetRowSource interface {
	Read() (spreadsheetSourceRow, error)
	Close() error
}

type xlsRowSource struct {
	rows     [][]internalxls.Cell
	presence [][]bool
	index    int
}

func (source *xlsRowSource) Read() (spreadsheetSourceRow, error) {
	if source.index >= len(source.rows) {
		return spreadsheetSourceRow{}, io.EOF
	}
	input := source.rows[source.index]
	row := spreadsheetSourceRow{
		cells: make([]spreadsheetCell, len(input)),
	}
	for index, cell := range input {
		row.cells[index] = spreadsheetCell{
			value: cell.Value,
			err:   cell.Error,
		}
	}
	if source.presence != nil {
		row.presence = append([]bool(nil), source.presence[source.index]...)
	}
	source.index++
	return row, nil
}

func (*xlsRowSource) Close() error { return nil }

// OpenSpreadsheet opens an explicitly configured XLS or XLSX workbook.
func OpenSpreadsheet(source io.ReaderAt, size int64, config SpreadsheetConfig) (*SpreadsheetReader, error) {
	if source == nil || size < 0 || config.FieldsPerRecord < 0 ||
		config.MaxWorkbookBytes < 0 || config.MaxRecordBytes < 0 ||
		config.MaxFieldBytes < 0 || config.MaxSheets < 0 ||
		(config.Format != FormatXLS && config.Format != FormatXLSX) {
		return nil, &Error{Kind: ErrorInvalidConfig, Op: "spreadsheet.open", Format: string(config.Format)}
	}
	if config.Format == FormatXLSX {
		source, err := openXLSXRows(source, size, config)
		if err != nil {
			return nil, err
		}
		return newSpreadsheetReader(source, config), nil
	}
	limit := config.MaxWorkbookBytes
	if limit == 0 {
		limit = defaultMaxWorkbookBytes
	}
	if size > limit {
		return nil, &Error{Kind: ErrorLimitExceeded, Op: "spreadsheet.open", Format: string(config.Format)}
	}
	data, err := io.ReadAll(io.NewSectionReader(source, 0, size))
	if err != nil {
		return nil, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.open", Format: string(config.Format), Err: err}
	}
	var workbook *internalxls.Workbook
	if config.PreserveCellPresence {
		workbook, err = internalxls.OpenWithPresence(data)
	} else {
		workbook, err = internalxls.Open(data)
	}
	if err != nil {
		return nil, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.open", Format: string(config.Format), Err: err}
	}
	sheetIndex := 0
	if config.Sheet != "" {
		sheetIndex = slices.IndexFunc(workbook.Sheets, func(sheet internalxls.Sheet) bool {
			return sheet.Name == config.Sheet
		})
		if sheetIndex < 0 {
			return nil, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.sheet", Format: string(config.Format), Err: errors.New("sheet not found")}
		}
	}
	reader := newSpreadsheetReader(&xlsRowSource{
		rows:     workbook.Sheets[sheetIndex].Rows,
		presence: workbook.Sheets[sheetIndex].Presence,
	}, config)
	return reader, nil
}

func newSpreadsheetReader(source spreadsheetRowSource, config SpreadsheetConfig) *SpreadsheetReader {
	return &SpreadsheetReader{
		source:           source,
		format:           config.Format,
		headerConfig:     cloneHeaderConfig(config.Header),
		normalize:        config.Normalize,
		fields:           config.FieldsPerRecord,
		variable:         config.AllowVariableFields,
		preserveErrors:   config.PreserveCellErrors,
		preservePresence: config.PreserveCellPresence,
		maxRecordBytes:   config.MaxRecordBytes,
		maxFieldBytes:    config.MaxFieldBytes,
	}
}

// Header returns a normalized copy of the configured first row.
func (reader *SpreadsheetReader) Header() (Row, error) {
	if reader.headerConfig == nil {
		return nil, nil
	}
	reader.readHeader()
	if reader.headerErr != nil {
		return nil, reader.headerErr
	}
	return append(Row(nil), reader.header...), nil
}

// Read returns the next worksheet row.
func (reader *SpreadsheetReader) Read() (Row, error) {
	if reader.closed {
		return nil, io.ErrClosedPipe
	}
	if reader.headerConfig != nil {
		reader.readHeader()
		if reader.headerErr != nil {
			return nil, reader.headerErr
		}
	}
	return reader.readRow()
}

// ReadCells returns the next worksheet row while preserving whether each cell
// was stored or absent. Values follow the same normalization and error policy
// as Read.
func (reader *SpreadsheetReader) ReadCells() (SpreadsheetRow, error) {
	if reader.closed {
		return nil, io.ErrClosedPipe
	}
	if !reader.preservePresence {
		return nil, &Error{
			Kind:   ErrorInvalidConfig,
			Op:     "spreadsheet.read",
			Format: string(reader.format),
			Err:    errors.New("cell presence was not enabled"),
		}
	}
	if reader.headerConfig != nil {
		reader.readHeader()
		if reader.headerErr != nil {
			return nil, reader.headerErr
		}
	}
	sourceRow, err := reader.readSpreadsheetCells()
	if err != nil {
		return nil, err
	}
	values, err := reader.spreadsheetValues(sourceRow.cells)
	if err != nil {
		return nil, err
	}
	row := make(SpreadsheetRow, len(sourceRow.cells))
	for index := range sourceRow.cells {
		row[index] = SpreadsheetCell{
			value:   values[index],
			present: sourceRow.presence[index],
		}
	}
	return row, nil
}

func (reader *SpreadsheetReader) readHeader() {
	if reader.headerRead {
		return
	}
	reader.headerRead = true
	row, err := reader.readRow()
	if errors.Is(err, io.EOF) {
		reader.headerErr = &Error{Kind: ErrorInvalidHeader, Op: "spreadsheet.header", Format: string(reader.format), Err: io.EOF}
		return
	}
	if err != nil {
		reader.headerErr = err
		return
	}
	reader.header, reader.headerErr = NormalizeHeader(row, *reader.headerConfig)
	if reader.fields == 0 && !reader.variable {
		reader.fields = len(reader.header)
	}
}

func (reader *SpreadsheetReader) readRow() (Row, error) {
	sourceRow, err := reader.readSpreadsheetCells()
	if err != nil {
		return nil, err
	}
	return reader.spreadsheetValues(sourceRow.cells)
}

func (reader *SpreadsheetReader) readSpreadsheetCells() (
	spreadsheetSourceRow,
	error,
) {
	sourceRow, err := reader.source.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return spreadsheetSourceRow{}, io.EOF
		}
		return spreadsheetSourceRow{}, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.read", Format: string(reader.format), Row: reader.index + 1, Err: err}
	}
	reader.index++
	if reader.preservePresence &&
		len(sourceRow.presence) != len(sourceRow.cells) {
		return spreadsheetSourceRow{}, &Error{
			Kind:   ErrorSpreadsheet,
			Op:     "spreadsheet.read",
			Format: string(reader.format),
			Row:    reader.index,
			Err:    errors.New("cell presence row is inconsistent"),
		}
	}
	if err = reader.validateLimits(sourceRow.cells); err != nil {
		return spreadsheetSourceRow{}, err
	}
	if reader.fields > 0 && !reader.variable {
		if len(sourceRow.cells) > reader.fields {
			return spreadsheetSourceRow{}, &Error{Kind: ErrorMalformedRow, Op: "spreadsheet.read", Format: string(reader.format), Row: reader.index, Err: errors.New("unexpected field count")}
		}
		missing := reader.fields - len(sourceRow.cells)
		sourceRow.cells = append(
			sourceRow.cells,
			make([]spreadsheetCell, missing)...,
		)
		if reader.preservePresence {
			sourceRow.presence = append(
				sourceRow.presence,
				make([]bool, missing)...,
			)
		}
	}
	return sourceRow, nil
}

func (reader *SpreadsheetReader) spreadsheetValues(
	cells []spreadsheetCell,
) (Row, error) {
	row := make(Row, len(cells))
	for index, cell := range cells {
		if cell.err != "" && !reader.preserveErrors {
			return nil, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.read", Format: string(reader.format), Row: reader.index, Field: index + 1, Err: errors.New(cell.err)}
		}
		if cell.err != "" {
			row[index] = cell.err
		} else {
			row[index] = cell.value
		}
	}
	return NormalizeRow(row, reader.normalize), nil
}

func (reader *SpreadsheetReader) validateLimits(
	cells []spreadsheetCell,
) error {
	if reader.maxFieldBytes > 0 {
		for index, cell := range cells {
			if len(reader.outputCellValue(cell)) > reader.maxFieldBytes {
				return &Error{
					Kind:   ErrorLimitExceeded,
					Op:     "spreadsheet.read",
					Format: string(reader.format),
					Row:    reader.index,
					Field:  index + 1,
				}
			}
		}
	}
	if reader.maxRecordBytes == 0 {
		return nil
	}
	remaining := reader.maxRecordBytes
	for _, cell := range cells {
		value := reader.outputCellValue(cell)
		if len(value) > remaining {
			return &Error{
				Kind:   ErrorLimitExceeded,
				Op:     "spreadsheet.read",
				Format: string(reader.format),
				Row:    reader.index,
			}
		}
		remaining -= len(value)
	}
	return nil
}

func (reader *SpreadsheetReader) outputCellValue(
	cell spreadsheetCell,
) string {
	if reader.preserveErrors && cell.err != "" {
		return cell.err
	}
	return cell.value
}

// Close releases iterator resources. It does not close the caller's source.
func (reader *SpreadsheetReader) Close() error {
	if reader.closed {
		return nil
	}
	reader.closed = true
	return reader.source.Close()
}

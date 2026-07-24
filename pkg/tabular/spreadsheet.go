package tabular

import (
	"errors"
	"io"

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
	MaxWorkbookBytes    int64
	ZIP                 ZIPConfig
}

// SpreadsheetReader presents format-independent workbook rows.
type SpreadsheetReader struct {
	source         spreadsheetRowSource
	index          int
	format         SpreadsheetFormat
	headerConfig   *HeaderConfig
	header         Row
	headerRead     bool
	headerErr      error
	normalize      NormalizationConfig
	fields         int
	variable       bool
	preserveErrors bool
	closed         bool
}

type spreadsheetCell struct {
	value string
	err   string
}

type spreadsheetRowSource interface {
	Read() ([]spreadsheetCell, error)
	Close() error
}

type xlsRowSource struct {
	rows  [][]internalxls.Cell
	index int
}

func (source *xlsRowSource) Read() ([]spreadsheetCell, error) {
	if source.index >= len(source.rows) {
		return nil, io.EOF
	}
	input := source.rows[source.index]
	source.index++
	row := make([]spreadsheetCell, len(input))
	for index, cell := range input {
		row[index] = spreadsheetCell{value: cell.Value, err: cell.Error}
	}
	return row, nil
}

func (*xlsRowSource) Close() error { return nil }

// OpenSpreadsheet opens an explicitly configured XLS or XLSX workbook.
func OpenSpreadsheet(source io.ReaderAt, size int64, config SpreadsheetConfig) (*SpreadsheetReader, error) {
	if source == nil || size < 0 || config.FieldsPerRecord < 0 || config.MaxWorkbookBytes < 0 ||
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
	workbook, err := internalxls.Open(data)
	if err != nil {
		return nil, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.open", Format: string(config.Format), Err: err}
	}
	sheetIndex := 0
	if config.Sheet != "" {
		sheetIndex = -1
		for index, sheet := range workbook.Sheets {
			if sheet.Name == config.Sheet {
				sheetIndex = index
				break
			}
		}
		if sheetIndex < 0 {
			return nil, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.sheet", Format: string(config.Format), Err: errors.New("sheet not found")}
		}
	}
	reader := newSpreadsheetReader(&xlsRowSource{rows: workbook.Sheets[sheetIndex].Rows}, config)
	return reader, nil
}

func newSpreadsheetReader(source spreadsheetRowSource, config SpreadsheetConfig) *SpreadsheetReader {
	return &SpreadsheetReader{
		source:         source,
		format:         config.Format,
		headerConfig:   cloneHeaderConfig(config.Header),
		normalize:      config.Normalize,
		fields:         config.FieldsPerRecord,
		variable:       config.AllowVariableFields,
		preserveErrors: config.PreserveCellErrors,
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
	if reader.headerErr == nil && reader.fields == 0 && !reader.variable {
		reader.fields = len(reader.header)
	}
}

func (reader *SpreadsheetReader) readRow() (Row, error) {
	cells, err := reader.source.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.read", Format: string(reader.format), Row: reader.index + 1, Err: err}
	}
	reader.index++
	if reader.fields > 0 && !reader.variable {
		if len(cells) > reader.fields {
			return nil, &Error{Kind: ErrorMalformedRow, Op: "spreadsheet.read", Format: string(reader.format), Row: reader.index, Err: errors.New("unexpected field count")}
		}
		if len(cells) < reader.fields {
			cells = append(cells, make([]spreadsheetCell, reader.fields-len(cells))...)
		}
	}
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

// Close releases iterator resources. It does not close the caller's source.
func (reader *SpreadsheetReader) Close() error {
	if reader.closed {
		return nil
	}
	reader.closed = true
	return reader.source.Close()
}

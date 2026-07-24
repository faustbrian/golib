package tabular

import (
	"encoding/xml"
	"errors"
	"io"
	"math"
	"strings"

	"github.com/xuri/excelize/v2"
)

type xlsxRowSource struct {
	workbook xlsxWorkbook
	rows     xlsxRows
	sheet    string
	row      int
}

type xlsxWorkbook interface {
	GetCellType(string, string) (excelize.CellType, error)
	Close() error
}

type xlsxRows interface {
	Next() bool
	Error() error
	Columns(...excelize.Options) ([]string, error)
	Close() error
}

func openXLSXRows(source io.ReaderAt, size int64, config SpreadsheetConfig) (spreadsheetRowSource, error) {
	archive, err := OpenZIP(source, size, config.ZIP)
	if err != nil {
		return nil, err
	}
	if err = validateXLSXWorksheets(archive); err != nil {
		return nil, err
	}
	maxTotal := config.ZIP.MaxTotalBytes
	if maxTotal == 0 {
		maxTotal = defaultMaxZIPTotalBytes
	}
	maxEntry := config.ZIP.MaxEntryBytes
	if maxEntry == 0 {
		maxEntry = defaultMaxZIPEntryBytes
	}
	if maxTotal > math.MaxInt64 || maxEntry > math.MaxInt64 {
		return nil, &Error{Kind: ErrorInvalidConfig, Op: "spreadsheet.open", Format: string(FormatXLSX), Err: errors.New("ZIP limits exceed supported integer range")}
	}
	workbook, err := excelize.OpenReader(io.NewSectionReader(source, 0, size), excelize.Options{
		RawCellValue:      true,
		UnzipSizeLimit:    int64(maxTotal),
		UnzipXMLSizeLimit: int64(maxEntry),
	})
	if err != nil {
		return nil, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.open", Format: string(FormatXLSX), Err: err}
	}
	sheets := workbook.GetSheetList()
	if len(sheets) == 0 {
		_ = workbook.Close()
		return nil, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.open", Format: string(FormatXLSX), Err: errors.New("workbook contains no sheets")}
	}
	sheet := sheets[0]
	if config.Sheet != "" {
		sheet = ""
		for _, candidate := range sheets {
			if candidate == config.Sheet {
				sheet = candidate
				break
			}
		}
		if sheet == "" {
			_ = workbook.Close()
			return nil, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.sheet", Format: string(FormatXLSX), Err: errors.New("sheet not found")}
		}
	}
	rows, err := workbook.Rows(sheet)
	if err != nil {
		_ = workbook.Close()
		return nil, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.rows", Format: string(FormatXLSX), Err: err}
	}
	return &xlsxRowSource{workbook: workbook, rows: rows, sheet: sheet}, nil
}

func validateXLSXWorksheets(archive *ZIPArchive) error {
	for _, entry := range archive.Entries() {
		if entry.Directory || !strings.HasPrefix(entry.Name, "xl/worksheets/") || !strings.HasSuffix(entry.Name, ".xml") {
			continue
		}
		reader, err := archive.Open(entry.Name)
		if err != nil {
			return err
		}
		decoder := xml.NewDecoder(reader)
		for {
			if _, err = decoder.Token(); err != nil {
				break
			}
		}
		_ = reader.Close()
		if err != nil && !errors.Is(err, io.EOF) {
			return &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.validate", Format: string(FormatXLSX), Err: err}
		}
	}
	return nil
}

func (source *xlsxRowSource) Read() ([]spreadsheetCell, error) {
	if !source.rows.Next() {
		if err := source.rows.Error(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	source.row++
	values, err := source.rows.Columns(excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, err
	}
	row := make([]spreadsheetCell, len(values))
	for index, value := range values {
		row[index].value = value
		if !strings.HasPrefix(value, "#") {
			continue
		}
		cellType, typeErr := source.workbook.GetCellType(source.sheet, cellName(index+1, source.row))
		if typeErr != nil {
			return nil, typeErr
		}
		if cellType == excelize.CellTypeError {
			row[index].err = value
		}
	}
	return row, nil
}

func (source *xlsxRowSource) Close() error {
	return errors.Join(source.rows.Close(), source.workbook.Close())
}

func cellName(column, row int) string {
	name, _ := excelize.CoordinatesToCellName(column, row)
	return name
}

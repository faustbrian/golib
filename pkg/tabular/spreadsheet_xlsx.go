package tabular

import (
	"encoding/xml"
	"errors"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/xuri/excelize/v2"
)

type xlsxRowSource struct {
	workbook xlsxWorkbook
	rows     xlsxRows
	presence xlsxPresenceReader
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
	if err = validateXLSXSheetLimit(archive, config.MaxSheets); err != nil {
		return nil, err
	}
	if err = validateXLSXWorksheets(archive); err != nil {
		return nil, err
	}
	maxTotal := zipLimitOrDefault(config.ZIP.MaxTotalBytes, defaultMaxZIPTotalBytes)
	maxEntry := zipLimitOrDefault(config.ZIP.MaxEntryBytes, defaultMaxZIPEntryBytes)
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
		index := slices.Index(sheets, config.Sheet)
		if index < 0 {
			_ = workbook.Close()
			return nil, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.sheet", Format: string(FormatXLSX), Err: errors.New("sheet not found")}
		}
		sheet = sheets[index]
	}
	rows, err := workbook.Rows(sheet)
	if err != nil {
		_ = workbook.Close()
		return nil, &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.rows", Format: string(FormatXLSX), Err: err}
	}
	var presence xlsxPresenceReader
	if config.PreserveCellPresence {
		presence, err = openXLSXPresence(archive, sheet)
		if err != nil {
			_ = rows.Close()
			_ = workbook.Close()
			return nil, err
		}
	}
	return &xlsxRowSource{
		workbook: workbook,
		rows:     rows,
		presence: presence,
		sheet:    sheet,
	}, nil
}

func zipLimitOrDefault(value, fallback uint64) uint64 {
	switch value {
	case 0:
		return fallback
	default:
		return value
	}
}

func validateXLSXSheetLimit(archive *ZIPArchive, maximum int) error {
	if maximum == 0 {
		return nil
	}
	reader, err := archive.Open("xl/workbook.xml")
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	var manifest struct {
		Sheets []struct{} `xml:"sheets>sheet"`
	}
	if err = xml.NewDecoder(reader).Decode(&manifest); err != nil {
		return &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.validate", Format: string(FormatXLSX), Err: err}
	}
	if len(manifest.Sheets) > maximum {
		return &Error{
			Kind:   ErrorLimitExceeded,
			Op:     "spreadsheet.open",
			Format: string(FormatXLSX),
			Err:    errors.New("workbook contains too many worksheets"),
		}
	}
	return nil
}

func validateXLSXWorksheets(archive *ZIPArchive) error {
	for _, entry := range archive.Entries() {
		if !xlsxWorksheetEntry(entry) {
			continue
		}
		reader, err := archive.Open(entry.Name)
		if err != nil {
			return err
		}
		err = xml.NewDecoder(reader).Decode(&struct{}{})
		_ = reader.Close()
		if err != nil {
			return &Error{Kind: ErrorSpreadsheet, Op: "spreadsheet.validate", Format: string(FormatXLSX), Err: err}
		}
	}
	return nil
}

func xlsxWorksheetEntry(entry ZIPEntry) bool {
	return !entry.Directory &&
		strings.HasPrefix(entry.Name, "xl/worksheets/") &&
		strings.HasSuffix(entry.Name, ".xml")
}

func (source *xlsxRowSource) Read() (spreadsheetSourceRow, error) {
	if !source.rows.Next() {
		if err := source.rows.Error(); err != nil {
			return spreadsheetSourceRow{}, err
		}
		if source.presence != nil {
			if _, err := source.presence.Read(); !errors.Is(err, io.EOF) {
				if err == nil {
					err = errors.New("worksheet presence rows are not exhausted")
				}
				return spreadsheetSourceRow{}, err
			}
		}
		return spreadsheetSourceRow{}, io.EOF
	}
	source.row++
	values, err := source.rows.Columns(excelize.Options{RawCellValue: true})
	if err != nil {
		return spreadsheetSourceRow{}, err
	}
	row := spreadsheetSourceRow{}
	if source.presence != nil {
		row.presence, err = source.presence.Read()
		if err != nil {
			return spreadsheetSourceRow{}, err
		}
	}
	width := max(len(values), len(row.presence))
	row.cells = make([]spreadsheetCell, width)
	for index, value := range values {
		row.cells[index].value = value
		if !strings.HasPrefix(value, "#") {
			continue
		}
		cellType, typeErr := source.workbook.GetCellType(source.sheet, cellName(index+1, source.row))
		if typeErr != nil {
			return spreadsheetSourceRow{}, typeErr
		}
		if cellType == excelize.CellTypeError {
			row.cells[index].err = value
		}
	}
	return row, nil
}

func (source *xlsxRowSource) Close() error {
	var presenceErr error
	if source.presence != nil {
		presenceErr = source.presence.Close()
	}
	return errors.Join(
		source.rows.Close(),
		presenceErr,
		source.workbook.Close(),
	)
}

func cellName(column, row int) string {
	name, _ := excelize.CoordinatesToCellName(column, row)
	return name
}

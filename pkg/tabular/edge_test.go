package tabular

import (
	"archive/zip"
	"bufio"
	"bytes"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestErrorKindImplementsError(t *testing.T) {
	t.Parallel()

	if got := ErrorMalformedRow.Error(); got != "malformed row" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestHeaderNormalizationSupportsUppercase(t *testing.T) {
	t.Parallel()

	header, err := NormalizeHeader(Row{"name"}, HeaderConfig{Case: HeaderCaseUpper})
	if err != nil || !reflect.DeepEqual(header, Row{"NAME"}) {
		t.Fatalf("NormalizeHeader() = %#v, %v", header, err)
	}
}

func TestEncodingDefaultsToUTF8(t *testing.T) {
	t.Parallel()

	value, err := DecodeBytes([]byte("Åbo"), "")
	if err != nil || value != "Åbo" {
		t.Fatalf("DecodeBytes() = %q, %v", value, err)
	}
	reader, err := DecodeReader(strings.NewReader("Åbo"), "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "Åbo" {
		t.Fatalf("decoded reader = %q, %v", data, err)
	}
}

func TestDecodeReaderSurfacesInvalidUTF8(t *testing.T) {
	t.Parallel()

	reader, err := DecodeReader(bytes.NewReader([]byte{0xff}), EncodingUTF8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadAll(reader); err == nil {
		t.Fatal("invalid UTF-8 produced no read error")
	}
}

func TestUTF8ReaderAcceptsEmptyDestination(t *testing.T) {
	t.Parallel()

	reader := &validatingUTF8Reader{source: bufio.NewReader(strings.NewReader("a"))}
	if count, err := reader.Read(nil); count != 0 || err != nil {
		t.Fatalf("Read(nil) = %d, %v", count, err)
	}
}

func TestDelimitedReaderHandlesNilSourceAndHeaderFailures(t *testing.T) {
	t.Parallel()

	if _, err := NewCSVReader(nil, DelimitedConfig{}); !errors.Is(err, ErrorInvalidConfig) {
		t.Fatalf("NewCSVReader(nil) error = %v", err)
	}
	reader, err := NewCSVReader(strings.NewReader("name,Name\n"), DelimitedConfig{
		Header: &HeaderConfig{Case: HeaderCaseLower, RejectDuplicates: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, first := reader.Header()
	_, second := reader.Header()
	if !errors.Is(first, ErrorDuplicateHeader) || !errors.Is(second, ErrorDuplicateHeader) {
		t.Fatalf("Header() errors = %v, %v", first, second)
	}
	withoutHeader, err := NewCSVReader(strings.NewReader("a\n"), DelimitedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if header, err := withoutHeader.Header(); header != nil || err != nil {
		t.Fatalf("Header() = %#v, %v", header, err)
	}
}

func TestDelimitedReaderPreservesHeaderErrorsAndConfiguration(t *testing.T) {
	t.Parallel()

	malformed, err := NewCSVReader(strings.NewReader("name,\"unterminated\n"), DelimitedConfig{
		Header: &HeaderConfig{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = malformed.Header(); !errors.Is(err, ErrorMalformedRow) {
		t.Fatalf("Header() error = %v, want malformed-row kind", err)
	}

	replacements := map[string]string{"name": "customer"}
	configured, err := NewCSVReader(strings.NewReader("name\n"), DelimitedConfig{
		Header: &HeaderConfig{Replace: replacements},
	})
	if err != nil {
		t.Fatal(err)
	}
	replacements["name"] = "mutated"
	header, err := configured.Header()
	if err != nil || !reflect.DeepEqual(header, Row{"customer"}) {
		t.Fatalf("Header() = %#v, %v", header, err)
	}
}

func TestFixedWidthReaderHandlesDefaultEncodingAndMissingFields(t *testing.T) {
	t.Parallel()

	reader, err := NewFixedWidthReader(strings.NewReader("a\n"), FixedWidthConfig{
		Fields:            []FixedWidthField{{Name: "first", Start: 0, End: 1}, {Name: "missing", Start: 2, End: 3}},
		AllowShortRecords: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := reader.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"a", ""}) {
		t.Fatalf("Read() = %#v, %v", row, err)
	}
	if _, err = NewFixedWidthReader(strings.NewReader(""), FixedWidthConfig{
		Encoding: Encoding("unknown"),
		Fields:   []FixedWidthField{{Name: "value", Start: 0, End: 1}},
	}); !errors.Is(err, ErrorInvalidEncoding) {
		t.Fatalf("unknown encoding error = %v", err)
	}
}

func TestZIPArchiveReportsUnsupportedCompression(t *testing.T) {
	t.Parallel()

	data := makeZIP(t, map[string]string{"data.csv": "a,b\n"})
	binaryLittleEndianPutUint16(data[8:10], 99)
	central := bytes.Index(data, []byte{'P', 'K', 1, 2})
	if central < 0 {
		t.Fatal("central directory not found")
	}
	binaryLittleEndianPutUint16(data[central+10:central+12], 99)
	archive, err := OpenZIP(bytes.NewReader(data), int64(len(data)), ZIPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = archive.Open("data.csv"); !errors.Is(err, ErrorArchive) {
		t.Fatalf("Open() error = %v, want archive kind", err)
	}
	if err = archive.Extract("missing.csv", io.Discard); !errors.Is(err, ErrorEntryNotFound) {
		t.Fatalf("Extract() error = %v, want entry-not-found kind", err)
	}
}

func TestSpreadsheetReaderCommonEdgeSemantics(t *testing.T) {
	t.Parallel()

	wantRead := errors.New("read failed")
	wantClose := errors.New("close failed")
	reader := newSpreadsheetReader(&stubSpreadsheetSource{readErr: wantRead, closeErr: wantClose}, SpreadsheetConfig{Format: FormatXLS})
	if header, err := reader.Header(); header != nil || err != nil {
		t.Fatalf("Header() = %#v, %v", header, err)
	}
	if _, err := reader.Read(); !errors.Is(err, ErrorSpreadsheet) || !errors.Is(err, wantRead) {
		t.Fatalf("Read() error = %v", err)
	}
	if err := reader.Close(); !errors.Is(err, wantClose) {
		t.Fatalf("Close() error = %v", err)
	}

	headerFailure := newSpreadsheetReader(&stubSpreadsheetSource{readErr: wantRead}, SpreadsheetConfig{
		Format: FormatXLS,
		Header: &HeaderConfig{},
	})
	if _, err := headerFailure.Header(); !errors.Is(err, ErrorSpreadsheet) || !errors.Is(err, wantRead) {
		t.Fatalf("Header() error = %v", err)
	}

	empty := newSpreadsheetReader(&stubSpreadsheetSource{}, SpreadsheetConfig{Format: FormatXLS, Header: &HeaderConfig{}})
	_, first := empty.Header()
	_, second := empty.Read()
	if !errors.Is(first, ErrorInvalidHeader) || !errors.Is(second, ErrorInvalidHeader) {
		t.Fatalf("empty header errors = %v, %v", first, second)
	}

	variable := newSpreadsheetReader(&stubSpreadsheetSource{rows: [][]spreadsheetCell{{{value: "a"}}}}, SpreadsheetConfig{
		Format: FormatXLS, FieldsPerRecord: 2, AllowVariableFields: true,
	})
	row, err := variable.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"a"}) {
		t.Fatalf("variable Read() = %#v, %v", row, err)
	}
}

func TestOpenSpreadsheetSurfacesReaderAndOOXMLFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("read failed")
	if _, err := OpenSpreadsheet(failingReaderAt{err: want}, 8, SpreadsheetConfig{Format: FormatXLS}); !errors.Is(err, ErrorSpreadsheet) || !errors.Is(err, want) {
		t.Fatalf("OpenSpreadsheet(XLS) error = %v", err)
	}

	data := makeZIP(t, map[string]string{"placeholder": "not OOXML"})
	if _, err := OpenSpreadsheet(bytes.NewReader(data), int64(len(data)), SpreadsheetConfig{Format: FormatXLSX}); !errors.Is(err, ErrorSpreadsheet) {
		t.Fatalf("OpenSpreadsheet(XLSX) error = %v", err)
	}
}

func TestXLSXReaderValidatesExtremeLimitsAndWorkbookStructure(t *testing.T) {
	t.Parallel()

	data := makeErrorXLSX(t)
	_, err := OpenSpreadsheet(bytes.NewReader(data), int64(len(data)), SpreadsheetConfig{
		Format: FormatXLSX,
		ZIP:    ZIPConfig{MaxTotalBytes: math.MaxUint64},
	})
	if !errors.Is(err, ErrorInvalidConfig) {
		t.Fatalf("extreme limit error = %v", err)
	}

	noSheets := rewriteZIPEntry(t, data, "xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheets/></workbook>`)
	_, err = OpenSpreadsheet(bytes.NewReader(noSheets), int64(len(noSheets)), SpreadsheetConfig{Format: FormatXLSX})
	if !errors.Is(err, ErrorSpreadsheet) {
		t.Fatalf("no-sheets error = %v", err)
	}

	missingSheet := removeZIPEntry(t, data, "xl/worksheets/sheet1.xml")
	_, err = OpenSpreadsheet(bytes.NewReader(missingSheet), int64(len(missingSheet)), SpreadsheetConfig{Format: FormatXLSX})
	if !errors.Is(err, ErrorSpreadsheet) {
		t.Fatalf("missing worksheet error = %v", err)
	}

	brokenRows := rewriteZIPEntry(t, data, "xl/worksheets/sheet1.xml", `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row>`)
	_, err = OpenSpreadsheet(bytes.NewReader(brokenRows), int64(len(brokenRows)), SpreadsheetConfig{Format: FormatXLSX})
	if !errors.Is(err, ErrorSpreadsheet) {
		t.Fatalf("broken rows error = %v", err)
	}

	unsupported := append([]byte(nil), data...)
	setZIPCompressionMethod(t, unsupported, "xl/worksheets/sheet1.xml", 99)
	_, err = OpenSpreadsheet(bytes.NewReader(unsupported), int64(len(unsupported)), SpreadsheetConfig{Format: FormatXLSX})
	if !errors.Is(err, ErrorArchive) {
		t.Fatalf("unsupported worksheet compression error = %v", err)
	}
}

func TestXLSXRowSourceSurfacesIteratorFailures(t *testing.T) {
	t.Parallel()

	wantRows := errors.New("row iterator failed")
	source := &xlsxRowSource{rows: &stubXLSXRows{rowErr: wantRows}}
	if _, err := source.Read(); !errors.Is(err, wantRows) {
		t.Fatalf("Read() iterator error = %v", err)
	}

	wantColumns := errors.New("columns failed")
	source = &xlsxRowSource{rows: &stubXLSXRows{next: true, columnsErr: wantColumns}}
	if _, err := source.Read(); !errors.Is(err, wantColumns) {
		t.Fatalf("Read() columns error = %v", err)
	}

	wantType := errors.New("cell type failed")
	source = &xlsxRowSource{
		rows:     &stubXLSXRows{next: true, values: []string{"#VALUE!"}},
		workbook: &stubXLSXWorkbook{typeErr: wantType},
		sheet:    "Data",
	}
	if _, err := source.Read(); !errors.Is(err, wantType) {
		t.Fatalf("Read() cell-type error = %v", err)
	}
}

func TestXLSXRowSourceSkipsCellTypeLookupForOrdinaryValues(t *testing.T) {
	t.Parallel()

	workbook := &stubXLSXWorkbook{}
	source := &xlsxRowSource{
		rows:     &stubXLSXRows{next: true, values: []string{"Alice", "Helsinki"}},
		workbook: workbook,
		sheet:    "Data",
	}
	row, err := source.Read()
	if err != nil {
		t.Fatal(err)
	}
	if workbook.typeCalls != 0 {
		t.Fatalf("GetCellType() calls = %d, want 0", workbook.typeCalls)
	}
	if row[0].value != "Alice" || row[1].value != "Helsinki" {
		t.Fatalf("Read() row = %#v", row)
	}
}

type stubSpreadsheetSource struct {
	rows     [][]spreadsheetCell
	readErr  error
	closeErr error
}

type failingReaderAt struct{ err error }

func (reader failingReaderAt) ReadAt([]byte, int64) (int, error) { return 0, reader.err }

type stubXLSXRows struct {
	next       bool
	rowErr     error
	columnsErr error
	values     []string
}

func (rows *stubXLSXRows) Next() bool {
	next := rows.next
	rows.next = false
	return next
}

func (rows *stubXLSXRows) Error() error { return rows.rowErr }

func (rows *stubXLSXRows) Columns(...excelize.Options) ([]string, error) {
	return rows.values, rows.columnsErr
}

func (*stubXLSXRows) Close() error { return nil }

type stubXLSXWorkbook struct {
	typeErr   error
	typeCalls int
}

func (workbook *stubXLSXWorkbook) GetCellType(string, string) (excelize.CellType, error) {
	workbook.typeCalls++
	return excelize.CellTypeUnset, workbook.typeErr
}

func (*stubXLSXWorkbook) Close() error { return nil }

func (source *stubSpreadsheetSource) Read() ([]spreadsheetCell, error) {
	if source.readErr != nil {
		return nil, source.readErr
	}
	if len(source.rows) == 0 {
		return nil, io.EOF
	}
	row := source.rows[0]
	source.rows = source.rows[1:]
	return row, nil
}

func (source *stubSpreadsheetSource) Close() error { return source.closeErr }

func rewriteZIPEntry(t *testing.T, data []byte, target, replacement string) []byte {
	t.Helper()
	return transformZIP(t, data, func(name string, contents []byte) ([]byte, bool) {
		if name == target {
			return []byte(replacement), true
		}
		return contents, true
	})
}

func removeZIPEntry(t *testing.T, data []byte, target string) []byte {
	t.Helper()
	return transformZIP(t, data, func(name string, contents []byte) ([]byte, bool) {
		return contents, name != target
	})
}

func transformZIP(t *testing.T, data []byte, transform func(string, []byte) ([]byte, bool)) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, file := range reader.File {
		entry, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(entry)
		if err := entry.Close(); err != nil {
			t.Fatal(err)
		}
		if err != nil {
			t.Fatal(err)
		}
		contents, keep := transform(file.Name, contents)
		if !keep {
			continue
		}
		destination, err := writer.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = destination.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func binaryLittleEndianPutUint16(destination []byte, value uint16) {
	destination[0] = byte(value)
	destination[1] = byte(value >> 8)
}

func closeTestResource(t *testing.T, resource io.Closer) {
	t.Helper()
	t.Cleanup(func() {
		if err := resource.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
}

func setZIPCompressionMethod(t *testing.T, data []byte, target string, method uint16) {
	t.Helper()
	for offset := 0; offset+30 <= len(data); {
		if !bytes.Equal(data[offset:offset+4], []byte{'P', 'K', 3, 4}) {
			break
		}
		nameLength := int(data[offset+26]) | int(data[offset+27])<<8
		extraLength := int(data[offset+28]) | int(data[offset+29])<<8
		compressedSize := int(data[offset+18]) | int(data[offset+19])<<8 |
			int(data[offset+20])<<16 | int(data[offset+21])<<24
		nameStart := offset + 30
		if string(data[nameStart:nameStart+nameLength]) == target {
			binaryLittleEndianPutUint16(data[offset+8:offset+10], method)
		}
		offset = nameStart + nameLength + extraLength + compressedSize
	}
	for offset := bytes.Index(data, []byte{'P', 'K', 1, 2}); offset >= 0 && offset+46 <= len(data); {
		nameLength := int(data[offset+28]) | int(data[offset+29])<<8
		extraLength := int(data[offset+30]) | int(data[offset+31])<<8
		commentLength := int(data[offset+32]) | int(data[offset+33])<<8
		nameStart := offset + 46
		if string(data[nameStart:nameStart+nameLength]) == target {
			binaryLittleEndianPutUint16(data[offset+10:offset+12], method)
			return
		}
		offset = nameStart + nameLength + extraLength + commentLength
		if offset+4 > len(data) || !bytes.Equal(data[offset:offset+4], []byte{'P', 'K', 1, 2}) {
			break
		}
	}
	t.Fatalf("ZIP entry %q not found", target)
}

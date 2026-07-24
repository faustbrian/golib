package tabular

import (
	"bytes"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestXLSReaderIngestsRealWorkbookFixture(t *testing.T) {
	t.Parallel()

	file, size := openSpreadsheetFixture(t, "testdata/spreadsheet/table.xls")
	closeTestResource(t, file)

	reader, err := OpenSpreadsheet(file, size, SpreadsheetConfig{
		Format: FormatXLS,
		Sheet:  "Table",
		Header: &HeaderConfig{
			Case:             HeaderCaseLower,
			RejectEmpty:      true,
			RejectDuplicates: true,
		},
	})
	if err != nil {
		t.Fatalf("OpenSpreadsheet() error = %v", err)
	}
	closeTestResource(t, reader)

	header, err := reader.Header()
	if err != nil {
		t.Fatalf("Header() error = %v", err)
	}
	if want := (Row{"code", "name", "description"}); !reflect.DeepEqual(header, want) {
		t.Fatalf("Header() = %#v, want %#v", header, want)
	}

	rows, err := readAllRows(reader)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	if len(rows) != 11 {
		t.Fatalf("row count = %d, want 11", len(rows))
	}
	if want := (Row{"code1", "name1", "description1"}); !reflect.DeepEqual(rows[0], want) {
		t.Fatalf("first row = %#v, want %#v", rows[0], want)
	}
	if want := (Row{"code11", "name11", "description11"}); !reflect.DeepEqual(rows[10], want) {
		t.Fatalf("last row = %#v, want %#v", rows[10], want)
	}

	header[0] = "changed"
	headerAgain, err := reader.Header()
	if err != nil || headerAgain[0] != "code" {
		t.Fatalf("second Header() = %#v, %v", headerAgain, err)
	}
}

func TestXLSReaderUsesFirstSheetWhenNameIsEmpty(t *testing.T) {
	t.Parallel()

	file, size := openSpreadsheetFixture(t, "testdata/spreadsheet/table.xls")
	closeTestResource(t, file)
	reader, err := OpenSpreadsheet(file, size, SpreadsheetConfig{Format: FormatXLS})
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, reader)
	row, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if want := (Row{"Code", "Name", "Description"}); !reflect.DeepEqual(row, want) {
		t.Fatalf("Read() = %#v, want %#v", row, want)
	}
}

func TestXLSReaderReportsMissingSheetsAndBrokenWorkbooks(t *testing.T) {
	t.Parallel()

	file, size := openSpreadsheetFixture(t, "testdata/spreadsheet/table.xls")
	closeTestResource(t, file)
	_, err := OpenSpreadsheet(file, size, SpreadsheetConfig{Format: FormatXLS, Sheet: "Missing"})
	if !errors.Is(err, ErrorSpreadsheet) {
		t.Fatalf("missing sheet error = %v, want spreadsheet kind", err)
	}

	broken, brokenSize := openSpreadsheetFixture(t, "testdata/spreadsheet/malformed.xls")
	closeTestResource(t, broken)
	_, err = OpenSpreadsheet(broken, brokenSize, SpreadsheetConfig{Format: FormatXLS})
	if !errors.Is(err, ErrorSpreadsheet) {
		t.Fatalf("broken workbook error = %v, want spreadsheet kind", err)
	}
}

func TestXLSReaderEnforcesMaterializationLimit(t *testing.T) {
	t.Parallel()

	file, size := openSpreadsheetFixture(t, "testdata/spreadsheet/table.xls")
	closeTestResource(t, file)
	_, err := OpenSpreadsheet(file, size, SpreadsheetConfig{
		Format:           FormatXLS,
		MaxWorkbookBytes: size - 1,
	})
	if !errors.Is(err, ErrorLimitExceeded) {
		t.Fatalf("OpenSpreadsheet() error = %v, want limit-exceeded kind", err)
	}
}

func TestSpreadsheetValidatesCommonConfiguration(t *testing.T) {
	t.Parallel()

	valid := strings.NewReader("data")
	tests := []struct {
		name   string
		source io.ReaderAt
		size   int64
		config SpreadsheetConfig
	}{
		{name: "nil source", size: 0, config: SpreadsheetConfig{Format: FormatXLS}},
		{name: "negative size", source: valid, size: -1, config: SpreadsheetConfig{Format: FormatXLS}},
		{name: "unknown format", source: valid, size: 4, config: SpreadsheetConfig{Format: SpreadsheetFormat("ods")}},
		{name: "negative fields", source: valid, size: 4, config: SpreadsheetConfig{Format: FormatXLS, FieldsPerRecord: -1}},
		{name: "negative limit", source: valid, size: 4, config: SpreadsheetConfig{Format: FormatXLS, MaxWorkbookBytes: -1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := OpenSpreadsheet(test.source, test.size, test.config)
			if !errors.Is(err, ErrorInvalidConfig) {
				t.Fatalf("OpenSpreadsheet() error = %v, want invalid-config kind", err)
			}
		})
	}
}

func TestSpreadsheetReaderRejectsWrongRowShape(t *testing.T) {
	t.Parallel()

	file, size := openSpreadsheetFixture(t, "testdata/spreadsheet/table.xls")
	closeTestResource(t, file)
	reader, err := OpenSpreadsheet(file, size, SpreadsheetConfig{
		Format:          FormatXLS,
		FieldsPerRecord: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, reader)
	_, err = reader.Read()
	if !errors.Is(err, ErrorMalformedRow) {
		t.Fatalf("Read() error = %v, want malformed-row kind", err)
	}
}

func TestSpreadsheetCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	file, size := openSpreadsheetFixture(t, "testdata/spreadsheet/table.xls")
	closeTestResource(t, file)
	reader, err := OpenSpreadsheet(file, size, SpreadsheetConfig{Format: FormatXLS})
	if err != nil {
		t.Fatal(err)
	}
	if err = reader.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err = reader.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if _, err = reader.Read(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Read() after close error = %v, want closed pipe", err)
	}
}

func FuzzOpenXLS(f *testing.F) {
	data, err := os.ReadFile("testdata/spreadsheet/table.xls")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Fuzz(func(_ *testing.T, data []byte) {
		reader, err := OpenSpreadsheet(bytes.NewReader(data), int64(len(data)), SpreadsheetConfig{
			Format:           FormatXLS,
			MaxWorkbookBytes: 1024 * 1024,
		})
		if err != nil {
			return
		}
		defer func() { _ = reader.Close() }()
		for {
			if _, err = reader.Read(); err != nil {
				return
			}
		}
	})
}

func BenchmarkXLSReader(b *testing.B) {
	data, err := os.ReadFile("testdata/spreadsheet/table.xls")
	if err != nil {
		b.Fatal(err)
	}
	data = append(data, make([]byte, 8*1024*1024-len(data))...)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for range b.N {
		reader, err := OpenSpreadsheet(strings.NewReader(string(data)), int64(len(data)), SpreadsheetConfig{Format: FormatXLS})
		if err != nil {
			b.Fatal(err)
		}
		if err = consumeRows(reader); err != nil {
			b.Fatal(err)
		}
		if err = reader.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func openSpreadsheetFixture(t *testing.T, name string) (*os.File, int64) {
	t.Helper()
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	return file, info.Size()
}

package tabular

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestXLSXReaderStreamsRawFixtureRows(t *testing.T) {
	t.Parallel()

	file, size := openSpreadsheetFixture(t, "testdata/spreadsheet/sample.xlsx")
	closeTestResource(t, file)
	reader, err := OpenSpreadsheet(file, size, SpreadsheetConfig{
		Format: FormatXLSX,
		Sheet:  "Orders",
		Header: &HeaderConfig{Case: HeaderCaseLower, RejectEmpty: true, RejectDuplicates: true},
	})
	if err != nil {
		t.Fatalf("OpenSpreadsheet() error = %v", err)
	}
	closeTestResource(t, reader)

	header, err := reader.Header()
	if err != nil {
		t.Fatalf("Header() error = %v", err)
	}
	if want := (Row{"name", "city", "amount", "active"}); !reflect.DeepEqual(header, want) {
		t.Fatalf("Header() = %#v, want %#v", header, want)
	}
	rows, err := readAllRows(reader)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	want := []Row{
		{"Alice", "Helsinki", "12.5", "1"},
		{"Björk", "Reykjavík", "25", "0"},
		{"Sparse", "", "", ""},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}
}

func TestXLSXReaderRejectsOrPreservesCellErrorsExplicitly(t *testing.T) {
	t.Parallel()

	data := makeErrorXLSX(t)
	for _, test := range []struct {
		name     string
		preserve bool
	}{
		{name: "reject"},
		{name: "preserve", preserve: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reader, err := OpenSpreadsheet(bytes.NewReader(data), int64(len(data)), SpreadsheetConfig{
				Format:             FormatXLSX,
				Header:             &HeaderConfig{},
				PreserveCellErrors: test.preserve,
			})
			if err != nil {
				t.Fatal(err)
			}
			closeTestResource(t, reader)
			if _, err = reader.Header(); err != nil {
				t.Fatal(err)
			}
			row, err := reader.Read()
			if test.preserve {
				if err != nil || !reflect.DeepEqual(row, Row{"#DIV/0!"}) {
					t.Fatalf("Read() = %#v, %v", row, err)
				}
				return
			}
			if !errors.Is(err, ErrorSpreadsheet) {
				t.Fatalf("Read() error = %v, want spreadsheet kind", err)
			}
		})
	}
}

func TestXLSXReaderReportsMissingSheetsBrokenFilesAndLimits(t *testing.T) {
	t.Parallel()

	file, size := openSpreadsheetFixture(t, "testdata/spreadsheet/sample.xlsx")
	closeTestResource(t, file)
	_, err := OpenSpreadsheet(file, size, SpreadsheetConfig{Format: FormatXLSX, Sheet: "Missing"})
	if !errors.Is(err, ErrorSpreadsheet) {
		t.Fatalf("missing sheet error = %v, want spreadsheet kind", err)
	}

	broken, brokenSize := openSpreadsheetFixture(t, "testdata/spreadsheet/malformed.xlsx")
	closeTestResource(t, broken)
	_, err = OpenSpreadsheet(broken, brokenSize, SpreadsheetConfig{Format: FormatXLSX})
	if !errors.Is(err, ErrorArchive) {
		t.Fatalf("broken workbook error = %v, want archive kind", err)
	}

	file, size = openSpreadsheetFixture(t, "testdata/spreadsheet/sample.xlsx")
	closeTestResource(t, file)
	_, err = OpenSpreadsheet(file, size, SpreadsheetConfig{
		Format: FormatXLSX,
		ZIP:    ZIPConfig{MaxEntryBytes: 32},
	})
	if !errors.Is(err, ErrorLimitExceeded) {
		t.Fatalf("limited workbook error = %v, want limit-exceeded kind", err)
	}
}

func TestXLSXCloseStopsIteration(t *testing.T) {
	t.Parallel()

	file, size := openSpreadsheetFixture(t, "testdata/spreadsheet/sample.xlsx")
	closeTestResource(t, file)
	reader, err := OpenSpreadsheet(file, size, SpreadsheetConfig{Format: FormatXLSX})
	if err != nil {
		t.Fatal(err)
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.Read(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("Read() error = %v, want closed pipe", err)
	}
	if err = reader.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func FuzzOpenSpreadsheet(f *testing.F) {
	data := makeErrorXLSX(f)
	f.Add(data)
	f.Fuzz(func(_ *testing.T, data []byte) {
		reader, err := OpenSpreadsheet(bytes.NewReader(data), int64(len(data)), SpreadsheetConfig{
			Format: FormatXLSX,
			ZIP:    ZIPConfig{MaxEntries: 20, MaxEntryBytes: 4096, MaxTotalBytes: 16384},
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

func BenchmarkXLSXReader(b *testing.B) {
	workbook := excelize.NewFile()
	sheet := workbook.GetSheetName(0)
	stream, err := workbook.NewStreamWriter(sheet)
	if err != nil {
		b.Fatal(err)
	}
	for row := 1; row <= 10_000; row++ {
		if err = stream.SetRow(fmt.Sprintf("A%d", row), []any{row, "Alice", "Helsinki"}); err != nil {
			b.Fatal(err)
		}
	}
	if err = stream.Flush(); err != nil {
		b.Fatal(err)
	}
	var output bytes.Buffer
	if err = workbook.Write(&output); err != nil {
		b.Fatal(err)
	}
	if err = workbook.Close(); err != nil {
		b.Fatal(err)
	}
	data := output.Bytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for range b.N {
		reader, err := OpenSpreadsheet(bytes.NewReader(data), int64(len(data)), SpreadsheetConfig{Format: FormatXLSX})
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

func makeErrorXLSX(t testingTB) []byte {
	t.Helper()
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`,
		"xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="Errors" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>Value</t></is></c></row>
<row r="2"><c r="A2" t="e"><v>#DIV/0!</v></c></row>
</sheetData></worksheet>`,
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, contents := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(entry, contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

package tabular

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
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

func TestXLSXReaderPreservesAbsentAndExplicitEmptyCells(t *testing.T) {
	t.Parallel()

	data := rewriteZIPEntry(
		t,
		makeErrorXLSX(t),
		"xl/worksheets/sheet1.xml",
		`<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1">
<c r="A1" t="inlineStr"><is><t>Absent</t></is></c>
<c r="B1" t="inlineStr"><is><t>Empty</t></is></c>
<c r="C1" t="inlineStr"><is><t>Value</t></is></c>
<c r="D1" t="inlineStr"><is><t>Trailing</t></is></c>
</row>
<row r="2">
<c r="A2"><v>42</v></c>
<c r="C2" t="inlineStr"><is><t></t></is></c>
</row>
</sheetData></worksheet>`,
	)
	reader, err := OpenSpreadsheet(
		bytes.NewReader(data),
		int64(len(data)),
		SpreadsheetConfig{
			Format:               FormatXLSX,
			Header:               &HeaderConfig{Case: HeaderCaseLower},
			PreserveCellPresence: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, reader)

	row, err := reader.ReadCells()
	if err != nil {
		t.Fatal(err)
	}
	if len(row) != 4 ||
		!row[0].Present() ||
		row[0].Value() != "42" ||
		row[1].Present() ||
		row[1].Value() != "" ||
		!row[2].Present() ||
		row[2].Value() != "" ||
		row[3].Present() ||
		row[3].Value() != "" {
		t.Fatalf("cells = %#v", row)
	}

	disabled, err := OpenSpreadsheet(
		bytes.NewReader(data),
		int64(len(data)),
		SpreadsheetConfig{Format: FormatXLSX},
	)
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, disabled)
	if _, err = disabled.ReadCells(); !errors.Is(err, ErrorInvalidConfig) {
		t.Fatalf("disabled ReadCells() error = %v", err)
	}
}

func TestXLSXReaderPadsMissingRowsWithAbsentCells(t *testing.T) {
	t.Parallel()

	data := rewriteZIPEntry(
		t,
		makeErrorXLSX(t),
		"xl/worksheets/sheet1.xml",
		`<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>First</t></is></c>
<c r="B1" t="inlineStr"><is><t>Second</t></is></c></row>
<row r="3"><c r="A3" t="inlineStr"><is><t>later</t></is></c></row>
</sheetData></worksheet>`,
	)
	reader, err := OpenSpreadsheet(
		bytes.NewReader(data),
		int64(len(data)),
		SpreadsheetConfig{
			Format:               FormatXLSX,
			Header:               &HeaderConfig{},
			PreserveCellPresence: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, reader)
	gap, err := reader.ReadCells()
	if err != nil {
		t.Fatal(err)
	}
	if len(gap) != 2 ||
		gap[0].Present() ||
		gap[1].Present() ||
		gap[0].Value() != "" ||
		gap[1].Value() != "" {
		t.Fatalf("gap row = %#v", gap)
	}
	later, err := reader.ReadCells()
	if err != nil ||
		len(later) != 2 ||
		!later[0].Present() ||
		later[0].Value() != "later" ||
		later[1].Present() {
		t.Fatalf("later row = %#v, %v", later, err)
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

	data := makeMultiSheetXLSX(t)
	_, err = OpenSpreadsheet(
		bytes.NewReader(data),
		int64(len(data)),
		SpreadsheetConfig{Format: FormatXLSX, MaxSheets: 1},
	)
	if !errors.Is(err, ErrorLimitExceeded) {
		t.Fatalf("sheet limit error = %v, want limit-exceeded kind", err)
	}

	data = renameZIPEntry(
		t,
		makeMultiSheetXLSX(t),
		"xl/worksheets/sheet2.xml",
		"xl/custom/sheet2.xml",
	)
	data = transformZIP(t, data, func(name string, contents []byte) ([]byte, bool) {
		if name == "xl/_rels/workbook.xml.rels" {
			contents = []byte(strings.ReplaceAll(
				string(contents),
				"worksheets/sheet2.xml",
				"custom/sheet2.xml",
			))
		}
		return contents, true
	})
	_, err = OpenSpreadsheet(
		bytes.NewReader(data),
		int64(len(data)),
		SpreadsheetConfig{Format: FormatXLSX, MaxSheets: 1},
	)
	if !errors.Is(err, ErrorLimitExceeded) {
		t.Fatalf(
			"related sheet limit error = %v, want limit-exceeded kind",
			err,
		)
	}

	data = rewriteZIPEntry(
		t,
		makeMultiSheetXLSX(t),
		"xl/worksheets/sheet2.xml",
		"<broken",
	)
	_, err = OpenSpreadsheet(
		bytes.NewReader(data),
		int64(len(data)),
		SpreadsheetConfig{Format: FormatXLSX, MaxSheets: 1},
	)
	if !errors.Is(err, ErrorLimitExceeded) {
		t.Fatalf(
			"early sheet limit error = %v, want limit-exceeded kind",
			err,
		)
	}

	data = removeZIPEntry(t, makeErrorXLSX(t), "xl/workbook.xml")
	_, err = OpenSpreadsheet(
		bytes.NewReader(data),
		int64(len(data)),
		SpreadsheetConfig{Format: FormatXLSX, MaxSheets: 1},
	)
	if !errors.Is(err, ErrorEntryNotFound) {
		t.Fatalf(
			"missing workbook manifest error = %v, want entry-not-found kind",
			err,
		)
	}

	data = rewriteZIPEntry(
		t,
		makeErrorXLSX(t),
		"xl/workbook.xml",
		"<broken",
	)
	_, err = OpenSpreadsheet(
		bytes.NewReader(data),
		int64(len(data)),
		SpreadsheetConfig{Format: FormatXLSX, MaxSheets: 1},
	)
	if !errors.Is(err, ErrorSpreadsheet) {
		t.Fatalf(
			"broken workbook manifest error = %v, want spreadsheet kind",
			err,
		)
	}

	data = addZIPEntry(
		t,
		makeErrorXLSX(t),
		"xl/worksheets/orphan.xml",
		`<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData/></worksheet>`,
	)
	reader, err := OpenSpreadsheet(
		bytes.NewReader(data),
		int64(len(data)),
		SpreadsheetConfig{Format: FormatXLSX, MaxSheets: 1},
	)
	if err != nil {
		t.Fatalf("orphan worksheet OpenSpreadsheet() error = %v", err)
	}
	closeTestResource(t, reader)
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
	if _, err = reader.ReadCells(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("ReadCells() error = %v, want closed pipe", err)
	}
	if err = reader.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func FuzzOpenSpreadsheet(f *testing.F) {
	data := makeErrorXLSX(f)
	f.Add(data)
	f.Add(rewriteZIPEntry(
		f,
		data,
		"xl/worksheets/sheet1.xml",
		`<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData><row r="2"><c r="A2"><v>42</v></c>
<c r="C2" t="inlineStr"><is><t></t></is></c></row></sheetData>
</worksheet>`,
	))
	f.Add(transformZIP(
		f,
		data,
		func(name string, contents []byte) ([]byte, bool) {
			if name == "xl/workbook.xml" {
				contents = []byte(strings.Replace(
					string(contents),
					"<sheets>",
					`<sheet name="Errors" r:id="rFake"/><sheets>`,
					1,
				))
			}
			return contents, true
		},
	))
	f.Fuzz(func(_ *testing.T, data []byte) {
		reader, err := OpenSpreadsheet(bytes.NewReader(data), int64(len(data)), SpreadsheetConfig{
			Format:               FormatXLSX,
			PreserveCellPresence: true,
			ZIP: ZIPConfig{
				MaxEntries:    20,
				MaxEntryBytes: 4096,
				MaxTotalBytes: 16384,
			},
		})
		if err != nil {
			return
		}
		defer func() { _ = reader.Close() }()
		for {
			if _, err = reader.ReadCells(); err != nil {
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

func makeMultiSheetXLSX(t testingTB) []byte {
	t.Helper()

	workbook := excelize.NewFile()
	if _, err := workbook.NewSheet("Second"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := workbook.Write(&output); err != nil {
		t.Fatal(err)
	}
	if err := workbook.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func renameZIPEntry(t testingTB, data []byte, oldName, newName string) []byte {
	t.Helper()

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	found := false
	for _, file := range reader.File {
		name := file.Name
		if name == oldName {
			name = newName
			found = true
		}
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		source, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.Copy(entry, source); err != nil {
			_ = source.Close()
			t.Fatal(err)
		}
		if err = source.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if !found {
		t.Fatal(fmt.Sprintf("ZIP entry %q not found", oldName))
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}

	return output.Bytes()
}

func addZIPEntry(t testingTB, data []byte, name, contents string) []byte {
	t.Helper()

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, file := range reader.File {
		entry, err := writer.CreateHeader(&file.FileHeader)
		if err != nil {
			t.Fatal(err)
		}
		source, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.Copy(entry, source); err != nil {
			_ = source.Close()
			t.Fatal(err)
		}
		if err = source.Close(); err != nil {
			t.Fatal(err)
		}
	}
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(entry, contents); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}

	return output.Bytes()
}

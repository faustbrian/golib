package tabular

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestXLSXPresenceSourceStreamsMissingAndDeclaredRows(t *testing.T) {
	t.Parallel()

	source := newTestXLSXPresenceSource(`
<worksheet><sheetData>
<row r="3" custom="value"><c r="B3"/><c/><c r="A3"/><ignored/></row>
<row><c/></row>
</sheetData></worksheet>`)
	first, err := source.Read()
	if err != nil || first != nil {
		t.Fatalf("first Read() = %#v, %v", first, err)
	}
	second, err := source.Read()
	if err != nil || second != nil {
		t.Fatalf("second Read() = %#v, %v", second, err)
	}
	third, err := source.Read()
	if err != nil || !reflect.DeepEqual(third, []bool{true, true, true}) {
		t.Fatalf("third Read() = %#v, %v", third, err)
	}
	fourth, err := source.Read()
	if err != nil || !reflect.DeepEqual(fourth, []bool{true}) {
		t.Fatalf("fourth Read() = %#v, %v", fourth, err)
	}
	if _, err = source.Read(); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal Read() error = %v", err)
	}
	if err = source.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	inconsistent := newTestXLSXPresenceSource("<worksheet/>")
	inconsistent.currentRow = 2
	inconsistent.pendingRow = 1
	if _, err = inconsistent.Read(); err == nil {
		t.Fatal("Read() accepted an out-of-order pending row")
	}

	ahead := newTestXLSXPresenceSource(`
<worksheet><sheetData><row r="1"/></sheetData></worksheet>`)
	ahead.currentRow = 2
	if _, err = ahead.Read(); err == nil {
		t.Fatal("Read() accepted a declared row behind its cursor")
	}
}

func TestXLSXPresenceSourceRejectsMalformedRowsAndCells(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"invalid row number": `
<worksheet><sheetData><row r="invalid"/></sheetData></worksheet>`,
		"zero row number": `
<worksheet><sheetData><row r="0"/></sheetData></worksheet>`,
		"cell row mismatch": `
<worksheet><sheetData><row r="1"><c r="A2"/></row></sheetData></worksheet>`,
		"invalid cell reference": `
<worksheet><sheetData><row r="1"><c r="invalid"/></row></sheetData></worksheet>`,
		"malformed cell": `
<worksheet><sheetData><row r="1"><c r="A1"><</row></sheetData></worksheet>`,
		"malformed row": `
<worksheet><sheetData><row r="1"><`,
		"malformed ignored element": `
<worksheet><sheetData><row r="1"><ignored><`,
	}
	for name, document := range tests {
		name, document := name, document
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := newTestXLSXPresenceSource(document).Read(); err == nil {
				t.Fatal("Read() accepted malformed worksheet XML")
			}
		})
	}

	unordered := newTestXLSXPresenceSource(`
<worksheet><sheetData><row r="2"/><row r="1"/></sheetData></worksheet>`)
	if row, _, err := unordered.nextDeclaredRow(); err != nil || row != 2 {
		t.Fatalf("first nextDeclaredRow() = %d, %v", row, err)
	}
	if _, _, err := unordered.nextDeclaredRow(); err == nil {
		t.Fatal("nextDeclaredRow() accepted unordered declarations")
	}

	empty := newTestXLSXPresenceSource(
		"<worksheet><sheetData/></worksheet>",
	)
	if _, _, err := empty.nextDeclaredRow(); !errors.Is(err, io.EOF) {
		t.Fatalf("empty nextDeclaredRow() error = %v", err)
	}
	if _, _, err := newTestXLSXPresenceSource("<").nextDeclaredRow(); err == nil {
		t.Fatal("nextDeclaredRow() accepted malformed XML")
	}
}

func TestXLSXPresenceSelectsWorksheetRelationships(t *testing.T) {
	t.Parallel()

	base := makeErrorXLSX(t)
	archive := openTestXLSXArchive(t, base)
	if entry, err := selectedXLSXWorksheetEntry(archive, "Errors"); err != nil ||
		entry != "xl/worksheets/sheet1.xml" {
		t.Fatalf("selected entry = %q, %v", entry, err)
	}
	if _, err := openXLSXPresence(archive, "Missing"); err == nil {
		t.Fatal("openXLSXPresence() accepted a missing sheet")
	}

	decoy := transformZIP(
		t,
		base,
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
	)
	if entry, err := selectedXLSXWorksheetEntry(
		openTestXLSXArchive(t, decoy),
		"Errors",
	); err != nil || entry != "xl/worksheets/sheet1.xml" {
		t.Fatalf("decoy selected entry = %q, %v", entry, err)
	}

	absolute := transformZIP(
		t,
		base,
		func(name string, contents []byte) ([]byte, bool) {
			if name == "xl/_rels/workbook.xml.rels" {
				contents = []byte(strings.Replace(
					string(contents),
					`Target="worksheets/sheet1.xml"`,
					`Target="/xl/worksheets/sheet1.xml"`,
					1,
				))
			}
			return contents, true
		},
	)
	if entry, err := selectedXLSXWorksheetEntry(
		openTestXLSXArchive(t, absolute),
		"",
	); err != nil || entry != "xl/worksheets/sheet1.xml" {
		t.Fatalf("absolute selected entry = %q, %v", entry, err)
	}

	tests := map[string]struct {
		data []byte
		open bool
	}{
		"missing workbook": {
			data: removeZIPEntry(t, base, "xl/workbook.xml"),
		},
		"malformed workbook": {
			data: rewriteZIPEntry(t, base, "xl/workbook.xml", "<"),
		},
		"invalid declaration": {
			data: rewriteZIPEntry(
				t,
				base,
				"xl/workbook.xml",
				`<workbook><sheets><sheet name="Errors"/></sheets></workbook>`,
			),
		},
		"missing declaration": {data: base},
		"missing relationships": {
			data: removeZIPEntry(t, base, "xl/_rels/workbook.xml.rels"),
		},
		"malformed relationships": {
			data: rewriteZIPEntry(
				t,
				base,
				"xl/_rels/workbook.xml.rels",
				"<",
			),
		},
		"invalid relationship": {
			data: transformZIP(
				t,
				base,
				func(name string, contents []byte) ([]byte, bool) {
					if name == "xl/_rels/workbook.xml.rels" {
						contents = []byte(strings.Replace(
							string(contents),
							"/worksheet\"",
							"/external\"",
							1,
						))
					}
					return contents, true
				},
			),
		},
		"external relationship": {
			data: transformZIP(
				t,
				base,
				func(name string, contents []byte) ([]byte, bool) {
					if name == "xl/_rels/workbook.xml.rels" {
						contents = []byte(strings.Replace(
							string(contents),
							`Target="worksheets/sheet1.xml"`,
							`Target="worksheets/sheet1.xml" TargetMode="External"`,
							1,
						))
					}
					return contents, true
				},
			),
		},
		"missing relationship": {
			data: transformZIP(
				t,
				base,
				func(name string, contents []byte) ([]byte, bool) {
					if name == "xl/_rels/workbook.xml.rels" {
						contents = []byte(strings.Replace(
							string(contents),
							`Id="rId1"`,
							`Id="rId2"`,
							1,
						))
					}
					return contents, true
				},
			),
		},
		"unsafe target": {
			data: transformZIP(
				t,
				base,
				func(name string, contents []byte) ([]byte, bool) {
					if name == "xl/_rels/workbook.xml.rels" {
						contents = []byte(strings.Replace(
							string(contents),
							"worksheets/sheet1.xml",
							"../../../outside.xml",
							1,
						))
					}
					return contents, true
				},
			),
		},
		"missing target": {
			data: transformZIP(
				t,
				base,
				func(name string, contents []byte) ([]byte, bool) {
					if name == "xl/_rels/workbook.xml.rels" {
						contents = []byte(strings.Replace(
							string(contents),
							"worksheets/sheet1.xml",
							"worksheets/missing.xml",
							1,
						))
					}
					return contents, true
				},
			),
			open: true,
		},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			archive := openTestXLSXArchive(t, test.data)
			var err error
			switch {
			case name == "missing declaration":
				_, err = selectedXLSXWorksheetEntry(archive, "Missing")
			case test.open:
				_, err = openXLSXPresence(archive, "Errors")
			default:
				_, err = selectedXLSXWorksheetEntry(archive, "Errors")
			}
			if err == nil {
				t.Fatal("worksheet selection accepted malformed metadata")
			}
		})
	}
}

func TestOpenXLSXRowsClosesResourcesAfterPresenceFailure(t *testing.T) {
	t.Parallel()

	data := transformZIP(
		t,
		makeErrorXLSX(t),
		func(name string, contents []byte) ([]byte, bool) {
			if name == "xl/_rels/workbook.xml.rels" {
				contents = []byte(strings.Replace(
					string(contents),
					`Target="worksheets/sheet1.xml"`,
					`Target="worksheets/sheet1.xml" TargetMode="External"`,
					1,
				))
			}
			return contents, true
		},
	)
	_, err := OpenSpreadsheet(
		bytes.NewReader(data),
		int64(len(data)),
		SpreadsheetConfig{
			Format:               FormatXLSX,
			PreserveCellPresence: true,
		},
	)
	if !errors.Is(err, ErrorSpreadsheet) {
		t.Fatalf("OpenSpreadsheet() error = %v", err)
	}
}

func TestXLSXPresenceFollowsNamedWorksheetRelationship(t *testing.T) {
	t.Parallel()

	data := rewriteZIPEntry(
		t,
		makeMultiSheetXLSX(t),
		"xl/worksheets/sheet2.xml",
		`<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>Value</t></is></c>
<c r="B1" t="inlineStr"><is><t>Missing</t></is></c>
<c r="C1" t="inlineStr"><is><t>Empty</t></is></c></row>
<row r="2"><c r="A2"><v>42</v></c>
<c r="C2" t="inlineStr"><is><t></t></is></c></row>
</sheetData></worksheet>`,
	)
	reader, err := OpenSpreadsheet(
		bytes.NewReader(data),
		int64(len(data)),
		SpreadsheetConfig{
			Format:               FormatXLSX,
			Sheet:                "Second",
			Header:               &HeaderConfig{},
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
	if len(row) != 3 ||
		!row[0].Present() ||
		row[0].Value() != "42" ||
		row[1].Present() ||
		!row[2].Present() ||
		row[2].Value() != "" {
		t.Fatalf("named-sheet cells = %#v", row)
	}
}

func newTestXLSXPresenceSource(document string) *xlsxPresenceSource {
	reader := io.NopCloser(strings.NewReader(document))
	return &xlsxPresenceSource{
		reader:  reader,
		decoder: xmlDecoder(reader),
	}
}

func xmlDecoder(reader io.Reader) *xml.Decoder {
	return xml.NewDecoder(reader)
}

func openTestXLSXArchive(t *testing.T, data []byte) *ZIPArchive {
	t.Helper()
	archive, err := OpenZIP(bytes.NewReader(data), int64(len(data)), ZIPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

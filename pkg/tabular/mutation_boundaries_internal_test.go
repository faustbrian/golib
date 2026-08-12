package tabular

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestZIPArchiveExactResourceBoundaries(t *testing.T) {
	t.Parallel()

	empty, err := OpenZIP(bytes.NewReader(nil), 0, ZIPConfig{})
	if empty != nil || !errors.Is(err, ErrorArchive) || errors.Is(err, ErrorInvalidConfig) {
		t.Fatalf("OpenZIP(empty) = %#v, %v, want archive error", empty, err)
	}

	one := makeZIP(t, map[string]string{"a": "x"})
	for name, config := range map[string]ZIPConfig{
		"entry count":                         {MaxEntries: 1},
		"entry bytes":                         {MaxEntryBytes: 1},
		"total bytes":                         {MaxTotalBytes: 1},
		"regular file with symlink rejection": {RejectSymlinks: true},
	} {
		archive, openErr := OpenZIP(bytes.NewReader(one), int64(len(one)), config)
		if openErr != nil || archive == nil || len(archive.Entries()) != 1 {
			t.Fatalf("%s exact boundary = %#v, %v", name, archive, openErr)
		}
	}

	three := makeOrderedZIP(t, []zipFixtureEntry{
		{name: "a", body: "x"}, {name: "b", body: "y"}, {name: "c", body: "z"},
	})
	if archive, openErr := OpenZIP(bytes.NewReader(three), int64(len(three)), ZIPConfig{MaxTotalBytes: 2}); archive != nil || !errors.Is(openErr, ErrorLimitExceeded) {
		t.Fatalf("combined total limit = %#v, %v", archive, openErr)
	}
}

func TestZIPArchiveRejectsSymlinkAndEveryUnsafeNameClass(t *testing.T) {
	t.Parallel()

	symlink := makeOrderedZIP(t, []zipFixtureEntry{{name: "link", mode: os.ModeSymlink, body: "target"}})
	if archive, err := OpenZIP(bytes.NewReader(symlink), int64(len(symlink)), ZIPConfig{RejectSymlinks: true}); archive != nil || !errors.Is(err, ErrorArchive) {
		t.Fatalf("OpenZIP(symlink) = %#v, %v", archive, err)
	}

	for _, name := range []string{"", `dir\\file`, "/absolute", "../escape", "dir/../escape", "//"} {
		if safeZIPName(name) {
			t.Fatalf("safeZIPName(%q) = true", name)
		}
	}
	for _, name := range []string{"file", "dir/file", "dir/"} {
		if !safeZIPName(name) {
			t.Fatalf("safeZIPName(%q) = false", name)
		}
	}
}

func TestFixedWidthExactLayoutAndRecordBoundaries(t *testing.T) {
	t.Parallel()
	_, err := NewFixedWidthReader(strings.NewReader(""), FixedWidthConfig{Fields: []FixedWidthField{
		{Name: "valid", Start: 0, End: 1},
		{Name: "invalid", Start: 0, End: 2},
	}})
	var tabularErr *Error
	if !errors.As(err, &tabularErr) || tabularErr.Field != 2 {
		t.Fatalf("invalid second field layout = %#v", err)
	}

	reader, err := NewFixedWidthReader(strings.NewReader("ab\n"), FixedWidthConfig{
		Fields:              []FixedWidthField{{Name: "a", Start: 0, End: 1}, {Name: "b", Start: 1, End: 2}},
		MaxRecordBytes:      3,
		RejectTrailingBytes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := reader.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"a", "b"}) {
		t.Fatalf("Read(exact width) = %#v, %v", row, err)
	}
	limited, err := NewFixedWidthReader(strings.NewReader("toolong\n"), FixedWidthConfig{
		Fields:         []FixedWidthField{{Name: "value", Start: 0, End: 1}},
		MaxRecordBytes: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = limited.Read()
	if !errors.As(err, &tabularErr) || tabularErr.Row != 1 || !errors.Is(err, ErrorLimitExceeded) {
		t.Fatalf("record limit error = %#v", err)
	}

	short, err := NewFixedWidthReader(strings.NewReader("a\n"), FixedWidthConfig{
		Fields:            []FixedWidthField{{Name: "a", Start: 0, End: 1}, {Name: "missing", Start: 1, End: 2}},
		AllowShortRecords: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err = short.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"a", ""}) {
		t.Fatalf("Read(field starting at record end) = %#v, %v", row, err)
	}

	invalid, err := NewFixedWidthReader(bytes.NewReader([]byte{'a', 0xff, '\n'}), FixedWidthConfig{
		Encoding: EncodingUTF8,
		Fields: []FixedWidthField{
			{Name: "first", Start: 0, End: 1},
			{Name: "invalid", Start: 1, End: 2},
		},
		AllowShortRecords: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = invalid.Read()
	if !errors.As(err, &tabularErr) || tabularErr.Field != 2 || tabularErr.Row != 1 {
		t.Fatalf("invalid second field error = %#v", err)
	}

	if value, extractErr := ExtractBytes([]byte("ab"), 0, 2); extractErr != nil || string(value) != "ab" {
		t.Fatalf("ExtractBytes(exact end) = %q, %v", value, extractErr)
	}
}

func TestSpreadsheetReaderExactStateAndLimitBoundaries(t *testing.T) {
	t.Parallel()

	reader := newSpreadsheetReader(&stubSpreadsheetSource{rows: [][]spreadsheetCell{
		{{value: "header"}},
		{{value: "value"}},
	}}, SpreadsheetConfig{Format: FormatXLSX, Header: &HeaderConfig{}})
	header, err := reader.Header()
	if err != nil || !reflect.DeepEqual(header, Row{"header"}) || reader.fields != 1 {
		t.Fatalf("Header() = %#v, %v, fields=%d", header, err, reader.fields)
	}
	row, err := reader.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"value"}) || reader.index != 2 {
		t.Fatalf("Read() = %#v, %v, index=%d", row, err, reader.index)
	}
	for name, configured := range map[string]*SpreadsheetReader{
		"header error": newSpreadsheetReader(&stubSpreadsheetSource{rows: [][]spreadsheetCell{{
			{value: "name"}, {value: "Name"},
		}}}, SpreadsheetConfig{Format: FormatXLSX, Header: &HeaderConfig{
			Case: HeaderCaseLower, RejectDuplicates: true,
		}}),
		"configured fields": newSpreadsheetReader(&stubSpreadsheetSource{rows: [][]spreadsheetCell{{
			{value: "header"},
		}}}, SpreadsheetConfig{Format: FormatXLSX, Header: &HeaderConfig{}, FieldsPerRecord: 2}),
		"variable fields": newSpreadsheetReader(&stubSpreadsheetSource{rows: [][]spreadsheetCell{{
			{value: "header"},
		}}}, SpreadsheetConfig{Format: FormatXLSX, Header: &HeaderConfig{}, AllowVariableFields: true}),
	} {
		_, _ = configured.Header()
		wantFields := 0
		if name == "configured fields" {
			wantFields = 2
		}
		if configured.fields != wantFields {
			t.Fatalf("%s inferred fields = %d, want %d", name, configured.fields, wantFields)
		}
	}

	wantRead := errors.New("read failed")
	failing := newSpreadsheetReader(&stubSpreadsheetSource{readErr: wantRead}, SpreadsheetConfig{Format: FormatXLSX})
	_, err = failing.Read()
	var tabularErr *Error
	if !errors.As(err, &tabularErr) || tabularErr.Row != 1 || !errors.Is(err, wantRead) {
		t.Fatalf("first source error = %#v", err)
	}

	presence := newSpreadsheetReader(&stubSpreadsheetSource{
		rows: [][]spreadsheetCell{{{value: "value"}}}, presence: [][]bool{{true}},
	}, SpreadsheetConfig{Format: FormatXLSX, PreserveCellPresence: true})
	cells, err := presence.ReadCells()
	if err != nil || len(cells) != 1 || !cells[0].Present() || cells[0].Value() != "value" {
		t.Fatalf("ReadCells() = %#v, %v", cells, err)
	}

	cellError := newSpreadsheetReader(&stubSpreadsheetSource{rows: [][]spreadsheetCell{{
		{value: "ok"}, {err: "bad"},
	}}}, SpreadsheetConfig{Format: FormatXLSX})
	_, err = cellError.Read()
	if !errors.As(err, &tabularErr) || tabularErr.Row != 1 || tabularErr.Field != 2 {
		t.Fatalf("second cell error = %#v", err)
	}

	preserved := newSpreadsheetReader(&stubSpreadsheetSource{rows: [][]spreadsheetCell{{
		{err: "#N/A"},
	}}}, SpreadsheetConfig{Format: FormatXLSX, PreserveCellErrors: true})
	row, err = preserved.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"#N/A"}) {
		t.Fatalf("preserved error = %#v, %v", row, err)
	}
	if got := preserved.outputCellValue(spreadsheetCell{value: "value"}); got != "value" {
		t.Fatalf("non-error output value = %q", got)
	}
	notPreserved := newSpreadsheetReader(&stubSpreadsheetSource{}, SpreadsheetConfig{Format: FormatXLSX})
	if got := notPreserved.outputCellValue(spreadsheetCell{value: "value", err: "#N/A"}); got != "value" {
		t.Fatalf("disabled error preservation output = %q", got)
	}

	for name, config := range map[string]SpreadsheetConfig{
		"field":  {Format: FormatXLSX, MaxFieldBytes: 2},
		"record": {Format: FormatXLSX, MaxRecordBytes: 4},
	} {
		exact := newSpreadsheetReader(&stubSpreadsheetSource{rows: [][]spreadsheetCell{{
			{value: "ab"}, {value: "cd"},
		}}}, config)
		row, err = exact.Read()
		if err != nil || !reflect.DeepEqual(row, Row{"ab", "cd"}) {
			t.Fatalf("%s exact limit = %#v, %v", name, row, err)
		}
	}
}

func TestSpreadsheetOpenExactResourceBoundaries(t *testing.T) {
	t.Parallel()

	if reader, err := OpenSpreadsheet(bytes.NewReader(nil), 0, SpreadsheetConfig{Format: FormatXLS}); reader != nil || !errors.Is(err, ErrorSpreadsheet) || errors.Is(err, ErrorInvalidConfig) {
		t.Fatalf("OpenSpreadsheet(empty) = %#v, %v", reader, err)
	}

	xls, err := os.ReadFile("testdata/spreadsheet/table.xls")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := OpenSpreadsheet(bytes.NewReader(xls), int64(len(xls)), SpreadsheetConfig{
		Format: FormatXLS, MaxWorkbookBytes: int64(len(xls)),
	})
	if err != nil {
		t.Fatalf("exact workbook limit error = %v", err)
	}
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	xlsx, err := os.ReadFile("testdata/spreadsheet/sample.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	reader, err = OpenSpreadsheet(bytes.NewReader(xlsx), int64(len(xlsx)), SpreadsheetConfig{
		Format: FormatXLSX,
		ZIP:    ZIPConfig{MaxTotalBytes: math.MaxInt64, MaxEntryBytes: math.MaxInt64},
	})
	if err != nil {
		t.Fatalf("exact integer limits error = %v", err)
	}
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	for name, limits := range map[string]ZIPConfig{
		"total": {MaxTotalBytes: uint64(math.MaxInt64) + 1, MaxEntryBytes: math.MaxInt64},
		"entry": {MaxTotalBytes: math.MaxInt64, MaxEntryBytes: uint64(math.MaxInt64) + 1},
	} {
		reader, err = OpenSpreadsheet(bytes.NewReader(xlsx), int64(len(xlsx)), SpreadsheetConfig{
			Format: FormatXLSX, ZIP: limits,
		})
		if reader != nil || !errors.Is(err, ErrorInvalidConfig) {
			t.Fatalf("%s excessive integer limit = %#v, %v", name, reader, err)
		}
	}
}

func TestXLSXMetadataExactClassificationAndReferences(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		entry ZIPEntry
		want  bool
	}{
		{entry: ZIPEntry{Name: "xl/worksheets/sheet1.xml"}, want: true},
		{entry: ZIPEntry{Name: "xl/worksheets/sheet1.xml", Directory: true}},
		{entry: ZIPEntry{Name: "xl/other/sheet1.xml"}},
		{entry: ZIPEntry{Name: "xl/worksheets/sheet1.bin"}},
	} {
		if got := xlsxWorksheetEntry(test.entry); got != test.want {
			t.Fatalf("xlsxWorksheetEntry(%+v) = %t, want %t", test.entry, got, test.want)
		}
	}

	for name, workbook := range map[string]string{
		"missing name": `<workbook><sheets><sheet id="rId1"/></sheets></workbook>`,
		"missing id":   `<workbook><sheets><sheet name="Sheet"/></sheets></workbook>`,
	} {
		archive := openTestXLSXArchive(t, makeZIP(t, map[string]string{"xl/workbook.xml": workbook}))
		if reference, err := selectedXLSXSheetReference(archive, ""); err == nil || reference != (xlsxSheetReference{}) {
			t.Fatalf("%s reference = %+v, %v", name, reference, err)
		}
	}
	valid := openTestXLSXArchive(t, makeZIP(t, map[string]string{
		"xl/workbook.xml": `<workbook><sheets><sheet name="Sheet" id="rId1"/></sheets></workbook>`,
	}))
	reference, err := selectedXLSXSheetReference(valid, "")
	if err != nil || reference.name != "Sheet" || reference.relationshipID != "rId1" {
		t.Fatalf("default reference = %+v, %v", reference, err)
	}

	duplicate := newTestXLSXPresenceSource(`<worksheet><sheetData><row r="2"/><row r="2"/></sheetData></worksheet>`)
	if row, _, err := duplicate.nextDeclaredRow(); err != nil || row != 2 {
		t.Fatalf("first duplicate row = %d, %v", row, err)
	}
	if _, _, err := duplicate.nextDeclaredRow(); err == nil {
		t.Fatal("duplicate row declaration was accepted")
	}
	zero := newTestXLSXPresenceSource(`<worksheet><sheetData><row r="0"/></sheetData></worksheet>`)
	if _, _, err := zero.nextDeclaredRow(); err == nil || err.Error() != "worksheet row reference is invalid" {
		t.Fatalf("zero row error = %v", err)
	}
	implicit := newTestXLSXPresenceSource(`<worksheet><sheetData><row><c/><c/></row></sheetData></worksheet>`)
	cells, err := implicit.Read()
	if err != nil || !reflect.DeepEqual(cells, []bool{true, true}) {
		t.Fatalf("implicit cells = %#v, %v", cells, err)
	}
}

func TestXLSXValidationSkipsNonWorksheetEntries(t *testing.T) {
	t.Parallel()

	archive := openTestXLSXArchive(t, makeOrderedZIP(t, []zipFixtureEntry{
		{name: "docProps/app.xml", body: "<Properties/>"},
		{name: "xl/worksheets/sheet1.xml", body: "<worksheet><"},
	}))
	if err := validateXLSXWorksheets(archive); err == nil {
		t.Fatal("worksheet validation stopped at a non-worksheet entry")
	}
}

func TestDelimitedObserverExactStateTransitions(t *testing.T) {
	t.Parallel()

	simple := delimitedRecordLimitReader{delimiter: []byte(",")}
	if !simple.canObserveSimple() {
		t.Fatal("plain one-byte delimiter was not recognized as simple")
	}
	simple.trimLeading = true
	if simple.canObserveSimple() {
		t.Fatal("trim-leading reader was recognized as simple")
	}

	quoted := delimitedRecordLimitReader{
		maximum: 10, recordBytes: 2, inQuotes: true, delimiter: []byte(","),
	}
	accepted, exceeded := quoted.observeBytes([]byte("a\""))
	if accepted != 2 || exceeded || quoted.recordBytes != 4 || !quoted.quotePending {
		t.Fatalf("quoted scan = accepted %d exceeded %t state %#v", accepted, exceeded, quoted)
	}
	for name, state := range map[string]delimitedRecordLimitReader{
		"outside quotes":    {delimiter: []byte(",")},
		"pending quote":     {inQuotes: true, quotePending: true, delimiter: []byte(",")},
		"pending carriage":  {inQuotes: true, quoteCarriage: true, delimiter: []byte(",")},
		"pending delimiter": {inQuotes: true, quoteDelimiterAt: 1, delimiter: []byte("§")},
	} {
		if state.canObserveQuotedRun() {
			t.Fatalf("%s state accepted quoted fast path", name)
		}
	}
	withoutQuote := delimitedRecordLimitReader{maximum: 3, inQuotes: true, delimiter: []byte(",")}
	accepted, exceeded = withoutQuote.observeQuotedRun([]byte("abc"))
	if accepted != 3 || exceeded || withoutQuote.recordBytes != 3 || withoutQuote.quotePending {
		t.Fatalf("quote-free run = accepted %d exceeded %t state %#v", accepted, exceeded, withoutQuote)
	}
	atQuote := delimitedRecordLimitReader{maximum: 1, inQuotes: true, delimiter: []byte(",")}
	accepted, exceeded = atQuote.observeQuotedRun([]byte("\"x"))
	if accepted != 1 || exceeded || !atQuote.quotePending {
		t.Fatalf("leading quote run = accepted %d exceeded %t state %#v", accepted, exceeded, atQuote)
	}
	exceededQuote := delimitedRecordLimitReader{maximum: 0, inQuotes: true, delimiter: []byte(",")}
	accepted, exceeded = exceededQuote.observeQuotedRun([]byte("\""))
	if accepted != 0 || !exceeded || exceededQuote.quotePending {
		t.Fatalf("exceeded quote run = accepted %d exceeded %t state %#v", accepted, exceeded, exceededQuote)
	}

	plain := delimitedRecordLimitReader{maximum: 8, delimiter: []byte(",")}
	accepted, exceeded = plain.observePlain([]byte("a\nb"))
	if accepted != 3 || exceeded || plain.recordBytes != 1 {
		t.Fatalf("plain scan = accepted %d exceeded %t state %#v", accepted, exceeded, plain)
	}

	advance := delimitedRecordLimitReader{maximum: 3, recordBytes: 1}
	accepted, exceeded = advance.advance(2)
	if accepted != 2 || exceeded || advance.recordBytes != 3 {
		t.Fatalf("exact advance = %d, %t, bytes=%d", accepted, exceeded, advance.recordBytes)
	}
	accepted, exceeded = advance.advance(1)
	if accepted != 0 || !exceeded || advance.recordBytes != 3 {
		t.Fatalf("excess advance = %d, %t, bytes=%d", accepted, exceeded, advance.recordBytes)
	}

	unicodeDelimiter := []byte("§")
	closedQuote := delimitedRecordLimitReader{
		inQuotes: true, quotePending: true, quoteDelimiterAt: 1, delimiter: unicodeDelimiter,
	}
	closedQuote.observeQuoted(unicodeDelimiter[1])
	if closedQuote.inQuotes || closedQuote.quotePending || closedQuote.quoteDelimiterAt != 0 || !closedQuote.atFieldStart {
		t.Fatalf("quoted delimiter state = %#v", closedQuote)
	}
	pendingQuote := delimitedRecordLimitReader{
		inQuotes: true, quotePending: true, delimiter: []byte(","),
	}
	pendingQuote.observeQuoted('"')
	if pendingQuote.quotePending {
		t.Fatalf("escaped quote state = %#v", pendingQuote)
	}
	partialDelimiterQuote := delimitedRecordLimitReader{
		delimiter: []byte("§"), delimiterAt: 1, atFieldStart: true,
	}
	partialDelimiterQuote.observeUnquoted('"')
	if partialDelimiterQuote.delimiterAt != 0 || partialDelimiterQuote.atFieldStart || partialDelimiterQuote.inQuotes {
		t.Fatalf("partial-delimiter quote state = %#v", partialDelimiterQuote)
	}

	carriage := delimitedRecordLimitReader{inQuotes: true, quotePending: true, quoteCarriage: true}
	carriage.observeQuoted('\n')
	if carriage.inQuotes || carriage.quotePending || carriage.quoteCarriage || carriage.recordBytes != 0 {
		t.Fatalf("quoted CRLF state = %#v", carriage)
	}

	leading := delimitedRecordLimitReader{trimLeading: true, atFieldStart: true, delimiter: []byte(",")}
	for _, value := range []byte("\u00a0") {
		leading.observeLeading(value)
	}
	if leading.leadingBytesAt != 0 || !leading.atFieldStart {
		t.Fatalf("Unicode whitespace state = %#v", leading)
	}
	for _, value := range []byte("ä") {
		leading.observeLeading(value)
	}
	if leading.leadingBytesAt != 0 || leading.atFieldStart {
		t.Fatalf("Unicode content state = %#v", leading)
	}

	delimiter := delimitedRecordLimitReader{delimiter: []byte("aba"), delimiterAt: 1, atFieldStart: true}
	if delimiter.observeDelimiter('x') || delimiter.delimiterAt != 0 || delimiter.atFieldStart {
		t.Fatalf("delimiter mismatch state = %#v", delimiter)
	}
	delimiter.delimiterAt = 1
	if !delimiter.observeDelimiter('a') || delimiter.delimiterAt != 1 {
		t.Fatalf("delimiter restart state = %#v", delimiter)
	}
}

func TestDelimitedReaderExactCommentAndParseErrorBoundaries(t *testing.T) {
	t.Parallel()

	reader, err := NewCSVReader(strings.NewReader("#comment\na,b\n"), DelimitedConfig{
		Comment: '#', MaxRecordBytes: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := reader.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"a", "b"}) {
		t.Fatalf("comment-bounded Read() = %#v, %v", row, err)
	}

	malformed, err := NewCSVReader(strings.NewReader("ok\n\"unterminated\n"), DelimitedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = malformed.Read(); err != nil {
		t.Fatal(err)
	}
	_, err = malformed.Read()
	var tabularErr *Error
	if !errors.As(err, &tabularErr) || tabularErr.Row != 2 || !errors.Is(err, ErrorMalformedRow) {
		t.Fatalf("second-row parse error = %#v", err)
	}
}

type zipFixtureEntry struct {
	name string
	body string
	mode os.FileMode
}

func makeOrderedZIP(t *testing.T, entries []zipFixtureEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, fixture := range entries {
		header := &zip.FileHeader{Name: fixture.name, Method: zip.Deflate}
		if fixture.mode != 0 {
			header.SetMode(fixture.mode)
		}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(entry, fixture.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

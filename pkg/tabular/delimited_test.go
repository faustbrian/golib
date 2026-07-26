package tabular

import (
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestDelimitedReaderStreamsFixtureWithNormalizedHeader(t *testing.T) {
	t.Parallel()

	file, err := os.Open("testdata/delimited/realistic.csv")
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, file)

	reader, err := NewCSVReader(file, DelimitedConfig{
		Header: &HeaderConfig{
			TrimSpace:        true,
			Case:             HeaderCaseLower,
			RejectEmpty:      true,
			RejectDuplicates: true,
		},
		Normalize: NormalizationConfig{TrimSpace: true},
	})
	if err != nil {
		t.Fatalf("NewCSVReader() error = %v", err)
	}

	header, err := reader.Header()
	if err != nil {
		t.Fatalf("Header() error = %v", err)
	}
	if want := (Row{"name", "city", "note"}); !reflect.DeepEqual(header, want) {
		t.Fatalf("Header() = %#v, want %#v", header, want)
	}

	rows, err := readAllRows(reader)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	want := []Row{
		{"Alice", "Helsinki", "Uses, commas"},
		{"Björk", "Reykjavík", "quoted\nnewline"},
		{"Matti", "Espoo", ""},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}

	header[0] = "changed"
	headerAgain, err := reader.Header()
	if err != nil {
		t.Fatalf("second Header() error = %v", err)
	}
	if headerAgain[0] != "name" {
		t.Fatal("Header() returned mutable internal state")
	}
}

func TestDelimitedReaderSupportsSemicolonAndComments(t *testing.T) {
	t.Parallel()

	file, err := os.Open("testdata/delimited/semicolon.csv")
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, file)

	reader, err := NewDelimitedReader(file, DelimitedConfig{
		Delimiter:           ';',
		Comment:             '#',
		TrimLeadingSpace:    true,
		AllowVariableFields: true,
	})
	if err != nil {
		t.Fatalf("NewDelimitedReader() error = %v", err)
	}

	rows, err := readAllRows(reader)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	want := []Row{{"id", "amount", "description"}, {"1", "12,50", "Nordic order"}, {"2", "", "trailing", ""}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}
}

func TestDelimitedReaderReportsMalformedAndWrongShapeRows(t *testing.T) {
	t.Parallel()
	malformed, err := os.ReadFile("testdata/delimited/malformed.csv")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		input string
		row   int
	}{
		{name: "wrong shape", input: "a,b\n1\n", row: 2},
		{name: "bad quote fixture", input: string(malformed), row: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			reader, err := NewCSVReader(strings.NewReader(test.input), DelimitedConfig{})
			if err != nil {
				t.Fatalf("NewCSVReader() error = %v", err)
			}
			if _, err = reader.Read(); err != nil {
				t.Fatalf("first Read() error = %v", err)
			}
			_, err = reader.Read()
			if !errors.Is(err, ErrorMalformedRow) {
				t.Fatalf("Read() error = %v, want malformed-row kind", err)
			}
			var tabularErr *Error
			if !errors.As(err, &tabularErr) || tabularErr.Row != test.row {
				t.Fatalf("Read() error = %#v, want row %d", err, test.row)
			}
		})
	}
}

func TestDelimitedReaderHandlesEmptyInputsDeterministically(t *testing.T) {
	t.Parallel()

	withoutHeader, err := NewCSVReader(strings.NewReader(""), DelimitedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = withoutHeader.Read(); !errors.Is(err, io.EOF) {
		t.Fatalf("Read() error = %v, want EOF", err)
	}

	withHeader, err := NewCSVReader(strings.NewReader(""), DelimitedConfig{Header: &HeaderConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = withHeader.Header()
	if !errors.Is(err, ErrorInvalidHeader) || !errors.Is(err, io.EOF) {
		t.Fatalf("Header() error = %v, want invalid-header wrapping EOF", err)
	}
	if _, readErr := withHeader.Read(); !errors.Is(readErr, ErrorInvalidHeader) {
		t.Fatalf("Read() error = %v, want cached header error %v", readErr, err)
	}
}

func TestDelimitedReaderValidatesConfiguration(t *testing.T) {
	t.Parallel()

	tests := []DelimitedConfig{
		{},
		{Delimiter: '"'},
		{Delimiter: ',', Comment: ','},
		{Delimiter: ',', Comment: '\r'},
		{Delimiter: ',', MaxRecordBytes: -1},
		{Delimiter: ',', MaxFieldBytes: -1},
	}
	for _, config := range tests {
		_, err := NewDelimitedReader(strings.NewReader(""), config)
		if !errors.Is(err, ErrorInvalidConfig) {
			t.Fatalf("NewDelimitedReader(%+v) error = %v, want invalid config", config, err)
		}
	}
}

func TestDelimitedReaderEnforcesRecordAndFieldLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		config DelimitedConfig
		header bool
		row    int
		field  int
	}{
		{
			name:  "record",
			input: "abc,d\n",
			config: DelimitedConfig{
				MaxRecordBytes: 5,
			},
			row: 1,
		},
		{
			name:  "quoted multiline record",
			input: "\"a\nb\",c\n",
			config: DelimitedConfig{
				MaxRecordBytes: 7,
			},
			row: 1,
		},
		{
			name:  "field",
			input: "abc,d\n",
			config: DelimitedConfig{
				MaxRecordBytes: 16,
				MaxFieldBytes:  2,
			},
			row:   1,
			field: 1,
		},
		{
			name:  "header field",
			input: "abc,d\n",
			config: DelimitedConfig{
				MaxRecordBytes: 16,
				MaxFieldBytes:  2,
				Header:         &HeaderConfig{},
			},
			header: true,
			row:    1,
			field:  1,
		},
		{
			name:  "field with row-shape error",
			input: "abc\n",
			config: DelimitedConfig{
				MaxRecordBytes:  16,
				MaxFieldBytes:   2,
				FieldsPerRecord: 2,
			},
			row:   1,
			field: 1,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			reader, err := NewCSVReader(
				strings.NewReader(test.input),
				test.config,
			)
			if err != nil {
				t.Fatal(err)
			}
			if test.header {
				_, err = reader.Header()
			} else {
				_, err = reader.Read()
			}
			if !errors.Is(err, ErrorLimitExceeded) {
				t.Fatalf("read error = %v, want limit-exceeded kind", err)
			}
			var tabularErr *Error
			if !errors.As(err, &tabularErr) ||
				tabularErr.Row != test.row ||
				tabularErr.Field != test.field {
				t.Fatalf(
					"read error = %#v, want row %d field %d",
					err,
					test.row,
					test.field,
				)
			}
		})
	}

	reader, err := NewCSVReader(
		strings.NewReader("ab,c\n"),
		DelimitedConfig{
			MaxRecordBytes: 5,
			MaxFieldBytes:  2,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	row, err := reader.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"ab", "c"}) {
		t.Fatalf("exact-limit Read() = %#v, %v", row, err)
	}

	fieldAndShape, err := NewCSVReader(
		strings.NewReader("a,b\nabc\n"),
		DelimitedConfig{
			MaxRecordBytes: 16,
			MaxFieldBytes:  2,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fieldAndShape.Read(); err != nil {
		t.Fatal(err)
	}
	if _, err = fieldAndShape.Read(); !errors.Is(
		err,
		ErrorLimitExceeded,
	) {
		t.Fatalf("field-and-shape error = %v", err)
	}
	var fieldAndShapeError *Error
	if !errors.As(err, &fieldAndShapeError) ||
		fieldAndShapeError.Row != 2 ||
		fieldAndShapeError.Field != 1 {
		t.Fatalf("field-and-shape error = %#v", err)
	}

	lazy, err := NewCSVReader(
		strings.NewReader("a\"x\nb\"y\n"),
		DelimitedConfig{
			LazyQuotes:     true,
			MaxRecordBytes: 4,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := readAllRows(lazy)
	if err != nil {
		t.Fatalf("lazy quoted rows: %v", err)
	}
	if want := []Row{{"a\"x"}, {"b\"y"}}; !reflect.DeepEqual(rows, want) {
		t.Fatalf("lazy quoted rows = %#v, want %#v", rows, want)
	}

	unicodeDelimiter, err := NewDelimitedReader(
		strings.NewReader("a§\"x\ny\"\n"),
		DelimitedConfig{
			Delimiter:      '§',
			LazyQuotes:     true,
			MaxRecordBytes: 8,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = unicodeDelimiter.Read(); !errors.Is(
		err,
		ErrorLimitExceeded,
	) {
		t.Fatalf("Unicode-delimited multiline error = %v", err)
	}

	unicodeDelimiterClose, err := NewDelimitedReader(
		strings.NewReader("\"a\"§b\n"),
		DelimitedConfig{
			Delimiter:      '§',
			MaxRecordBytes: 7,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	row, err = unicodeDelimiterClose.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"a", "b"}) {
		t.Fatalf("Unicode-delimiter close Read() = %#v, %v", row, err)
	}

	unicodeDelimiterMismatch, err := NewDelimitedReader(
		strings.NewReader("\"a\"¨\nb\"§c\n"),
		DelimitedConfig{
			Delimiter:      '§',
			LazyQuotes:     true,
			MaxRecordBytes: 6,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = unicodeDelimiterMismatch.Read(); !errors.Is(
		err,
		ErrorLimitExceeded,
	) {
		t.Fatalf("Unicode-delimiter mismatch error = %v", err)
	}

	quotedCRLF, err := NewCSVReader(
		strings.NewReader("\"a\"\r\n"),
		DelimitedConfig{
			MaxRecordBytes: 5,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	row, err = quotedCRLF.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"a"}) {
		t.Fatalf("quoted CRLF Read() = %#v, %v", row, err)
	}

	quotedLF, err := NewCSVReader(
		strings.NewReader("\"a\"\n"),
		DelimitedConfig{
			MaxRecordBytes: 4,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	row, err = quotedLF.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"a"}) {
		t.Fatalf("quoted LF Read() = %#v, %v", row, err)
	}

	lazyQuotedField, err := NewCSVReader(
		strings.NewReader("\"a\"x\nb\",c\n"),
		DelimitedConfig{
			LazyQuotes:     true,
			MaxRecordBytes: 5,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lazyQuotedField.Read(); !errors.Is(
		err,
		ErrorLimitExceeded,
	) {
		t.Fatalf("lazy quoted-field multiline error = %v", err)
	}

	lazyQuotedCarriageReturn, err := NewCSVReader(
		strings.NewReader("\"a\"\rb\nc\",d\n"),
		DelimitedConfig{
			LazyQuotes:     true,
			MaxRecordBytes: 6,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lazyQuotedCarriageReturn.Read(); !errors.Is(
		err,
		ErrorLimitExceeded,
	) {
		t.Fatalf("lazy quoted-field carriage-return error = %v", err)
	}

	trimmedUnicodeSpace, err := NewCSVReader(
		strings.NewReader("\u00a0\"a\nb\",c\n"),
		DelimitedConfig{
			LazyQuotes:       true,
			TrimLeadingSpace: true,
			MaxRecordBytes:   7,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = trimmedUnicodeSpace.Read(); !errors.Is(
		err,
		ErrorLimitExceeded,
	) {
		t.Fatalf("Unicode-space multiline error = %v", err)
	}

	commentedQuote, err := NewCSVReader(
		strings.NewReader("#\"\na,b\n"),
		DelimitedConfig{
			Comment:        '#',
			MaxRecordBytes: 6,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	row, err = commentedQuote.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"a", "b"}) {
		t.Fatalf("commented quote Read() = %#v, %v", row, err)
	}

	unicodeComment, err := NewCSVReader(
		strings.NewReader("§\"\na,b\n"),
		DelimitedConfig{
			Comment:        '§',
			MaxRecordBytes: 6,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	row, err = unicodeComment.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"a", "b"}) {
		t.Fatalf("Unicode-comment Read() = %#v, %v", row, err)
	}

	unicodeCommentMismatch, err := NewCSVReader(
		strings.NewReader("¨a,b\n"),
		DelimitedConfig{
			Comment:        '§',
			MaxRecordBytes: 7,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	row, err = unicodeCommentMismatch.Read()
	if err != nil || !reflect.DeepEqual(row, Row{"¨a", "b"}) {
		t.Fatalf("Unicode-comment mismatch Read() = %#v, %v", row, err)
	}

	strictMalformedQuote, err := NewCSVReader(
		strings.NewReader("\"a\"x\nb,c\n"),
		DelimitedConfig{
			MaxRecordBytes: 5,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = strictMalformedQuote.Read(); !errors.Is(
		err,
		ErrorMalformedRow,
	) || errors.Is(err, ErrorLimitExceeded) {
		t.Fatalf("strict malformed-quote error = %v", err)
	}

	unboundedValue := strings.Repeat("a", (1<<20)+1)
	unbounded, err := NewCSVReader(
		strings.NewReader(unboundedValue+"\n"),
		DelimitedConfig{},
	)
	if err != nil {
		t.Fatal(err)
	}
	row, err = unbounded.Read()
	if err != nil || !reflect.DeepEqual(row, Row{unboundedValue}) {
		t.Fatalf("zero-limit Read() row count = %d, %v", len(row), err)
	}
}

func TestDelimitedRecordLimitReaderBoundsBeforeParserAllocation(t *testing.T) {
	t.Parallel()

	reader := &delimitedRecordLimitReader{
		source:  strings.NewReader("abc"),
		maximum: 1,
	}
	buffer := make([]byte, 8)
	count, err := reader.Read(buffer)
	if count != 1 ||
		!errors.Is(err, errDelimitedRecordLimit) ||
		string(buffer[:count]) != "a" {
		t.Fatalf("Read() = %d, %v, %q", count, err, buffer[:count])
	}
	if count, err = reader.Read(buffer); count != 0 ||
		!errors.Is(err, errDelimitedRecordLimit) {
		t.Fatalf("second Read() = %d, %v", count, err)
	}

	immediate := &delimitedRecordLimitReader{
		source:  strings.NewReader("a"),
		maximum: 0,
	}
	if count, err = immediate.Read(buffer); count != 0 ||
		!errors.Is(err, errDelimitedRecordLimit) {
		t.Fatalf("immediate Read() = %d, %v", count, err)
	}

	simpleAtFieldLimit := &delimitedRecordLimitReader{
		maximum:      0,
		delimiter:    []byte(","),
		atFieldStart: true,
	}
	if accepted, exceeded := simpleAtFieldLimit.observeBytes(
		[]byte("a"),
	); accepted != 0 || !exceeded {
		t.Fatalf(
			"simple field-start limit = %d, %t",
			accepted,
			exceeded,
		)
	}

	simpleQuoteLimit := &delimitedRecordLimitReader{
		maximum:      0,
		delimiter:    []byte(","),
		atFieldStart: true,
	}
	if accepted, exceeded := simpleQuoteLimit.observeBytes(
		[]byte("\""),
	); accepted != 0 || !exceeded {
		t.Fatalf("simple quote limit = %d, %t", accepted, exceeded)
	}

	simpleWithoutSpecial := &delimitedRecordLimitReader{
		maximum:   3,
		delimiter: []byte(","),
	}
	if accepted, exceeded := simpleWithoutSpecial.observeBytes(
		[]byte("abc"),
	); accepted != 3 || exceeded {
		t.Fatalf(
			"simple no-special scan = %d, %t",
			accepted,
			exceeded,
		)
	}

	simpleRunLimit := &delimitedRecordLimitReader{
		maximum:      2,
		delimiter:    []byte(","),
		atFieldStart: true,
	}
	if accepted, exceeded := simpleRunLimit.observeBytes(
		[]byte("abcd,\n"),
	); accepted != 2 || !exceeded {
		t.Fatalf("simple run limit = %d, %t", accepted, exceeded)
	}

	observer := delimitedRecordLimitReader{
		delimiter:    []byte(","),
		atFieldStart: true,
		atLineStart:  true,
	}
	for _, value := range []byte{'"', 'a', '"', '"', '"', ',', '\n'} {
		observer.observe(value)
	}
	if observer.inQuotes ||
		observer.quotePending ||
		observer.recordBytes != 0 {
		t.Fatalf("observer state = %#v", observer)
	}

	delimiter := &delimitedRecordLimitReader{
		delimiter: []byte("aba"),
	}
	firstDelimiterByte := delimiter.observeDelimiter('a')
	secondDelimiterByte := delimiter.observeDelimiter('b')
	thirdDelimiterByte := delimiter.observeDelimiter('a')
	if !firstDelimiterByte ||
		!secondDelimiterByte ||
		!thirdDelimiterByte ||
		!delimiter.atFieldStart {
		t.Fatalf("completed delimiter state = %#v", delimiter)
	}
	delimiter.atFieldStart = true
	matchedDelimiterPrefix := delimiter.observeDelimiter('a')
	mismatchedDelimiter := delimiter.observeDelimiter('x')
	if !matchedDelimiterPrefix ||
		mismatchedDelimiter ||
		delimiter.atFieldStart {
		t.Fatalf("mismatched delimiter state = %#v", delimiter)
	}
	delimiter.atFieldStart = true
	restartedDelimiterPrefix := delimiter.observeDelimiter('a')
	restartedDelimiter := delimiter.observeDelimiter('a')
	if !restartedDelimiterPrefix ||
		!restartedDelimiter ||
		delimiter.delimiterAt != 1 {
		t.Fatalf("restarted delimiter state = %#v", delimiter)
	}
}

func TestDelimitedReaderCanUseLazyQuotesExplicitly(t *testing.T) {
	t.Parallel()

	reader, err := NewCSVReader(strings.NewReader("a,b\n1,unquoted\"quote\n"), DelimitedConfig{
		LazyQuotes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := readAllRows(reader)
	if err != nil {
		t.Fatal(err)
	}
	if want := []Row{{"a", "b"}, {"1", "unquoted\"quote"}}; !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}
}

func FuzzDelimitedReader(f *testing.F) {
	f.Add(",", "a,b\n1,2\n")
	f.Add(";", "name;city\nBjörk;Reykjavík\n")
	f.Fuzz(func(t *testing.T, delimiterText, input string) {
		delimiter := []rune(delimiterText)
		if len(delimiter) != 1 {
			t.Skip()
		}
		reader, err := NewDelimitedReader(strings.NewReader(input), DelimitedConfig{
			Delimiter:           delimiter[0],
			AllowVariableFields: true,
		})
		if err != nil {
			return
		}
		for {
			_, err = reader.Read()
			if err != nil {
				return
			}
		}
	})
}

func BenchmarkDelimitedReader(b *testing.B) {
	data := strings.Repeat("1,Alice,Helsinki\n", 20_000)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for range b.N {
		reader, err := NewCSVReader(strings.NewReader(data), DelimitedConfig{})
		if err != nil {
			b.Fatal(err)
		}
		if err = consumeRows(reader); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDelimitedReaderWithLimits(b *testing.B) {
	data := strings.Repeat("1,Alice,Helsinki\n", 20_000)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for range b.N {
		reader, err := NewCSVReader(strings.NewReader(data), DelimitedConfig{
			MaxRecordBytes: 1 << 20,
			MaxFieldBytes:  1 << 20,
		})
		if err != nil {
			b.Fatal(err)
		}
		if err = consumeRows(reader); err != nil {
			b.Fatal(err)
		}
	}
}

func TestDelimitedReaderDefaultPathAvoidsNormalizationAllocation(t *testing.T) {
	const rowCount = 1_000

	data := strings.Repeat("1,Alice,Helsinki\n", rowCount)
	var readErr error
	allocations := testing.AllocsPerRun(5, func() {
		reader, err := NewCSVReader(strings.NewReader(data), DelimitedConfig{})
		if err != nil {
			readErr = err
			return
		}
		readErr = consumeRows(reader)
	})
	if readErr != nil {
		t.Fatal(readErr)
	}
	if allocations > 2_200 {
		t.Fatalf("default CSV read allocated %.0f times for %d rows, want at most 2200", allocations, rowCount)
	}
}

func TestDelimitedReaderBuffersLargeSources(t *testing.T) {
	source := &countingReader{
		Reader: strings.NewReader(strings.Repeat("1,Alice,Helsinki\n", 60_000)),
	}
	reader, err := NewCSVReader(source, DelimitedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err = consumeRows(reader); err != nil {
		t.Fatal(err)
	}
	if source.reads > 20 {
		t.Fatalf("source Read() calls = %d, want at most 20", source.reads)
	}
}

type countingReader struct {
	*strings.Reader
	reads int
}

func (reader *countingReader) Read(buffer []byte) (int, error) {
	reader.reads++
	return reader.Reader.Read(buffer)
}

func consumeRows(reader interface{ Read() (Row, error) }) error {
	for {
		_, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func readAllRows(reader interface{ Read() (Row, error) }) ([]Row, error) {
	var rows []Row
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
}

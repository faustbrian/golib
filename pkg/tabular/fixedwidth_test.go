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

func TestFixedWidthReaderParsesBytePositionedLatin1Fixture(t *testing.T) {
	t.Parallel()

	file, err := os.Open("testdata/fixedwidth/nordic-latin1.txt")
	if err != nil {
		t.Fatal(err)
	}
	closeTestResource(t, file)

	reader, err := NewFixedWidthReader(file, FixedWidthConfig{
		Encoding: EncodingISO88591,
		Fields: []FixedWidthField{
			{Name: "code", Start: 0, End: 3},
			{Name: "name", Start: 3, End: 13, TrimSpace: true},
			{Name: "city", Start: 13, End: 23, TrimSpace: true},
		},
	})
	if err != nil {
		t.Fatalf("NewFixedWidthReader() error = %v", err)
	}

	rows, err := readAllRows(reader)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	want := []Row{{"001", "Åke", "Malmö"}, {"002", "Björk", "Reykjavík"}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}
	if got := reader.Fields(); !reflect.DeepEqual(got, []string{"code", "name", "city"}) {
		t.Fatalf("Fields() = %#v", got)
	}
}

func TestExtractBytesUsesHalfOpenByteOffsets(t *testing.T) {
	t.Parallel()

	got, err := ExtractBytes([]byte("abcdef"), 1, 4)
	if err != nil {
		t.Fatalf("ExtractBytes() error = %v", err)
	}
	if string(got) != "bcd" {
		t.Fatalf("ExtractBytes() = %q, want bcd", got)
	}
	got[0] = 'X'
	if string(got) != "Xcd" {
		t.Fatal("ExtractBytes() result is unexpectedly immutable")
	}
}

func TestExtractBytesRejectsInvalidRanges(t *testing.T) {
	t.Parallel()

	for _, offsets := range [][2]int{{-1, 1}, {1, 1}, {2, 1}, {0, 4}} {
		_, err := ExtractBytes([]byte("abc"), offsets[0], offsets[1])
		if !errors.Is(err, ErrorInvalidLayout) {
			t.Fatalf("ExtractBytes(%v) error = %v, want invalid-layout kind", offsets, err)
		}
	}
}

func TestFixedWidthReaderRejectsInvalidLayouts(t *testing.T) {
	t.Parallel()

	tests := []FixedWidthConfig{
		{},
		{Fields: []FixedWidthField{{Name: "", Start: 0, End: 1}}},
		{Fields: []FixedWidthField{{Name: "a", Start: -1, End: 1}}},
		{Fields: []FixedWidthField{{Name: "a", Start: 1, End: 1}}},
		{Fields: []FixedWidthField{{Name: "a", Start: 2, End: 3}, {Name: "b", Start: 0, End: 2}}},
		{Fields: []FixedWidthField{{Name: "a", Start: 0, End: 3}, {Name: "b", Start: 2, End: 4}}},
		{Fields: []FixedWidthField{{Name: "a", Start: 0, End: 1}}, MaxRecordBytes: -1},
	}
	for _, config := range tests {
		_, err := NewFixedWidthReader(strings.NewReader(""), config)
		if !errors.Is(err, ErrorInvalidLayout) {
			t.Fatalf("NewFixedWidthReader(%+v) error = %v, want invalid layout", config, err)
		}
	}
}

func TestFixedWidthReaderReportsShortAndTrailingRecords(t *testing.T) {
	t.Parallel()
	malformed, err := os.ReadFile("testdata/fixedwidth/malformed-short.txt")
	if err != nil {
		t.Fatal(err)
	}

	fields := []FixedWidthField{{Name: "a", Start: 0, End: 2}, {Name: "b", Start: 2, End: 4}}
	for _, test := range []struct {
		name   string
		input  string
		config FixedWidthConfig
	}{
		{name: "short fixture", input: string(malformed), config: FixedWidthConfig{
			Fields: []FixedWidthField{{Name: "record", Start: 0, End: 32}},
		}},
		{name: "trailing", input: "abcde\n", config: FixedWidthConfig{Fields: fields, RejectTrailingBytes: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reader, err := NewFixedWidthReader(strings.NewReader(test.input), test.config)
			if err != nil {
				t.Fatal(err)
			}
			_, err = reader.Read()
			if !errors.Is(err, ErrorMalformedRow) {
				t.Fatalf("Read() error = %v, want malformed-row kind", err)
			}
			var tabularErr *Error
			if !errors.As(err, &tabularErr) || tabularErr.Row != 1 {
				t.Fatalf("Read() error = %#v, want row 1", err)
			}
		})
	}
}

func TestFixedWidthReaderCanAllowShortRecordsExplicitly(t *testing.T) {
	t.Parallel()

	reader, err := NewFixedWidthReader(strings.NewReader("abc\n"), FixedWidthConfig{
		Fields:            []FixedWidthField{{Name: "a", Start: 0, End: 2}, {Name: "b", Start: 2, End: 4}},
		AllowShortRecords: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if want := (Row{"ab", "c"}); !reflect.DeepEqual(row, want) {
		t.Fatalf("Read() = %#v, want %#v", row, want)
	}
}

func TestFixedWidthReaderReportsEncodingAndRecordLimitErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		max   int
		kind  ErrorKind
	}{
		{name: "encoding", input: string([]byte{0xff, '\n'}), max: 10, kind: ErrorInvalidEncoding},
		{name: "limit", input: "toolong\n", max: 3, kind: ErrorLimitExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reader, err := NewFixedWidthReader(strings.NewReader(test.input), FixedWidthConfig{
				Encoding:          EncodingUTF8,
				Fields:            []FixedWidthField{{Name: "value", Start: 0, End: 1}},
				MaxRecordBytes:    test.max,
				AllowShortRecords: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = reader.Read()
			if !errors.Is(err, test.kind) {
				t.Fatalf("Read() error = %v, want kind %v", err, test.kind)
			}
		})
	}
}

func TestFixedWidthReaderReturnsEOFForEmptyInput(t *testing.T) {
	t.Parallel()

	reader, err := NewFixedWidthReader(strings.NewReader(""), FixedWidthConfig{
		Fields: []FixedWidthField{{Name: "value", Start: 0, End: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reader.Read(); !errors.Is(err, io.EOF) {
		t.Fatalf("Read() error = %v, want EOF", err)
	}
}

func FuzzExtractBytes(f *testing.F) {
	f.Add([]byte("abcdef"), 1, 4)
	f.Fuzz(func(t *testing.T, record []byte, start, end int) {
		field, err := ExtractBytes(record, start, end)
		if err != nil {
			return
		}
		if len(field) != end-start {
			t.Fatalf("field length = %d, want %d", len(field), end-start)
		}
	})
}

func FuzzFixedWidthReader(f *testing.F) {
	f.Add([]byte("001Alice     Helsinki  \n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		reader, err := NewFixedWidthReader(bytes.NewReader(data), FixedWidthConfig{
			Fields: []FixedWidthField{
				{Name: "code", Start: 0, End: 3},
				{Name: "name", Start: 3, End: 13, TrimSpace: true},
			},
			AllowShortRecords: true,
			MaxRecordBytes:    4096,
		})
		if err != nil {
			t.Fatal(err)
		}
		for {
			if _, err = reader.Read(); err != nil {
				return
			}
		}
	})
}

func BenchmarkFixedWidthReader(b *testing.B) {
	data := strings.Repeat("001Alice     Helsinki  \n", 20_000)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for range b.N {
		reader, err := NewFixedWidthReader(strings.NewReader(data), FixedWidthConfig{
			Encoding: EncodingUTF8,
			Fields: []FixedWidthField{
				{Name: "code", Start: 0, End: 3},
				{Name: "name", Start: 3, End: 13, TrimSpace: true},
				{Name: "city", Start: 13, End: 23, TrimSpace: true},
			},
		})
		if err != nil {
			b.Fatal(err)
		}
		if err = consumeRows(reader); err != nil {
			b.Fatal(err)
		}
	}
}

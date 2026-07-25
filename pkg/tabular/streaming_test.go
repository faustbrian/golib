package tabular

import (
	"io"
	"reflect"
	"testing"
)

func TestTextReadersProcessChunkedInputOneRecordAtATime(t *testing.T) {
	t.Parallel()

	t.Run("delimited quoted newline", func(t *testing.T) {
		t.Parallel()
		input := "\"line\nvalue\",x\nnext,y\n"
		source := &chunkReader{data: []byte(input), maximum: 1}
		reader, err := NewCSVReader(source, DelimitedConfig{})
		if err != nil {
			t.Fatal(err)
		}
		if source.consumed != 0 {
			t.Fatalf("constructor consumed %d bytes", source.consumed)
		}
		row, err := reader.Read()
		if err != nil || !reflect.DeepEqual(row, Row{"line\nvalue", "x"}) {
			t.Fatalf("Read() = %#v, %v", row, err)
		}
		if source.consumed >= len(input) {
			t.Fatalf("first row consumed all %d input bytes", source.consumed)
		}
	})

	t.Run("fixed width", func(t *testing.T) {
		t.Parallel()
		input := "abc\ndef\n"
		source := &chunkReader{data: []byte(input), maximum: 1}
		reader, err := NewFixedWidthReader(source, FixedWidthConfig{
			Fields: []FixedWidthField{{Name: "value", Start: 0, End: 3}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if source.consumed != 0 {
			t.Fatalf("constructor consumed %d bytes", source.consumed)
		}
		row, err := reader.Read()
		if err != nil || !reflect.DeepEqual(row, Row{"abc"}) {
			t.Fatalf("Read() = %#v, %v", row, err)
		}
		if source.consumed >= len(input) {
			t.Fatalf("first row consumed all %d input bytes", source.consumed)
		}
	})
}

type chunkReader struct {
	data     []byte
	maximum  int
	consumed int
}

func (reader *chunkReader) Read(destination []byte) (int, error) {
	if reader.consumed == len(reader.data) {
		return 0, io.EOF
	}
	if len(destination) > reader.maximum {
		destination = destination[:reader.maximum]
	}
	count := copy(destination, reader.data[reader.consumed:])
	reader.consumed += count
	return count, nil
}

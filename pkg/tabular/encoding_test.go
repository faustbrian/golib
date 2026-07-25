package tabular

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestDecodeBytesPreservesSupportedNordicCharacters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		encoding Encoding
		input    []byte
		want     string
	}{
		{name: "utf8", encoding: EncodingUTF8, input: []byte("Åland"), want: "Åland"},
		{name: "latin1", encoding: EncodingISO88591, input: []byte{0xc5, 'l', 'a', 'n', 'd'}, want: "Åland"},
		{name: "windows1252", encoding: EncodingWindows1252, input: []byte{0x80, '1', '0'}, want: "€10"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := DecodeBytes(test.input, test.encoding)
			if err != nil {
				t.Fatalf("DecodeBytes() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("DecodeBytes() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDecodeBytesRejectsInvalidUTF8AndUnknownEncoding(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		encoding Encoding
		input    []byte
	}{
		{name: "invalid utf8", encoding: EncodingUTF8, input: []byte{0xff}},
		{name: "unknown", encoding: Encoding("ebcdic"), input: []byte("data")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeBytes(test.input, test.encoding)
			if !errors.Is(err, ErrorInvalidEncoding) {
				t.Fatalf("DecodeBytes() error = %v, want invalid-encoding kind", err)
			}
		})
	}
}

func TestDecodeReaderStreamsConvertedText(t *testing.T) {
	t.Parallel()

	reader, err := DecodeReader(strings.NewReader(string([]byte{0xc5, 'b', 'o'})), EncodingISO88591)
	if err != nil {
		t.Fatalf("DecodeReader() error = %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != "Åbo" {
		t.Fatalf("decoded text = %q, want %q", got, "Åbo")
	}
}

func TestDecodeReaderRejectsNilAndUnknownConfiguration(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		reader   io.Reader
		encoding Encoding
	}{
		{reader: nil, encoding: EncodingUTF8},
		{reader: strings.NewReader("data"), encoding: Encoding("unknown")},
	} {
		_, err := DecodeReader(test.reader, test.encoding)
		if !errors.Is(err, ErrorInvalidEncoding) {
			t.Fatalf("DecodeReader() error = %v, want invalid-encoding kind", err)
		}
	}
}

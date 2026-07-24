package tabular

import (
	"bufio"
	"bytes"
	"io"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// Encoding names a supported source character encoding.
type Encoding string

const (
	// EncodingUTF8 selects strict UTF-8 validation.
	EncodingUTF8 Encoding = "utf-8"
	// EncodingISO88591 selects the ISO-8859-1 single-byte encoding.
	EncodingISO88591 Encoding = "iso-8859-1"
	// EncodingWindows1252 selects the Windows-1252 single-byte encoding.
	EncodingWindows1252 Encoding = "windows-1252"
)

// DecodeBytes validates and converts source bytes to UTF-8.
func DecodeBytes(source []byte, sourceEncoding Encoding) (string, error) {
	if sourceEncoding == "" {
		sourceEncoding = EncodingUTF8
	}
	if sourceEncoding == EncodingUTF8 {
		if !utf8.Valid(source) {
			return "", &Error{Kind: ErrorInvalidEncoding, Op: "encoding.decode", Format: string(sourceEncoding)}
		}
		return string(source), nil
	}

	decoder, err := decoderFor(sourceEncoding)
	if err != nil {
		return "", err
	}
	converted, _ := io.ReadAll(transform.NewReader(bytes.NewReader(source), decoder.NewDecoder()))
	return string(converted), nil
}

// DecodeReader returns a streaming UTF-8 view of source.
func DecodeReader(source io.Reader, sourceEncoding Encoding) (io.Reader, error) {
	if source == nil {
		return nil, &Error{Kind: ErrorInvalidEncoding, Op: "encoding.reader", Format: string(sourceEncoding)}
	}
	if sourceEncoding == "" {
		sourceEncoding = EncodingUTF8
	}
	if sourceEncoding == EncodingUTF8 {
		return &validatingUTF8Reader{source: bufio.NewReader(source)}, nil
	}
	decoder, err := decoderFor(sourceEncoding)
	if err != nil {
		return nil, err
	}
	return transform.NewReader(source, decoder.NewDecoder()), nil
}

type validatingUTF8Reader struct {
	source  *bufio.Reader
	pending []byte
}

func (reader *validatingUTF8Reader) Read(destination []byte) (int, error) {
	if len(destination) == 0 {
		return 0, nil
	}
	for len(reader.pending) == 0 {
		character, size, err := reader.source.ReadRune()
		if err != nil {
			return 0, err
		}
		if character == utf8.RuneError && size == 1 {
			return 0, &Error{Kind: ErrorInvalidEncoding, Op: "encoding.reader", Format: string(EncodingUTF8)}
		}
		reader.pending = utf8.AppendRune(reader.pending, character)
	}
	count := copy(destination, reader.pending)
	reader.pending = reader.pending[count:]
	return count, nil
}

func decoderFor(sourceEncoding Encoding) (encoding.Encoding, error) {
	switch sourceEncoding {
	case EncodingUTF8:
		return unicode.UTF8, nil
	case EncodingISO88591:
		return charmap.ISO8859_1, nil
	case EncodingWindows1252:
		return charmap.Windows1252, nil
	default:
		return nil, &Error{Kind: ErrorInvalidEncoding, Op: "encoding.decoder", Format: string(sourceEncoding)}
	}
}

package xmlwire

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/internal/outputlimit"
	"github.com/faustbrian/golib/pkg/wire/internal/valuecheck"
)

const (
	// DefaultMaxBytes is the default maximum XML document size.
	DefaultMaxBytes int64 = 1 << 20
	// DefaultMaxDepth is the default maximum nested XML element depth.
	DefaultMaxDepth = 1000
)

// ErrPayloadTooLarge identifies XML documents over the configured limit.
var ErrPayloadTooLarge = errors.New("payload exceeds size limit")

// ErrNestingTooDeep identifies XML documents over the configured depth limit.
var ErrNestingTooDeep = errors.New("XML nesting exceeds depth limit")

// DecodeOptions controls XML parsing and validation.
type DecodeOptions struct {
	MaxBytes       int64
	MaxDepth       int
	AllowNonStrict bool
	ExpectedRoot   xml.Name
	CharsetReader  func(string, io.Reader) (io.Reader, error)
}

// EncodeOptions controls XML serialization.
type EncodeOptions struct {
	MaxBytes      int64
	Indent        string
	IncludeHeader bool
}

// Decode parses one complete XML document into target.
func Decode(payload []byte, target any, options DecodeOptions) error {
	return DecodeReader(bytes.NewReader(payload), target, options)
}

// DecodeReader reads a bounded stream and parses one complete XML document.
func DecodeReader(reader io.Reader, target any, options DecodeOptions) error {
	if err := validateTarget(target); err != nil {
		return &wire.Error{Kind: wire.ErrorKindTarget, Format: wire.FormatXML, Op: "decode", Err: err}
	}
	if err := validateDecodeOptions(options); err != nil {
		return err
	}
	payload, err := readBounded(reader, options.MaxBytes)
	if err != nil {
		return err
	}

	root, err := rootName(payload, options)
	if err != nil {
		return err
	}
	if options.ExpectedRoot != (xml.Name{}) && root != options.ExpectedRoot {
		return validationError("validate root", fmt.Errorf(
			"got {%s}%s, want {%s}%s",
			root.Space,
			root.Local,
			options.ExpectedRoot.Space,
			options.ExpectedRoot.Local,
		))
	}

	decoder := decoderFor(payload, options)
	if err := decoder.Decode(target); err != nil {
		return classifyDecodeError("decode", err)
	}

	return nil
}

// Root validates an XML document and returns its namespace-resolved root name.
func Root(payload []byte, options DecodeOptions) (xml.Name, error) {
	if err := validateDecodeOptions(options); err != nil {
		return xml.Name{}, err
	}
	bounded, err := readBounded(bytes.NewReader(payload), options.MaxBytes)
	if err != nil {
		return xml.Name{}, err
	}

	return rootName(bounded, options)
}

// Encode serializes value using encoding/xml's deterministic struct traversal.
func Encode(value any, options EncodeOptions) ([]byte, error) {
	if err := valuecheck.Validate(value); err != nil {
		return nil, validationError("encode", err)
	}
	output, err := outputlimit.New(options.MaxBytes, DefaultMaxBytes)
	if err != nil {
		return nil, validationError("encode options", err)
	}
	if options.IncludeHeader {
		if _, err := output.Write([]byte(xml.Header)); err != nil {
			return nil, encodeSizeError(err)
		}
	}
	encoder := xml.NewEncoder(output)
	if options.Indent != "" {
		encoder.Indent("", options.Indent)
	}
	if err := encoder.Encode(value); err != nil {
		if errors.Is(err, outputlimit.ErrLimit) {
			return nil, encodeSizeError(err)
		}
		return nil, validationError("encode", err)
	}
	return output.Bytes(), nil
}

func encodeSizeError(err error) error {
	return &wire.Error{
		Kind:   wire.ErrorKindSizeLimit,
		Format: wire.FormatXML,
		Op:     "encode",
		Err:    errors.Join(ErrPayloadTooLarge, err),
	}
}

// EncodeWriter serializes value and writes the complete XML document to
// writer.
func EncodeWriter(writer io.Writer, value any, options EncodeOptions) error {
	payload, err := Encode(value, options)
	if err != nil {
		return err
	}
	if writer == nil {
		return validationError("write", errors.New("writer must not be nil"))
	}
	if _, err := io.Copy(writer, bytes.NewReader(payload)); err != nil {
		return &wire.Error{Kind: wire.ErrorKindWrite, Format: wire.FormatXML, Op: "write", Err: err}
	}

	return nil
}

// CharsetReader converts a deliberately limited set of common vendor
// encodings to UTF-8. Unknown labels and undefined code points return errors.
func CharsetReader(label string, input io.Reader) (io.Reader, error) {
	label = strings.ToLower(strings.TrimSpace(label))
	payload, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}

	switch label {
	case "utf-8", "utf8":
		if !utf8.Valid(payload) {
			return nil, charsetError{message: "invalid UTF-8"}
		}
		return bytes.NewReader(payload), nil
	case "us-ascii", "ascii":
		for _, value := range payload {
			if value > 0x7f {
				return nil, charsetError{message: fmt.Sprintf("invalid US-ASCII byte 0x%02x", value)}
			}
		}
		return bytes.NewReader(payload), nil
	case "iso-8859-1", "latin1", "latin-1":
		return bytes.NewReader(singleByteToUTF8(payload, nil)), nil
	case "windows-1252", "cp1252":
		converted, err := windows1252ToUTF8(payload)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(converted), nil
	default:
		return nil, charsetError{message: fmt.Sprintf("unsupported charset %q", label)}
	}
}

func decoderFor(payload []byte, options DecodeOptions) *xml.Decoder {
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	decoder.Strict = !options.AllowNonStrict
	decoder.CharsetReader = options.CharsetReader
	if decoder.CharsetReader == nil {
		decoder.CharsetReader = CharsetReader
	}
	return decoder
}

func rootName(payload []byte, options DecodeOptions) (xml.Name, error) {
	decoder := decoderFor(payload, options)
	var root xml.Name
	depth := 0
	maxDepth := options.MaxDepth
	if maxDepth == 0 {
		maxDepth = DefaultMaxDepth
	}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if root == (xml.Name{}) {
				return xml.Name{}, parseError("read root", errors.New("document has no root element"))
			}
			return root, nil
		}
		if err != nil {
			return xml.Name{}, classifyDecodeError("read root", err)
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				if root != (xml.Name{}) {
					return xml.Name{}, parseError("read root", errors.New("document has multiple root elements"))
				}
				root = typed.Name
			}
			depth++
			if depth > maxDepth {
				return xml.Name{}, &wire.Error{
					Kind:   wire.ErrorKindSizeLimit,
					Format: wire.FormatXML,
					Op:     "read root",
					Err:    ErrNestingTooDeep,
				}
			}
		case xml.EndElement:
			depth--
		}
	}
}

func validateDecodeOptions(options DecodeOptions) error {
	if options.MaxDepth < 0 {
		return validationError("decode options", errors.New("max depth must not be negative"))
	}
	return nil
}

func readBounded(reader io.Reader, configuredMax int64) ([]byte, error) {
	if reader == nil {
		return nil, validationError("read", errors.New("reader must not be nil"))
	}
	if configuredMax < 0 {
		return nil, validationError("read", errors.New("max bytes must not be negative"))
	}
	maxBytes := configuredMax
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}
	limit := maxBytes + 1
	if maxBytes == math.MaxInt64 {
		limit = maxBytes
	}
	payload, err := io.ReadAll(io.LimitReader(reader, limit))
	if err != nil {
		return nil, parseError("read", err)
	}
	if int64(len(payload)) > maxBytes {
		return nil, &wire.Error{Kind: wire.ErrorKindSizeLimit, Format: wire.FormatXML, Op: "read", Err: ErrPayloadTooLarge}
	}
	return payload, nil
}

func validateTarget(target any) error {
	if target == nil {
		return errors.New("target must be a non-nil pointer")
	}
	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return errors.New("target must be a non-nil pointer")
	}
	return nil
}

type charsetError struct {
	message string
}

func (e charsetError) Error() string {
	return e.message
}

func classifyDecodeError(op string, err error) error {
	var syntaxError *xml.SyntaxError
	var encodingError charsetError
	if errors.As(err, &syntaxError) || errors.As(err, &encodingError) {
		return parseError(op, err)
	}
	var numberError *strconv.NumError
	var unmarshalError xml.UnmarshalError
	if errors.As(err, &numberError) || errors.As(err, &unmarshalError) {
		return validationError(op, err)
	}
	return parseError(op, err)
}

func parseError(op string, err error) error {
	return &wire.Error{Kind: wire.ErrorKindParse, Format: wire.FormatXML, Op: op, Err: err}
}

func validationError(op string, err error) error {
	return &wire.Error{Kind: wire.ErrorKindValidation, Format: wire.FormatXML, Op: op, Err: err}
}

func singleByteToUTF8(payload []byte, replacements map[byte]rune) []byte {
	var output strings.Builder
	output.Grow(len(payload))
	for _, value := range payload {
		if replacement, ok := replacements[value]; ok {
			output.WriteRune(replacement)
		} else {
			output.WriteRune(rune(value))
		}
	}
	return []byte(output.String())
}

func windows1252ToUTF8(payload []byte) ([]byte, error) {
	for _, value := range payload {
		if value >= 0x80 && value <= 0x9f {
			if _, ok := windows1252[value]; !ok {
				return nil, charsetError{message: fmt.Sprintf("undefined Windows-1252 byte 0x%02x", value)}
			}
		}
	}
	return singleByteToUTF8(payload, windows1252), nil
}

var windows1252 = map[byte]rune{
	0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…', 0x86: '†',
	0x87: '‡', 0x88: 'ˆ', 0x89: '‰', 0x8a: 'Š', 0x8b: '‹', 0x8c: 'Œ',
	0x8e: 'Ž', 0x91: '‘', 0x92: '’', 0x93: '“', 0x94: '”', 0x95: '•',
	0x96: '–', 0x97: '—', 0x98: '˜', 0x99: '™', 0x9a: 'š', 0x9b: '›',
	0x9c: 'œ', 0x9e: 'ž', 0x9f: 'Ÿ',
}

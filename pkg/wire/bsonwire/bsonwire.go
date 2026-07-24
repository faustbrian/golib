package bsonwire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"reflect"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/internal/outputlimit"
	"github.com/faustbrian/golib/pkg/wire/internal/valuecheck"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Official BSON data types are re-exported for complete interoperability
// without a second, subtly different local type system.
type (
	A             = bson.A
	D             = bson.D
	E             = bson.E
	M             = bson.M
	Raw           = bson.Raw
	RawArray      = bson.RawArray
	RawValue      = bson.RawValue
	ObjectID      = bson.ObjectID
	DateTime      = bson.DateTime
	Decimal128    = bson.Decimal128
	Binary        = bson.Binary
	Regex         = bson.Regex
	Timestamp     = bson.Timestamp
	JavaScript    = bson.JavaScript
	CodeWithScope = bson.CodeWithScope
	MinKey        = bson.MinKey
	MaxKey        = bson.MaxKey
	Undefined     = bson.Undefined
	DBPointer     = bson.DBPointer
	Symbol        = bson.Symbol
)

// DefaultMaxBytes is the default maximum BSON document size.
const DefaultMaxBytes int64 = 1 << 20

// ErrPayloadTooLarge identifies BSON documents over the configured byte limit.
var ErrPayloadTooLarge = errors.New("payload exceeds size limit")

var errDuplicateKey = errors.New("duplicate BSON key")

// DecodeOptions controls BSON decoding and explicit interoperability choices.
type DecodeOptions struct {
	MaxBytes               int64
	AllowDuplicateKeys     bool
	ObjectIDAsHexString    bool
	AllowTruncatingDoubles bool
}

// EncodeOptions controls BSON document encoding.
type EncodeOptions struct {
	MaxBytes             int64
	AllowDuplicateKeys   bool
	MinimizeIntegerWidth bool
	UseJSONStructTags    bool
}

// Decode parses exactly one complete BSON document into target.
func Decode(payload []byte, target any, options DecodeOptions) error {
	return DecodeReader(bytes.NewReader(payload), target, options)
}

// DecodeReader reads a bounded stream and parses exactly one BSON document.
func DecodeReader(reader io.Reader, target any, options DecodeOptions) error {
	if err := validateTarget(target); err != nil {
		return wrap(wire.ErrorKindTarget, "decode", err)
	}
	if options.MaxBytes < 0 {
		return wrap(wire.ErrorKindValidation, "decode options", errors.New("max bytes must not be negative"))
	}
	payload, err := readBounded(reader, options.MaxBytes)
	if err != nil {
		return err
	}
	raw, err := validateDocument(payload, options.AllowDuplicateKeys)
	if err != nil {
		return wrap(wire.ErrorKindParse, "decode", err)
	}
	decoder := bson.NewDecoder(bson.NewDocumentReader(bytes.NewReader(raw)))
	if options.ObjectIDAsHexString {
		decoder.ObjectIDAsHexString()
	}
	if options.AllowTruncatingDoubles {
		decoder.AllowTruncatingDoubles()
	}
	if err := decoder.Decode(target); err != nil {
		return wrap(wire.ErrorKindValidation, "decode", err)
	}
	return nil
}

// Encode serializes a BSON document. Struct and D ordering is stable; M map
// ordering follows Go map iteration and is intentionally not guaranteed.
func Encode(value any, options EncodeOptions) ([]byte, error) {
	if err := valuecheck.Validate(value); err != nil {
		return nil, wrap(wire.ErrorKindEncode, "encode", err)
	}
	if !isDocumentValue(value) {
		return nil, wrap(wire.ErrorKindUnsupported, "encode", errors.New("BSON requires a top-level document"))
	}
	output, err := outputlimit.New(options.MaxBytes, DefaultMaxBytes)
	if err != nil {
		return nil, wrap(wire.ErrorKindValidation, "encode options", err)
	}
	encoder := bson.NewEncoder(bson.NewDocumentWriter(output))
	if options.MinimizeIntegerWidth {
		encoder.IntMinSize()
	}
	if options.UseJSONStructTags {
		encoder.UseJSONStructTags()
	}
	if err := encoder.Encode(value); err != nil {
		if errors.Is(err, outputlimit.ErrLimit) {
			return nil, wrap(wire.ErrorKindSizeLimit, "encode", errors.Join(ErrPayloadTooLarge, err))
		}
		return nil, wrap(wire.ErrorKindEncode, "encode", err)
	}
	if _, err := validateDocument(output.Bytes(), options.AllowDuplicateKeys); err != nil {
		return nil, wrap(wire.ErrorKindValidation, "encode", err)
	}
	return output.Bytes(), nil
}

// EncodeWriter serializes value and writes one complete BSON document.
func EncodeWriter(writer io.Writer, value any, options EncodeOptions) error {
	payload, err := Encode(value, options)
	if err != nil {
		return err
	}
	if writer == nil {
		return wrap(wire.ErrorKindValidation, "write", errors.New("writer must not be nil"))
	}
	if _, err := io.Copy(writer, bytes.NewReader(payload)); err != nil {
		return wrap(wire.ErrorKindWrite, "write", err)
	}
	return nil
}

func isDocumentValue(value any) bool {
	if value == nil {
		return false
	}
	if _, ok := value.(bson.Marshaler); ok {
		return true
	}
	typeOf := reflect.TypeOf(value)
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if typeOf == reflect.TypeFor[bson.D]() || typeOf == reflect.TypeFor[bson.Raw]() {
		return true
	}
	return typeOf.Kind() == reflect.Struct || typeOf.Kind() == reflect.Map
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

func readBounded(reader io.Reader, configuredMax int64) ([]byte, error) {
	if reader == nil {
		return nil, wrap(wire.ErrorKindValidation, "read", errors.New("reader must not be nil"))
	}
	maxBytes := configuredMax
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}
	readLimit := maxBytes + 1
	if maxBytes == math.MaxInt64 {
		readLimit = maxBytes
	}
	payload, err := io.ReadAll(io.LimitReader(reader, readLimit))
	if err != nil {
		return nil, wrap(wire.ErrorKindParse, "read", err)
	}
	if int64(len(payload)) > maxBytes {
		return nil, wrap(wire.ErrorKindSizeLimit, "read", ErrPayloadTooLarge)
	}
	return payload, nil
}

func validateDocument(payload []byte, allowDuplicates bool) (bson.Raw, error) {
	if len(payload) < 5 {
		return nil, errors.New("BSON document is shorter than its minimum length")
	}
	declared := int64(binary.LittleEndian.Uint32(payload[:4]))
	if declared != int64(len(payload)) {
		return nil, errors.New("BSON length prefix does not match payload length")
	}
	raw := bson.Raw(payload)
	if err := raw.Validate(); err != nil {
		return nil, err
	}
	if !allowDuplicates {
		if err := validateDocumentKeys(raw); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

func validateDocumentKeys(raw bson.Raw) error {
	// validateDocument has already recursively validated element boundaries.
	elements, _ := raw.Elements()
	seen := make(map[string]struct{}, len(elements))
	for _, element := range elements {
		key := element.Key()
		if _, exists := seen[key]; exists {
			return errors.Join(errDuplicateKey, errors.New(key))
		}
		seen[key] = struct{}{}
		if err := validateValueKeys(element.Value()); err != nil {
			return err
		}
	}
	return nil
}

func validateValueKeys(value bson.RawValue) error {
	if document, ok := value.DocumentOK(); ok {
		return validateDocumentKeys(document)
	}
	if array, ok := value.ArrayOK(); ok {
		values, _ := array.Values()
		for _, item := range values {
			if err := validateValueKeys(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func wrap(kind wire.ErrorKind, op string, err error) error {
	return &wire.Error{Kind: kind, Format: wire.FormatBSON, Op: op, Err: err}
}

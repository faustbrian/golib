package msgpackwire

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/internal/outputlimit"
	"github.com/faustbrian/golib/pkg/wire/internal/valuecheck"
	"github.com/vmihailenco/msgpack/v5"
	"github.com/vmihailenco/msgpack/v5/msgpcode"
)

// DefaultMaxBytes is the default maximum MessagePack object size.
const DefaultMaxBytes int64 = 1 << 20

const (
	// DefaultMaxNestedLevels bounds recursive arrays and maps.
	DefaultMaxNestedLevels = 32
	// DefaultMaxArrayElements bounds allocation amplification from arrays.
	DefaultMaxArrayElements = 128 << 10
	// DefaultMaxMapPairs bounds allocation amplification from maps.
	DefaultMaxMapPairs = 64 << 10
)

// ErrPayloadTooLarge identifies MessagePack objects over the byte limit.
var ErrPayloadTooLarge = errors.New("payload exceeds size limit")

// DecodeOptions controls MessagePack decoding and integer interoperability.
type DecodeOptions struct {
	MaxBytes               int64
	MaxNestedLevels        int
	MaxArrayElements       int
	MaxMapPairs            int
	AllowDuplicateKeys     bool
	DisallowUnknownFields  bool
	NormalizeNumericWidths bool
}

// EncodeOptions controls deterministic MessagePack serialization.
type EncodeOptions struct {
	MaxBytes        int64
	CompactIntegers bool
	CompactFloats   bool
	StructAsArray   bool
}

// Decode parses exactly one MessagePack object into target.
func Decode(payload []byte, target any, options DecodeOptions) error {
	return DecodeReader(bytes.NewReader(payload), target, options)
}

// DecodeReader reads one bounded MessagePack object into target.
func DecodeReader(reader io.Reader, target any, options DecodeOptions) error {
	if err := validateTarget(target); err != nil {
		return wrap(wire.ErrorKindTarget, "decode", err)
	}
	if err := validateOptions(options); err != nil {
		return wrap(wire.ErrorKindValidation, "decode options", err)
	}
	payload, err := readBounded(reader, options.MaxBytes)
	if err != nil {
		return err
	}
	if err := validateMessagePackStructure(payload, options); err != nil {
		kind := wire.ErrorKindParse
		if errors.Is(err, errStructuralLimit) {
			kind = wire.ErrorKindSizeLimit
		}
		return wrap(kind, "decode", err)
	}
	decoder := msgpack.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields(options.DisallowUnknownFields)
	decoder.UseLooseInterfaceDecoding(options.NormalizeNumericWidths)
	if err := validateNumericPayload(payload, target, options.AllowDuplicateKeys); err != nil {
		kind := wire.ErrorKindValidation
		if errors.Is(err, errDuplicateKey) {
			kind = wire.ErrorKindParse
		}
		return wrap(kind, "decode", err)
	}
	if err := decoder.Decode(target); err != nil {
		return classifyDecodeError(err, target)
	}
	return nil
}

var errStructuralLimit = errors.New("MessagePack structural limit exceeded")

var errDuplicateKey = errors.New("duplicate MessagePack map key")

func validateOptions(options DecodeOptions) error {
	switch {
	case options.MaxBytes < 0:
		return errors.New("max bytes must not be negative")
	case options.MaxNestedLevels < 0:
		return errors.New("max nested levels must not be negative")
	case options.MaxArrayElements < 0:
		return errors.New("max array elements must not be negative")
	case options.MaxMapPairs < 0:
		return errors.New("max map pairs must not be negative")
	default:
		return nil
	}
}

// Encode serializes value with lexically sorted map keys.
func Encode(value any, options EncodeOptions) ([]byte, error) {
	if err := valuecheck.Validate(value); err != nil {
		return nil, wrap(wire.ErrorKindEncode, "encode", err)
	}
	output, err := outputlimit.New(options.MaxBytes, DefaultMaxBytes)
	if err != nil {
		return nil, wrap(wire.ErrorKindValidation, "encode options", err)
	}
	encoder := msgpack.NewEncoder(output)
	encoder.SetSortMapKeys(true)
	encoder.UseCompactInts(options.CompactIntegers)
	encoder.UseCompactFloats(options.CompactFloats)
	encoder.UseArrayEncodedStructs(options.StructAsArray)
	if err := encoder.Encode(value); err != nil {
		if errors.Is(err, outputlimit.ErrLimit) {
			return nil, wrap(wire.ErrorKindSizeLimit, "encode", errors.Join(ErrPayloadTooLarge, err))
		}
		kind := wire.ErrorKindEncode
		if strings.Contains(err.Error(), "unsupported") {
			kind = wire.ErrorKindUnsupported
		}
		return nil, wrap(kind, "encode", err)
	}
	return output.Bytes(), nil
}

// EncodeWriter serializes value and writes one complete MessagePack object.
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

func classifyDecodeError(err error, target any) error {
	message := err.Error()
	if strings.Contains(message, "unknown ext id") {
		return wrap(wire.ErrorKindUnsupported, "decode", err)
	}
	if reflect.ValueOf(target).Elem().Kind() == reflect.Interface &&
		strings.Contains(message, "decoding string/bytes length") {
		return wrap(wire.ErrorKindUnsupported, "decode", errors.New("untyped MessagePack maps require string keys"))
	}
	return wrap(wire.ErrorKindValidation, "decode", err)
}

func validateMessagePackStructure(payload []byte, options DecodeOptions) error {
	decoder := msgpack.NewDecoder(bytes.NewReader(payload))
	limits := structuralLimits{
		maxNestedLevels:  defaultLimit(options.MaxNestedLevels, DefaultMaxNestedLevels),
		maxArrayElements: defaultLimit(options.MaxArrayElements, DefaultMaxArrayElements),
		maxMapPairs:      defaultLimit(options.MaxMapPairs, DefaultMaxMapPairs),
	}
	if err := validateMessagePackValue(decoder, limits, 0); err != nil {
		return err
	}
	if _, err := decoder.PeekCode(); err == nil {
		return errors.New("multiple MessagePack objects")
	}
	return nil
}

type structuralLimits struct {
	maxNestedLevels  int
	maxArrayElements int
	maxMapPairs      int
}

func defaultLimit(configured, fallback int) int {
	if configured == 0 {
		return fallback
	}
	return configured
}

func validateMessagePackValue(decoder *msgpack.Decoder, limits structuralLimits, depth int) error {
	code, err := decoder.PeekCode()
	if err != nil {
		return err
	}
	if msgpcode.IsFixedArray(code) || code == msgpcode.Array16 || code == msgpcode.Array32 {
		length, err := decoder.DecodeArrayLen()
		if err != nil {
			return err
		}
		if length > limits.maxArrayElements {
			return fmt.Errorf("%w: array has %d elements, maximum is %d", errStructuralLimit, length, limits.maxArrayElements)
		}
		if depth+1 > limits.maxNestedLevels {
			return fmt.Errorf("%w: nesting exceeds %d levels", errStructuralLimit, limits.maxNestedLevels)
		}
		for range length {
			if err := validateMessagePackValue(decoder, limits, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if msgpcode.IsFixedMap(code) || code == msgpcode.Map16 || code == msgpcode.Map32 {
		length, err := decoder.DecodeMapLen()
		if err != nil {
			return err
		}
		if length > limits.maxMapPairs {
			return fmt.Errorf("%w: map has %d pairs, maximum is %d", errStructuralLimit, length, limits.maxMapPairs)
		}
		if depth+1 > limits.maxNestedLevels {
			return fmt.Errorf("%w: nesting exceeds %d levels", errStructuralLimit, limits.maxNestedLevels)
		}
		for range length {
			if err := validateMessagePackValue(decoder, limits, depth+1); err != nil {
				return err
			}
			if err := validateMessagePackValue(decoder, limits, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return decoder.Skip()
}

func validateNumericPayload(payload []byte, target any, allowDuplicateKeys bool) error {
	decoder := msgpack.NewDecoder(bytes.NewReader(payload))
	decoder.SetMapDecoder(func(decoder *msgpack.Decoder) (any, error) {
		return decodeNumericMap(decoder)
	})
	var source any
	if err := decoder.Decode(&source); err != nil {
		return nil //nolint:nilerr // the main decoder owns syntax errors
	}
	if !allowDuplicateKeys {
		if err := rejectDuplicateKeys(source); err != nil {
			return err
		}
	}
	return validateNumericFit(source, reflect.TypeOf(target))
}

func rejectDuplicateKeys(source any) error {
	switch value := source.(type) {
	case numericMap:
		for index, entry := range value {
			for previous := range index {
				if reflect.DeepEqual(value[previous].key, entry.key) {
					return errDuplicateKey
				}
			}
			if err := rejectDuplicateKeys(entry.key); err != nil {
				return err
			}
			if err := rejectDuplicateKeys(entry.value); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range value {
			if err := rejectDuplicateKeys(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateNumericFit(source any, target reflect.Type) error {
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if target == reflect.TypeFor[time.Time]() || source == nil {
		return nil
	}
	switch target.Kind() {
	case reflect.Interface:
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, ok := integerValue(source)
		if !ok || reflect.New(target).Elem().OverflowInt(value) {
			return errors.New("MessagePack integer overflows signed target")
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, ok := unsignedValue(source)
		if !ok || reflect.New(target).Elem().OverflowUint(value) {
			return errors.New("MessagePack integer overflows unsigned target")
		}
	case reflect.Float32, reflect.Float64:
		if value, ok := floatValue(source); ok {
			if reflect.New(target).Elem().OverflowFloat(value) ||
				target.Kind() == reflect.Float32 && float64(float32(value)) != value {
				return errors.New("MessagePack float overflows or loses precision in target")
			}
		}
	case reflect.Struct:
		if object, ok := source.(numericMap); ok {
			for _, field := range numericStructFields(target) {
				if value, exists := object.valueByStringKey(field.name); exists {
					if err := validateNumericFit(value, field.target); err != nil {
						return err
					}
				}
			}
			return nil
		}
		object := reflect.ValueOf(source)
		if object.Kind() != reflect.Slice && object.Kind() != reflect.Array {
			return nil
		}
		fields := numericStructFields(target)
		for position, field := range fields {
			if position < object.Len() {
				if err := validateNumericFit(object.Index(position).Interface(), field.target); err != nil {
					return err
				}
			}
		}
	case reflect.Map:
		object, ok := source.(numericMap)
		if !ok {
			return nil
		}
		for _, entry := range object {
			if err := validateNumericFit(entry.key, target.Key()); err != nil {
				return err
			}
			if err := validateNumericFit(entry.value, target.Elem()); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		items, ok := source.([]any)
		if !ok {
			return nil
		}
		for _, value := range items {
			if err := validateNumericFit(value, target.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

type numericStructField struct {
	name   string
	target reflect.Type
}

func numericStructFields(target reflect.Type) []numericStructField {
	fields := make([]numericStructField, 0, target.NumField())
	for index := range target.NumField() {
		field := target.Field(index)
		tag := strings.Split(field.Tag.Get("msgpack"), ",")
		if tag[0] == "-" || field.Name == "_msgpack" {
			continue
		}
		if shouldInlineNumericField(field, tag[1:]) {
			embedded := field.Type
			for embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			fields = append(fields, numericStructFields(embedded)...)
			continue
		}
		if !field.IsExported() {
			continue
		}
		name := tag[0]
		if name == "" {
			name = field.Name
		}
		fields = append(fields, numericStructField{name: name, target: field.Type})
	}
	return fields
}

func shouldInlineNumericField(field reflect.StructField, options []string) bool {
	if !field.Anonymous || hasTagOption(options, "noinline") {
		return false
	}
	target := field.Type
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	return target.Kind() == reflect.Struct
}

func hasTagOption(options []string, expected string) bool {
	for _, option := range options {
		if option == expected {
			return true
		}
	}
	return false
}

type numericMapEntry struct {
	key   any
	value any
}

type numericMap []numericMapEntry

func decodeNumericMap(decoder *msgpack.Decoder) (any, error) {
	size, err := decoder.DecodeMapLen()
	if err != nil {
		return nil, err
	}
	entries := make(numericMap, 0, size)
	for range size {
		key, err := decoder.DecodeInterface()
		if err != nil {
			return nil, err
		}
		value, err := decoder.DecodeInterface()
		if err != nil {
			return nil, err
		}
		entries = append(entries, numericMapEntry{key: key, value: value})
	}
	return entries, nil
}

func (mapping numericMap) valueByStringKey(name string) (any, bool) {
	for _, entry := range mapping {
		if key, ok := entry.key.(string); ok && key == name {
			return entry.value, true
		}
	}
	return nil, false
}

func integerValue(source any) (int64, bool) {
	value := reflect.ValueOf(source)
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		unsigned := value.Uint()
		if unsigned <= math.MaxInt64 {
			return int64(unsigned), true
		}
	}
	return 0, false
}

func unsignedValue(source any) (uint64, bool) {
	value := reflect.ValueOf(source)
	switch value.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return value.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		signed := value.Int()
		if signed >= 0 {
			return uint64(signed), true
		}
	}
	return 0, false
}

func floatValue(source any) (float64, bool) {
	value := reflect.ValueOf(source)
	if value.Kind() == reflect.Float32 || value.Kind() == reflect.Float64 {
		return value.Float(), true
	}
	return 0, false
}

func wrap(kind wire.ErrorKind, op string, err error) error {
	return &wire.Error{Kind: kind, Format: wire.FormatMessagePack, Op: op, Err: err}
}

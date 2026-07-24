package cborwire

import (
	"bytes"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/internal/outputlimit"
	"github.com/faustbrian/golib/pkg/wire/internal/valuecheck"
	"github.com/fxamacker/cbor/v2"
)

// DefaultMaxBytes is the default maximum CBOR item size.
const DefaultMaxBytes int64 = 1 << 20

// ErrPayloadTooLarge identifies CBOR items over the configured byte limit.
var ErrPayloadTooLarge = errors.New("payload exceeds size limit")

// DeterministicProfile identifies a defined deterministic CBOR encoding.
type DeterministicProfile uint8

const (
	// Canonical uses the canonical encoding defined by RFC 7049 section 3.9.
	Canonical DeterministicProfile = iota
	// CoreDeterministic uses RFC 8949 core deterministic encoding.
	CoreDeterministic
	// CTAP2Deterministic uses the CTAP2 canonical CBOR profile.
	CTAP2Deterministic
)

// DecodeOptions controls CBOR interoperability and resource constraints.
// Zero resource limits use fxamacker/cbor's bounded defaults.
type DecodeOptions struct {
	MaxBytes              int64
	MaxNestedLevels       int
	MaxArrayElements      int
	MaxMapPairs           int
	DisallowUnknownFields bool
	AllowTags             bool
	AllowIndefiniteLength bool
}

// EncodeOptions controls the deterministic profile and tag behavior.
type EncodeOptions struct {
	MaxBytes  int64
	Profile   DeterministicProfile
	AllowTags bool
	TimeTag   bool
}

// Decode parses exactly one CBOR data item into target.
func Decode(payload []byte, target any, options DecodeOptions) error {
	return DecodeReader(bytes.NewReader(payload), target, options)
}

// DecodeReader reads a bounded stream and parses exactly one CBOR data item.
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
	mode, err := decodeMode(options)
	if err != nil {
		return wrap(wire.ErrorKindValidation, "decode options", err)
	}
	if err := mode.Unmarshal(payload, target); err != nil {
		return classifyDecodeError(err)
	}
	return nil
}

// Encode serializes value using a defined deterministic CBOR profile.
func Encode(value any, options EncodeOptions) ([]byte, error) {
	if err := valuecheck.Validate(value); err != nil {
		return nil, wrap(wire.ErrorKindEncode, "encode", err)
	}
	if options.TimeTag && !options.AllowTags {
		return nil, wrap(wire.ErrorKindValidation, "encode options", errors.New("time tag requires tags to be allowed"))
	}
	encodingOptions, err := encodingOptions(options.Profile)
	if err != nil {
		return nil, wrap(wire.ErrorKindValidation, "encode options", err)
	}
	if options.AllowTags {
		encodingOptions.TagsMd = cbor.TagsAllowed
	} else {
		encodingOptions.TagsMd = cbor.TagsForbidden
	}
	encodingOptions.Time = cbor.TimeUnix
	if options.TimeTag {
		encodingOptions.TimeTag = cbor.EncTagRequired
	}
	// Every field above is selected from a validated library constant, so mode
	// construction cannot fail after the profile and tag relationship checks.
	mode, _ := encodingOptions.EncMode()
	output, err := outputlimit.New(options.MaxBytes, DefaultMaxBytes)
	if err != nil {
		return nil, wrap(wire.ErrorKindValidation, "encode options", err)
	}
	err = mode.NewEncoder(output).Encode(value)
	if err != nil {
		if errors.Is(err, outputlimit.ErrLimit) {
			return nil, wrap(wire.ErrorKindSizeLimit, "encode", errors.Join(ErrPayloadTooLarge, err))
		}
		var tagsError *cbor.TagsMdError
		if errors.As(err, &tagsError) || strings.Contains(err.Error(), "TagsMd is TagsForbidden") {
			return nil, wrap(wire.ErrorKindUnsupported, "encode", err)
		}
		return nil, wrap(wire.ErrorKindEncode, "encode", err)
	}
	return output.Bytes(), nil
}

// EncodeWriter serializes value and writes one complete CBOR data item.
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

func decodeMode(options DecodeOptions) (cbor.DecMode, error) {
	decodeOptions := cbor.DecOptions{
		DupMapKey:        cbor.DupMapKeyEnforcedAPF,
		MaxNestedLevels:  options.MaxNestedLevels,
		MaxArrayElements: options.MaxArrayElements,
		MaxMapPairs:      options.MaxMapPairs,
		IntDec:           cbor.IntDecConvertNone,
	}
	if options.DisallowUnknownFields {
		decodeOptions.ExtraReturnErrors = cbor.ExtraDecErrorUnknownField
	}
	if options.AllowTags {
		decodeOptions.TagsMd = cbor.TagsAllowed
	} else {
		decodeOptions.TagsMd = cbor.TagsForbidden
	}
	if options.AllowIndefiniteLength {
		decodeOptions.IndefLength = cbor.IndefLengthAllowed
	} else {
		decodeOptions.IndefLength = cbor.IndefLengthForbidden
	}
	return decodeOptions.DecMode()
}

func encodingOptions(profile DeterministicProfile) (cbor.EncOptions, error) {
	switch profile {
	case Canonical:
		return cbor.CanonicalEncOptions(), nil
	case CoreDeterministic:
		return cbor.CoreDetEncOptions(), nil
	case CTAP2Deterministic:
		return cbor.CTAP2EncOptions(), nil
	default:
		return cbor.EncOptions{}, errors.New("unknown deterministic profile")
	}
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

func classifyDecodeError(err error) error {
	var nested *cbor.MaxNestedLevelError
	var array *cbor.MaxArrayElementsError
	var mapping *cbor.MaxMapPairsError
	if errors.As(err, &nested) || errors.As(err, &array) || errors.As(err, &mapping) {
		return wrap(wire.ErrorKindSizeLimit, "decode", err)
	}
	var indefinite *cbor.IndefiniteLengthError
	var tags *cbor.TagsMdError
	var unacceptable cbor.UnacceptableDataItemError
	if errors.As(err, &indefinite) || errors.As(err, &tags) || errors.As(err, &unacceptable) {
		return wrap(wire.ErrorKindUnsupported, "decode", err)
	}
	var target *cbor.UnmarshalTypeError
	var unknown *cbor.UnknownFieldError
	if errors.As(err, &target) || errors.As(err, &unknown) {
		return wrap(wire.ErrorKindValidation, "decode", err)
	}
	return wrap(wire.ErrorKindParse, "decode", err)
}

func wrap(kind wire.ErrorKind, op string, err error) error {
	return &wire.Error{Kind: kind, Format: wire.FormatCBOR, Op: op, Err: err}
}

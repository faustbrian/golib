// Package encoding provides optional deterministic codecs for math values.
package encoding

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/bigfloat"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/math/rational"
)

const (
	version      byte = 1
	kindInteger  byte = 1
	kindRational byte = 2
	kindDecimal  byte = 3
	kindFloat    byte = 4
)

const magic = "GM"

// MarshalInteger returns the versioned deterministic integer encoding.
func MarshalInteger(value integer.Integer) ([]byte, error) {
	result := header(kindInteger)

	return appendSigned(result, value.Big()), nil
}

// UnmarshalInteger decodes a versioned integer under explicit limits.
func UnmarshalInteger(data []byte, limits gomath.Limits) (integer.Integer, error) {
	reader, err := newReader(data, kindInteger, limits)
	if err != nil {
		return integer.Integer{}, err
	}
	value, err := reader.signed()
	if err != nil || !reader.done() {
		return integer.Integer{}, invalidEncoding(err)
	}

	result, err := integer.FromBig(value, limits)
	if err != nil {
		return integer.Integer{}, err
	}
	encoded, err := MarshalInteger(result)
	if err != nil || !bytes.Equal(data, encoded) {
		return integer.Integer{}, invalidEncoding(err)
	}

	return result, nil
}

// MarshalRational returns the versioned deterministic normalized encoding.
func MarshalRational(value rational.Rational) ([]byte, error) {
	result := appendSigned(header(kindRational), value.Numerator())

	return appendMagnitude(result, value.Denominator()), nil
}

// UnmarshalRational decodes a versioned rational under explicit limits.
func UnmarshalRational(data []byte, limits gomath.Limits) (rational.Rational, error) {
	reader, err := newReader(data, kindRational, limits)
	if err != nil {
		return rational.Rational{}, err
	}
	numerator, err := reader.signed()
	if err != nil {
		return rational.Rational{}, invalidEncoding(err)
	}
	denominator, err := reader.magnitude()
	if err != nil || denominator.Sign() == 0 || !reader.done() {
		return rational.Rational{}, invalidEncoding(err)
	}

	result, err := rational.NewChecked(numerator, denominator, limits)
	if err != nil {
		return rational.Rational{}, err
	}
	encoded, err := MarshalRational(result)
	if err != nil || !bytes.Equal(data, encoded) {
		return rational.Rational{}, invalidEncoding(err)
	}

	return result, nil
}

// MarshalDecimal returns the versioned coefficient-and-exponent encoding.
func MarshalDecimal(value decimal.Decimal) ([]byte, error) {
	result := appendSigned(header(kindDecimal), value.Coefficient())
	buffer := make([]byte, binary.MaxVarintLen32)
	length := binary.PutVarint(buffer, int64(value.Exponent()))

	return append(result, buffer[:length]...), nil
}

// UnmarshalDecimal decodes a versioned representation-preserving decimal.
func UnmarshalDecimal(data []byte, limits gomath.Limits) (decimal.Decimal, error) {
	reader, err := newReader(data, kindDecimal, limits)
	if err != nil {
		return decimal.Decimal{}, err
	}
	coefficient, err := reader.signed()
	if err != nil {
		return decimal.Decimal{}, invalidEncoding(err)
	}
	exponent, err := reader.varint()
	if err != nil || exponent < -1<<31 || exponent > 1<<31-1 || !reader.done() {
		return decimal.Decimal{}, invalidEncoding(err)
	}

	result, err := decimal.FromBig(coefficient, int32(exponent), limits)
	if err != nil {
		return decimal.Decimal{}, err
	}
	encoded, err := MarshalDecimal(result)
	if err != nil || !bytes.Equal(data, encoded) {
		return decimal.Decimal{}, invalidEncoding(err)
	}

	return result, nil
}

// MarshalFloat returns a versioned encoding that retains binary precision and
// the shared rounding policy.
func MarshalFloat(value bigfloat.Float) ([]byte, error) {
	return marshalFloat(value, (*big.Float).GobEncode)
}

func marshalFloat(value bigfloat.Float, encode func(*big.Float) ([]byte, error)) ([]byte, error) {
	payload, err := encode(value.Big())
	if err != nil {
		return nil, fmt.Errorf("encode big float: %w", err)
	}
	if payload == nil {
		return nil, fmt.Errorf("%w: empty big float encoding", gomath.ErrInvalidSyntax)
	}
	result := append(header(kindFloat), byte(value.Rounding()))
	result = appendLength(result, len(payload))

	return append(result, payload...), nil
}

// UnmarshalFloat decodes a versioned binary Float under explicit limits.
func UnmarshalFloat(data []byte, limits gomath.Limits) (bigfloat.Float, error) {
	reader, err := newReader(data, kindFloat, limits)
	if err != nil {
		return bigfloat.Float{}, err
	}
	rounding, err := reader.byte()
	if err != nil {
		return bigfloat.Float{}, invalidEncoding(err)
	}
	payload, err := reader.bytes()
	if err != nil || !reader.done() {
		return bigfloat.Float{}, invalidEncoding(err)
	}
	var decoded big.Float
	if err := decoded.GobDecode(payload); err != nil {
		return bigfloat.Float{}, invalidEncoding(err)
	}
	result, err := bigfloat.FromBig(&decoded, bigfloat.Context{
		Precision: decoded.Prec(),
		Rounding:  gomath.RoundingMode(rounding),
		Limits:    limits,
	})
	if err != nil {
		return bigfloat.Float{}, err
	}
	encoded, err := MarshalFloat(result.Value)
	if err != nil || !bytes.Equal(data, encoded) {
		return bigfloat.Float{}, invalidEncoding(err)
	}

	return result.Value, nil
}

type reader struct {
	data   []byte
	offset int
}

func newReader(data []byte, kind byte, limits gomath.Limits) (*reader, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	maximumBytes := limits.MaxIntermediateBits/8 + 64
	if len(data) < 4 || len(data) > maximumBytes {
		return nil, fmt.Errorf("%w: binary payload size", gomath.ErrLimitExceeded)
	}
	if data[0] != magic[0] || data[1] != magic[1] || data[2] != version || data[3] != kind {
		return nil, gomath.ErrInvalidSyntax
	}

	return &reader{data: data, offset: 4}, nil
}

func (r *reader) signed() (*big.Int, error) {
	sign, err := r.byte()
	if err != nil || sign > 2 {
		return nil, gomath.ErrInvalidSyntax
	}
	magnitude, err := r.magnitude()
	if err != nil {
		return nil, err
	}
	if sign == 0 && magnitude.Sign() != 0 || sign != 0 && magnitude.Sign() == 0 {
		return nil, gomath.ErrInvalidSyntax
	}
	if sign == 2 {
		magnitude.Neg(magnitude)
	}

	return magnitude, nil
}

func (r *reader) magnitude() (*big.Int, error) {
	payload, err := r.bytes()
	if err != nil {
		return nil, err
	}
	if len(payload) > 0 && payload[0] == 0 {
		return nil, gomath.ErrInvalidSyntax
	}

	return new(big.Int).SetBytes(payload), nil
}

func (r *reader) bytes() ([]byte, error) {
	length, count := binary.Uvarint(r.data[r.offset:])
	if count <= 0 {
		return nil, gomath.ErrInvalidSyntax
	}
	r.offset += count
	if length > uint64(len(r.data)-r.offset) {
		return nil, gomath.ErrInvalidSyntax
	}
	result := r.data[r.offset : r.offset+int(length)]
	r.offset += int(length)

	return result, nil
}

func (r *reader) varint() (int64, error) {
	value, count := binary.Varint(r.data[r.offset:])
	if count <= 0 {
		return 0, gomath.ErrInvalidSyntax
	}
	r.offset += count

	return value, nil
}

func (r *reader) byte() (byte, error) {
	if r.offset >= len(r.data) {
		return 0, gomath.ErrInvalidSyntax
	}
	value := r.data[r.offset]
	r.offset++

	return value, nil
}

func (r *reader) done() bool { return r.offset == len(r.data) }

func header(kind byte) []byte { return []byte{magic[0], magic[1], version, kind} }

func appendSigned(destination []byte, value *big.Int) []byte {
	sign := byte(0)
	if value.Sign() > 0 {
		sign = 1
	} else if value.Sign() < 0 {
		sign = 2
	}
	destination = append(destination, sign)

	return appendMagnitude(destination, new(big.Int).Abs(value))
}

func appendMagnitude(destination []byte, value *big.Int) []byte {
	payload := value.Bytes()
	destination = appendLength(destination, len(payload))

	return append(destination, payload...)
}

func appendLength(destination []byte, length int) []byte {
	buffer := make([]byte, binary.MaxVarintLen64)
	count := binary.PutUvarint(buffer, uint64(length))

	return append(destination, buffer[:count]...)
}

func invalidEncoding(err error) error {
	if err == nil {
		return gomath.ErrInvalidSyntax
	}

	return fmt.Errorf("%w: %v", gomath.ErrInvalidSyntax, err)
}

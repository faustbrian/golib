package authorization

import (
	"errors"
	"math"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"time"
)

var ErrInvalidFloat = errors.New("attribute float must be finite")

// ValueKind is the closed set of supported attribute representations.
type ValueKind uint8

const (
	ValueMissing ValueKind = iota
	ValueNull
	ValueString
	ValueBool
	ValueInt
	ValueFloat
	ValueTime
	ValueIP
	ValueStringSet
)

// Value is an immutable typed attribute value. Its zero value represents an
// invalid or missing value and is distinct from explicit null.
type Value struct {
	kind      ValueKind
	stringVal string
	boolVal   bool
	intVal    int64
	floatVal  float64
	timeVal   time.Time
	ipVal     netip.Addr
	setVal    []string
}

func NullValue() Value {
	return Value{kind: ValueNull}
}

func StringValue(value string) Value {
	return Value{kind: ValueString, stringVal: value}
}

func BoolValue(value bool) Value {
	return Value{kind: ValueBool, boolVal: value}
}

func IntValue(value int64) Value {
	return Value{kind: ValueInt, intVal: value}
}

func FloatValue(value float64) (Value, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return Value{}, ErrInvalidFloat
	}

	return Value{kind: ValueFloat, floatVal: value}, nil
}

func MustFloatValue(value float64) Value {
	attribute, err := FloatValue(value)
	if err != nil {
		panic(err)
	}

	return attribute
}

func TimeValue(value time.Time) Value {
	return Value{kind: ValueTime, timeVal: value.Round(0).UTC()}
}

func IPValue(value netip.Addr) Value {
	return Value{kind: ValueIP, ipVal: value.Unmap()}
}

func StringSetValue(values []string) Value {
	set := append([]string(nil), values...)
	sort.Strings(set)
	unique := set[:0]
	for _, value := range set {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}

	return Value{kind: ValueStringSet, setVal: unique}
}

func (value Value) Kind() ValueKind {
	return value.kind
}

func (value Value) String() (string, bool) {
	return value.stringVal, value.kind == ValueString
}

func (value Value) Bool() (bool, bool) {
	return value.boolVal, value.kind == ValueBool
}

func (value Value) Int() (int64, bool) {
	return value.intVal, value.kind == ValueInt
}

func (value Value) Float() (float64, bool) {
	return value.floatVal, value.kind == ValueFloat
}

func (value Value) Time() (time.Time, bool) {
	return value.timeVal, value.kind == ValueTime
}

func (value Value) IP() (netip.Addr, bool) {
	return value.ipVal, value.kind == ValueIP
}

func (value Value) StringSet() ([]string, bool) {
	if value.kind != ValueStringSet {
		return nil, false
	}

	return append([]string(nil), value.setVal...), true
}

// CollectionLength reports the cardinality of collection values.
func (value Value) CollectionLength() (int, bool) {
	if value.kind != ValueStringSet {
		return 0, false
	}

	return len(value.setVal), true
}

// Equal reports structural equality without type coercion.
func (value Value) Equal(other Value) bool {
	if value.kind != other.kind {
		return false
	}

	switch value.kind {
	case ValueMissing, ValueNull:
		return true
	case ValueString:
		return value.stringVal == other.stringVal
	case ValueBool:
		return value.boolVal == other.boolVal
	case ValueInt:
		return value.intVal == other.intVal
	case ValueFloat:
		return value.floatVal == other.floatVal
	case ValueTime:
		return value.timeVal.Equal(other.timeVal)
	case ValueIP:
		return value.ipVal == other.ipVal
	case ValueStringSet:
		return slices.Equal(value.setVal, other.setVal)
	default:
		return false
	}
}

// Compare orders comparable values of the same kind without coercion.
func (value Value) Compare(other Value) (int, bool) {
	if value.kind != other.kind {
		return 0, false
	}

	switch value.kind {
	case ValueString:
		return strings.Compare(value.stringVal, other.stringVal), true
	case ValueInt:
		return compareOrdered(value.intVal, other.intVal), true
	case ValueFloat:
		return compareOrdered(value.floatVal, other.floatVal), true
	case ValueTime:
		return value.timeVal.Compare(other.timeVal), true
	default:
		return 0, false
	}
}

func compareOrdered[T ~int64 | ~float64](left, right T) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

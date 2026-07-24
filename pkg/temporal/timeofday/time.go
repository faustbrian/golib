// Package timeofday provides immutable date-independent local time values and
// circular daily interval algebra.
package timeofday

import (
	"fmt"
	"time"
	"unicode/utf8"

	temporal "github.com/faustbrian/golib/pkg/temporal"
)

const day = 24 * time.Hour

// WrapPolicy controls whether local-time shifts may cross a day boundary.
type WrapPolicy uint8

const (
	// Wrap applies arithmetic modulo one fixed 24-hour day.
	Wrap WrapPolicy = iota
	// RejectOverflow rejects shifts outside the local day.
	RejectOverflow
)

// Time is an immutable local time-of-day. The end boundary has offset 24 hours
// and remains distinct from ordinary midnight at offset zero.
type Time struct {
	offset           time.Duration
	fractionalDigits uint8
	hasSeconds       bool
}

// New constructs a local time with explicit fractional-second precision.
// digits is between zero and nine and nanosecond must be exactly representable
// at that precision.
func New(hour, minute, second, nanosecond, digits int) (Time, error) {
	if hour < 0 || hour >= 24 ||
		minute < 0 || minute >= 60 ||
		second < 0 || second >= 60 ||
		nanosecond < 0 || nanosecond >= 1_000_000_000 ||
		digits < 0 || digits > 9 ||
		nanosecond%precisionUnit(digits) != 0 {
		return Time{}, temporal.ErrInvalidTime
	}

	return Time{
		offset: time.Duration(hour)*time.Hour +
			time.Duration(minute)*time.Minute +
			time.Duration(second)*time.Second +
			time.Duration(nanosecond),
		fractionalDigits: uint8(digits),
		hasSeconds:       true,
	}, nil
}

// Midnight returns ordinary 00:00:00.
func Midnight() Time {
	return Time{hasSeconds: true}
}

// Noon returns 12:00:00.
func Noon() Time {
	return Time{offset: 12 * time.Hour, hasSeconds: true}
}

// EndOfDay returns the distinct 24:00 end-boundary representation.
func EndOfDay() Time {
	return Time{offset: day}
}

// FromOffset constructs a local time from a fixed offset since midnight.
// The exact 24-hour offset produces the distinct end boundary.
func FromOffset(offset time.Duration, digits int) (Time, error) {
	if offset < 0 || offset > day {
		return Time{}, temporal.ErrInvalidTime
	}
	if digits < 0 || digits > 9 || offset%time.Duration(precisionUnit(digits)) != 0 {
		return Time{}, temporal.ErrPrecision
	}
	if offset == day {
		return EndOfDay(), nil
	}
	hour := int(offset / time.Hour)
	offset %= time.Hour
	minute := int(offset / time.Minute)
	offset %= time.Minute
	second := int(offset / time.Second)
	nanosecond := int(offset % time.Second)
	return New(hour, minute, second, nanosecond, digits)
}

// Parse strictly decodes HH:MM, HH:MM:SS, or a dot-fraction ISO local time.
func Parse(value string, limits temporal.Limits) (Time, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return Time{}, err
	}
	if len(value) > limits.ParseBytes {
		return Time{}, &temporal.LimitError{
			Field: "parse_bytes",
			Value: len(value),
			Max:   limits.ParseBytes,
		}
	}
	if !utf8.ValidString(value) {
		return Time{}, fmt.Errorf("%w: invalid UTF-8", temporal.ErrParse)
	}
	if value == "24:00" {
		return EndOfDay(), nil
	}
	if len(value) < 5 || value[2] != ':' {
		return Time{}, temporal.ErrParse
	}

	hour, ok := twoDigits(value[0:2])
	if !ok {
		return Time{}, temporal.ErrParse
	}
	minute, ok := twoDigits(value[3:5])
	if !ok {
		return Time{}, temporal.ErrParse
	}
	if len(value) == 5 {
		parsed, err := New(hour, minute, 0, 0, 0)
		parsed.hasSeconds = false
		return parsed, err
	}
	if len(value) < 8 || value[5] != ':' {
		return Time{}, temporal.ErrParse
	}
	second, ok := twoDigits(value[6:8])
	if !ok {
		return Time{}, temporal.ErrParse
	}
	if len(value) == 8 {
		return New(hour, minute, second, 0, 0)
	}
	if value[8] != '.' {
		return Time{}, temporal.ErrParse
	}

	digits := len(value) - 9
	if digits < 1 || digits > limits.Precision {
		return Time{}, temporal.ErrPrecision
	}
	fraction := value[9:]
	for index := range fraction {
		if fraction[index] < '0' || fraction[index] > '9' {
			return Time{}, temporal.ErrParse
		}
	}
	nanosecond := 0
	for index := range fraction {
		nanosecond = nanosecond*10 + int(fraction[index]-'0')
	}
	for index := digits; index < 9; index++ {
		nanosecond *= 10
	}

	return New(hour, minute, second, nanosecond, digits)
}

// Components returns hour, minute, second, and nanosecond. EndOfDay returns
// 24, 0, 0, 0.
func (t Time) Components() (int, int, int, int) {
	if t.IsEndBoundary() {
		return 24, 0, 0, 0
	}

	remainder := t.offset
	hour := int(remainder / time.Hour)
	remainder %= time.Hour
	minute := int(remainder / time.Minute)
	remainder %= time.Minute
	second := int(remainder / time.Second)
	nanosecond := int(remainder % time.Second)

	return hour, minute, second, nanosecond
}

// FractionalDigits returns the preserved fractional-second precision.
func (t Time) FractionalDigits() int {
	return int(t.fractionalDigits)
}

// HasSeconds reports whether text encoding includes the seconds component.
func (t Time) HasSeconds() bool {
	return t.hasSeconds
}

// IsEndBoundary reports whether t is the distinct 24:00 value.
func (t Time) IsEndBoundary() bool {
	return t.offset == day
}

// Offset returns the fixed elapsed offset since ordinary midnight.
func (t Time) Offset() time.Duration {
	return t.offset
}

// Compare orders local values from midnight through the end boundary.
func (t Time) Compare(other Time) int {
	switch {
	case t.offset < other.offset:
		return -1
	case t.offset > other.offset:
		return 1
	default:
		return 0
	}
}

// Equal reports semantic local-time equality. Textual precision is metadata and
// does not affect equality; 24:00 remains unequal to 00:00.
func (t Time) Equal(other Time) bool {
	return t.offset == other.offset
}

// Clamp restricts t to the inclusive range from minimum through maximum.
func (t Time) Clamp(minimum, maximum Time) (Time, error) {
	if minimum.Compare(maximum) > 0 {
		return Time{}, temporal.ErrInvalidTime
	}
	if t.Compare(minimum) < 0 {
		return minimum, nil
	}
	if t.Compare(maximum) > 0 {
		return maximum, nil
	}

	return t, nil
}

// Shift adds a fixed duration according to policy.
func (t Time) Shift(duration time.Duration, policy WrapPolicy) (Time, error) {
	if duration == 0 {
		return t, nil
	}

	switch policy {
	case Wrap:
		delta := duration % day
		value := t.offset%day + delta
		value %= day
		if value < 0 {
			value += day
		}
		return Time{offset: value, fractionalDigits: 9, hasSeconds: true}, nil
	case RejectOverflow:
		if duration > 0 && t.offset > time.Duration(1<<63-1)-duration {
			return Time{}, temporal.ErrOverflow
		}
		value := t.offset + duration
		if value < 0 || value > day {
			return Time{}, temporal.ErrOverflow
		}
		return Time{offset: value, fractionalDigits: 9, hasSeconds: true}, nil
	default:
		return Time{}, temporal.ErrUnsupported
	}
}

// Difference returns other minus t as a signed fixed duration.
func (t Time) Difference(other Time) time.Duration {
	return other.offset - t.offset
}

// CircularDistance returns the shortest unsigned distance on a 24-hour clock.
func (t Time) CircularDistance(other Time) time.Duration {
	distance := t.Difference(other)
	if distance < 0 {
		distance = -distance
	}
	if distance > day-distance {
		return day - distance
	}

	return distance
}

// Round rounds t to a positive unit that evenly divides the fixed 24-hour
// daily universe. Exact half-unit ties round upward toward the end boundary.
func (t Time) Round(unit time.Duration, mode RoundingMode) (Time, error) {
	if unit <= 0 || unit > day || day%unit != 0 {
		return Time{}, temporal.ErrStep
	}
	if mode < RoundFloor || mode > RoundCeil {
		return Time{}, temporal.ErrUnsupported
	}
	remainder := t.offset % unit
	if remainder == 0 {
		return t, nil
	}
	value := t.offset - remainder
	switch mode {
	case RoundFloor:
	case RoundNearest:
		if remainder >= unit-remainder {
			value += unit
		}
	case RoundCeil:
		value += unit
	}
	if value == day {
		return EndOfDay(), nil
	}
	return Time{offset: value, hasSeconds: true}, nil
}

// String returns the strict round-trippable ISO local representation.
func (t Time) String() string {
	if t.IsEndBoundary() {
		return "24:00"
	}

	hour, minute, second, nanosecond := t.Components()
	if !t.hasSeconds {
		return fmt.Sprintf("%02d:%02d", hour, minute)
	}
	if t.fractionalDigits == 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hour, minute, second)
	}
	fraction := fmt.Sprintf("%09d", nanosecond)[:t.fractionalDigits]
	return fmt.Sprintf("%02d:%02d:%02d.%s", hour, minute, second, fraction)
}

func precisionUnit(digits int) int {
	unit := 1
	for index := digits; index < 9; index++ {
		unit *= 10
	}

	return unit
}

func twoDigits(value string) (int, bool) {
	if len(value) != 2 || value[0] < '0' || value[0] > '9' || value[1] < '0' || value[1] > '9' {
		return 0, false
	}

	return int(value[0]-'0')*10 + int(value[1]-'0'), true
}

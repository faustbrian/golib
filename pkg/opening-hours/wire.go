package openinghours

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxJSONBytes bounds canonical and parsed schedule documents.
	MaxJSONBytes = 1 << 20
	maxJSONDepth = 32
	wireVersion  = 1
)

type wireSchedule struct {
	Version          int              `json:"version"`
	Timezone         string           `json:"timezone"`
	Weekly           []wireWeekday    `json:"weekly"`
	Exceptions       []wireException  `json:"exceptions"`
	Composition      *wireComposition `json:"composition,omitempty"`
	Metadata         wireMetadata     `json:"metadata"`
	Effective        *wireEffective   `json:"effective,omitempty"`
	OutsideEffective string           `json:"outside_effective"`
}

type wireMetadata struct {
	Label    string `json:"label"`
	Source   string `json:"source"`
	Revision string `json:"revision"`
}

type wireEffective struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

type wireWeekday struct {
	Weekday string   `json:"weekday"`
	Rule    wireRule `json:"rule"`
}

type wireRule struct {
	State  string      `json:"state"`
	Ranges []wireRange `json:"ranges"`
}

type wireRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type wireException struct {
	Date      string   `json:"date"`
	Operation string   `json:"operation"`
	Rule      wireRule `json:"rule"`
	Priority  int      `json:"priority"`
	Source    string   `json:"source"`
	Revision  string   `json:"revision"`
	Set       string   `json:"set,omitempty"`
}

type wireComposition struct {
	Operation string       `json:"operation"`
	Left      wireSchedule `json:"left"`
	Right     wireSchedule `json:"right"`
}

var weekdayNames = [...]string{
	"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday",
}

// CanonicalJSON returns stable, compact, versioned JSON.
func (s Schedule) CanonicalJSON() ([]byte, error) {
	encoded, _ := json.Marshal(s.toWire()) // wireSchedule has no failing JSON values.
	if len(encoded) > MaxJSONBytes {
		return nil, newError("canonical json", CodeLimitExceeded)
	}

	return encoded, nil
}

func (s Schedule) toWire() wireSchedule {
	wire := wireSchedule{
		Version: wireVersion, Weekly: make([]wireWeekday, 0, 7),
		Exceptions: make([]wireException, 0), OutsideEffective: "closed",
	}
	if s.data == nil {
		return wire
	}
	wire.Timezone = s.data.timezone
	wire.Metadata = wireMetadata{
		Label: s.data.metadata.Label, Source: s.data.metadata.Source,
		Revision: s.data.metadata.Revision,
	}
	if s.data.outsideEffective == OutsideError {
		wire.OutsideEffective = "error"
	}
	if s.data.hasEffectiveStart || s.data.hasEffectiveEnd {
		wire.Effective = &wireEffective{}
		if s.data.hasEffectiveStart {
			wire.Effective.Start = formatDate(s.data.effectiveStart)
		}
		if s.data.hasEffectiveEnd {
			wire.Effective.End = formatDate(s.data.effectiveEnd)
		}
	}
	if s.data.composition != nil {
		wire.Composition = &wireComposition{
			Operation: compositionName(s.data.composition.operation),
			Left:      s.data.composition.left.toWire(), Right: s.data.composition.right.toWire(),
		}
		return wire
	}
	for weekday, name := range weekdayNames {
		wire.Weekly = append(wire.Weekly, wireWeekday{Weekday: name, Rule: ruleToWire(s.data.weekly[weekday])})
	}
	for _, exception := range s.data.exceptions {
		wire.Exceptions = append(wire.Exceptions, wireException{
			Date: formatDate(exception.date), Operation: exceptionName(exception.operation),
			Rule: ruleToWire(exception.rule), Priority: exception.priority,
			Source: exception.source, Revision: exception.revision,
			Set: exception.set,
		})
	}

	return wire
}

func ruleToWire(rule DayRule) wireRule {
	result := wireRule{State: dayStateName(rule.state), Ranges: make([]wireRange, 0, len(rule.ranges))}
	for _, item := range rule.ranges {
		result.Ranges = append(result.Ranges, wireRange{
			Start: formatLocalTime(item.start), End: formatLocalTime(item.end),
		})
	}

	return result
}

func dayStateName(state DayState) string {
	switch state {
	case DayOpenRanges:
		return "ranges"
	case DayOpenAllDay:
		return "all_day"
	case DayClosed:
		return "closed"
	case DayInherited:
		return "inherited"
	default:
		return "inherited"
	}
}

func exceptionName(operation ExceptionOperation) string {
	switch operation {
	case ExceptionAdd:
		return "add"
	case ExceptionSubtract:
		return "subtract"
	case ExceptionClose:
		return "close"
	case ExceptionReplace:
		return "replace"
	default:
		return "replace"
	}
}

func compositionName(operation compositionOperation) string {
	switch operation {
	case compositionIntersection:
		return "intersection"
	case compositionSubtract:
		return "subtract"
	case compositionOverlay:
		return "overlay"
	case compositionUnion:
		return "union"
	default:
		return "union"
	}
}

func formatDate(date Date) string {
	return date.String()
}

func formatLocalTime(localTime LocalTime) string {
	base := fmt.Sprintf("%02d:%02d:%02d", localTime.Hour(), localTime.Minute(), localTime.Second())
	if localTime.Nanosecond() == 0 {
		return base
	}

	return base + "." + strings.TrimRight(fmt.Sprintf("%09d", localTime.Nanosecond()), "0")
}

// ParseJSON strictly parses a bounded canonical schedule document.
func ParseJSON(data []byte) (Schedule, error) {
	if len(data) > MaxJSONBytes || !utf8.Valid(data) || validateJSON(data) != nil {
		return Schedule{}, newError("parse json", CodeInvalidEncoding)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var wire wireSchedule
	if err := decoder.Decode(&wire); err != nil {
		return Schedule{}, newError("parse json", CodeInvalidEncoding)
	}
	return scheduleFromWire(wire, 1)
}

func scheduleFromWire(wire wireSchedule, depth int) (Schedule, error) {
	if wire.Version != wireVersion {
		return Schedule{}, newError("parse json", CodeUnsupportedVersion)
	}
	if depth > MaxCompositionDepth {
		return Schedule{}, newError("parse json", CodeLimitExceeded)
	}
	if wire.Timezone == "" && len(wire.Weekly) == 0 && len(wire.Exceptions) == 0 && wire.Composition == nil {
		return Schedule{}, nil
	}
	if wire.Composition != nil {
		if len(wire.Weekly) != 0 || len(wire.Exceptions) != 0 {
			return Schedule{}, newError("parse json", CodeInvalidEncoding)
		}
		left, err := scheduleFromWire(wire.Composition.Left, depth+1)
		if err != nil {
			return Schedule{}, err
		}
		right, err := scheduleFromWire(wire.Composition.Right, depth+1)
		if err != nil {
			return Schedule{}, err
		}
		if wire.Timezone != left.Timezone() || wire.Timezone != right.Timezone() {
			return Schedule{}, newError("parse json", CodeTimezoneMismatch)
		}
		switch wire.Composition.Operation {
		case "union":
			return left.Union(right)
		case "intersection":
			return left.Intersection(right)
		case "subtract":
			return left.Subtract(right)
		case "overlay":
			return left.Overlay(right)
		default:
			return Schedule{}, newError("parse json", CodeInvalidEncoding)
		}
	}

	weekly := make(map[time.Weekday]DayRule, len(wire.Weekly))
	for _, item := range wire.Weekly {
		weekday, ok := parseWeekday(item.Weekday)
		if !ok {
			return Schedule{}, newError("parse json", CodeInvalidEncoding)
		}
		if _, duplicate := weekly[weekday]; duplicate {
			return Schedule{}, newError("parse json", CodeInvalidEncoding)
		}
		rule, err := ruleFromWire(item.Rule)
		if err != nil {
			return Schedule{}, err
		}
		weekly[weekday] = rule
	}
	exceptions := make([]Exception, 0, len(wire.Exceptions))
	for _, item := range wire.Exceptions {
		date, err := parseDate(item.Date)
		if err != nil {
			return Schedule{}, err
		}
		operation, ok := parseExceptionOperation(item.Operation)
		if !ok {
			return Schedule{}, newError("parse json", CodeInvalidEncoding)
		}
		rule, err := ruleFromWire(item.Rule)
		if err != nil {
			return Schedule{}, err
		}
		exception, err := NewException(ExceptionConfig{
			Date: date, Operation: operation, Rule: rule, Priority: item.Priority,
			Source: item.Source, Revision: item.Revision,
		})
		if err != nil {
			return Schedule{}, err
		}
		if item.Set != "" {
			if len(item.Set) > maxProvenanceBytes || !utf8.ValidString(item.Set) {
				return Schedule{}, newError("parse json", CodeInvalidEncoding)
			}
			exception.set = item.Set
		}
		exceptions = append(exceptions, exception)
	}

	metadata := Metadata{
		Label: wire.Metadata.Label, Source: wire.Metadata.Source,
		Revision: wire.Metadata.Revision,
	}
	outside := OutsideClosed
	if wire.OutsideEffective == "error" {
		outside = OutsideError
	} else if wire.OutsideEffective != "" && wire.OutsideEffective != "closed" {
		return Schedule{}, newError("parse json", CodeInvalidEncoding)
	}
	var effectiveStart, effectiveEnd *Date
	if wire.Effective != nil {
		if wire.Effective.Start != "" {
			value, err := parseDate(wire.Effective.Start)
			if err != nil {
				return Schedule{}, err
			}
			effectiveStart = &value
		}
		if wire.Effective.End != "" {
			value, err := parseDate(wire.Effective.End)
			if err != nil {
				return Schedule{}, err
			}
			effectiveEnd = &value
		}
	}
	return NewSchedule(Config{
		Timezone: wire.Timezone, Weekly: weekly, Exceptions: exceptions,
		ConflictPolicy: ResolveCanonical, Metadata: metadata,
		EffectiveStart: effectiveStart, EffectiveEnd: effectiveEnd,
		OutsideEffective: outside,
	})
}

func ruleFromWire(wire wireRule) (DayRule, error) {
	if wire.Ranges == nil {
		return DayRule{}, newError("parse json", CodeInvalidEncoding)
	}
	switch wire.State {
	case "inherited":
		if len(wire.Ranges) != 0 {
			return DayRule{}, newError("parse json", CodeInvalidEncoding)
		}
		return Inherited(), nil
	case "closed":
		if len(wire.Ranges) != 0 {
			return DayRule{}, newError("parse json", CodeInvalidEncoding)
		}
		return Closed(), nil
	case "all_day":
		if len(wire.Ranges) != 0 {
			return DayRule{}, newError("parse json", CodeInvalidEncoding)
		}
		return OpenAllDay(), nil
	case "ranges":
		ranges := make([]Range, 0, len(wire.Ranges))
		for _, item := range wire.Ranges {
			start, err := parseLocalTime(item.Start)
			if err != nil {
				return DayRule{}, err
			}
			end, err := parseLocalTime(item.End)
			if err != nil {
				return DayRule{}, err
			}
			itemRange, err := NewRange(start, end)
			if err != nil {
				return DayRule{}, err
			}
			ranges = append(ranges, itemRange)
		}
		return OpenRanges(ranges, RejectOverlap)
	default:
		return DayRule{}, newError("parse json", CodeInvalidEncoding)
	}
}

func parseWeekday(input string) (time.Weekday, bool) {
	for weekday, name := range weekdayNames {
		if input == name {
			return time.Weekday(weekday), true
		}
	}

	return 0, false
}

func parseExceptionOperation(input string) (ExceptionOperation, bool) {
	switch input {
	case "replace":
		return ExceptionReplace, true
	case "add":
		return ExceptionAdd, true
	case "subtract":
		return ExceptionSubtract, true
	case "close":
		return ExceptionClose, true
	default:
		return 0, false
	}
}

func parseDate(input string) (Date, error) {
	parsed, err := time.Parse("2006-01-02", input)
	if err != nil || parsed.Format("2006-01-02") != input {
		return Date{}, newError("parse date", CodeInvalidDate)
	}

	return NewDate(parsed.Year(), parsed.Month(), parsed.Day())
}

func parseLocalTime(input string) (LocalTime, error) {
	parsed, err := time.Parse("15:04:05.999999999", input)
	if err != nil || formatLocalTime(LocalTime{nanosecond: int64(
		time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute +
			time.Duration(parsed.Second())*time.Second + time.Duration(parsed.Nanosecond()),
	)}) != input {
		return LocalTime{}, newError("parse local time", CodeInvalidTime)
	}

	return LocalTime{nanosecond: int64(
		time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute +
			time.Duration(parsed.Second())*time.Second + time.Duration(parsed.Nanosecond()),
	)}, nil
}

func validateJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := validateJSONValue(decoder, 0); err != nil {
		return err
	}

	return requireEOF(decoder)
}

func validateJSONValue(decoder *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return newError("validate json", CodeLimitExceeded)
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			key := keyToken.(string) // encoding/json object keys are always strings.
			if _, duplicate := keys[key]; duplicate {
				return newError("validate json", CodeInvalidEncoding)
			}
			keys[key] = struct{}{}
			if err := validateJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := validateJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return newError("validate json", CodeInvalidEncoding)
	}
	_, err = decoder.Token()

	return err
}

func requireEOF(decoder *json.Decoder) error {
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return newError("parse json", CodeInvalidEncoding)
		}
		return err
	}

	return nil
}

// Equal reports canonical equality, including provenance and composition shape.
func (s Schedule) Equal(other Schedule) bool {
	left, leftErr := s.CanonicalJSON()
	right, rightErr := other.CanonicalJSON()

	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

// Compare returns -1, 0, or 1 according to the schedules' canonical byte
// ordering. It includes provenance and composition shape, like Equal.
func (s Schedule) Compare(other Schedule) (int, error) {
	left, err := s.CanonicalJSON()
	if err != nil {
		return 0, err
	}
	right, err := other.CanonicalJSON()
	if err != nil {
		return 0, err
	}

	return bytes.Compare(left, right), nil
}

// Hash returns the SHA-256 hash of the canonical schedule encoding.
func (s Schedule) Hash() [sha256.Size]byte {
	encoded, err := s.CanonicalJSON()
	if err != nil {
		return [sha256.Size]byte{}
	}

	return sha256.Sum256(encoded)
}

// MarshalJSON implements json.Marshaler with the canonical encoding.
func (s Schedule) MarshalJSON() ([]byte, error) { return s.CanonicalJSON() }

// UnmarshalJSON implements json.Unmarshaler using the strict parser.
func (s *Schedule) UnmarshalJSON(data []byte) error {
	if s == nil {
		return newError("unmarshal json", CodeInvalidState)
	}
	parsed, err := ParseJSON(data)
	if err != nil {
		return err
	}
	*s = parsed

	return nil
}

// MarshalText implements encoding.TextMarshaler using canonical JSON text.
func (s Schedule) MarshalText() ([]byte, error) { return s.CanonicalJSON() }

// UnmarshalText implements encoding.TextUnmarshaler using the strict parser.
func (s *Schedule) UnmarshalText(data []byte) error { return s.UnmarshalJSON(data) }

// String returns canonical JSON or a bounded error marker.
func (s Schedule) String() string {
	encoded, err := s.CanonicalJSON()
	if err != nil {
		return "<invalid opening-hours schedule>"
	}

	return string(encoded)
}

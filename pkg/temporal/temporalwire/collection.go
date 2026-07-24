package temporalwire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	"github.com/faustbrian/golib/pkg/temporal/notation"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

// CollectionDocument is a stable wire envelope for one normalized interval
// set. Values are canonical ISO 80000 elements in normalized order.
type CollectionDocument struct {
	Version string   `json:"version" yaml:"version" toml:"version"`
	Kind    Kind     `json:"kind" yaml:"kind" toml:"kind"`
	Values  []string `json:"values" yaml:"values" toml:"values"`
}

// FromInstantSet constructs a versioned normalized instant-set document.
func FromInstantSet(set instant.Set, limits temporal.Limits) (CollectionDocument, error) {
	values := make([]string, 0, set.Len())
	for _, period := range set.Periods() {
		encoded, err := notation.FormatInstant(period, notation.ISO80000, limits)
		if err != nil {
			return CollectionDocument{}, err
		}
		values = append(values, encoded)
	}
	return newCollection(KindInstantSet, values, limits)
}

// FromDateSet constructs a versioned normalized civil-date-set document.
func FromDateSet(set dateperiod.Set, limits temporal.Limits) (CollectionDocument, error) {
	values := make([]string, 0, set.Len())
	for _, period := range set.Periods() {
		encoded, err := notation.FormatDate(period, notation.ISO80000, limits)
		if err != nil {
			return CollectionDocument{}, err
		}
		values = append(values, encoded)
	}
	return newCollection(KindDateSet, values, limits)
}

// FromDailySet constructs a versioned normalized daily-set document.
func FromDailySet(set timeofday.IntervalSet, limits temporal.Limits) (CollectionDocument, error) {
	values := make([]string, 0, set.Len())
	for _, interval := range set.Intervals() {
		encoded, err := notation.FormatDailyInterval(interval, notation.ISO80000, limits)
		if err != nil {
			return CollectionDocument{}, err
		}
		values = append(values, encoded)
	}
	return newCollection(KindDailySet, values, limits)
}

func newCollection(kind Kind, values []string, limits temporal.Limits) (CollectionDocument, error) {
	document := CollectionDocument{Version: Version1, Kind: kind, Values: append([]string(nil), values...)}
	if err := document.validate(limits); err != nil {
		return CollectionDocument{}, err
	}
	return document, nil
}

// InstantSet decodes and normalizes an instant-set document.
func (d CollectionDocument) InstantSet(limits temporal.Limits) (instant.Set, error) {
	if err := d.expect(KindInstantSet); err != nil {
		return instant.Set{}, err
	}
	periods := make([]instant.Period, 0, len(d.Values))
	for _, value := range d.Values {
		period, err := notation.ParseInstant(value, notation.ISO80000, limits)
		if err != nil {
			return instant.Set{}, err
		}
		periods = append(periods, period)
	}
	return instant.NewSet(limits, periods...)
}

// DateSet decodes and normalizes a civil-date-set document.
func (d CollectionDocument) DateSet(limits temporal.Limits) (dateperiod.Set, error) {
	if err := d.expect(KindDateSet); err != nil {
		return dateperiod.Set{}, err
	}
	periods := make([]dateperiod.Period, 0, len(d.Values))
	for _, value := range d.Values {
		period, err := notation.ParseDate(value, notation.ISO80000, limits)
		if err != nil {
			return dateperiod.Set{}, err
		}
		periods = append(periods, period)
	}
	return dateperiod.NewSet(limits, periods...)
}

// DailySet decodes and normalizes a daily interval-set document.
func (d CollectionDocument) DailySet(limits temporal.Limits) (timeofday.IntervalSet, error) {
	if err := d.expect(KindDailySet); err != nil {
		return timeofday.IntervalSet{}, err
	}
	intervals := make([]timeofday.Interval, 0, len(d.Values))
	for _, value := range d.Values {
		interval, err := notation.ParseDailyInterval(value, notation.ISO80000, limits)
		if err != nil {
			return timeofday.IntervalSet{}, err
		}
		intervals = append(intervals, interval)
	}
	return timeofday.NewIntervalSet(limits, intervals...)
}

func (d CollectionDocument) expect(kind Kind) error {
	if d.Version != Version1 || d.Kind != kind {
		return temporal.ErrUnsupported
	}
	return nil
}

func (d CollectionDocument) validate(limits temporal.Limits) error {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return err
	}
	if len(d.Values) > limits.InputPeriods {
		return &temporal.LimitError{Field: "input_periods", Value: len(d.Values), Max: limits.InputPeriods}
	}
	switch d.Kind {
	case KindInstantSet:
		_, err := d.InstantSet(limits)
		return err
	case KindDateSet:
		_, err := d.DateSet(limits)
		return err
	case KindDailySet:
		_, err := d.DailySet(limits)
		return err
	default:
		return temporal.ErrUnsupported
	}
}

// MarshalCollection returns deterministic JSON for a valid collection.
func MarshalCollection(document CollectionDocument, limits temporal.Limits) ([]byte, error) {
	limits = limits.Resolve()
	if err := document.validate(limits); err != nil {
		return nil, err
	}
	document.Values = append([]string(nil), document.Values...)
	payload, _ := json.Marshal(document)
	if len(payload) > limits.FormatBytes {
		return nil, &temporal.LimitError{Field: "format_bytes", Value: len(payload), Max: limits.FormatBytes}
	}
	return payload, nil
}

// UnmarshalCollection strictly decodes exactly one collection document.
func UnmarshalCollection(payload []byte, limits temporal.Limits) (CollectionDocument, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return CollectionDocument{}, err
	}
	if len(payload) > limits.ParseBytes {
		return CollectionDocument{}, &temporal.LimitError{Field: "parse_bytes", Value: len(payload), Max: limits.ParseBytes}
	}
	if !utf8.Valid(payload) {
		return CollectionDocument{}, temporal.ErrParse
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var document CollectionDocument
	if err := decoder.Decode(&document); err != nil {
		return CollectionDocument{}, fmt.Errorf("%w: %w", temporal.ErrParse, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return CollectionDocument{}, temporal.ErrParse
	}
	if err := document.validate(limits); err != nil {
		return CollectionDocument{}, err
	}
	document.Values = append([]string(nil), document.Values...)
	return document, nil
}

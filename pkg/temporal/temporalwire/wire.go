// Package temporalwire provides versioned, format-neutral documents for
// encoding temporal values through wire or the standard JSON package.
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

// Version1 is the stable initial document schema identifier.
const Version1 = "temporal/v1"

// Kind identifies the temporal value encoded by a document.
type Kind string

const (
	KindInstantPeriod Kind = "instant-period"
	KindDatePeriod    Kind = "date-period"
	KindDailyInterval Kind = "daily-interval"
	KindTime          Kind = "time-of-day"
	KindDuration      Kind = "fixed-duration"
	KindInstantSet    Kind = "instant-set"
	KindDateSet       Kind = "date-set"
	KindDailySet      Kind = "daily-set"
)

// Document is a format-neutral stable wire representation. Value is canonical
// ISO 80000 notation for intervals and strict ISO text for scalar values.
type Document struct {
	Version string `json:"version" yaml:"version" toml:"version"`
	Kind    Kind   `json:"kind" yaml:"kind" toml:"kind"`
	Value   string `json:"value" yaml:"value" toml:"value"`
}

func FromInstant(value instant.Period, limits temporal.Limits) (Document, error) {
	encoded, err := notation.FormatInstant(value, notation.ISO80000, limits)
	return fromEncoded(KindInstantPeriod, encoded, err)
}

func FromDate(value dateperiod.Period, limits temporal.Limits) (Document, error) {
	encoded, err := notation.FormatDate(value, notation.ISO80000, limits)
	return fromEncoded(KindDatePeriod, encoded, err)
}

func FromDailyInterval(value timeofday.Interval, limits temporal.Limits) (Document, error) {
	encoded, err := notation.FormatDailyInterval(value, notation.ISO80000, limits)
	return fromEncoded(KindDailyInterval, encoded, err)
}

func FromTime(value timeofday.Time, limits temporal.Limits) (Document, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return Document{}, err
	}
	encoded := value.String()
	if len(encoded) > limits.FormatBytes {
		return Document{}, &temporal.LimitError{Field: "format_bytes", Value: len(encoded), Max: limits.FormatBytes}
	}
	return Document{Version: Version1, Kind: KindTime, Value: encoded}, nil
}

func FromDuration(value timeofday.Duration, limits temporal.Limits) (Document, error) {
	encoded, err := notation.FormatDuration(value, limits)
	return fromEncoded(KindDuration, encoded, err)
}

func fromEncoded(kind Kind, encoded string, err error) (Document, error) {
	if err != nil {
		return Document{}, err
	}
	return Document{Version: Version1, Kind: kind, Value: encoded}, nil
}

func (d Document) Instant(limits temporal.Limits) (instant.Period, error) {
	if err := d.expect(KindInstantPeriod); err != nil {
		return instant.Period{}, err
	}
	return notation.ParseInstant(d.Value, notation.ISO80000, limits)
}

func (d Document) Date(limits temporal.Limits) (dateperiod.Period, error) {
	if err := d.expect(KindDatePeriod); err != nil {
		return dateperiod.Period{}, err
	}
	return notation.ParseDate(d.Value, notation.ISO80000, limits)
}

func (d Document) DailyInterval(limits temporal.Limits) (timeofday.Interval, error) {
	if err := d.expect(KindDailyInterval); err != nil {
		return timeofday.Interval{}, err
	}
	return notation.ParseDailyInterval(d.Value, notation.ISO80000, limits)
}

func (d Document) Time(limits temporal.Limits) (timeofday.Time, error) {
	if err := d.expect(KindTime); err != nil {
		return timeofday.Time{}, err
	}
	return timeofday.Parse(d.Value, limits)
}

func (d Document) Duration(limits temporal.Limits) (timeofday.Duration, error) {
	if err := d.expect(KindDuration); err != nil {
		return timeofday.Duration{}, err
	}
	return notation.ParseDuration(d.Value, limits)
}

func (d Document) expect(kind Kind) error {
	if d.Version != Version1 || d.Kind != kind {
		return temporal.ErrUnsupported
	}
	return nil
}

func (d Document) validate(limits temporal.Limits) error {
	switch d.Kind {
	case KindInstantPeriod:
		_, err := d.Instant(limits)
		return err
	case KindDatePeriod:
		_, err := d.Date(limits)
		return err
	case KindDailyInterval:
		_, err := d.DailyInterval(limits)
		return err
	case KindTime:
		_, err := d.Time(limits)
		return err
	case KindDuration:
		_, err := d.Duration(limits)
		return err
	default:
		return temporal.ErrUnsupported
	}
}

// Marshal returns deterministic JSON for a valid versioned document.
func Marshal(document Document, limits temporal.Limits) ([]byte, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if err := document.validate(limits); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(document)
	if len(payload) > limits.FormatBytes {
		return nil, &temporal.LimitError{Field: "format_bytes", Value: len(payload), Max: limits.FormatBytes}
	}
	return payload, nil
}

// Unmarshal strictly decodes exactly one versioned JSON document.
func Unmarshal(payload []byte, limits temporal.Limits) (Document, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return Document{}, err
	}
	if len(payload) > limits.ParseBytes {
		return Document{}, &temporal.LimitError{Field: "parse_bytes", Value: len(payload), Max: limits.ParseBytes}
	}
	if !utf8.Valid(payload) {
		return Document{}, temporal.ErrParse
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("%w: %w", temporal.ErrParse, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Document{}, temporal.ErrParse
	}
	if err := document.validate(limits); err != nil {
		return Document{}, err
	}
	return document, nil
}

package temporalwire_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	"github.com/faustbrian/golib/pkg/temporal/temporalwire"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
	"github.com/faustbrian/golib/pkg/wire/jsonwire"
)

func TestVersionedDocumentsRoundTripEverySupportedValue(t *testing.T) {
	t.Parallel()

	instantValue, _ := instant.New(
		time.Date(2026, time.January, 2, 3, 4, 5, 6, time.UTC),
		time.Date(2026, time.January, 3, 4, 5, 6, 7, time.UTC),
		temporal.OpenClosed,
	)
	dateValue, _ := dateperiod.New(
		calendar.MustDate(2026, time.January, 2),
		calendar.MustDate(2026, time.March, 4),
		temporal.Closed,
	)
	start, _ := timeofday.Parse("22:00", temporal.Limits{})
	end, _ := timeofday.Parse("02:30", temporal.Limits{})
	dailyValue, _ := timeofday.Between(start, end, temporal.OpenClosed)
	localValue, _ := timeofday.Parse("12:34:56.123", temporal.Limits{})
	durationValue := timeofday.NewDuration(-(49*time.Hour + 123*time.Nanosecond))

	tests := []struct {
		name  string
		make  func() (temporalwire.Document, error)
		check func(temporalwire.Document) error
	}{
		{"instant", func() (temporalwire.Document, error) {
			return temporalwire.FromInstant(instantValue, temporal.Limits{})
		}, func(document temporalwire.Document) error {
			decoded, err := document.Instant(temporal.Limits{})
			if err == nil && (!decoded.SetEqual(instantValue) || decoded.Bounds() != instantValue.Bounds()) {
				return errors.New("instant mismatch")
			}
			return err
		}},
		{"date", func() (temporalwire.Document, error) { return temporalwire.FromDate(dateValue, temporal.Limits{}) }, func(document temporalwire.Document) error {
			decoded, err := document.Date(temporal.Limits{})
			if err == nil && (decoded.Start() != dateValue.Start() || decoded.End() != dateValue.End() || decoded.Bounds() != dateValue.Bounds()) {
				return errors.New("date mismatch")
			}
			return err
		}},
		{"daily", func() (temporalwire.Document, error) {
			return temporalwire.FromDailyInterval(dailyValue, temporal.Limits{})
		}, func(document temporalwire.Document) error {
			decoded, err := document.DailyInterval(temporal.Limits{})
			if err == nil && !decoded.Equal(dailyValue) {
				return errors.New("daily mismatch")
			}
			return err
		}},
		{"time", func() (temporalwire.Document, error) { return temporalwire.FromTime(localValue, temporal.Limits{}) }, func(document temporalwire.Document) error {
			decoded, err := document.Time(temporal.Limits{})
			if err == nil && decoded.String() != localValue.String() {
				return errors.New("time mismatch")
			}
			return err
		}},
		{"duration", func() (temporalwire.Document, error) {
			return temporalwire.FromDuration(durationValue, temporal.Limits{})
		}, func(document temporalwire.Document) error {
			decoded, err := document.Duration(temporal.Limits{})
			if err == nil && decoded != durationValue {
				return errors.New("duration mismatch")
			}
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document, err := test.make()
			if err != nil || document.Version != temporalwire.Version1 {
				t.Fatalf("document = %+v, %v", document, err)
			}
			payload, err := temporalwire.Marshal(document, temporal.Limits{})
			if err != nil {
				t.Fatalf("Marshal(): %v", err)
			}
			decoded, err := temporalwire.Unmarshal(payload, temporal.Limits{})
			if err != nil {
				t.Fatalf("Unmarshal(): %v", err)
			}
			if err := test.check(decoded); err != nil {
				t.Fatalf("value round trip: %v", err)
			}
		})
	}
}

func TestGoWireJSONRoundTripsVersionedDocument(t *testing.T) {
	t.Parallel()

	document, err := temporalwire.FromTime(timeofday.Noon(), temporal.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := jsonwire.Encode(document, jsonwire.EncodeOptions{MaxBytes: 1024})
	if err != nil {
		t.Fatalf("jsonwire.Encode(): %v", err)
	}
	var decoded temporalwire.Document
	if err := jsonwire.Decode(payload, &decoded, jsonwire.DecodeOptions{
		MaxBytes:              1024,
		DisallowUnknownFields: true,
	}); err != nil {
		t.Fatalf("jsonwire.Decode(): %v", err)
	}
	value, err := decoded.Time(temporal.DefaultLimits())
	if err != nil || !value.Equal(timeofday.Noon()) {
		t.Fatalf("decoded wire value = %v, %v", value, err)
	}
}

func TestWireDocumentsRejectWrongKindVersionAndHostileJSON(t *testing.T) {
	t.Parallel()

	valid := temporalwire.Document{Version: temporalwire.Version1, Kind: temporalwire.KindTime, Value: "08:00"}
	wrongKind := valid
	wrongKind.Kind = temporalwire.KindDatePeriod
	if _, err := wrongKind.Time(temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("Time(kind) error = %v", err)
	}
	wrongVersion := valid
	wrongVersion.Version = "temporal/v2"
	if _, err := wrongVersion.Time(temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("Time(version) error = %v", err)
	}
	if _, err := valid.Instant(temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("Instant(kind) error = %v", err)
	}
	if _, err := valid.Date(temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("Date(kind) error = %v", err)
	}
	if _, err := valid.DailyInterval(temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("DailyInterval(kind) error = %v", err)
	}
	if _, err := valid.Duration(temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("Duration(kind) error = %v", err)
	}
	if _, err := (temporalwire.Document{Version: temporalwire.Version1, Kind: temporalwire.KindTime, Value: "bad"}).Time(temporal.Limits{}); err == nil {
		t.Fatal("Time(bad value) error = nil")
	}

	for _, payload := range [][]byte{
		nil,
		[]byte(`{"version":"temporal/v1","kind":"time","value":"08:00","extra":true}`),
		[]byte(`{"version":"temporal/v1","kind":"time","value":"08:00"} trailing`),
		[]byte(`{"version":"temporal/v1","kind":"unknown","value":"08:00"}`),
		{0xff},
		[]byte(strings.Repeat("x", temporal.DefaultLimits().ParseBytes+1)),
	} {
		if _, err := temporalwire.Unmarshal(payload, temporal.Limits{}); err == nil {
			t.Fatalf("Unmarshal(%q) error = nil", payload)
		}
	}
	if _, err := temporalwire.Unmarshal([]byte(`{}`), temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Unmarshal(limits) error = %v", err)
	}
	if _, err := temporalwire.Unmarshal([]byte(`{}`), temporal.Limits{ParseBytes: 1}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Unmarshal(bytes) error = %v", err)
	}
	if _, err := temporalwire.Marshal(valid, temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Marshal(limits) error = %v", err)
	}
	if _, err := temporalwire.Marshal(valid, temporal.Limits{FormatBytes: 1}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Marshal(bytes) error = %v", err)
	}
	if _, err := temporalwire.Marshal(temporalwire.Document{Version: temporalwire.Version1, Kind: "unknown", Value: "x"}, temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("Marshal(kind) error = %v", err)
	}
	if _, err := temporalwire.Marshal(temporalwire.Document{Version: temporalwire.Version1, Kind: temporalwire.KindTime, Value: "bad"}, temporal.Limits{}); err == nil {
		t.Fatal("Marshal(value) error = nil")
	}
	if _, err := temporalwire.FromTime(timeofday.Midnight(), temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("FromTime(limits) error = %v", err)
	}
	if _, err := temporalwire.FromTime(timeofday.Midnight(), temporal.Limits{FormatBytes: 1}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("FromTime(bytes) error = %v", err)
	}
	if _, err := temporalwire.FromDuration(timeofday.ZeroDuration(), temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("FromDuration(limits) error = %v", err)
	}
}

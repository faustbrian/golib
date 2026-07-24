package temporalwire_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	"github.com/faustbrian/golib/pkg/temporal/temporalwire"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func TestCollectionDocumentRoundTripsNormalizedSets(t *testing.T) {
	base := time.Unix(0, 0).UTC()
	first, _ := instant.Range(base, base.Add(time.Hour))
	second, _ := instant.New(base.Add(2*time.Hour), base.Add(3*time.Hour), temporal.Closed)
	instantSet, _ := instant.NewSet(temporal.DefaultLimits(), second, first)
	instantDocument, err := temporalwire.FromInstantSet(instantSet, temporal.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := temporalwire.MarshalCollection(instantDocument, temporal.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := temporalwire.UnmarshalCollection(encoded, temporal.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	instantRoundTrip, err := decoded.InstantSet(temporal.DefaultLimits())
	if err != nil || !instantRoundTrip.Equal(instantSet) {
		t.Fatalf("InstantSet() = %+v, %v", instantRoundTrip, err)
	}

	dateSet, _ := dateperiod.NewSet(temporal.DefaultLimits(), mustDatePeriod(t, 2026, time.January, 1, 2))
	dateDocument, err := temporalwire.FromDateSet(dateSet, temporal.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	dateRoundTrip, err := dateDocument.DateSet(temporal.DefaultLimits())
	if err != nil || !dateRoundTrip.Equal(dateSet) {
		t.Fatalf("DateSet() = %+v, %v", dateRoundTrip, err)
	}

	daily, _ := timeofday.Between(timeofday.Noon(), timeofday.EndOfDay(), temporal.ClosedOpen)
	dailySet, _ := timeofday.NewIntervalSet(temporal.DefaultLimits(), daily)
	dailyDocument, err := temporalwire.FromDailySet(dailySet, temporal.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	dailyRoundTrip, err := dailyDocument.DailySet(temporal.DefaultLimits())
	if err != nil || !dailyRoundTrip.Equal(dailySet) {
		t.Fatalf("DailySet() = %+v, %v", dailyRoundTrip, err)
	}
}

func TestCollectionDocumentIsStrictBoundedAndImmutable(t *testing.T) {
	document := temporalwire.CollectionDocument{
		Version: temporalwire.Version1,
		Kind:    temporalwire.KindInstantSet,
		Values:  []string{"[1970-01-01T00:00:00Z,1970-01-01T01:00:00Z)"},
	}
	encoded, err := temporalwire.MarshalCollection(document, temporal.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	document.Values[0] = "bad"
	decoded, err := temporalwire.UnmarshalCollection(encoded, temporal.DefaultLimits())
	if err != nil || decoded.Values[0] == "bad" {
		t.Fatalf("UnmarshalCollection() = %+v, %v", decoded, err)
	}

	invalid := [][]byte{
		[]byte(`{"version":"temporal/v1","kind":"instant-set","values":[],"extra":true}`),
		[]byte(`{"version":"temporal/v1","kind":"instant-set","values":[]} trailing`),
		{0xff},
	}
	for _, payload := range invalid {
		if _, err := temporalwire.UnmarshalCollection(payload, temporal.DefaultLimits()); err == nil {
			t.Fatalf("UnmarshalCollection(%q) accepted invalid input", payload)
		}
	}
	limits := temporal.DefaultLimits()
	limits.InputPeriods = 1
	tooMany := temporalwire.CollectionDocument{Version: temporalwire.Version1, Kind: temporalwire.KindInstantSet, Values: []string{"bad", "bad"}}
	if _, err := temporalwire.MarshalCollection(tooMany, limits); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("MarshalCollection(limit) error = %v", err)
	}
	limits = temporal.DefaultLimits()
	limits.ParseBytes = 1
	if _, err := temporalwire.UnmarshalCollection(encoded, limits); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("UnmarshalCollection(limit) error = %v", err)
	}
}

func TestCollectionDocumentRejectsKindAndElementMismatches(t *testing.T) {
	limits := temporal.DefaultLimits()
	tests := []temporalwire.CollectionDocument{
		{},
		{Version: "future", Kind: temporalwire.KindInstantSet},
		{Version: temporalwire.Version1, Kind: "unknown"},
		{Version: temporalwire.Version1, Kind: temporalwire.KindInstantSet, Values: []string{"bad"}},
		{Version: temporalwire.Version1, Kind: temporalwire.KindDateSet, Values: []string{"bad"}},
		{Version: temporalwire.Version1, Kind: temporalwire.KindDailySet, Values: []string{"bad"}},
	}
	for _, document := range tests {
		if _, err := temporalwire.MarshalCollection(document, limits); err == nil {
			t.Fatalf("MarshalCollection(%+v) accepted invalid document", document)
		}
	}
	instantDocument := temporalwire.CollectionDocument{Version: temporalwire.Version1, Kind: temporalwire.KindInstantSet}
	if _, err := instantDocument.DateSet(limits); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("DateSet(kind mismatch) error = %v", err)
	}
	if _, err := instantDocument.DailySet(limits); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("DailySet(kind mismatch) error = %v", err)
	}
	dateDocument := temporalwire.CollectionDocument{Version: temporalwire.Version1, Kind: temporalwire.KindDateSet}
	if _, err := dateDocument.InstantSet(limits); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("InstantSet(kind mismatch) error = %v", err)
	}
}

func TestCollectionDocumentPropagatesCodecAndConfigurationLimits(t *testing.T) {
	base := time.Unix(0, 0).UTC()
	instantPeriod, _ := instant.Range(base, base.Add(time.Hour))
	instantSet, _ := instant.NewSet(temporal.DefaultLimits(), instantPeriod)
	dateSet, _ := dateperiod.NewSet(temporal.DefaultLimits(), mustDatePeriod(t, 2026, time.January, 1, 2))
	dailyInterval, _ := timeofday.Between(timeofday.Midnight(), timeofday.Noon(), temporal.ClosedOpen)
	dailySet, _ := timeofday.NewIntervalSet(temporal.DefaultLimits(), dailyInterval)
	invalidLimits := temporal.DefaultLimits()
	invalidLimits.Precision = 10
	if _, err := temporalwire.FromInstantSet(instantSet, invalidLimits); err == nil {
		t.Fatal("FromInstantSet accepted invalid limits")
	}
	if _, err := temporalwire.FromDateSet(dateSet, invalidLimits); err == nil {
		t.Fatal("FromDateSet accepted invalid limits")
	}
	if _, err := temporalwire.FromDailySet(dailySet, invalidLimits); err == nil {
		t.Fatal("FromDailySet accepted invalid limits")
	}
	empty, _ := instant.NewSet(temporal.DefaultLimits())
	if _, err := temporalwire.FromInstantSet(empty, invalidLimits); err == nil {
		t.Fatal("FromInstantSet(empty) accepted invalid limits")
	}

	document, err := temporalwire.FromInstantSet(instantSet, temporal.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	formatLimits := temporal.DefaultLimits()
	formatLimits.FormatBytes = 1
	if _, err := temporalwire.MarshalCollection(document, formatLimits); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("MarshalCollection(format limit) error = %v", err)
	}
	if _, err := temporalwire.MarshalCollection(document, invalidLimits); err == nil {
		t.Fatal("MarshalCollection accepted invalid limits")
	}
	if _, err := temporalwire.UnmarshalCollection([]byte(`{}`), invalidLimits); err == nil {
		t.Fatal("UnmarshalCollection accepted invalid limits")
	}
	invalidElement := []byte(`{"version":"temporal/v1","kind":"instant-set","values":["bad"]}`)
	if _, err := temporalwire.UnmarshalCollection(invalidElement, temporal.DefaultLimits()); err == nil {
		t.Fatal("UnmarshalCollection accepted invalid element")
	}
}

func mustDatePeriod(t *testing.T, year int, month time.Month, startDay, endDay int) dateperiod.Period {
	t.Helper()
	start := calendar.MustDate(year, month, startDay)
	end := calendar.MustDate(year, month, endDay)
	period, err := dateperiod.New(start, end, temporal.Closed)
	if err != nil {
		t.Fatal(err)
	}
	return period
}

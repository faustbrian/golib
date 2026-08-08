package temporalwire_test

import (
	"testing"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/temporalwire"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func TestWireDocumentsAcceptEveryExactLimit(t *testing.T) {
	t.Parallel()

	encodedTime := timeofday.Noon().String()
	document, err := temporalwire.FromTime(timeofday.Noon(), temporal.Limits{FormatBytes: len(encodedTime)})
	if err != nil {
		t.Fatalf("FromTime(exact limit): %v", err)
	}
	payload, err := temporalwire.Marshal(document, temporal.Limits{})
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if _, err := temporalwire.Marshal(document, temporal.Limits{FormatBytes: len(payload)}); err != nil {
		t.Fatalf("Marshal(exact limit): %v", err)
	}
	if _, err := temporalwire.Unmarshal(payload, temporal.Limits{ParseBytes: len(payload)}); err != nil {
		t.Fatalf("Unmarshal(exact limit): %v", err)
	}
}

func TestWireCollectionsAcceptEveryExactLimit(t *testing.T) {
	t.Parallel()

	document := temporalwire.CollectionDocument{
		Version: temporalwire.Version1,
		Kind:    temporalwire.KindDailySet,
		Values:  []string{"[08:00,17:00)"},
	}
	payload, err := temporalwire.MarshalCollection(document, temporal.Limits{InputPeriods: 1})
	if err != nil {
		t.Fatalf("MarshalCollection(exact input limit): %v", err)
	}
	if _, err := temporalwire.MarshalCollection(document, temporal.Limits{
		InputPeriods: 1,
		FormatBytes:  len(payload),
	}); err != nil {
		t.Fatalf("MarshalCollection(exact format limit): %v", err)
	}
	if _, err := temporalwire.UnmarshalCollection(payload, temporal.Limits{
		InputPeriods: 1,
		ParseBytes:   len(payload),
	}); err != nil {
		t.Fatalf("UnmarshalCollection(exact parse limit): %v", err)
	}
}

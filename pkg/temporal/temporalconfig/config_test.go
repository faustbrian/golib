package temporalconfig_test

import (
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	configdecode "github.com/faustbrian/golib/pkg/config/decode"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	"github.com/faustbrian/golib/pkg/temporal/temporalconfig"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func TestConfigurationWrappersRoundTripCanonicalText(t *testing.T) {
	t.Parallel()

	instantValue, _ := instant.New(
		time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		time.Date(2026, time.January, 3, 3, 4, 5, 0, time.UTC),
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
	timeValue, _ := timeofday.Parse("12:34:56.123", temporal.Limits{})
	durationValue := timeofday.NewDuration(-49*time.Hour - 123*time.Nanosecond)

	instantConfig := temporalconfig.NewInstantPeriod(instantValue)
	instantText, err := instantConfig.MarshalText()
	var decodedInstant temporalconfig.InstantPeriod
	if err != nil || decodedInstant.UnmarshalText(instantText) != nil || !decodedInstant.Value().SetEqual(instantValue) || decodedInstant.Value().Bounds() != instantValue.Bounds() {
		t.Fatalf("instant config round trip = %q, %v", instantText, err)
	}

	dateConfig := temporalconfig.NewDatePeriod(dateValue)
	dateText, err := dateConfig.MarshalText()
	var decodedDate temporalconfig.DatePeriod
	if err != nil || decodedDate.UnmarshalText(dateText) != nil || decodedDate.Value().Start() != dateValue.Start() || decodedDate.Value().End() != dateValue.End() || decodedDate.Value().Bounds() != dateValue.Bounds() {
		t.Fatalf("date config round trip = %q, %v", dateText, err)
	}

	dailyConfig := temporalconfig.NewDailyInterval(dailyValue)
	dailyText, err := dailyConfig.MarshalText()
	var decodedDaily temporalconfig.DailyInterval
	if err != nil || decodedDaily.UnmarshalText(dailyText) != nil || !decodedDaily.Value().Equal(dailyValue) {
		t.Fatalf("daily config round trip = %q, %v", dailyText, err)
	}

	timeConfig := temporalconfig.NewTime(timeValue)
	timeText, err := timeConfig.MarshalText()
	var decodedTime temporalconfig.Time
	if err != nil || decodedTime.UnmarshalText(timeText) != nil || decodedTime.Value().String() != timeValue.String() {
		t.Fatalf("time config round trip = %q, %v", timeText, err)
	}

	durationConfig := temporalconfig.NewDuration(durationValue)
	durationText, err := durationConfig.MarshalText()
	var decodedDuration temporalconfig.Duration
	if err != nil || decodedDuration.UnmarshalText(durationText) != nil || decodedDuration.Value() != durationValue {
		t.Fatalf("duration config round trip = %q, %v", durationText, err)
	}
}

func TestConfigurationWrappersAssignOnlyAfterSuccessfulParse(t *testing.T) {
	t.Parallel()

	timeValue, _ := timeofday.Parse("08:00", temporal.Limits{})
	timeConfig := temporalconfig.NewTime(timeValue)
	if err := timeConfig.UnmarshalText([]byte("bad")); err == nil || !timeConfig.Value().Equal(timeValue) {
		t.Fatal("failed time parse replaced the prior value")
	}
	durationValue := timeofday.NewDuration(time.Hour)
	durationConfig := temporalconfig.NewDuration(durationValue)
	if err := durationConfig.UnmarshalText([]byte("P1M")); err == nil || durationConfig.Value() != durationValue {
		t.Fatal("failed duration parse replaced the prior value")
	}

	instantConfig := temporalconfig.NewInstantPeriod(instant.Period{})
	if err := instantConfig.UnmarshalText([]byte("bad")); err == nil {
		t.Fatal("invalid instant config error = nil")
	}
	dateConfig := temporalconfig.NewDatePeriod(dateperiod.Period{})
	if err := dateConfig.UnmarshalText([]byte("bad")); err == nil {
		t.Fatal("invalid date config error = nil")
	}
	dailyConfig := temporalconfig.NewDailyInterval(timeofday.Interval{})
	if err := dailyConfig.UnmarshalText([]byte("bad")); err == nil {
		t.Fatal("invalid daily config error = nil")
	}
}

func TestGoConfigDecodesTemporalTextThroughTheAdapter(t *testing.T) {
	t.Parallel()

	var value temporalconfig.DailyInterval
	if err := configdecode.Value("[22:00,02:30)", &value); err != nil {
		t.Fatalf("decode.Value(): %v", err)
	}
	if value.Value().Kind() != timeofday.Circular || !value.Value().Includes(timeofday.Midnight()) {
		t.Fatalf("decoded interval = %+v", value.Value())
	}
}

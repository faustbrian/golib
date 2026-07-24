package openinghoursconfig_test

import (
	"testing"

	configdecode "github.com/faustbrian/golib/pkg/config/decode"
	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
	"github.com/faustbrian/golib/pkg/opening-hours/openinghoursconfig"
)

func TestParse(t *testing.T) {
	if _, err := openinghoursconfig.Parse(`{"version":1,"timezone":"","weekly":[],"exceptions":[],"metadata":{"label":"","source":"","revision":""},"outside_effective":"closed"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := openinghoursconfig.Parse(`{}`); err == nil {
		t.Fatal("Parse accepted invalid config")
	}
}

func TestValueIntegratesWithGoConfigDecode(t *testing.T) {
	schedule, _ := openinghours.NewSchedule(openinghours.Config{Timezone: "UTC"})
	encoded, _ := schedule.CanonicalJSON()
	var value openinghoursconfig.Value
	if err := configdecode.Value(string(encoded), &value); err != nil {
		t.Fatal(err)
	}
	if !value.Schedule().Equal(schedule) {
		t.Fatal("config decode changed schedule")
	}
	if err := configdecode.Value(42, &value); err == nil {
		t.Fatal("config accepted a non-string schedule")
	}
	wrapped := openinghoursconfig.NewValue(schedule)
	if text, err := wrapped.MarshalText(); err != nil || string(text) != string(encoded) {
		t.Fatalf("MarshalText = %s, %v", text, err)
	}
	var nilValue *openinghoursconfig.Value
	if err := nilValue.UnmarshalConfigValue(string(encoded)); err == nil {
		t.Fatal("nil config target accepted")
	}
	if err := value.UnmarshalConfigValue(`{}`); err == nil {
		t.Fatal("invalid canonical config accepted")
	}
}

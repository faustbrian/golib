package search_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func TestValuesPreserveExactTypedRepresentationsAndOwnership(t *testing.T) {
	t.Parallel()

	instant := time.Date(2026, time.August, 9, 12, 30, 45, 123, time.FixedZone("EEST", 3*60*60))
	fields := map[string]search.Value{
		"name":     search.StringValue("Helsinki"),
		"distance": mustNumber(t, "0.1000000000000000001"),
		"updated":  search.TimeValue(instant),
		"enabled":  search.BoolValue(true),
		"empty":    search.StringValue(""),
		"nothing":  search.NullValue(),
		"position": mustGeo(t, 60.1699, 24.9384),
		"locales": search.ArrayValue([]search.Value{
			search.StringValue("fi-FI"),
			search.StringValue("sv-FI"),
		}),
		"address": search.ObjectValue(map[string]search.Value{
			"city": search.StringValue("Helsinki"),
		}),
	}

	object := search.ObjectValue(fields)
	fields["name"] = search.StringValue("changed")

	assertJSON(t, object, `{"address":{"city":"Helsinki"},"distance":0.1000000000000000001,"empty":"","enabled":true,"locales":["fi-FI","sv-FI"],"name":"Helsinki","nothing":null,"position":{"lat":60.1699,"lon":24.9384},"updated":"2026-08-09T09:30:45.000000123Z"}`)

	if _, exists := fields["missing"]; exists {
		t.Fatal("missing field unexpectedly exists")
	}
}

func TestValueRejectsAmbiguousOrUnboundedInput(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "NaN", "Inf", "01", "1.", "1e9999999"} {
		if _, err := search.NumberValue(input); err == nil {
			t.Fatalf("NumberValue(%q) unexpectedly succeeded", input)
		}
	}

	if _, err := search.GeoValue(91, 0); err == nil {
		t.Fatal("GeoValue() accepted latitude outside its bounds")
	}
	if _, err := search.GeoValue(0, 181); err == nil {
		t.Fatal("GeoValue() accepted longitude outside its bounds")
	}
}

func TestValueJSONRejectsInvalidUTF8WithoutReplacement(t *testing.T) {
	t.Parallel()

	invalid := string([]byte{0xff})
	for _, value := range []search.Value{
		search.StringValue(invalid),
		search.ObjectValue(map[string]search.Value{invalid: search.StringValue("value")}),
		search.ArrayValue([]search.Value{search.StringValue(invalid)}),
	} {
		if _, err := json.Marshal(value); err == nil {
			t.Fatalf("Marshal(%v) accepted invalid UTF-8", value.Kind())
		}
	}
}

func mustNumber(t *testing.T, value string) search.Value {
	t.Helper()

	result, err := search.NumberValue(value)
	if err != nil {
		t.Fatalf("NumberValue() error = %v", err)
	}

	return result
}

func mustGeo(t *testing.T, latitude, longitude float64) search.Value {
	t.Helper()

	result, err := search.GeoValue(latitude, longitude)
	if err != nil {
		t.Fatalf("GeoValue() error = %v", err)
	}

	return result
}

func assertJSON(t *testing.T, value search.Value, expected string) {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(encoded) != expected {
		t.Fatalf("json.Marshal() = %s, want %s", encoded, expected)
	}
}

package httpx

import "testing"

func TestSplitDelimitedRespectsQuotedDelimitersAndBounds(t *testing.T) {
	t.Parallel()

	got, ok := SplitDelimited(`for=192.0.2.1;host="api,one.example", for=10.0.0.1`, ',', 128, 4)
	if !ok || len(got) != 2 {
		t.Fatalf("SplitDelimited() = %v, %v", got, ok)
	}
	for _, value := range []string{`one,"unterminated`, "one\r,two", "one,,two", "12345"} {
		maximum := 128
		if value == "12345" {
			maximum = 4
		}
		if _, ok := SplitDelimited(value, ',', maximum, 4); ok {
			t.Fatalf("SplitDelimited(%q) succeeded", value)
		}
	}
}

func TestSplitDelimitedRejectsEveryMalformedBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value              string
		maxBytes, maxItems int
	}{
		{"", 10, 1}, {"a", 0, 1}, {"a", 10, 0},
		{"a\r", 10, 1}, {"a\n", 10, 1}, {"a\x00", 10, 1}, {"a\x7f", 10, 1}, {"a\x01", 10, 1},
		{`"a`, 10, 1}, {`"a\`, 10, 1}, {",a", 10, 2}, {"a,", 10, 2}, {"a,b", 10, 1},
	} {
		if _, ok := SplitDelimited(tc.value, ',', tc.maxBytes, tc.maxItems); ok {
			t.Fatalf("SplitDelimited(%q) succeeded", tc.value)
		}
	}
	if got, ok := SplitDelimited(`"a\\\",b", c`, ',', 32, 2); !ok || len(got) != 2 {
		t.Fatalf("escaped quoted split = %v, %v", got, ok)
	}
	if got, ok := SplitDelimited("a\t,b", ',', 10, 2); !ok || len(got) != 2 {
		t.Fatalf("tab split = %v, %v", got, ok)
	}
}

func TestParseQualityUsesHTTPQValueGrammar(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]float64{"0": 0, "0.5": 0.5, "0.123": 0.123, "1": 1, "1.000": 1} {
		got, ok := ParseQuality(value)
		if !ok || got != want {
			t.Fatalf("ParseQuality(%q) = %v, %v", value, got, ok)
		}
	}
	for _, value := range []string{"", ".5", "+1", "0.1234", "1.001", "2", "NaN"} {
		if _, ok := ParseQuality(value); ok {
			t.Fatalf("ParseQuality(%q) succeeded", value)
		}
	}
}

func TestParseQualityCoversGrammarBoundaries(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "00", "2.0", "0.0000", "0.a", "1.001"} {
		if _, ok := ParseQuality(value); ok {
			t.Fatalf("ParseQuality(%q) succeeded", value)
		}
	}
	for _, tc := range []struct {
		value string
		want  float64
	}{{"0.", 0}, {"1.", 1}, {"0.25", .25}, {"1.000", 1}} {
		if got, ok := ParseQuality(tc.value); !ok || got != tc.want {
			t.Fatalf("ParseQuality(%q) = %v, %v", tc.value, got, ok)
		}
	}
}

func TestValidFieldValueRejectsControlsAndBounds(t *testing.T) {
	t.Parallel()

	if !ValidFieldValue("max-age=60", 32) {
		t.Fatal("valid field rejected")
	}
	for _, value := range []string{"bad\rvalue", "bad\x7fvalue", "toolong"} {
		maximum := 32
		if value == "toolong" {
			maximum = 3
		}
		if ValidFieldValue(value, maximum) {
			t.Fatalf("ValidFieldValue(%q) succeeded", value)
		}
	}
}

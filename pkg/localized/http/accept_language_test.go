package http_test

import (
	"errors"
	"testing"

	localized "github.com/faustbrian/golib/pkg/localized"
	localizedhttp "github.com/faustbrian/golib/pkg/localized/http"
	localizedmatch "github.com/faustbrian/golib/pkg/localized/match"
)

func httpFixture(t *testing.T) localized.Text {
	t.Helper()
	value, err := localized.TextFromMap(map[string]string{
		"en-US": "Hello", "fi": "Hei", "sv": "Hej",
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestParseAcceptLanguagePreservesStableWeightedPreferences(t *testing.T) {
	t.Parallel()
	preferences, err := localizedhttp.ParseAcceptLanguage("fi;q=0.7, en-US, sv;q=0.7", localizedhttp.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(preferences) != 3 || preferences[0].Locale != mustLocale(t, "fi") || preferences[0].Weight != 0.7 || preferences[1].Locale != mustLocale(t, "en-US") || preferences[1].Weight != 1 {
		t.Fatalf("preferences = %+v", preferences)
	}

	result, err := localizedmatch.Best(httpFixture(t), preferences...)
	if err != nil {
		t.Fatal(err)
	}
	if result.Locale != mustLocale(t, "en-US") || result.Text != "Hello" {
		t.Fatalf("Best() = %+v", result)
	}
}

func TestAcceptLanguageSelectSupportsBoundedWildcard(t *testing.T) {
	t.Parallel()
	result, err := localizedhttp.Select(httpFixture(t), "de;q=0, *;q=0.5", localizedhttp.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != localizedmatch.Matched || !result.Present || result.Locale != mustLocale(t, "en-US") {
		t.Fatalf("Select() = %+v", result)
	}
}

func TestParseAcceptLanguageRejectsHostileInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		header  string
		options localizedhttp.ParseOptions
		want    error
	}{
		{"invalid range", "not_a_tag", localizedhttp.ParseOptions{}, localizedhttp.ErrInvalidRange},
		{"invalid weight", "en;q=1.1", localizedhttp.ParseOptions{}, localizedhttp.ErrInvalidWeight},
		{"excess precision", "en;q=0.1234", localizedhttp.ParseOptions{}, localizedhttp.ErrInvalidWeight},
		{"unknown parameter", "en;level=1", localizedhttp.ParseOptions{}, localizedhttp.ErrInvalidParameter},
		{"duplicate", "en-US, EN-us", localizedhttp.ParseOptions{}, localizedhttp.ErrDuplicateRange},
		{"bytes", "en-US", localizedhttp.ParseOptions{MaxBytes: 4}, localizedhttp.ErrHeaderLimit},
		{"candidates", "en,fi", localizedhttp.ParseOptions{MaxCandidates: 1}, localizedhttp.ErrCandidateLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := localizedhttp.ParseAcceptLanguage(test.header, test.options)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEmptyAcceptLanguageSelectsNothing(t *testing.T) {
	t.Parallel()
	result, err := localizedhttp.Select(httpFixture(t), "", localizedhttp.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != localizedmatch.Missing || result.Present {
		t.Fatalf("Select() = %+v", result)
	}
}

func TestAcceptLanguageBoundaryMatrix(t *testing.T) {
	t.Parallel()

	if got := localizedhttp.ErrInvalidRange.Error(); got != "localized http: invalid range" {
		t.Fatalf("Error() = %q", got)
	}
	tests := []struct {
		name    string
		header  string
		options localizedhttp.ParseOptions
		want    error
	}{
		{"negative bytes", "en", localizedhttp.ParseOptions{MaxBytes: -1}, localizedhttp.ErrHeaderLimit},
		{"negative candidates", "en", localizedhttp.ParseOptions{MaxCandidates: -1}, localizedhttp.ErrCandidateLimit},
		{"invalid UTF-8", string([]byte{0xff}), localizedhttp.ParseOptions{}, localizedhttp.ErrInvalidRange},
		{"multiple parameters", "en;q=1;level=2", localizedhttp.ParseOptions{}, localizedhttp.ErrInvalidParameter},
		{"empty range", ",en", localizedhttp.ParseOptions{}, localizedhttp.ErrInvalidRange},
		{"missing parameter value", "en;q", localizedhttp.ParseOptions{}, localizedhttp.ErrInvalidParameter},
		{"non-digit weight", "en;q=0.a", localizedhttp.ParseOptions{}, localizedhttp.ErrInvalidWeight},
		{"parse failure", "en-", localizedhttp.ParseOptions{}, localizedhttp.ErrInvalidRange},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := localizedhttp.ParseAcceptLanguage(test.header, test.options)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}

	value := httpFixture(t)
	for _, header := range []string{"en;q=1.000", "en;q=0.000"} {
		if _, err := localizedhttp.ParseAcceptLanguage(header, localizedhttp.ParseOptions{}); err != nil {
			t.Fatalf("ParseAcceptLanguage(%q) error = %v", header, err)
		}
	}
	if _, err := localizedhttp.ParseAcceptLanguage("en;q=1.001", localizedhttp.ParseOptions{}); !errors.Is(err, localizedhttp.ErrInvalidWeight) {
		t.Fatalf("ParseAcceptLanguage(1.001) error = %v", err)
	}
	weighted, err := localizedhttp.Select(value, "fi;q=0.5,en-US;q=0.9", localizedhttp.ParseOptions{})
	if err != nil || weighted.Locale != mustLocale(t, "en-US") {
		t.Fatalf("Select(weighted) = %+v, %v", weighted, err)
	}
	matched, err := localizedhttp.Select(value, "en-CA", localizedhttp.ParseOptions{})
	if err != nil || !matched.Present || matched.Kind != localizedmatch.Matched {
		t.Fatalf("Select(matched) = %+v, %v", matched, err)
	}
	if _, err := localizedhttp.Select(value, "en;q=2", localizedhttp.ParseOptions{}); !errors.Is(err, localizedhttp.ErrInvalidWeight) {
		t.Fatalf("Select(parse failure) error = %v", err)
	}
	for _, test := range []struct {
		name   string
		value  localized.Text
		header string
	}{
		{"zero weight", value, "en;q=0"},
		{"empty wildcard", localized.Text{}, "*"},
		{"unsupported", value, "qaa"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := localizedhttp.Select(test.value, test.header, localizedhttp.ParseOptions{})
			if err != nil || result.Kind != localizedmatch.Missing {
				t.Fatalf("Select() = %+v, %v", result, err)
			}
		})
	}
	empty, _ := localized.TextFromMap(map[string]string{"en": ""})
	result, err := localizedhttp.Select(empty, "*", localizedhttp.ParseOptions{})
	if err != nil || !result.Present || !result.Empty {
		t.Fatalf("Select(empty wildcard) = %+v, %v", result, err)
	}
}

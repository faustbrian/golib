package jsonapi

import (
	"errors"
	"reflect"
	"testing"
)

const (
	atomicExtension = "https://jsonapi.org/ext/atomic"
	cursorProfile   = "http://jsonapi.org/profiles/ethanresnick/cursor-pagination/"
)

func TestCheckContentType(t *testing.T) {
	t.Parallel()

	negotiator, err := NewNegotiator(
		[]string{atomicExtension},
		[]string{cursorProfile},
	)
	if err != nil {
		t.Fatalf("create negotiator: %v", err)
	}

	tests := map[string]struct {
		header    string
		want      MediaType
		status    int
		errorCode string
	}{
		"base media type": {
			header: MediaTypeJSONAPI,
			want:   MediaType{},
		},
		"supported extension and profiles": {
			header: `application/vnd.api+json;ext="https://jsonapi.org/ext/atomic";profile="http://jsonapi.org/profiles/ethanresnick/cursor-pagination/ https://example.com/unknown"`,
			want: MediaType{
				Extensions: []string{atomicExtension},
				Profiles:   []string{cursorProfile, "https://example.com/unknown"},
			},
		},
		"missing header": {
			status:    415,
			errorCode: "unsupported-media-type",
		},
		"wrong media type": {
			header:    "application/json",
			status:    415,
			errorCode: "unsupported-media-type",
		},
		"unknown parameter": {
			header:    "application/vnd.api+json;charset=utf-8",
			status:    415,
			errorCode: "unsupported-parameter",
		},
		"unsupported extension": {
			header:    `application/vnd.api+json;ext="https://example.com/unsupported"`,
			status:    415,
			errorCode: "unsupported-extension",
		},
		"invalid extension URI": {
			header:    `application/vnd.api+json;ext="not-a-uri"`,
			status:    415,
			errorCode: "invalid-parameter",
		},
		"empty extension list": {
			header:    `application/vnd.api+json;ext=""`,
			status:    415,
			errorCode: "invalid-parameter",
		},
		"URI lists use ASCII spaces only": {
			header:    "application/vnd.api+json;ext=\"https://example.com/one\thttps://example.com/two\"",
			status:    415,
			errorCode: "invalid-parameter",
		},
		"invalid profile URI": {
			header:    `application/vnd.api+json;profile="relative"`,
			status:    415,
			errorCode: "invalid-parameter",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := negotiator.CheckContentType(test.header)
			if test.status == 0 {
				if err != nil {
					t.Fatalf("check content type: %v", err)
				}
				if !reflect.DeepEqual(got, test.want) {
					t.Fatalf("unexpected media type: got %#v, want %#v", got, test.want)
				}
				return
			}

			assertNegotiationError(t, err, test.status, test.errorCode)
		})
	}
}

func TestNegotiateAccept(t *testing.T) {
	t.Parallel()

	negotiator, err := NewNegotiator(
		[]string{atomicExtension},
		[]string{cursorProfile},
	)
	if err != nil {
		t.Fatalf("create negotiator: %v", err)
	}

	tests := map[string]struct {
		header      string
		want        MediaType
		contentType string
		status      int
	}{
		"missing header accepts base": {
			contentType: MediaTypeJSONAPI,
		},
		"wildcard accepts base": {
			header:      "text/html, */*;q=0.5",
			contentType: MediaTypeJSONAPI,
		},
		"application wildcard accepts base": {
			header:      "application/*",
			contentType: MediaTypeJSONAPI,
		},
		"supported extension is selected": {
			header:      `application/vnd.api+json;ext="https://jsonapi.org/ext/atomic"`,
			want:        MediaType{Extensions: []string{atomicExtension}},
			contentType: `application/vnd.api+json; ext="https://jsonapi.org/ext/atomic"`,
		},
		"known profile is applied and unknown profile ignored": {
			header:      `application/vnd.api+json;profile="http://jsonapi.org/profiles/ethanresnick/cursor-pagination/ https://example.com/unknown"`,
			want:        MediaType{Profiles: []string{cursorProfile}},
			contentType: `application/vnd.api+json; profile="http://jsonapi.org/profiles/ethanresnick/cursor-pagination/"`,
		},
		"unknown profile does not override base quality": {
			header: `application/vnd.api+json;profile="https://example.com/unknown";q=0, ` +
				`application/vnd.api+json;q=1`,
			contentType: MediaTypeJSONAPI,
		},
		"higher quality valid candidate wins": {
			header:      `application/vnd.api+json;profile="http://jsonapi.org/profiles/ethanresnick/cursor-pagination/";q=0.7, application/vnd.api+json;ext="https://jsonapi.org/ext/atomic";q=0.9`,
			want:        MediaType{Extensions: []string{atomicExtension}},
			contentType: `application/vnd.api+json; ext="https://jsonapi.org/ext/atomic"`,
		},
		"duplicate range uses higher quality": {
			header:      `application/vnd.api+json;ext="https://jsonapi.org/ext/atomic";q=0.2, application/vnd.api+json;ext="https://jsonapi.org/ext/atomic";q=0.8`,
			want:        MediaType{Extensions: []string{atomicExtension}},
			contentType: `application/vnd.api+json; ext="https://jsonapi.org/ext/atomic"`,
		},
		"invalid candidate is ignored when base is available": {
			header:      "application/vnd.api+json;charset=utf-8, application/vnd.api+json",
			contentType: MediaTypeJSONAPI,
		},
		"malformed candidate is ignored when base is available": {
			header:      ";, application/vnd.api+json",
			contentType: MediaTypeJSONAPI,
		},
		"invalid quality is ignored when base is available": {
			header:      "application/vnd.api+json;q=invalid, application/vnd.api+json;q=0.5",
			contentType: MediaTypeJSONAPI,
		},
		"out of range quality is ignored when base is available": {
			header:      "application/vnd.api+json;q=2, application/vnd.api+json;q=0.5",
			contentType: MediaTypeJSONAPI,
		},
		"NaN quality is invalid": {
			header: "application/vnd.api+json;q=NaN",
			status: 406,
		},
		"exponent quality is invalid": {
			header: "application/vnd.api+json;q=1e-1",
			status: 406,
		},
		"signed quality is invalid": {
			header: "application/vnd.api+json;q=+0.5",
			status: 406,
		},
		"quality precision is limited to three digits": {
			header: "application/vnd.api+json;q=0.1234",
			status: 406,
		},
		"one quality permits only zero fractional digits": {
			header: "application/vnd.api+json;q=1.001",
			status: 406,
		},
		"parameterized wildcard is ignored when base is available": {
			header:      "*/*;charset=utf-8, application/vnd.api+json",
			contentType: MediaTypeJSONAPI,
		},
		"invalid profile candidate is ignored when base is available": {
			header:      `application/vnd.api+json;profile="relative", application/vnd.api+json`,
			contentType: MediaTypeJSONAPI,
		},
		"tab-separated URI list is invalid": {
			header: "application/vnd.api+json;profile=\"https://example.com/one\thttps://example.com/two\"",
			status: 406,
		},
		"unsupported extension candidate is ignored when base is available": {
			header:      `application/vnd.api+json;ext="https://example.com/unsupported", application/vnd.api+json;q=0.5`,
			contentType: MediaTypeJSONAPI,
		},
		"all JSON API candidates have invalid parameters": {
			header: "application/vnd.api+json;charset=utf-8",
			status: 406,
		},
		"all extension candidates are unsupported": {
			header: `application/vnd.api+json;ext="https://example.com/one", application/vnd.api+json;ext="https://example.com/two"`,
			status: 406,
		},
		"zero quality is unacceptable": {
			header: "application/vnd.api+json;q=0",
			status: 406,
		},
		"specific zero quality overrides wildcard": {
			header: "application/vnd.api+json;q=0, */*;q=1",
			status: 406,
		},
		"unrelated media type is unacceptable": {
			header: "application/json",
			status: 406,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := negotiator.NegotiateAccept(test.header)
			if test.status == 0 {
				if err != nil {
					t.Fatalf("negotiate accept: %v", err)
				}
				if !reflect.DeepEqual(got.MediaType, test.want) {
					t.Fatalf("unexpected media type: got %#v, want %#v", got.MediaType, test.want)
				}
				if got.ContentType != test.contentType {
					t.Fatalf("unexpected content type: got %q, want %q", got.ContentType, test.contentType)
				}
				if !got.VaryAccept {
					t.Fatal("expected negotiation to require Vary: Accept")
				}
				return
			}

			assertNegotiationError(t, err, test.status, "not-acceptable")
		})
	}
}

func TestNegotiateAcceptReportsCapabilityDependentVary(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		extensions []string
		profiles   []string
		want       bool
	}{
		"none":      {},
		"extension": {extensions: []string{atomicExtension}, want: true},
		"profile":   {profiles: []string{cursorProfile}, want: true},
		"both": {
			extensions: []string{atomicExtension},
			profiles:   []string{cursorProfile},
			want:       true,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			negotiator, err := NewNegotiator(test.extensions, test.profiles)
			if err != nil {
				t.Fatalf("construct negotiator: %v", err)
			}
			got, err := negotiator.NegotiateAccept("")
			if err != nil {
				t.Fatalf("negotiate missing Accept: %v", err)
			}
			if got.VaryAccept != test.want {
				t.Fatalf("unexpected Vary decision: got %v, want %v", got.VaryAccept, test.want)
			}
		})
	}
}

func TestNegotiateAcceptUsesSpecificityQualityAndCanonicalTieBreak(t *testing.T) {
	t.Parallel()

	negotiator, err := NewNegotiator([]string{atomicExtension}, []string{cursorProfile})
	if err != nil {
		t.Fatalf("construct negotiator: %v", err)
	}

	_, err = negotiator.NegotiateAccept("*/*;q=1, application/*;q=0")
	assertNegotiationError(t, err, 406, "not-acceptable")

	extension := `application/vnd.api+json;ext="` + atomicExtension + `";q=0.5`
	profile := `application/vnd.api+json;profile="` + cursorProfile + `";q=0.5`
	got, err := negotiator.NegotiateAccept(profile + "," + extension)
	if err != nil {
		t.Fatalf("negotiate equal-quality representations: %v", err)
	}
	want := `application/vnd.api+json; ext="` + atomicExtension + `"`
	if got.ContentType != want {
		t.Fatalf("unexpected canonical tie break: got %q, want %q", got.ContentType, want)
	}
}

func TestAcceptCandidateSpecificity(t *testing.T) {
	t.Parallel()

	negotiator, err := NewNegotiator([]string{atomicExtension}, []string{cursorProfile})
	if err != nil {
		t.Fatalf("construct negotiator: %v", err)
	}
	tests := map[string]struct {
		header      string
		specificity int
		matchesAll  bool
	}{
		"all wildcard":         {header: "*/*", specificity: 0, matchesAll: true},
		"application wildcard": {header: "application/*", specificity: 1, matchesAll: true},
		"base":                 {header: MediaTypeJSONAPI, specificity: 2, matchesAll: true},
		"extension": {
			header:      `application/vnd.api+json;ext="` + atomicExtension + `"`,
			specificity: 3,
		},
		"profile": {
			header:      `application/vnd.api+json;profile="` + cursorProfile + `"`,
			specificity: 3,
		},
		"extension and profile": {
			header: `application/vnd.api+json;ext="` + atomicExtension +
				`";profile="` + cursorProfile + `"`,
			specificity: 4,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			candidate, ok := negotiator.acceptCandidate(test.header)
			if !ok {
				t.Fatalf("candidate rejected: %q", test.header)
			}
			if candidate.specificity != test.specificity || candidate.matchesAll != test.matchesAll {
				t.Fatalf("unexpected candidate classification: %#v", candidate)
			}
		})
	}
}

func TestQualityForRepresentationHonorsSpecificityBeforeQuality(t *testing.T) {
	t.Parallel()

	representation := acceptCandidate{contentType: MediaTypeJSONAPI}
	tests := map[string]struct {
		ranges  []acceptCandidate
		quality float64
		matched bool
	}{
		"unmatched": {
			ranges: []acceptCandidate{{contentType: "application/json", specificity: 2, quality: 1}},
		},
		"more specific lower quality": {
			ranges: []acceptCandidate{
				{matchesAll: true, specificity: 0, quality: 1},
				{contentType: MediaTypeJSONAPI, specificity: 2, quality: 0.25},
			},
			quality: 0.25,
			matched: true,
		},
		"same specificity higher quality": {
			ranges: []acceptCandidate{
				{contentType: MediaTypeJSONAPI, specificity: 2, quality: 0.25},
				{contentType: MediaTypeJSONAPI, specificity: 2, quality: 0.75},
			},
			quality: 0.75,
			matched: true,
		},
		"same specificity retains higher quality": {
			ranges: []acceptCandidate{
				{contentType: MediaTypeJSONAPI, specificity: 2, quality: 0.75},
				{contentType: MediaTypeJSONAPI, specificity: 2, quality: 0.25},
			},
			quality: 0.75,
			matched: true,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			quality, matched := qualityForRepresentation(representation, test.ranges)
			if quality != test.quality || matched != test.matched {
				t.Fatalf("unexpected match: quality %v matched %v", quality, matched)
			}
		})
	}
}

func TestNewNegotiatorRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	_, err := NewNegotiator([]string{"not-a-uri"}, nil)
	if err == nil {
		t.Fatal("expected configuration error")
	}
	_, err = NewNegotiator(nil, []string{"relative"})
	if err == nil {
		t.Fatal("expected invalid profile configuration error")
	}
	_, err = NewNegotiator([]string{atomicExtension, atomicExtension}, nil)
	if err == nil {
		t.Fatal("expected duplicate extension configuration error")
	}
	_, err = NewNegotiator(nil, []string{cursorProfile, cursorProfile})
	if err == nil {
		t.Fatal("expected duplicate profile configuration error")
	}
	_, err = NewNegotiator([]string{"https://example.com/extensions/雪"}, nil)
	if err == nil {
		t.Fatal("expected non-RFC extension URI configuration error")
	}
}

func TestNegotiationHeaderUtilities(t *testing.T) {
	t.Parallel()

	mediaType := MediaType{
		Extensions: []string{"https://example.com/z", "https://example.com/z", "https://example.com/a"},
		Profiles:   []string{"https://example.com/profile", "https://example.com/profile"},
	}
	want := `application/vnd.api+json; ext="https://example.com/a https://example.com/z"; profile="https://example.com/profile"`
	if got := mediaType.String(); got != want {
		t.Fatalf("unexpected canonical media type: got %q, want %q", got, want)
	}

	header := `application/vnd.api+json;profile="https://example.com/a,b", application/vnd.api+json`
	values := splitHeaderValues(header)
	if len(values) != 2 || values[0] != `application/vnd.api+json;profile="https://example.com/a,b"` {
		t.Fatalf("quoted comma was split: %#v", values)
	}
	escaped := splitHeaderValues(`application/vnd.api+json;profile="https://example.com/a\"b,c", text/plain`)
	if len(escaped) != 2 {
		t.Fatalf("escaped quote changed header splitting: %#v", escaped)
	}
	if got := unknownParameter(map[string]string{
		"profile": "known",
		"zeta":    "unknown",
		"ext":     "known",
		"alpha":   "unknown",
	}); got != "alpha" {
		t.Fatalf("unexpected unknown parameter: %q", got)
	}

	limits := DefaultNegotiationLimits()
	for _, value := range []string{
		"https://example.com/one\thttps://example.com/two",
		"https://example.com/one\u00a0https://example.com/two",
		"https://example.com/one  https://example.com/two",
		" https://example.com/one",
		"https://example.com/one ",
	} {
		if _, err := parseURIList(value, "ext", limits); err == nil {
			t.Fatalf("invalid URI-list separator accepted: %q", value)
		}
	}
}

func TestHTTPQualityGrammarBoundaries(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]float64{
		"0":     0,
		"0.":    0,
		"0.125": 0.125,
		"1":     1,
		"1.000": 1,
	} {
		got, err := parseQuality(value)
		if err != nil || got != want {
			t.Fatalf("parse quality %q: got %v, err %v, want %v", value, got, err, want)
		}
	}
	for _, value := range []string{"", ".5", "2", "0.0000", "0.x", "1.01"} {
		if _, err := parseQuality(value); err == nil {
			t.Fatalf("invalid quality accepted: %q", value)
		}
	}
}

func TestEmptyURIListReportsMissingValue(t *testing.T) {
	t.Parallel()

	_, err := parseURIList("", "ext", DefaultNegotiationLimits())
	if err == nil || err.Error() != "ext parameter must contain at least one URI" {
		t.Fatalf("unexpected empty URI-list error: %v", err)
	}
}

func assertNegotiationError(t *testing.T, err error, status int, code string) {
	t.Helper()

	if err == nil {
		t.Fatal("expected negotiation error")
	}
	var negotiationError *NegotiationError
	if !errors.As(err, &negotiationError) {
		t.Fatalf("expected NegotiationError, got %T: %v", err, err)
	}
	if negotiationError.Status != status || negotiationError.Code != code {
		t.Fatalf(
			"unexpected error: got status %d code %q, want status %d code %q",
			negotiationError.Status,
			negotiationError.Code,
			status,
			code,
		)
	}
}

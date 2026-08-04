package canonical_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/canonical"
)

var testLimits = canonical.Limits{
	MaxInputBytes:  1024,
	MaxOutputBytes: 1024,
	MaxDepth:       16,
}

func TestJSONUsesRFC8785CanonicalForm(t *testing.T) {
	t.Parallel()

	first := []byte(` { "z": 1.0, "a": [true, {"x": 1e+2}] } `)
	second := []byte(`{"a":[true,{"x":100}],"z":1}`)

	canonicalFirst, err := canonical.JSON(first, testLimits)
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	canonicalSecond, err := canonical.JSON(second, testLimits)
	if err != nil {
		t.Fatalf("JSON() second error = %v", err)
	}
	if got, want := string(canonicalFirst), `{"a":[true,{"x":100}],"z":1}`; got != want {
		t.Fatalf("JSON() = %s, want %s", got, want)
	}
	if string(canonicalFirst) != string(canonicalSecond) {
		t.Fatalf("canonical values differ: %s != %s", canonicalFirst, canonicalSecond)
	}

	firstFingerprint, err := canonical.JSONFingerprint("jcs-v1", first, testLimits)
	if err != nil {
		t.Fatalf("JSONFingerprint() error = %v", err)
	}
	secondFingerprint, err := canonical.JSONFingerprint("jcs-v1", second, testLimits)
	if err != nil {
		t.Fatalf("JSONFingerprint() second error = %v", err)
	}
	if !firstFingerprint.Equal(secondFingerprint) {
		t.Fatal("equivalent JSON produced different fingerprints")
	}
}

func TestJSONRejectsHostileOrAmbiguousInput(t *testing.T) {
	t.Parallel()

	tests := map[string][]byte{
		"duplicate key":   []byte(`{"value":1,"value":2}`),
		"malformed":       []byte(`{"value":`),
		"invalid utf8":    {'"', 0xff, '"'},
		"lone surrogate":  []byte(`{"value":"\ud800"}`),
		"bad surrogate":   []byte(`{"value":"\ud800\u0041"}`),
		"bad low escape":  []byte(`{"value":"\ud800\uzzzz"}`),
		"low surrogate":   []byte(`{"value":"\udc00"}`),
		"short escape":    []byte(`{"value":"\u12`),
		"bad hex escape":  []byte(`{"value":"\uzzzz"}`),
		"number overflow": []byte(`{"value":1e9999}`),
		"negative zero":   []byte(`{"value":-0}`),
		"negative 0.0":    []byte(`{"value":-0.0}`),
		"trailing value":  []byte(`{} {}`),
		"bare escape":     []byte(`\`),
		"open escape":     {'"', '\\'},
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := canonical.JSON(input, testLimits)

			assertReason(t, err, idempotency.ReasonInvalidPayload)
		})
	}
}

func TestJSONAcceptsValidEscapesAndSurrogatePairs(t *testing.T) {
	t.Parallel()

	result, err := canonical.JSON(
		[]byte(`{"line":"\n","music":"\ud834\udd1e"}`),
		testLimits,
	)
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if got, want := string(result), `{"line":"\n","music":"𝄞"}`; got != want {
		t.Fatalf("JSON() = %q, want %q", got, want)
	}
}

func TestJSONAcceptsEscapesAndSurrogateRangeBoundaries(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		"escaped characters": {
			input: `{"quote":"\"","reverse-solidus":"\\","solidus":"\/"}`,
			want:  `{"quote":"\"","reverse-solidus":"\\","solidus":"/"}`,
		},
		"first surrogate pair": {input: `"\ud800\udc00"`, want: `"𐀀"`},
		"last surrogate pair":  {input: `"\udbff\udfff"`, want: `"􏿿"`},
		"before high range":    {input: `"\ud7ff"`, want: `"퟿"`},
		"after low range":      {input: `"\ue000"`, want: `""`},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, err := canonical.JSON([]byte(test.input), testLimits)
			if err != nil {
				t.Fatalf("JSON() error = %v", err)
			}
			if got := string(result); got != test.want {
				t.Fatalf("JSON() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestJSONRejectsValuesAdjacentToSurrogateRanges(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		input string
		cause string
	}{
		"below low range": {input: `"\ud800\udbff"`, cause: "invalid low surrogate"},
		"above low range": {input: `"\ud800\ue000"`, cause: "invalid low surrogate"},
		"last high alone": {input: `"\udbff"`, cause: "missing low surrogate"},
		"first low alone": {input: `"\udc00"`, cause: "unexpected low surrogate"},
		"last low alone":  {input: `"\udfff"`, cause: "unexpected low surrogate"},
		"low after pair": {
			input: `"\ud800\udc00\udc00"`,
			cause: "unexpected low surrogate",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := canonical.JSON([]byte(test.input), testLimits)
			assertReason(t, err, idempotency.ReasonInvalidPayload)
			assertCause(t, err, test.cause)
		})
	}
}

func TestJSONReportsMalformedEscapeBoundary(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		input []byte
		cause string
	}{
		"outside string":     {input: []byte(`\`), cause: "escape outside string"},
		"after string start": {input: []byte{'"', '\\'}, cause: "incomplete escape"},
		"short unicode":      {input: []byte(`{"value":"\u12`), cause: "incomplete Unicode escape"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := canonical.JSON(test.input, testLimits)
			assertReason(t, err, idempotency.ReasonInvalidPayload)
			assertCause(t, err, test.cause)
		})
	}
}

func TestJSONReportsTruncatedSurrogateAtExactBoundaries(t *testing.T) {
	t.Parallel()

	for name, input := range map[string][]byte{
		"truncated low surrogate": []byte(`"\ud800\u123`),
		"wrong pair separator":    []byte(`"\ud800xu1234"`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := canonical.JSON(input, testLimits)
			assertReason(t, err, idempotency.ReasonInvalidPayload)
			assertCause(t, err, "missing low surrogate")
		})
	}
}

func TestJSONDoesNotTreatFourHexDigitsAsAnIncompleteUnicodeEscape(t *testing.T) {
	t.Parallel()

	_, err := canonical.JSON([]byte(`"\u1234`), testLimits)
	assertReason(t, err, idempotency.ReasonInvalidPayload)
	assertCauseNot(t, err, "incomplete Unicode escape")
}

func TestJSONAcceptsPositiveZeroAndNegativeNonzero(t *testing.T) {
	t.Parallel()

	result, err := canonical.JSON([]byte(`{"negative":-1,"zero":0}`), testLimits)
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if got, want := string(result), `{"negative":-1,"zero":0}`; got != want {
		t.Fatalf("JSON() = %s, want %s", got, want)
	}
}

func TestJSONEnforcesAllResourceLimits(t *testing.T) {
	t.Parallel()

	t.Run("input", func(t *testing.T) {
		t.Parallel()
		_, err := canonical.JSON([]byte(`{"value":1}`), canonical.Limits{
			MaxInputBytes:  4,
			MaxOutputBytes: 1024,
			MaxDepth:       16,
		})
		assertField(t, err, idempotency.ReasonLimitExceeded, "input")
	})

	t.Run("output", func(t *testing.T) {
		t.Parallel()
		_, err := canonical.JSON([]byte(`{"value":1}`), canonical.Limits{
			MaxInputBytes:  1024,
			MaxOutputBytes: 4,
			MaxDepth:       16,
		})
		assertField(t, err, idempotency.ReasonLimitExceeded, "output")
	})

	t.Run("depth", func(t *testing.T) {
		t.Parallel()
		_, err := canonical.JSON([]byte(`[[[0]]]`), canonical.Limits{
			MaxInputBytes:  1024,
			MaxOutputBytes: 1024,
			MaxDepth:       2,
		})
		assertField(t, err, idempotency.ReasonLimitExceeded, "depth")
	})
}

func TestJSONAcceptsExactResourceLimits(t *testing.T) {
	t.Parallel()

	input := []byte(`[[0]]`)
	result, err := canonical.JSON(input, canonical.Limits{
		MaxInputBytes:  len(input),
		MaxOutputBytes: len(input),
		MaxDepth:       2,
	})
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if got := string(result); got != string(input) {
		t.Fatalf("JSON() = %s, want %s", got, input)
	}

	result, err = canonical.JSON([]byte(`[[],[]]`), canonical.Limits{
		MaxInputBytes:  7,
		MaxOutputBytes: 7,
		MaxDepth:       2,
	})
	if err != nil {
		t.Fatalf("JSON() sibling arrays error = %v", err)
	}
	if got, want := string(result), `[[],[]]`; got != want {
		t.Fatalf("JSON() = %s, want %s", got, want)
	}
}

func TestJSONRejectsNonPositiveLimits(t *testing.T) {
	t.Parallel()

	tests := map[string]canonical.Limits{
		"input":  {MaxOutputBytes: 1, MaxDepth: 1},
		"output": {MaxInputBytes: 1, MaxDepth: 1},
		"depth":  {MaxInputBytes: 1, MaxOutputBytes: 1},
	}
	for name, limits := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := canonical.JSON([]byte(`null`), limits)
			assertField(t, err, idempotency.ReasonInvalidConfiguration, name)
		})
	}
}

func TestBytesFingerprintRequiresAnExplicitBound(t *testing.T) {
	t.Parallel()

	fingerprint, err := canonical.BytesFingerprint("raw-v1", []byte("stable bytes"), 32)
	if err != nil {
		t.Fatalf("BytesFingerprint() error = %v", err)
	}
	want, err := idempotency.NewFingerprint("raw-v1", []byte("stable bytes"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	if !fingerprint.Equal(want) {
		t.Fatal("BytesFingerprint() did not hash the supplied bytes")
	}

	_, err = canonical.BytesFingerprint("raw-v1", []byte("too large"), 4)
	assertField(t, err, idempotency.ReasonLimitExceeded, "input")
	_, err = canonical.BytesFingerprint("raw-v1", nil, 0)
	assertField(t, err, idempotency.ReasonInvalidConfiguration, "max_bytes")
	_, err = canonical.BytesFingerprint("", []byte("stable bytes"), 32)
	assertReason(t, err, idempotency.ReasonInvalidFingerprint)
}

func TestBytesFingerprintAcceptsExactBound(t *testing.T) {
	t.Parallel()

	input := []byte("exact")
	fingerprint, err := canonical.BytesFingerprint("raw-v1", input, len(input))
	if err != nil {
		t.Fatalf("BytesFingerprint() error = %v", err)
	}
	want, err := idempotency.NewFingerprint("raw-v1", input)
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	if !fingerprint.Equal(want) {
		t.Fatal("BytesFingerprint() changed input at the exact byte bound")
	}
}

func TestJSONFingerprintPreservesCanonicalizationErrors(t *testing.T) {
	t.Parallel()

	_, err := canonical.JSONFingerprint(
		"jcs-v1",
		[]byte(`{"value":1,"value":2}`),
		testLimits,
	)

	assertReason(t, err, idempotency.ReasonInvalidPayload)
}

func TestCanonicalErrorsDoNotExposePayloads(t *testing.T) {
	t.Parallel()

	secret := "customer-secret-value"
	_, err := canonical.JSON([]byte(`{"secret":"`+secret+`","secret":2}`), testLimits)
	if err == nil {
		t.Fatal("JSON() error = nil")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error exposed payload: %v", err)
	}
}

func assertReason(t *testing.T, err error, want idempotency.Reason) {
	t.Helper()
	var semanticError *idempotency.Error
	if !errors.As(err, &semanticError) {
		t.Fatalf("error = %v, want *idempotency.Error", err)
	}
	if semanticError.Reason != want {
		t.Fatalf("reason = %q, want %q", semanticError.Reason, want)
	}
}

func assertField(t *testing.T, err error, reason idempotency.Reason, field string) {
	t.Helper()
	var semanticError *idempotency.Error
	if !errors.As(err, &semanticError) {
		t.Fatalf("error = %v, want *idempotency.Error", err)
	}
	if semanticError.Reason != reason || semanticError.Field != field {
		t.Fatalf("error = %#v, want reason %q field %q", semanticError, reason, field)
	}
}

func assertCause(t *testing.T, err error, want string) {
	t.Helper()

	var semanticError *idempotency.Error
	if !errors.As(err, &semanticError) {
		t.Fatalf("error = %v, want *idempotency.Error", err)
	}
	if semanticError.Cause == nil || semanticError.Cause.Error() != want {
		t.Fatalf("cause = %v, want %q", semanticError.Cause, want)
	}
}

func assertCauseNot(t *testing.T, err error, unwanted string) {
	t.Helper()

	var semanticError *idempotency.Error
	if !errors.As(err, &semanticError) {
		t.Fatalf("error = %v, want *idempotency.Error", err)
	}
	if semanticError.Cause == nil {
		t.Fatal("cause = nil")
	}
	if semanticError.Cause.Error() == unwanted {
		t.Fatalf("cause = %q, do not want %q", semanticError.Cause, unwanted)
	}
}

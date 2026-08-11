package httpsignature

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMutationContractBodyEntryFailuresBeforeDelegation(t *testing.T) {
	assertBodyIntegrationConstructorsRejectEachIndependentField(t)

	delegate := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})
	for _, config := range []BufferedContentDigestRoundTripperConfig{
		{Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1},
		{Transport: delegate, Algorithms: []DigestAlgorithm{SHA256}},
	} {
		if _, err := NewBufferedContentDigestRoundTripper(config); !errors.Is(err, ErrInvalidBodyIntegration) {
			t.Fatalf("invalid buffered digest transport config error = %v", err)
		}
	}
	if _, err := NewBufferedContentDigestRoundTripper(BufferedContentDigestRoundTripperConfig{
		Transport: delegate, Algorithms: []DigestAlgorithm{"unsupported"}, MaxBytes: 1,
	}); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("invalid buffered digest transport error = %v", err)
	}
	transport, err := NewBufferedContentDigestRoundTripper(BufferedContentDigestRoundTripperConfig{
		Transport: delegate, Algorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	var nilTransport *BufferedContentDigestRoundTripper
	if _, err := nilTransport.RoundTrip(httptest.NewRequest(http.MethodPost, "https://example.com", nil)); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("nil buffered digest transport error = %v", err)
	}
	if _, err := transport.RoundTrip(nil); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("nil buffered digest request error = %v", err)
	}

	for _, test := range []struct {
		name    string
		request *http.Request
		want    error
	}{
		{
			name: "ambiguous protected header",
			request: &http.Request{Method: http.MethodPost, URL: &url.URL{Scheme: "https", Host: "example.com"},
				Header: http.Header{"Signature": []string{"one"}, "signature": []string{"two"}}, Body: http.NoBody},
			want: ErrAmbiguousProtectedField,
		},
		{
			name: "existing digest",
			request: &http.Request{Method: http.MethodPost, URL: &url.URL{Scheme: "https", Host: "example.com"},
				Header: http.Header{"Content-Digest": []string{"sha-256=:AA==:"}}, Body: http.NoBody},
			want: ErrExistingDigest,
		},
		{
			name: "body read failure",
			request: &http.Request{Method: http.MethodPost, URL: &url.URL{Scheme: "https", Host: "example.com"},
				Header: make(http.Header), Body: &mutationContractReadFailureBody{}},
			want: ErrBodyRead,
		},
		{
			name: "ambiguous trailer",
			request: &http.Request{Method: http.MethodPost, URL: &url.URL{Scheme: "https", Host: "example.com"},
				Header: make(http.Header), Trailer: http.Header{"Signature": []string{"one"}, "signature": []string{"two"}}, Body: http.NoBody},
			want: ErrAmbiguousProtectedField,
		},
	} {
		if _, err := transport.RoundTrip(test.request); !errors.Is(err, test.want) {
			t.Fatalf("%s error = %v, want %v", test.name, err, test.want)
		}
	}

	mapErrors := make(chan error, 1)
	for _, config := range []BufferedContentDigestVerificationMiddlewareConfig{
		{RequiredAlgorithms: []DigestAlgorithm{SHA256}, MapError: func(http.ResponseWriter, *http.Request, error) {}},
		{RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1},
	} {
		if _, err := NewBufferedContentDigestVerificationMiddleware(config); !errors.Is(err, ErrInvalidBodyIntegration) {
			t.Fatalf("invalid buffered verification middleware config error = %v", err)
		}
	}
	if _, err := NewBufferedContentDigestVerificationMiddleware(BufferedContentDigestVerificationMiddlewareConfig{
		RequiredAlgorithms: []DigestAlgorithm{"unsupported"}, MaxBytes: 1,
		MapError: func(http.ResponseWriter, *http.Request, error) {},
	}); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("invalid buffered verification middleware error = %v", err)
	}
	middleware, err := NewBufferedContentDigestVerificationMiddleware(BufferedContentDigestVerificationMiddlewareConfig{
		RequiredAlgorithms: []DigestAlgorithm{SHA256}, MaxBytes: 1,
		MapError: func(_ http.ResponseWriter, _ *http.Request, err error) { mapErrors <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	assertMapped := func(name string, handler http.Handler, request *http.Request, want error) {
		t.Helper()
		middleware(handler).ServeHTTP(httptest.NewRecorder(), request)
		select {
		case got := <-mapErrors:
			if !errors.Is(got, want) {
				t.Fatalf("%s mapped error = %v, want %v", name, got, want)
			}
		default:
			t.Fatalf("%s did not map an error", name)
		}
	}
	assertMapped("nil next", nil, httptest.NewRequest(http.MethodPost, "https://example.com", nil), ErrInvalidBodyIntegration)
	assertMapped("nil request", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil, ErrInvalidBodyIntegration)
	ambiguous := httptest.NewRequest(http.MethodPost, "https://example.com", nil)
	ambiguous.Header = http.Header{"Signature": []string{"one"}, "signature": []string{"two"}}
	assertMapped("ambiguous request", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), ambiguous, ErrAmbiguousProtectedField)

	var nilContext context.Context
	if _, err := readBoundedAndClose(nilContext, http.NoBody, 1); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("nil body context error = %v", err)
	}
	if _, err := readBoundedAndClose(context.Background(), http.NoBody, 0); !errors.Is(err, ErrInvalidBodyIntegration) {
		t.Fatalf("invalid body limit error = %v", err)
	}
}

// This file sorts before the parallel lifecycle suites. These deterministic
// checks reject malformed replay state before a mutated guard can block.
func TestMutationContractReplayValidationFailsBeforeStateAccess(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	config := MemoryReplayConfig{
		Capacity: 2, MaxTTL: time.Minute, MaxKeyIDBytes: 8, MaxNonceBytes: 8,
		Now: func() time.Time { return now },
	}
	store, err := NewMemoryReplayStore(config)
	if err != nil {
		t.Fatal(err)
	}
	record := ReplayRecord{KeyID: "key", Nonce: "nonce", ExpiresAt: now.Add(time.Minute)}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Consume(canceled, record); !errors.Is(err, context.Canceled) {
		t.Fatalf("Consume(canceled) error = %v, want context.Canceled", err)
	}
	if err := store.Consume(context.Background(), record); err != nil {
		t.Fatalf("first Consume(valid) error = %v", err)
	}
	if err := store.Consume(context.Background(), record); !errors.Is(err, ErrReplayDetected) {
		t.Fatalf("second Consume(valid) error = %v, want ErrReplayDetected", err)
	}

	freshStore := func() MemoryReplayStore {
		candidate, createErr := NewMemoryReplayStore(config)
		if createErr != nil {
			t.Fatal(createErr)
		}
		return *candidate
	}
	cases := []struct {
		name   string
		mutate func(*MemoryReplayStore)
	}{
		{name: "mutex", mutate: func(candidate *MemoryReplayStore) { candidate.mu = nil }},
		{name: "state", mutate: func(candidate *MemoryReplayStore) { candidate.state = nil }},
		{name: "entries", mutate: func(candidate *MemoryReplayStore) { candidate.state.entries = nil }},
		{name: "capacity", mutate: func(candidate *MemoryReplayStore) { candidate.capacity = 0 }},
		{name: "ttl", mutate: func(candidate *MemoryReplayStore) { candidate.maxTTL = 0 }},
		{name: "key bound", mutate: func(candidate *MemoryReplayStore) { candidate.maxKeyIDBytes = 0 }},
		{name: "nonce bound", mutate: func(candidate *MemoryReplayStore) { candidate.maxNonceBytes = 0 }},
		{name: "clock", mutate: func(candidate *MemoryReplayStore) { candidate.now = nil }},
	}
	for _, test := range cases {
		candidate := freshStore()
		test.mutate(&candidate)
		if err := candidate.Consume(context.Background(), record); !errors.Is(err, ErrInvalidReplayConfig) {
			t.Fatalf("%s invalid store error = %v, want ErrInvalidReplayConfig", test.name, err)
		}
	}
}

func TestMutationContractCanonicalParserExactBoundaries(t *testing.T) {
	request := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/"}, RequestURI: "/", Host: "example.com"}
	related := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/related"}, RequestURI: "/related", Host: "example.com"}
	unrelated := &http.Request{}
	external := &ExternalRequestContext{Scheme: "https", Authority: "example.com", RequestTarget: "/"}
	messageContext := MessageContext{Request: request, RelatedRequest: related, ExternalRequest: external}
	if externalForRequest(messageContext, request) != external || externalForRequest(messageContext, related) != external {
		t.Fatal("external request context was not selected for an owned request")
	}
	if externalForRequest(messageContext, unrelated) != nil {
		t.Fatal("external request context was selected for an unrelated request")
	}
	absolute := &http.Request{
		Method:     http.MethodGet,
		URL:        &url.URL{Scheme: "https", Host: "example.com", Path: "/"},
		RequestURI: "https://example.com/",
		Host:       "example.com",
	}
	if _, err := requestParts(absolute, &ExternalRequestContext{
		Scheme: "https", Authority: "example.com", RequestTarget: absolute.RequestURI,
	}); err != nil {
		t.Fatalf("matching external absolute target error = %v", err)
	}
	if _, err := requestParts(absolute, &ExternalRequestContext{
		Scheme: "http", Authority: "example.com", RequestTarget: absolute.RequestURI,
	}); err == nil {
		t.Fatal("contradictory external absolute target succeeded")
	}

	invalidParameter := []Parameter{{Name: "UPPER", Value: true}}
	for _, component := range []ComponentIdentifier{
		{Name: "@method", Parameters: invalidParameter},
		{Name: "@", Parameters: []Parameter{{Name: "req", Value: true}}},
	} {
		if _, err := serializeComponentIdentifier(component); err == nil {
			t.Fatalf("serializeComponentIdentifier(%#v) succeeded", component)
		}
		if _, err := serializeSignatureParameters(SignatureInput{Components: []ComponentIdentifier{component}}); err == nil {
			t.Fatalf("serializeSignatureParameters(%#v) succeeded", component)
		}
	}
	if got := formPercentEncode("azAZ09*-._"); got != "azAZ09*-._" {
		t.Fatalf("formPercentEncode boundary bytes = %q", got)
	}

	if value, err := queryParameter("x="+string([]byte{0xff}), "x", 9); err != nil || value != "%EF%BF%BD" {
		t.Fatalf("query value at exact encoded limit = %q, %v", value, err)
	}
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "a%41", want: "aA"},
		{input: "%Ag", want: "%Ag"},
		{input: "%gA", want: "%gA"},
		{input: "%AgZ", want: "%AgZ"},
	} {
		if got := decodeFormComponent(test.input); got != test.want {
			t.Fatalf("decodeFormComponent(%q) = %q, want %q", test.input, got, test.want)
		}
	}

	for _, test := range []struct {
		first byte
		width int
		min   byte
		max   byte
	}{
		{0xc1, 0, 0, 0}, {0xc2, 2, 0x80, 0xbf}, {0xdf, 2, 0x80, 0xbf},
		{0xe0, 3, 0xa0, 0xbf}, {0xe1, 3, 0x80, 0xbf}, {0xec, 3, 0x80, 0xbf},
		{0xed, 3, 0x80, 0x9f}, {0xee, 3, 0x80, 0xbf}, {0xef, 3, 0x80, 0xbf},
		{0xf0, 4, 0x90, 0xbf}, {0xf1, 4, 0x80, 0xbf}, {0xf3, 4, 0x80, 0xbf},
		{0xf4, 4, 0x80, 0x8f}, {0xf5, 0, 0, 0},
	} {
		width, minimum, maximum := utf8Sequence(test.first)
		if width != test.width || minimum != test.min || maximum != test.max {
			t.Fatalf("utf8Sequence(%x) = (%d,%x,%x), want (%d,%x,%x)",
				test.first, width, minimum, maximum, test.width, test.min, test.max)
		}
	}

	for _, test := range []struct {
		input []byte
		want  string
	}{
		{input: []byte{0xe1, 0x80, 'x'}, want: "�x"},
		{input: []byte{0xe1, 'x', 0x80}, want: "�x�"},
		{input: []byte{0xf0, 0x90, 'x', 0x80}, want: "�x�"},
		{input: []byte{0xf0, 0x90, 0x80}, want: "�"},
	} {
		if got := decodeUTF8WithReplacement(test.input); got != test.want {
			t.Fatalf("decodeUTF8WithReplacement(%x) = %q, want %q", test.input, got, test.want)
		}
	}

	for _, test := range []struct {
		input byte
		want  byte
		ok    bool
	}{
		{'/', 0, false}, {'0', 0, true}, {'9', 9, true}, {':', 0, false},
		{'@', 0, false}, {'A', 10, true}, {'F', 15, true}, {'G', 0, false},
		{'`', 0, false}, {'a', 10, true}, {'f', 15, true}, {'g', 0, false},
	} {
		got, ok := hexValue(test.input)
		if got != test.want || ok != test.ok {
			t.Fatalf("hexValue(%q) = (%d,%t), want (%d,%t)", test.input, got, ok, test.want, test.ok)
		}
	}
}

func TestMutationContractFieldOwnershipTruthTables(t *testing.T) {
	request := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Scheme: "https", Host: "example.com", Path: "/"},
		Header: http.Header{
			"Cookie": []string{"a=1;b=2"},
			"X-Test": []string{"a;b"},
		},
	}
	if _, err := resolveField(MessageContext{Request: request}, "cookie", componentParameters{}, DefaultMaxSignatureBaseBytes); err == nil {
		t.Fatal("noncanonical request Cookie succeeded without binary wrapping")
	} else if err.Error() != "cookie coverage requires canonical semicolon spacing" {
		t.Fatalf("noncanonical request Cookie error = %q", err)
	}
	request.Header["Cookie"] = []string{"a=1", "b=2"}
	if _, err := resolveField(MessageContext{Request: request}, "cookie", componentParameters{}, DefaultMaxSignatureBaseBytes); err == nil {
		t.Fatal("multi-line request Cookie succeeded without binary wrapping")
	} else if err.Error() != "cookie coverage requires one canonical field value" {
		t.Fatalf("multi-line request Cookie error = %q", err)
	}
	if _, err := resolveField(MessageContext{Request: request}, "cookie", componentParameters{bs: true}, DefaultMaxSignatureBaseBytes); err != nil {
		t.Fatalf("binary-wrapped request Cookie error = %v", err)
	}
	if value, err := resolveField(MessageContext{Request: request}, "x-test", componentParameters{}, DefaultMaxSignatureBaseBytes); err != nil || value != "a;b" {
		t.Fatalf("ordinary request field = %q,%v", value, err)
	}

	response := &http.Response{Header: http.Header{"Cookie": []string{"a=1;b=2"}}}
	if value, err := resolveField(MessageContext{Response: response}, "cookie", componentParameters{}, DefaultMaxSignatureBaseBytes); err != nil || value != "a=1;b=2" {
		t.Fatalf("response Cookie = %q,%v", value, err)
	}
	if values, handled, err := managedFieldValues(MessageContext{Request: &http.Request{}}, "host", componentParameters{}, true); err != nil || !handled || values != nil {
		t.Fatalf("absent managed Host = %#v,%t,%v", values, handled, err)
	}
	related := &http.Request{Header: http.Header{"X-Test": []string{"related"}}}
	if value, err := resolveField(MessageContext{Response: &http.Response{}, RelatedRequest: related}, "x-test", componentParameters{req: true}, DefaultMaxSignatureBaseBytes); err != nil || value != "related" {
		t.Fatalf("related request field = %q,%v", value, err)
	}

	for _, test := range []struct {
		name     string
		response *http.Response
		want     []string
	}{
		{
			name: "not marked close despite explicit response shape",
			response: &http.Response{
				ProtoMajor: 1, ProtoMinor: 1, StatusCode: http.StatusNoContent, ContentLength: -1,
			},
		},
		{
			name:     "close marker ignored outside HTTP 1",
			response: &http.Response{ProtoMajor: 2, Close: true},
		},
		{
			name: "explicit received close",
			response: &http.Response{
				ProtoMajor: 1, ProtoMinor: 1, StatusCode: http.StatusNoContent, ContentLength: -1, Close: true,
			},
			want: []string{"close"},
		},
	} {
		got, err := responseConnectionFieldValues(test.response, ResponseTransportReceived)
		if err != nil || !sameFieldValues(got, test.want) {
			t.Fatalf("%s = %#v,%v, want %#v", test.name, got, err, test.want)
		}
	}
	if _, err := preservedResponseContentLengthFieldValues(http.Header{
		"Content-Length": []string{"1"}, "content-length": []string{"1"},
	}); err == nil {
		t.Fatalf("case-colliding response Content-Length error = %v", err)
	}
}

func TestMutationContractSizeAccountingMatchesCanonicalBytes(t *testing.T) {
	inputs := []SignatureInput{
		{},
		{Components: []ComponentIdentifier{{Name: "@method"}}},
		{
			Components: []ComponentIdentifier{
				{Name: "@method", Parameters: []Parameter{{Name: "req", Value: true}}},
				{Name: "content-type"},
			},
			Parameters: []Parameter{
				{Name: "created", Value: int64(-10)},
				{Name: "nonce", Value: string([]byte{0x61, 0x22, 0x5c, 0x62})},
				{Name: "flag", Value: false},
			},
		},
	}
	for _, input := range inputs {
		serialized, err := serializeSignatureParameters(input)
		if err != nil {
			t.Fatal(err)
		}
		for budget := -1; budget <= len(serialized)+1; budget++ {
			want := len(serialized) <= budget
			if got := signatureParametersFit(input, budget); got != want {
				t.Fatalf("signatureParametersFit(%q, %d) = %t, want %t", serialized, budget, got, want)
			}
		}
	}

	fieldCases := []struct {
		values []string
		binary bool
		size   int
	}{
		{values: nil, size: 0},
		{values: []string{""}, size: 0},
		{values: []string{"a", "bc"}, size: len("a, bc")},
		{values: []string{"a"}, binary: true, size: 2 + base64.StdEncoding.EncodedLen(1)},
		{
			values: []string{"a", "bc"}, binary: true,
			size: 2 + base64.StdEncoding.EncodedLen(1) + 2 + 2 + base64.StdEncoding.EncodedLen(2),
		},
	}
	for _, test := range fieldCases {
		for budget := -1; budget <= test.size+1; budget++ {
			want := test.size <= budget
			if got := fieldValuesFit(test.values, test.binary, budget); got != want {
				t.Fatalf("fieldValuesFit(%q, %t, %d) = %t, want %t",
					test.values, test.binary, budget, got, want)
			}
		}
	}

	for _, test := range []struct {
		value int64
		want  int
	}{
		{math.MinInt64, 20}, {-100, 4}, {-10, 3}, {-1, 2}, {0, 1},
		{1, 1}, {9, 1}, {10, 2}, {99, 2}, {100, 3}, {math.MaxInt64, 19},
	} {
		if got := decimalIntegerLength(test.value); got != test.want {
			t.Fatalf("decimalIntegerLength(%d) = %d, want %d", test.value, got, test.want)
		}
	}

	for size := 0; size <= 10; size++ {
		if got, want := base64EncodedLength(size), base64.StdEncoding.EncodedLen(size); got != want {
			t.Fatalf("base64EncodedLength(%d) = %d, want %d", size, got, want)
		}
	}
	if got := base64EncodedLength(math.MaxInt); got != math.MaxInt {
		t.Fatalf("base64EncodedLength(MaxInt) = %d, want MaxInt", got)
	}

	for _, test := range []struct {
		left, right int
		value       int
		ok          bool
	}{
		{-1, 0, 0, false}, {0, -1, 0, false}, {math.MaxInt, 1, 0, false},
		{0, 0, 0, true}, {1, 2, 3, true}, {math.MaxInt - 1, 1, math.MaxInt, true},
	} {
		value, ok := safeSizeAdd(test.left, test.right)
		if value != test.value || ok != test.ok {
			t.Fatalf("safeSizeAdd(%d,%d) = (%d,%t), want (%d,%t)",
				test.left, test.right, value, ok, test.value, test.ok)
		}
	}
	for _, test := range []struct {
		value, multiplier int
		result            int
		ok                bool
	}{
		{-1, 1, 0, false}, {1, -1, 0, false}, {math.MaxInt, 2, 0, false},
		{0, 0, 0, true}, {0, math.MaxInt, 0, true}, {2, 3, 6, true}, {math.MaxInt, 1, math.MaxInt, true},
	} {
		result, ok := safeSizeMultiply(test.value, test.multiplier)
		if result != test.result || ok != test.ok {
			t.Fatalf("safeSizeMultiply(%d,%d) = (%d,%t), want (%d,%t)",
				test.value, test.multiplier, result, ok, test.result, test.ok)
		}
	}

	for _, test := range []struct {
		remaining int
		size      int
		want      bool
		after     int
	}{
		{remaining: 3, size: -1, want: false, after: 3},
		{remaining: 3, size: 4, want: false, after: 3},
		{remaining: 3, size: 0, want: true, after: 3},
		{remaining: 3, size: 3, want: true, after: 0},
		{remaining: 3, size: 2, want: true, after: 1},
	} {
		remaining := test.remaining
		if got := consumeSize(&remaining, test.size); got != test.want || remaining != test.after {
			t.Fatalf("consumeSize(%d,%d) = (%t,%d), want (%t,%d)",
				test.remaining, test.size, got, remaining, test.want, test.after)
		}
	}
}

func TestMutationContractDerivedSourceFitsExactBudgets(t *testing.T) {
	for _, test := range []struct {
		request  *http.Request
		external *ExternalRequestContext
		name     string
		budget   int
		want     bool
	}{
		{request: nil, name: "@path", budget: -1, want: true},
		{request: &http.Request{}, name: "@path", budget: -1, want: true},
		{request: &http.Request{URL: &url.URL{}}, name: "@path", budget: -1, want: true},
	} {
		if got := derivedSourceFits(test.request, test.external, test.name, test.budget); got != test.want {
			t.Fatalf("derivedSourceFits(%s,%d) = %t, want %t", test.name, test.budget, got, test.want)
		}
	}

	request := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Scheme: "https", Host: "h", Path: "/p"},
		Host:   "h", RequestURI: "/p",
	}
	for _, test := range []struct {
		name       string
		exact, low int
	}{
		{name: "@scheme", exact: 5, low: 4},
		{name: "@authority", exact: 5, low: 4},
		{name: "@request-target", exact: 5, low: 4},
		{name: "@path", exact: 5, low: 4},
		{name: "@query", exact: 5, low: 4},
		{name: "@query-param", exact: 6, low: 5},
		{name: "@target-uri", exact: 11, low: 10},
	} {
		if !derivedSourceFits(request, nil, test.name, test.exact) {
			t.Fatalf("%s rejected exact budget %d", test.name, test.exact)
		}
		if derivedSourceFits(request, nil, test.name, test.low) {
			t.Fatalf("%s accepted low budget %d", test.name, test.low)
		}
	}
	if !derivedSourceFits(request, nil, "@unknown", 5) {
		t.Fatal("unknown derived source rejected after common source bounds")
	}

	external := &ExternalRequestContext{Scheme: "http", Authority: "a", RequestTarget: "/"}
	if !derivedSourceFits(request, external, "@target-uri", 9) ||
		derivedSourceFits(request, external, "@target-uri", 8) {
		t.Fatal("external target URI exact budget classified incorrectly")
	}
}

func TestMutationContractStructuredFieldNormalizationBoundaries(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: `:aGVsbG8:`, want: `:aGVsbG8=:`},
		{input: `:iZ==:`, want: `:iQ==:`},
		{input: `:aGVsbG8==:`, want: `:aGVsbG8==:`},
		{input: `:a:`, want: `:a:`},
		{input: `:a=GV:`, want: `:a=GV:`},
		{input: `:aGVs-bG8:`, want: `:aGVs-bG8:`},
		{input: `token:aGVsbG8:`, want: `token:aGVsbG8:`},
		{input: `"quoted :aGVsbG8: value"`, want: `"quoted :aGVsbG8: value"`},
		{input: `"escaped \\" :aGVsbG8:`, want: `"escaped \\" :aGVsbG8=:`},
		{input: `:aGVsbG8`, want: `:aGVsbG8`},
	} {
		if got := normalizeRFC8941BinaryValue(test.input); got != test.want {
			t.Fatalf("normalizeRFC8941BinaryValue(%q) = %q, want %q", test.input, got, test.want)
		}
	}

	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	for value := 0; value <= 0xff; value++ {
		character := byte(value)
		want := strings.ContainsRune(alphabet, rune(character))
		if got := validRFC8941Base64Alphabet(string([]byte{character})); got != want {
			t.Fatalf("validRFC8941Base64Alphabet(%x) = %t, want %t", character, got, want)
		}
	}
	if !validRFC8941Base64Alphabet("") {
		t.Fatal("empty base64 alphabet was rejected")
	}

	for _, test := range []struct {
		value string
		point int
		want  bool
	}{
		{"0.", 1, true}, {"9.", 1, true}, {"-0.", 2, true}, {"x=0.", 3, true},
		{"(0.)", 2, true}, {" 0. ", 2, true}, {"a0.", 2, false}, {"0.0", 1, false},
		{"/.", 1, false}, {":.", 1, false}, {"", 0, false},
	} {
		if got := isEmptyRFC8941DecimalFraction(test.value, test.point); got != test.want {
			t.Fatalf("isEmptyRFC8941DecimalFraction(%q,%d) = %t, want %t", test.value, test.point, got, test.want)
		}
	}

	if got := combineStructuredFieldLines(nil); got != nil {
		t.Fatalf("combine nil = %#v", got)
	}
	one := []string{"a"}
	if got := combineStructuredFieldLines(one); len(got) != 1 || got[0] != "a" {
		t.Fatalf("combine one = %#v", got)
	}
	if got := combineStructuredFieldLines([]string{"a", "b"}); len(got) != 1 || got[0] != "a, b" {
		t.Fatalf("combine two = %#v", got)
	}
	for _, test := range []struct{ input, want string }{
		{input: ",\tX", want: ", X"},
		{input: "X\t,", want: "X ,"},
		{input: "\t,", want: " ,"},
		{input: ",\t", want: ", "},
		{input: "X\t", want: "X\t"},
	} {
		got := normalizeStructuredFieldOWS([]string{test.input})
		if len(got) != 1 || got[0] != test.want {
			t.Fatalf("normalizeStructuredFieldOWS(%q) = %#v, want %q", test.input, got, test.want)
		}
	}
	for _, value := range []string{`"1."`, `" 1. "`, `"a1. b"`, `"escaped \\" 1."`} {
		if got := restoreRFC8941IntegralDecimals(value); got != value {
			t.Fatalf("restoreRFC8941IntegralDecimals(%q) = %q", value, got)
		}
	}
}

func TestMutationContractHTTPHelperBoundaries(t *testing.T) {
	body := []byte("abc")
	for _, test := range []struct {
		count int
		err   error
		want  error
	}{
		{count: -1, want: ErrBodyRead},
		{count: 0, want: ErrBodyRead},
		{count: len(body) - 1, want: ErrBodyRead},
		{count: len(body), want: nil},
		{count: len(body), err: errors.New("private"), want: ErrBodyRead},
		{count: len(body) + 1, want: ErrBodyRead},
	} {
		writer := &mutationContractResponseWriter{header: http.Header{"Old": []string{"value"}}, count: test.count, err: test.err}
		err := copyResponse(writer, &http.Response{StatusCode: http.StatusCreated, Header: http.Header{"New": []string{"value"}}}, body)
		if !errors.Is(err, test.want) || (test.want == nil && err != nil) {
			t.Fatalf("copyResponse(count=%d, err=%v) = %v, want %v", test.count, test.err, err, test.want)
		}
		if writer.status != http.StatusCreated || writer.header.Get("Old") != "" || writer.header.Get("New") != "value" {
			t.Fatalf("copyResponse headers/status = %#v/%d", writer.header, writer.status)
		}
	}

	for _, test := range []struct {
		header   http.Header
		length   int64
		required bool
		want     string
		wantErr  bool
	}{
		{header: make(http.Header), length: 0, required: false, want: ""},
		{header: make(http.Header), length: 0, required: true, want: "0"},
		{header: http.Header{"Content-Length": []string{"1"}}, length: 1, want: "1"},
		{header: http.Header{"Content-Length": []string{"1"}}, length: 2, wantErr: true},
		{header: http.Header{"Content-Length": []string{"1", "1"}}, length: 1, wantErr: true},
		{header: http.Header{"Content-Length": []string{"1"}, "content-length": []string{"1"}}, length: 1, wantErr: true},
	} {
		err := normalizeBufferedContentLength(test.header, test.length, test.required)
		if (err != nil) != test.wantErr {
			t.Fatalf("normalizeBufferedContentLength(%#v,%d,%t) = %v", test.header, test.length, test.required, err)
		}
		if err == nil && test.header.Get("Content-Length") != test.want {
			t.Fatalf("normalized content length = %q, want %q", test.header.Get("Content-Length"), test.want)
		}
	}

	for _, test := range []struct {
		header   http.Header
		fallback int64
		want     int64
		field    string
		wantErr  bool
	}{
		{header: make(http.Header), fallback: 0, want: 0, field: ""},
		{header: make(http.Header), fallback: 1, want: 1, field: "1"},
		{header: http.Header{"Content-Length": []string{"0"}}, want: 0, field: "0"},
		{header: http.Header{"Content-Length": []string{"01"}}, want: 1, field: "1"},
		{header: http.Header{"Content-Length": []string{""}}, wantErr: true},
		{header: http.Header{"Content-Length": []string{"/"}}, wantErr: true},
		{header: http.Header{"Content-Length": []string{":"}}, wantErr: true},
		{header: http.Header{"Content-Length": []string{"1", "2"}}, wantErr: true},
	} {
		got, err := normalizeBufferedRepresentationContentLength(test.header, test.fallback)
		if got != test.want || (err != nil) != test.wantErr {
			t.Fatalf("normalize representation(%#v,%d) = %d,%v", test.header, test.fallback, got, err)
		}
		if err == nil && test.header.Get("Content-Length") != test.field {
			t.Fatalf("representation field = %q, want %q", test.header.Get("Content-Length"), test.field)
		}
	}

	if !responseDigestBufferingConfigured(false, nil, 0) ||
		responseDigestBufferingConfigured(true, nil, 1) ||
		responseDigestBufferingConfigured(true, []DigestAlgorithm{SHA256}, 0) ||
		!responseDigestBufferingConfigured(true, []DigestAlgorithm{SHA256}, 1) {
		t.Fatal("response digest buffering truth table is incorrect")
	}

	components := []ComponentIdentifier{
		{Name: "other"},
		{Name: "content-digest", Parameters: []Parameter{{Name: "req", Value: true}}},
		{Name: "content-digest", Parameters: []Parameter{{Name: "tr", Value: true}}},
		{Name: "content-digest", Parameters: []Parameter{{Name: "key", Value: "sha-256"}}},
		{Name: "content-digest", Parameters: []Parameter{{Name: "key", Value: int64(1)}}},
		{Name: "content-digest"},
	}
	covered, whole, keyed := responseContentDigestCoverage(SignatureInput{Components: components})
	if !covered || !whole || len(keyed) != 1 {
		t.Fatalf("digest coverage = %t,%t,%#v", covered, whole, keyed)
	}
	if !componentParameterTrue(components[1], "req") || componentParameterTrue(components[1], "missing") ||
		componentParameterTrue(ComponentIdentifier{Parameters: []Parameter{{Name: "req", Value: false}}}, "req") ||
		componentParameterTrue(ComponentIdentifier{Parameters: []Parameter{{Name: "req", Value: "true"}}}, "req") {
		t.Fatal("componentParameterTrue truth table is incorrect")
	}
}

func TestMutationContractMessagePrimitiveBoundaries(t *testing.T) {
	const tokenPunctuation = "!#$%&'*+-.^_`|~"
	for value := 0; value <= 0xff; value++ {
		character := byte(value)
		want := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune(tokenPunctuation, rune(character))
		if got := validHTTPToken(string([]byte{character})); got != want {
			t.Fatalf("validHTTPToken(%x) = %t, want %t", character, got, want)
		}
	}
	if validHTTPToken("") || !validHTTPToken("Az09!~") {
		t.Fatal("validHTTPToken aggregate boundaries are incorrect")
	}

	for status := -1; status <= 600; status++ {
		wantWrite := (status < 100 || status > 199) && status != http.StatusNoContent && status != http.StatusNotModified
		if got := responseWriteBodyAllowed(status); got != wantWrite {
			t.Fatalf("responseWriteBodyAllowed(%d) = %t, want %t", status, got, wantWrite)
		}
		wantBody := status >= 200 && status != http.StatusNoContent && status != http.StatusResetContent && status != http.StatusNotModified
		if got := responseBodyAllowed(status); got != wantBody {
			t.Fatalf("responseBodyAllowed(%d) = %t, want %t", status, got, wantBody)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "https://example.com/path", nil)
	input := SignatureInput{Components: []ComponentIdentifier{{Name: "@method"}}}
	base, err := CreateSignatureBase(MessageContext{Request: request}, input)
	if err != nil {
		t.Fatal(err)
	}
	for limit := 1; limit <= len(base)+1; limit++ {
		got, createErr := CreateSignatureBase(MessageContext{Request: request, MaxSignatureBaseBytes: limit}, input)
		if limit < len(base) {
			if got != "" || !errors.Is(createErr, ErrSignatureBaseLimit) {
				t.Fatalf("base limit %d = %q,%v, want limit failure", limit, got, createErr)
			}
		} else if got != base || createErr != nil {
			t.Fatalf("base limit %d = %q,%v, want %q", limit, got, createErr, base)
		}
	}
	if _, err := CreateSignatureBase(MessageContext{Request: request, MaxSignatureBaseBytes: -1}, input); !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("negative base limit error = %v", err)
	}
	if got, err := CreateSignatureBase(MessageContext{Request: request, MaxSignatureBaseBytes: math.MaxInt}, input); got != base || err != nil {
		t.Fatalf("maximum base limit = %q,%v", got, err)
	}
	if got, err := resolveDerived(MessageContext{Request: request}, "@method", componentParameters{}, len(request.Method)); got != request.Method || err != nil {
		t.Fatalf("resolveDerived method exact bound = %q,%v", got, err)
	}
	if _, err := resolveDerived(MessageContext{Request: request}, "@method", componentParameters{}, len(request.Method)-1); !errors.Is(err, ErrSignatureBaseLimit) {
		t.Fatalf("resolveDerived method low bound error = %v", err)
	}
	queryRequest := httptest.NewRequest(http.MethodGet, "https://example.com/?x=", nil)
	queryInput := SignatureInput{Components: []ComponentIdentifier{{Name: "@query-param", Parameters: []Parameter{{Name: "name", Value: "x"}}}}}
	if _, err := CreateSignatureBase(MessageContext{Request: queryRequest}, queryInput); err != nil {
		t.Fatalf("empty query parameter signature base error = %v", err)
	}
}

func TestMutationContractTransportFieldMatrices(t *testing.T) {
	body := func() io.ReadCloser { return io.NopCloser(strings.NewReader("x")) }
	request := func(method string) *http.Request {
		return &http.Request{Method: method, URL: &url.URL{Scheme: "https", Host: "example.com"}}
	}
	for _, test := range []struct {
		name       string
		response   *http.Response
		wantValues []string
		wantErr    bool
	}{
		{name: "none", response: &http.Response{ProtoMajor: 1, ProtoMinor: 1, Body: body(), Request: request(http.MethodGet)}},
		{name: "identity", response: &http.Response{ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"identity"}, Body: body(), Request: request(http.MethodGet)}},
		{name: "chunked get", response: &http.Response{ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"chunked"}, Body: body(), Request: request(http.MethodGet)}, wantValues: []string{"chunked"}},
		{name: "chunked head", response: &http.Response{ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"chunked"}, Request: request(http.MethodHead)}, wantValues: []string{"chunked"}},
		{name: "unsupported no body", response: &http.Response{ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"gzip"}, Request: request(http.MethodGet)}},
		{name: "unsupported no request", response: &http.Response{ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"gzip"}, Body: body()}},
		{name: "unsupported http10", response: &http.Response{ProtoMajor: 1, ProtoMinor: 0, TransferEncoding: []string{"gzip"}, Body: body(), Request: request(http.MethodGet)}},
		{name: "unsupported get", response: &http.Response{ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"gzip"}, Body: body(), Request: request(http.MethodGet)}, wantErr: true},
		{name: "unsupported head", response: &http.Response{ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"gzip"}, Body: body(), Request: request(http.MethodHead)}, wantErr: true},
	} {
		values, err := writeResponseTransferEncodingFieldValues(test.response)
		if !sameFieldValues(values, test.wantValues) || (err != nil) != test.wantErr {
			t.Fatalf("%s transfer encoding = %#v,%v", test.name, values, err)
		}
	}

	for _, test := range []struct {
		name      string
		response  *http.Response
		want      string
		wantError bool
	}{
		{name: "matching", response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Length": []string{"1"}}, ContentLength: 1, Request: request(http.MethodGet)}, want: "1"},
		{name: "matching zero", response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Length": []string{"0"}}, ContentLength: 0, Request: request(http.MethodGet)}, want: "0"},
		{name: "mismatch", response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Length": []string{"1"}}, ContentLength: -1, Request: request(http.MethodGet)}, wantError: true},
		{name: "bodyless mismatch ignored", response: &http.Response{StatusCode: 204, Header: http.Header{"Content-Length": []string{"1"}}, ContentLength: -1, Request: request(http.MethodGet)}, want: "1"},
		{name: "head mismatch", response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Length": []string{"1"}}, ContentLength: -1, Request: request(http.MethodHead)}, wantError: true},
		{name: "transfer conflict", response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Length": []string{"1"}}, ContentLength: 1, TransferEncoding: []string{"chunked"}, Request: request(http.MethodGet)}, wantError: true},
	} {
		values, err := receivedResponseContentLengthFieldValues(test.response)
		if (err != nil) != test.wantError || (!test.wantError && (len(values) != 1 || values[0] != test.want)) {
			t.Fatalf("%s received content length = %#v,%v", test.name, values, err)
		}
	}

	for _, test := range []struct {
		name      string
		response  *http.Response
		want      string
		wantError bool
	}{
		{name: "positive", response: &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, ContentLength: 1, Body: body(), Request: request(http.MethodGet)}, want: "1"},
		{name: "negative", response: &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, ContentLength: -1, Body: body(), Request: request(http.MethodGet)}},
		{name: "post zero", response: &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, Body: http.NoBody, Request: request(http.MethodPost)}, want: "0"},
		{name: "put zero", response: &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, Body: http.NoBody, Request: request(http.MethodPut)}, want: "0"},
		{name: "patch zero", response: &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, Body: http.NoBody, Request: request(http.MethodPatch)}, want: "0"},
		{name: "get zero", response: &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, Body: http.NoBody, Request: request(http.MethodGet)}, want: "0"},
		{name: "identity delete zero", response: &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"identity"}, Body: http.NoBody, Request: request(http.MethodDelete)}, want: "0"},
		{name: "post bodyless status zero", response: &http.Response{StatusCode: 204, ProtoMajor: 1, ProtoMinor: 1, Body: http.NoBody, Request: request(http.MethodPost)}, want: "0"},
		{name: "put bodyless status zero", response: &http.Response{StatusCode: 204, ProtoMajor: 1, ProtoMinor: 1, Body: http.NoBody, Request: request(http.MethodPut)}, want: "0"},
		{name: "patch bodyless status zero", response: &http.Response{StatusCode: 204, ProtoMajor: 1, ProtoMinor: 1, Body: http.NoBody, Request: request(http.MethodPatch)}, want: "0"},
		{name: "identity delete bodyless status", response: &http.Response{StatusCode: 204, ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"identity"}, Body: http.NoBody, Request: request(http.MethodDelete)}, want: "0"},
		{name: "identity get bodyless status", response: &http.Response{StatusCode: 204, ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"identity"}, Body: http.NoBody, Request: request(http.MethodGet)}},
		{name: "identity head bodyless status", response: &http.Response{StatusCode: 204, ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"identity"}, Body: http.NoBody, Request: request(http.MethodHead)}},
		{name: "identity delete HTTP 1.0 bodyless status", response: &http.Response{StatusCode: 204, ProtoMajor: 1, ProtoMinor: 0, TransferEncoding: []string{"identity"}, Body: http.NoBody, Request: request(http.MethodDelete)}},
		{name: "identity delete nil body bodyless status", response: &http.Response{StatusCode: 204, ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"identity"}, Request: request(http.MethodDelete)}},
		{name: "multiple identity bodyless status", response: &http.Response{StatusCode: 204, ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"identity", "identity"}, Body: http.NoBody, Request: request(http.MethodDelete)}},
		{name: "nonidentity bodyless status", response: &http.Response{StatusCode: 204, ProtoMajor: 1, ProtoMinor: 1, TransferEncoding: []string{"gzip"}, Body: http.NoBody, Request: request(http.MethodDelete)}},
		{name: "head zero", response: &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, Body: http.NoBody, Request: request(http.MethodHead)}, want: "0"},
		{name: "body probe", response: &http.Response{StatusCode: 200, ProtoMajor: 1, ProtoMinor: 1, Body: body(), Request: request(http.MethodGet)}, wantError: true},
	} {
		values, err := writeResponseContentLengthFieldValues(test.response)
		if (err != nil) != test.wantError {
			t.Fatalf("%s write content length error = %v", test.name, err)
		}
		if !test.wantError {
			if test.want == "" && len(values) != 0 || test.want != "" && (len(values) != 1 || values[0] != test.want) {
				t.Fatalf("%s write content length = %#v, want %q", test.name, values, test.want)
			}
		}
	}

	for _, test := range []struct {
		received []string
		recvErr  error
		written  []string
		writeErr error
		wantErr  bool
	}{
		{received: []string{"x"}, written: []string{"x"}},
		{received: []string{"x"}, written: []string{"y"}, wantErr: true},
		{received: []string{"x"}, recvErr: errors.New("x"), written: []string{"x"}, wantErr: true},
		{received: []string{"x"}, written: []string{"x"}, writeErr: errors.New("x"), wantErr: true},
	} {
		values, err := matchingResponseFieldValues(test.received, test.recvErr, test.written, test.writeErr)
		if (err != nil) != test.wantErr || !test.wantErr && !sameFieldValues(values, test.received) {
			t.Fatalf("matchingResponseFieldValues = %#v,%v", values, err)
		}
	}

	for _, test := range []struct {
		request *http.Request
		name    string
		want    bool
	}{
		{request: &http.Request{RequestURI: "", Close: true, Header: make(http.Header)}, name: "connection", want: true},
		{request: &http.Request{RequestURI: "", Close: false, Header: make(http.Header)}, name: "connection", want: false},
		{request: &http.Request{RequestURI: "/", Close: true, Header: make(http.Header)}, name: "connection", want: false},
		{request: &http.Request{RequestURI: "", Header: make(http.Header)}, name: "user-agent", want: true},
		{request: &http.Request{RequestURI: "/", Header: make(http.Header)}, name: "user-agent", want: false},
	} {
		_, handled, _ := requestTransportFieldValues(test.request, test.name)
		if handled != test.want {
			t.Fatalf("requestTransportFieldValues(%q,%q) handled=%t, want %t", test.request.RequestURI, test.name, handled, test.want)
		}
	}
	for _, test := range []struct {
		response *http.Response
		want     bool
	}{
		{response: &http.Response{ProtoMajor: 2}},
		{response: &http.Response{ProtoMajor: 1, ProtoMinor: 0}},
		{response: &http.Response{ProtoMajor: 1, ProtoMinor: 1, ContentLength: 0}, want: true},
		{response: &http.Response{ProtoMajor: 1, ProtoMinor: 1, ContentLength: -1, TransferEncoding: []string{"chunked"}}, want: true},
		{response: &http.Response{ProtoMajor: 1, ProtoMinor: 1, ContentLength: -1, Request: request(http.MethodHead)}, want: true},
		{response: &http.Response{ProtoMajor: 1, ProtoMinor: 1, ContentLength: -1, StatusCode: http.StatusNoContent}, want: true},
		{response: &http.Response{ProtoMajor: 1, ProtoMinor: 1, ContentLength: -1, StatusCode: http.StatusOK}},
	} {
		if got := receivedResponseCloseFieldIsExplicit(test.response); got != test.want {
			t.Fatalf("receivedResponseCloseFieldIsExplicit(%#v) = %t, want %t", test.response, got, test.want)
		}
	}
}

func TestMutationContractSizingHelperBoundaries(t *testing.T) {
	if !structuredItemFits("@method", nil, len(`"@method"`)) || structuredItemFits("@method", nil, len(`"@method"`)-1) {
		t.Fatal("structuredItemFits exact boundary is incorrect")
	}
	for _, test := range []struct {
		parameter Parameter
		want      int
	}{
		{Parameter{Name: "x", Value: true}, len(`;x`)},
		{Parameter{Name: "x", Value: false}, len(`;x=?0`)},
		{Parameter{Name: "x", Value: "a"}, len(`;x="a"`)},
		{Parameter{Name: "x", Value: int64(-10)}, len(`;x=-10`)},
		{Parameter{Name: "x", Value: float64(1)}, len(`;x=1.0`)},
		{Parameter{Name: "x", Value: []byte("x")}, len(`;x=:eA==:`)},
		{Parameter{Name: "x", Value: SFToken("t")}, len(`;x=t`)},
		{Parameter{Name: "x", Value: struct{}{}}, len(`;x=`)},
	} {
		if got := parameterUpperBound(test.parameter); got != test.want {
			t.Fatalf("parameterUpperBound(%#v) = %d, want %d", test.parameter, got, test.want)
		}
	}
	if got := parameterUpperBound(Parameter{Name: "x", Value: float64(1e15)}); got != len(`;x=`) {
		t.Fatalf("out-of-range decimal bound = %d", got)
	}

	for value := 0; value <= 0xff; value++ {
		character := byte(value)
		want := 1
		if character == '"' || character == '\\' {
			want = 2
		}
		if got := escapedStringLength(string([]byte{character})); got != want {
			t.Fatalf("escapedStringLength(%x) = %d, want %d", character, got, want)
		}
	}

	request := &http.Request{URL: &url.URL{Host: "host", Path: "/x", RawPath: "/%78", RawQuery: "a=b"}, Host: "", TLS: nil}
	for _, test := range []struct {
		name   string
		budget int
		want   bool
	}{
		{"@scheme", 21, true}, {"@scheme", 20, false},
		{"@authority", 21, true}, {"@authority", 20, false},
		{"@request-target", 21, true}, {"@request-target", 20, false},
		{"@query-param", 63, true}, {"@query-param", 62, false},
	} {
		if got := derivedSourceFits(request, nil, test.name, test.budget); got != test.want {
			t.Fatalf("derivedSourceFits fallback %s/%d = %t, want %t", test.name, test.budget, got, test.want)
		}
	}
	opaque := &http.Request{URL: &url.URL{Scheme: "https", Host: "h", Opaque: "//h/opaque", RawQuery: "x=y"}}
	if !derivedSourceFits(opaque, nil, "@request-target", 26) || derivedSourceFits(opaque, nil, "@request-target", 25) {
		t.Fatal("opaque target bound is incorrect")
	}
	small := &http.Request{URL: &url.URL{Host: "host"}, RequestURI: "/"}
	if !derivedSourceFits(small, nil, "@scheme", 4) || !derivedSourceFits(small, nil, "@authority", 4) ||
		derivedSourceFits(small, nil, "@scheme", 3) || derivedSourceFits(small, nil, "@authority", 3) ||
		derivedSourceFits(small, nil, "@path", 0) {
		t.Fatal("derived source fallback exact boundaries are incorrect")
	}
	urlAuthority := &http.Request{URL: &url.URL{Scheme: "h", Host: "url-host"}, RequestURI: "/"}
	if !derivedSourceFits(urlAuthority, nil, "@target-uri", len("h://url-host/")) ||
		derivedSourceFits(urlAuthority, nil, "@target-uri", len("h://url-host/")-1) {
		t.Fatal("URL authority fallback target bound is incorrect")
	}
}

func TestMutationContractReplayAndVerificationBoundaries(t *testing.T) {
	entry := &replayEntry{index: 0}
	heap := replayExpiryHeap{entry}
	if got := (&heap).Pop(); got != entry || entry.index != -1 || len(heap) != 0 {
		t.Fatalf("replay heap Pop() = %#v, index=%d, len=%d", got, entry.index, len(heap))
	}

	now := time.Unix(1_700_000_000, 0)
	base := VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256}, RequiredComponents: []ComponentIdentifier{{Name: "@method"}},
		Created: ParameterRequired, Expires: ParameterForbidden, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden, MaxAge: time.Minute, ClockSkew: time.Second,
		ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) { return ResolvedKey{}, nil }),
	}
	replay := replayStoreFunc(func(context.Context, ReplayRecord) error { return nil })
	for _, test := range []struct {
		policy  ParameterPolicy
		replay  ReplayStore
		timeout time.Duration
		valid   bool
	}{
		{ParameterForbidden, nil, 0, true},
		{ParameterForbidden, replay, 0, false},
		{ParameterForbidden, nil, time.Second, false},
		{ParameterRequired, nil, 0, false},
		{ParameterRequired, replay, 0, false},
		{ParameterRequired, nil, time.Second, false},
		{ParameterRequired, replay, -1, false},
		{ParameterRequired, replay, time.Second, true},
	} {
		config := base
		config.Nonce, config.Replay, config.ReplayTimeout = test.policy, test.replay, test.timeout
		_, err := NewVerificationProfile(config)
		if (err == nil) != test.valid {
			t.Fatalf("nonce profile (%d,%t,%s) error = %v, valid=%t", test.policy, test.replay != nil, test.timeout, err, test.valid)
		}
	}

	profile, err := NewVerificationProfile(base)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	notBefore := now.Add(-10 * time.Second)
	resolved := ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: notBefore, NotAfter: now.Add(10 * time.Second), FreshUntil: now.Add(time.Minute)}
	for _, created := range []time.Time{notBefore.Add(-time.Second), notBefore, now} {
		if err := profile.validateKey(resolved, HMACSHA256, true, created); err != nil {
			t.Fatalf("validateKey(created=%s) error = %v", created, err)
		}
	}
}

func TestMutationContractBodyHelperBoundaries(t *testing.T) {
	exactBody := &mutationContractBody{Reader: strings.NewReader(strings.Repeat("x", 32*1024))}
	exact, err := readBoundedAndClose(context.Background(), exactBody, 32*1024)
	if err != nil || len(exact) != 32*1024 || exactBody.closed != 1 {
		t.Fatalf("exact read buffer boundary = %d bytes, closed %d, error %v", len(exact), exactBody.closed, err)
	}

	closed := &mutationContractBody{Reader: strings.NewReader("")}
	closeResponseBody(&http.Response{Body: closed})
	closeResponseBody(&http.Response{})
	closeResponseBody(nil)
	if closed.closed != 1 {
		t.Fatalf("closeResponseBody close count = %d", closed.closed)
	}
	completion := make(chan struct{})
	closeFailure := &trailerSigningBody{
		body: &mutationContractFailingCloseBody{Reader: strings.NewReader("")}, ctx: context.Background(), maxBytes: 1,
		done: completion, eofObserved: true,
	}
	if err := closeFailure.Close(); !errors.Is(err, ErrBodyRead) {
		t.Fatalf("trailer signing close failure = %v", err)
	}
	select {
	case <-completion:
		if !errors.Is(closeFailure.completionError(), ErrBodyRead) {
			t.Fatalf("close completion error = %v", closeFailure.completionError())
		}
	default:
		t.Fatal("close failure did not complete terminal state")
	}

	for _, header := range []http.Header{
		{"Content-Digest": []string{"value"}},
		{"Signature-Input": []string{"value"}},
		{"Signature": []string{"value"}},
	} {
		if !hasSignatureOrDigestFields(header) {
			t.Fatalf("hasSignatureOrDigestFields(%#v) = false", header)
		}
	}
	if hasSignatureOrDigestFields(http.Header{"Signature": nil}) || hasSignatureOrDigestFields(nil) {
		t.Fatal("empty protected fields reported present")
	}

	validTrailer, err := normalizeTrailerFields(http.Header{"x-final": []string{"one"}})
	if err != nil || validTrailer.Get("X-Final") != "one" {
		t.Fatalf("normalizeTrailerFields(valid) = %#v,%v", validTrailer, err)
	}
	for _, header := range []http.Header{
		{"": nil}, {"Bad Name": nil}, {"Content-Length": nil}, {"X": nil, "x": nil},
	} {
		if _, err := normalizeTrailerFields(header); err == nil {
			t.Fatalf("normalizeTrailerFields(%#v) succeeded", header)
		}
	}
	names := applicationTrailerNames(http.Header{"Content-Digest": nil, "Signature-Input": nil, "Signature": nil, "x-final": nil})
	if len(names) != 1 {
		t.Fatalf("applicationTrailerNames = %#v", names)
	}
	if !sameTrailerNames(names, map[string]struct{}{"X-Final": {}}) || sameTrailerNames(names, nil) ||
		sameTrailerNames(names, map[string]struct{}{"X-Other": {}}) {
		t.Fatal("sameTrailerNames truth table is incorrect")
	}

	for _, status := range []int{199, 200, 204, 299, 300, 304} {
		writer := &trailerResponseWriter{ResponseWriter: httptest.NewRecorder(), request: httptest.NewRequest(http.MethodGet, "https://example.com", nil), maxBytes: 1}
		writer.WriteHeader(status)
		wantFailure := status >= 200 && !responseBodyAllowed(status)
		if (writer.failure != nil) != wantFailure {
			t.Fatalf("trailer WriteHeader(%d) failure = %v, wantFailure=%t", status, writer.failure, wantFailure)
		}
	}

	for _, count := range []int{-1, 0, 1, 2} {
		underlying := &mutationContractResponseWriter{header: make(http.Header), count: count}
		writer := &trailerResponseWriter{ResponseWriter: underlying, request: httptest.NewRequest(http.MethodGet, "https://example.com", nil), maxBytes: 1, status: http.StatusOK}
		got, err := writer.Write([]byte("x"))
		valid := count == 1
		if valid && (err != nil || got != 1) {
			t.Fatalf("trailer valid Write count %d = %d,%v", count, got, err)
		}
		if !valid && (!errors.Is(err, ErrBodyRead) || (count < 0 || count > 1) && got != 0) {
			t.Fatalf("trailer Write count %d = %d,%v", count, got, err)
		}
	}
	underlyingError := &mutationContractResponseWriter{header: make(http.Header), count: 1, err: errors.New("private")}
	errorWriter := &trailerResponseWriter{ResponseWriter: underlyingError, request: httptest.NewRequest(http.MethodGet, "https://example.com", nil), maxBytes: 1, status: http.StatusOK}
	if got, err := errorWriter.Write([]byte("x")); got != 1 || !errors.Is(err, ErrBodyRead) {
		t.Fatalf("trailer Write error = %d,%v", got, err)
	}

	for _, request := range []*http.Request{
		{Proto: "HTTP/1.0", ProtoMajor: 1, ProtoMinor: 0},
		{Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1},
		{Proto: "HTTP/2.0", ProtoMajor: 2, ProtoMinor: 0},
	} {
		response := &http.Response{}
		configureTrailerSigningResponse(response, request)
		if request.ProtoMajor == 1 {
			if response.Proto != request.Proto || response.ProtoMajor != 1 || response.ProtoMinor != request.ProtoMinor ||
				response.ContentLength != -1 || len(response.TransferEncoding) != 1 || response.TransferEncoding[0] != "chunked" {
				t.Fatalf("HTTP/1 trailer response = %#v", response)
			}
		} else if response.Proto != "" || response.ContentLength != 0 || len(response.TransferEncoding) != 0 {
			t.Fatalf("HTTP/2 trailer response was mutated: %#v", response)
		}
	}

	for _, declaration := range []string{"", "Bad Name", "Content-Length", "X-Final"} {
		header := make(http.Header)
		header["Trailer"] = []string{declaration}
		_, err := responseTrailerNames(header)
		wantErr := declaration != "X-Final"
		if (err != nil) != wantErr {
			t.Fatalf("responseTrailerNames(%q) error = %v, wantErr=%t", declaration, err, wantErr)
		}
	}

	request := httptest.NewRequest(http.MethodPost, "https://example.com", strings.NewReader("x"))
	request.Trailer = http.Header{"X-Final": []string{"value"}}
	var captured *http.Request
	buffered := &BufferedContentDigestRoundTripper{
		transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			captured = request
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		}),
		algorithms: []DigestAlgorithm{SHA256}, maxBytes: 1,
	}
	if _, err := buffered.RoundTrip(request); err != nil {
		t.Fatalf("buffered digest trailer RoundTrip() error = %v", err)
	}
	if captured == nil || captured.ContentLength != -1 || len(captured.TransferEncoding) != 1 || captured.TransferEncoding[0] != "chunked" {
		t.Fatalf("buffered trailer transport state = %#v", captured)
	}

	for _, test := range []struct {
		header       http.Header
		uncompressed bool
	}{
		{header: http.Header{"X": []string{"one"}, "x": []string{"two"}}},
		{header: make(http.Header), uncompressed: true},
	} {
		responseBody := &mutationContractBody{Reader: strings.NewReader("x")}
		response := &http.Response{StatusCode: http.StatusOK, Header: test.header, Body: responseBody, Request: httptest.NewRequest(http.MethodGet, "https://example.com", nil), Uncompressed: test.uncompressed}
		transport := &BufferedTrailerVerifyingRoundTripper{transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { return response, nil }), maxBytes: 1}
		if got, err := transport.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com", nil)); got != nil || err == nil {
			t.Fatalf("buffered trailer boundary result = %#v,%v", got, err)
		}
		if responseBody.closed != 1 {
			t.Fatalf("buffered trailer boundary close count = %d", responseBody.closed)
		}
	}
}

type mutationContractResponseWriter struct {
	header http.Header
	status int
	count  int
	err    error
}

func (writer *mutationContractResponseWriter) Header() http.Header { return writer.header }
func (writer *mutationContractResponseWriter) Write([]byte) (int, error) {
	return writer.count, writer.err
}
func (writer *mutationContractResponseWriter) WriteHeader(status int) { writer.status = status }

type mutationContractBody struct {
	io.Reader
	closed int
}

type mutationContractFailingCloseBody struct{ io.Reader }

type mutationContractReadFailureBody struct{}

func (*mutationContractReadFailureBody) Read([]byte) (int, error) { return 0, errors.New("private") }
func (*mutationContractReadFailureBody) Close() error             { return nil }

func (*mutationContractFailingCloseBody) Close() error { return errors.New("private") }

func (body *mutationContractBody) Close() error {
	body.closed++
	return nil
}

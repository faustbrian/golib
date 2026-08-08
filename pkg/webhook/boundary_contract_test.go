package webhook

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewDelivererRejectsEachUnsafeConfigurationBoundary(t *testing.T) {
	t.Parallel()

	mutations := map[string]func(*DeliveryConfig){
		"client":               func(config *DeliveryConfig) { config.Client = nil },
		"signer":               func(config *DeliveryConfig) { config.Signer = nil },
		"policy":               func(config *DeliveryConfig) { config.EndpointPolicy = nil },
		"ID generator":         func(config *DeliveryConfig) { config.IDGenerator = nil },
		"request zero":         func(config *DeliveryConfig) { config.MaxRequestBytes = 0 },
		"request negative":     func(config *DeliveryConfig) { config.MaxRequestBytes = -1 },
		"request unbounded":    func(config *DeliveryConfig) { config.MaxRequestBytes = math.MaxInt64 },
		"response zero":        func(config *DeliveryConfig) { config.MaxResponseBytes = 0 },
		"response negative":    func(config *DeliveryConfig) { config.MaxResponseBytes = -1 },
		"response unbounded":   func(config *DeliveryConfig) { config.MaxResponseBytes = math.MaxInt64 },
		"fan-out zero":         func(config *DeliveryConfig) { config.MaxFanOut = 0 },
		"fan-out negative":     func(config *DeliveryConfig) { config.MaxFanOut = -1 },
		"signature count zero": func(config *DeliveryConfig) { config.HeaderLimits.MaxSignatures = 0 },
		"signature bytes zero": func(config *DeliveryConfig) { config.HeaderLimits.MaxBytes = 0 },
		"attempts zero":        func(config *DeliveryConfig) { config.Retry.MaxAttempts = 0 },
		"attempts negative":    func(config *DeliveryConfig) { config.Retry.MaxAttempts = -1 },
		"base delay negative":  func(config *DeliveryConfig) { config.Retry.BaseDelay = -1 },
		"maximum below base":   func(config *DeliveryConfig) { config.Retry.MaxDelay = config.Retry.BaseDelay - 1 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := validDeliveryConfig(t)
			mutate(&config)
			if _, err := NewDeliverer(config); !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("NewDeliverer() error = %v, want ErrInvalidConfiguration", err)
			}
		})
	}
}

func TestRetryPolicyPreservesEveryDelayBoundary(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	policy := RetryPolicy{BaseDelay: time.Second, MaxDelay: 5 * time.Second}
	tests := map[string]struct {
		attempt    int
		retryAfter string
		want       time.Duration
	}{
		"zero seconds":        {attempt: 1, retryAfter: "0", want: 0},
		"maximum seconds":     {attempt: 1, retryAfter: "5", want: 5 * time.Second},
		"seconds above limit": {attempt: 1, retryAfter: "6", want: 5 * time.Second},
		"negative seconds":    {attempt: 1, retryAfter: "-1", want: time.Second},
		"date at now":         {attempt: 1, retryAfter: now.UTC().Format(http.TimeFormat), want: time.Second},
		"first fallback":      {attempt: 1, retryAfter: "invalid", want: time.Second},
		"second fallback":     {attempt: 2, retryAfter: "invalid", want: 2 * time.Second},
		"overflow fallback":   {attempt: 4, retryAfter: "invalid", want: 5 * time.Second},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := policy.Delay(test.attempt, now, test.retryAfter); got != test.want {
				t.Fatalf("Delay() = %v, want %v", got, test.want)
			}
		})
	}
	subsecond := RetryPolicy{MaxDelay: 500 * time.Millisecond}
	if got := subsecond.Delay(1, now, "1"); got != subsecond.MaxDelay {
		t.Fatalf("Delay() subsecond maximum = %v, want %v", got, subsecond.MaxDelay)
	}
}

func TestDeliverPreservesRequestStatusAndResponseBoundaries(t *testing.T) {
	t.Parallel()

	for name, status := range map[string]int{
		"below success": http.StatusOK - 1,
		"success start": http.StatusOK,
		"success end":   299,
		"above success": 300,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			deliverer := deliveryFixture(t, &responseDoer{response: response(status, http.NoBody)}, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
			result, err := deliverer.DeliverOnce(context.Background(), validDelivery(t))
			if status >= http.StatusOK && status < 300 {
				if err != nil || result.Attempts[0].Classification != FailureNone {
					t.Fatalf("DeliverOnce() = %#v, %v", result, err)
				}
				return
			}
			if !errors.Is(err, ErrDeliveryFailed) || result.Attempts[0].Classification != FailureTerminal {
				t.Fatalf("DeliverOnce() = %#v, %v", result, err)
			}
		})
	}

	deliverer := deliveryFixture(t, &responseDoer{response: response(http.StatusNoContent, http.NoBody)}, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	delivery := validDelivery(t)
	delivery.Body = bytes.Repeat([]byte{'x'}, int(deliverer.maxRequestBytes))
	if _, err := deliverer.DeliverOnce(context.Background(), delivery); err != nil {
		t.Fatalf("DeliverOnce() exact request boundary error = %v", err)
	}
	delivery.Body = append(delivery.Body, 'x')
	if _, err := deliverer.DeliverOnce(context.Background(), delivery); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("DeliverOnce() oversized request error = %v", err)
	}

	if body, err := readResponse(response(http.StatusOK, io.NopCloser(strings.NewReader("x"))), 1); err != nil || string(body) != "x" {
		t.Fatalf("readResponse() exact boundary = %q, %v", body, err)
	}
	if _, err := readResponse(response(http.StatusOK, io.NopCloser(strings.NewReader("xx"))), 1); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("readResponse() oversized error = %v", err)
	}
}

func TestDeliveryIdentifiersAndFanOutBoundariesFailIndependently(t *testing.T) {
	t.Parallel()

	for name, generator := range map[string]func() (string, error){
		"delivery error": func() (string, error) { return "", errors.New("entropy") },
		"delivery empty": func() (string, error) { return "", nil },
	} {
		t.Run(name, func(t *testing.T) {
			deliverer := deliveryFixture(t, &responseDoer{response: response(http.StatusNoContent, http.NoBody)}, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
			deliverer.id = generator
			if _, err := deliverer.DeliverOnce(context.Background(), validDelivery(t)); !errors.Is(err, ErrDeliveryFailed) {
				t.Fatalf("DeliverOnce() error = %v", err)
			}
		})
	}
	for name, attemptResult := range map[string]struct {
		id  string
		err error
	}{
		"attempt error": {err: errors.New("entropy")},
		"attempt empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			deliverer := deliveryFixture(t, &responseDoer{response: response(http.StatusNoContent, http.NoBody)}, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
			calls := 0
			deliverer.id = func() (string, error) {
				calls++
				if calls == 1 {
					return "delivery", nil
				}

				return attemptResult.id, attemptResult.err
			}
			if _, err := deliverer.DeliverOnce(context.Background(), validDelivery(t)); !errors.Is(err, ErrDeliveryFailed) {
				t.Fatalf("DeliverOnce() error = %v", err)
			}
		})
	}
	deliverer := deliveryFixture(t, &responseDoer{response: response(http.StatusNoContent, http.NoBody)}, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	deliverer.retry.MaxAttempts = 0
	if _, err := deliverer.Deliver(context.Background(), validDelivery(t)); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("Deliver() zero attempt safeguard error = %v", err)
	}

	deliverer = deliveryFixture(t, &responseDoer{response: response(http.StatusNoContent, http.NoBody)}, time.Unix(1_700_000_000, 0), func(context.Context, time.Duration) error { return nil })
	deliverer.maxFanOut = 1
	for _, workers := range []int{-1, 0} {
		if _, err := deliverer.FanOut(context.Background(), []DeliveryRequest{validDelivery(t)}, workers); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("FanOut(workers=%d) error = %v", workers, err)
		}
	}
	if results, err := deliverer.FanOut(context.Background(), []DeliveryRequest{validDelivery(t)}, 1); err != nil || len(results) != 1 || results[0].Err != nil {
		t.Fatalf("FanOut() exact boundaries = %#v, %v", results, err)
	}
}

func TestDeliveryWireRejectsEachUnsafeFieldIndependently(t *testing.T) {
	t.Parallel()

	valid := validDelivery(t)
	for name, mutate := range map[string]func(*DeliveryRequest){
		"nil endpoint": func(delivery *DeliveryRequest) { delivery.Endpoint = nil },
		"relative":     func(delivery *DeliveryRequest) { delivery.Endpoint = &url.URL{Path: "/hook"} },
		"userinfo":     func(delivery *DeliveryRequest) { delivery.Endpoint.User = url.User("user") },
		"fragment":     func(delivery *DeliveryRequest) { delivery.Endpoint.Fragment = "fragment" },
		"event ID":     func(delivery *DeliveryRequest) { delivery.EventID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			delivery := valid
			endpoint := *valid.Endpoint
			delivery.Endpoint = &endpoint
			mutate(&delivery)
			if _, err := MarshalDeliveryRequest(delivery, 1024); !errors.Is(err, ErrDeliveryEncoding) {
				t.Fatalf("MarshalDeliveryRequest() error = %v", err)
			}
		})
	}
	if _, err := MarshalDeliveryRequest(valid, 0); !errors.Is(err, ErrDeliveryEncoding) {
		t.Fatalf("MarshalDeliveryRequest(0) error = %v", err)
	} else if !strings.Contains(err.Error(), "positive output limit") {
		t.Fatalf("MarshalDeliveryRequest(0) diagnostic = %v", err)
	}
	encoded, err := MarshalDeliveryRequest(valid, 1024)
	if err != nil {
		t.Fatalf("MarshalDeliveryRequest() error = %v", err)
	}
	if _, err := MarshalDeliveryRequest(valid, len(encoded)); err != nil {
		t.Fatalf("MarshalDeliveryRequest() exact limit error = %v", err)
	}
	if _, err := UnmarshalDeliveryRequest(encoded, len(encoded)); err != nil {
		t.Fatalf("UnmarshalDeliveryRequest() exact limit error = %v", err)
	}
	if _, err := UnmarshalDeliveryRequest(encoded, 0); !errors.Is(err, ErrDeliveryEncoding) {
		t.Fatalf("UnmarshalDeliveryRequest(0) error = %v", err)
	} else if !strings.Contains(err.Error(), "positive input limit") {
		t.Fatalf("UnmarshalDeliveryRequest(0) diagnostic = %v", err)
	}

	wires := map[string]string{
		"version":  `{"version":"v2","endpoint":"https://example.com","event_id":"event"}`,
		"event ID": `{"version":"v1","endpoint":"https://example.com"}`,
		"relative": `{"version":"v1","endpoint":"/hook","event_id":"event"}`,
		"userinfo": `{"version":"v1","endpoint":"https://user@example.com","event_id":"event"}`,
		"fragment": `{"version":"v1","endpoint":"https://example.com/#fragment","event_id":"event"}`,
	}
	for name, wire := range wires {
		t.Run("unmarshal "+name, func(t *testing.T) {
			if _, err := UnmarshalDeliveryRequest([]byte(wire), 1024); !errors.Is(err, ErrDeliveryEncoding) {
				t.Fatalf("UnmarshalDeliveryRequest() error = %v", err)
			}
		})
	}
}

func TestHTTPHeaderLimitsAndFieldsPreserveExactBoundaries(t *testing.T) {
	t.Parallel()

	valid := Signature{Version: "v1", Algorithm: SHA256, KeyID: "key", Timestamp: time.Unix(1_700_000_000, 0), Nonce: "nonce", Value: "ubEWwKQcbLvNqyq4NDMHO4k5KX8euM7Z9nnpaZC2mD8"}
	if err := SetSignatureHeaders(nil, []Signature{valid}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("SetSignatureHeaders(nil) error = %v", err)
	}
	if err := SetSignatureHeaders(make(http.Header), nil); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("SetSignatureHeaders(empty) error = %v", err)
	}
	header := make(http.Header)
	if err := SetSignatureHeaders(header, []Signature{valid}); err != nil {
		t.Fatalf("SetSignatureHeaders() error = %v", err)
	}
	valueBytes := len(header.Get(SignatureHeader))
	if _, err := ParseSignatureHeaders(header, HeaderLimits{MaxSignatures: 1, MaxBytes: valueBytes}); err != nil {
		t.Fatalf("ParseSignatureHeaders() exact limits error = %v", err)
	}
	for name, limits := range map[string]HeaderLimits{
		"count zero":  {MaxSignatures: 0, MaxBytes: valueBytes},
		"count low":   {MaxSignatures: 0, MaxBytes: valueBytes},
		"bytes zero":  {MaxSignatures: 1, MaxBytes: 0},
		"bytes short": {MaxSignatures: 1, MaxBytes: valueBytes - 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseSignatureHeaders(header, limits); err == nil {
				t.Fatal("ParseSignatureHeaders() unexpectedly accepted invalid limits")
			}
		})
	}
	if _, err := ParseSignatureHeaders(header, HeaderLimits{MaxSignatures: 0, MaxBytes: valueBytes}); err == nil || !strings.Contains(err.Error(), "signature count") {
		t.Fatalf("ParseSignatureHeaders() count diagnostic = %v", err)
	}
	if _, err := ParseSignatureHeaders(header, HeaderLimits{MaxSignatures: 1, MaxBytes: 0}); err == nil || !strings.Contains(err.Error(), "header limits") {
		t.Fatalf("ParseSignatureHeaders() byte diagnostic = %v", err)
	}

	for name, mutate := range map[string]func(*Signature){
		"version":   func(signature *Signature) { signature.Version = "v2" },
		"key ID":    func(signature *Signature) { signature.KeyID = "" },
		"zero time": func(signature *Signature) { signature.Timestamp = time.Time{} },
		"past time": func(signature *Signature) { signature.Timestamp = time.Unix(-1, 0) },
		"nonce":     func(signature *Signature) { signature.Nonce = "" },
		"algorithm": func(signature *Signature) { signature.Algorithm = "md5" },
		"encoding":  func(signature *Signature) { signature.Value = "%" },
		"length":    func(signature *Signature) { signature.Value = "YQ" },
	} {
		t.Run("format "+name, func(t *testing.T) {
			signature := valid
			mutate(&signature)
			if _, err := formatSignatureHeader(signature); !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("formatSignatureHeader() error = %v", err)
			}
		})
	}
	epochSignature := valid
	epochSignature.Timestamp = time.Unix(0, 0)
	if _, err := formatSignatureHeader(epochSignature); err != nil {
		t.Fatalf("formatSignatureHeader() Unix epoch error = %v", err)
	}

	if value, ok := strictField("name=value", "name"); !ok || value != "value" {
		t.Fatalf("strictField() = %q, %v", value, ok)
	}
	if _, ok := strictField("other=value", "name"); ok {
		t.Fatal("strictField() accepted the wrong name")
	}
	if _, ok := strictField("name=", "name"); ok {
		t.Fatal("strictField() accepted an empty value")
	}

	validHeader := "v1;algorithm=sha256;keyid=a2V5;timestamp=1700000000;nonce=bm9uY2U;signature=ubEWwKQcbLvNqyq4NDMHO4k5KX8euM7Z9nnpaZC2mD8"
	malformed := map[string]string{
		"key encoding":        strings.Replace(validHeader, "keyid=a2V5", "keyid=%", 1),
		"key UTF-8":           strings.Replace(validHeader, "keyid=a2V5", "keyid=_w", 1),
		"timestamp encoding":  strings.Replace(validHeader, "timestamp=1700000000", "timestamp=x", 1),
		"timestamp negative":  strings.Replace(validHeader, "timestamp=1700000000", "timestamp=-1", 1),
		"timestamp canonical": strings.Replace(validHeader, "timestamp=1700000000", "timestamp=01700000000", 1),
		"nonce encoding":      strings.Replace(validHeader, "nonce=bm9uY2U", "nonce=%", 1),
		"nonce UTF-8":         strings.Replace(validHeader, "nonce=bm9uY2U", "nonce=_w", 1),
		"signature encoding":  strings.Replace(validHeader, "signature=ubEWwKQcbLvNqyq4NDMHO4k5KX8euM7Z9nnpaZC2mD8", "signature=%", 1),
		"signature length":    strings.Replace(validHeader, "signature=ubEWwKQcbLvNqyq4NDMHO4k5KX8euM7Z9nnpaZC2mD8", "signature=YQ", 1),
	}
	for name, value := range malformed {
		t.Run("parse "+name, func(t *testing.T) {
			candidate := http.Header{SignatureHeader: {value}}
			if _, err := ParseSignatureHeaders(candidate, HeaderLimits{MaxSignatures: 1, MaxBytes: 512}); !errors.Is(err, ErrMalformedSignatureHeader) {
				t.Fatalf("ParseSignatureHeaders() error = %v", err)
			}
		})
	}
	zeroTimestamp := strings.Replace(validHeader, "timestamp=1700000000", "timestamp=0", 1)
	if _, err := ParseSignatureHeaders(http.Header{SignatureHeader: {zeroTimestamp}}, HeaderLimits{MaxSignatures: 1, MaxBytes: 512}); err != nil {
		t.Fatalf("ParseSignatureHeaders() Unix epoch error = %v", err)
	}
}

func TestFixedHeadersAndEventIDsPreserveExactBoundaries(t *testing.T) {
	t.Parallel()

	exact := strings.Repeat("x", maxFixedSignedHeaderBytes)
	if value, err := fixedSignedHeader(http.Header{"X-Test": {exact}}, "X-Test"); err != nil || value != exact {
		t.Fatalf("fixedSignedHeader() exact boundary = %q, %v", value, err)
	}
	invalidHeaders := []http.Header{
		{"X-Test": {"one", "two"}},
		{"X-Test": {exact + "x"}},
		{"X-Test": {string([]byte{0xff})}},
		{"X-Test": {"line\nbreak"}},
	}
	for _, header := range invalidHeaders {
		if _, err := fixedSignedHeader(header, "X-Test"); !errors.Is(err, ErrMalformedSignedHeader) {
			t.Fatalf("fixedSignedHeader() error = %v", err)
		}
	}

	request := &http.Request{Header: http.Header{"X-Event": {"x"}}}
	if value, err := HeaderEventID("X-Event", 1)(request, nil); err != nil || value != "x" {
		t.Fatalf("HeaderEventID() exact boundary = %q, %v", value, err)
	}
	for name, extractor := range map[string]EventIDExtractor{
		"nil request": HeaderEventID("X-Event", 1),
		"empty name":  HeaderEventID("", 1),
		"zero limit":  HeaderEventID("X-Event", 0),
	} {
		t.Run(name, func(t *testing.T) {
			candidate := request
			if name == "nil request" {
				candidate = nil
			}
			if _, err := extractor(candidate, nil); !errors.Is(err, ErrMissingEventID) {
				t.Fatalf("HeaderEventID() error = %v", err)
			}
		})
	}
	if _, err := HeaderEventID("X-Event", 0)(request, nil); err == nil || !strings.Contains(err.Error(), "positive header limit") {
		t.Fatalf("HeaderEventID() limit diagnostic = %v", err)
	}
	if _, err := HeaderEventID("X-Event", 1)(&http.Request{Header: http.Header{"X-Event": {"xx"}}}, nil); !errors.Is(err, ErrMissingEventID) {
		t.Fatalf("HeaderEventID() oversized error = %v", err)
	}
}

func TestSignerVerifierAndNonceValidationBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	if _, err := NewSigner(SignerConfig{Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: []byte("one")}, {ID: "key", Secret: []byte("two")}}}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("NewSigner() duplicate error = %v", err)
	}
	if !validNonce(strings.Repeat("x", maxNonceBytes)) {
		t.Fatal("validNonce() rejected the exact byte limit")
	}
	for _, nonce := range []string{"", strings.Repeat("x", maxNonceBytes+1), string([]byte{0xff})} {
		if validNonce(nonce) {
			t.Fatalf("validNonce(%q) = true", nonce)
		}
	}

	key := []byte("secret")
	message := Message{Timestamp: now, Method: http.MethodPost, Path: "/", Host: "example.com"}
	signer, _ := NewSigner(SignerConfig{Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: key}}, Clock: func() time.Time { return now }, NonceGenerator: func() (string, error) { return "nonce", nil }})
	valid, _ := signer.Sign(message)
	verifier, _ := NewVerifier(VerifierConfig{Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: key}}, Clock: func() time.Time { return now }, Tolerance: time.Minute})
	for name, mutate := range map[string]func(*Signature){
		"version":   func(signature *Signature) { signature.Version = "v2" },
		"algorithm": func(signature *Signature) { signature.Algorithm = SHA512 },
		"timestamp": func(signature *Signature) { signature.Timestamp = time.Time{} },
		"nonce":     func(signature *Signature) { signature.Nonce = "" },
		"key":       func(signature *Signature) { signature.KeyID = "missing" },
		"encoding":  func(signature *Signature) { signature.Value = "%" },
	} {
		t.Run("verify "+name, func(t *testing.T) {
			signature := valid[0]
			mutate(&signature)
			if _, err := verifier.Verify(message, []Signature{signature}); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("Verify() error = %v", err)
			}
		})
	}
	boundMessage := message
	boundMessage.Nonce = valid[0].Nonce
	wrongNonce := valid[0]
	wrongNonce.Nonce = "other"
	if verification, err := verifier.Verify(boundMessage, []Signature{wrongNonce, valid[0]}); err != nil || verification.KeyID != "key" {
		t.Fatalf("Verify() nonce candidate isolation = %#v, %v", verification, err)
	}
	for name, mutate := range map[string]func(*Signature){
		"version":       func(signature *Signature) { signature.Version = "v2" },
		"algorithm":     func(signature *Signature) { signature.Algorithm = SHA512 },
		"timestamp":     func(signature *Signature) { signature.Timestamp = time.Time{} },
		"tolerance":     func(signature *Signature) { signature.Timestamp = now.Add(-2 * time.Minute) },
		"message time":  func(signature *Signature) { signature.Timestamp = now.Add(time.Second) },
		"message nonce": func(signature *Signature) { signature.Nonce = "other" },
		"key":           func(signature *Signature) { signature.KeyID = "missing" },
		"encoding":      func(signature *Signature) { signature.Value = "%" },
	} {
		t.Run("skip "+name, func(t *testing.T) {
			invalid := valid[0]
			mutate(&invalid)
			if verification, err := verifier.Verify(message, []Signature{invalid, valid[0]}); err != nil || verification.KeyID != "key" {
				t.Fatalf("Verify() = %#v, %v", verification, err)
			}
		})
	}
	forgedMessage := message
	forgedMessage.Nonce = "nonce"
	canonical, err := Canonicalize(forgedMessage, "missing", SHA256)
	if err != nil {
		t.Fatalf("Canonicalize() forged key error = %v", err)
	}
	forgedValue, err := sign(SHA256, nil, canonical)
	if err != nil {
		t.Fatalf("sign() forged key error = %v", err)
	}
	forged := Signature{
		Version: "v1", Algorithm: SHA256, KeyID: "missing", Timestamp: now,
		Nonce: "nonce", Value: base64.RawURLEncoding.EncodeToString(forgedValue),
	}
	if _, err := verifier.Verify(message, []Signature{forged}); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify() unknown-key forgery error = %v", err)
	}
	if _, err := Canonicalize(Message{Timestamp: time.Unix(0, 0), Nonce: "nonce", Method: http.MethodPost}, "key", SHA256); err != nil {
		t.Fatalf("Canonicalize() Unix epoch error = %v", err)
	}
	store := &recordingReplayStore{recorded: true}
	for name, config := range map[string]VerifierConfig{
		"store without namespace": {Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: key}}, ReplayStore: store, ReplayTTL: time.Minute},
		"store without TTL":       {Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: key}}, ReplayStore: store, ReplayNamespace: "tenant"},
		"store negative TTL":      {Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: key}}, ReplayStore: store, ReplayTTL: -1, ReplayNamespace: "tenant"},
		"namespace without store": {Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: key}}, ReplayNamespace: "tenant"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewVerifier(config); !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("NewVerifier() error = %v", err)
			}
		})
	}
}

func TestMiddlewareObservationAndSSRFBoundaries(t *testing.T) {
	t.Parallel()

	verifier := verifierFixture(t, time.Unix(1_700_000_000, 0))
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, status := range []int{http.StatusBadRequest, 599} {
		if _, err := verifier.Middleware(MiddlewareConfig{FailureStatus: status}, next); err != nil {
			t.Fatalf("Middleware(%d) error = %v", status, err)
		}
	}
	for _, status := range []int{http.StatusBadRequest - 1, 600} {
		if _, err := verifier.Middleware(MiddlewareConfig{FailureStatus: status}, next); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("Middleware(%d) error = %v", status, err)
		}
	}
	start := time.Unix(2, 0)
	if got := elapsed(func() time.Time { return start }, start); got != 0 {
		t.Fatalf("elapsed(equal) = %v", got)
	}
	if got := elapsed(func() time.Time { return start.Add(time.Nanosecond) }, start); got != time.Nanosecond {
		t.Fatalf("elapsed(forward) = %v", got)
	}

	resolver := &staticResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	for _, maxAddresses := range []int{-1, 0} {
		if _, err := NewSSRFPolicy(SSRFPolicyConfig{Resolver: resolver, MaxAddresses: maxAddresses}); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("NewSSRFPolicy(%d) error = %v", maxAddresses, err)
		}
	}
	policy, _ := NewSSRFPolicy(SSRFPolicyConfig{Resolver: resolver, MaxAddresses: 1})
	for name, endpoint := range map[string]*url.URL{
		"missing host": {Scheme: "https"},
		"empty name":   {Scheme: "https", Host: ":"},
		"invalid port": {Scheme: "https", Host: "example.com:not-a-port"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := policy.Validate(context.Background(), endpoint); !errors.Is(err, ErrEndpointRejected) {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
	for _, endpoint := range []string{"https://example.com:1/hook", "https://example.com:65535/hook"} {
		if err := policy.Validate(context.Background(), mustURL(t, endpoint)); err != nil {
			t.Fatalf("Validate(%q) error = %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{"https://example.com:0/hook", "https://example.com:65536/hook"} {
		if err := policy.Validate(context.Background(), mustURL(t, endpoint)); !errors.Is(err, ErrEndpointRejected) {
			t.Fatalf("Validate(%q) error = %v", endpoint, err)
		}
	}
	if err := policy.Validate(context.Background(), &url.URL{Scheme: "https", Host: "[::1]x"}); !errors.Is(err, ErrEndpointRejected) {
		t.Fatalf("Validate(malformed bracketed host) error = %v", err)
	}
	for name, test := range map[string]struct {
		hostPort string
		want     string
		wantErr  bool
	}{
		"missing bracket": {hostPort: "[::1", wantErr: true},
		"IPv6 no port":    {hostPort: "[::1]"},
		"IPv6 bad suffix": {hostPort: "[::1]x", wantErr: true},
		"IPv6 empty port": {hostPort: "[::1]:", wantErr: true},
		"IPv6 port":       {hostPort: "[::1]:443", want: "443"},
		"host only":       {hostPort: "example.com"},
		"host port":       {hostPort: "example.com:443", want: "443"},
		"host empty port": {hostPort: "example.com:", wantErr: true},
	} {
		t.Run("endpoint port "+name, func(t *testing.T) {
			got, err := endpointPort(test.hostPort)
			if test.wantErr {
				if err == nil {
					t.Fatalf("endpointPort(%q) = %q, nil", test.hostPort, got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("endpointPort(%q) = %q, %v", test.hostPort, got, err)
			}
		})
	}
	if !asciiHost(string(rune(127))) || asciiHost(string(rune(128))) {
		t.Fatal("asciiHost() did not preserve the ASCII boundary")
	}
	for name, failingResolver := range map[string]*staticResolver{
		"lookup error": {err: errors.New("DNS unavailable")},
		"empty answer": {},
	} {
		t.Run(name, func(t *testing.T) {
			failingPolicy, err := NewSSRFPolicy(SSRFPolicyConfig{Resolver: failingResolver, MaxAddresses: 1})
			if err != nil {
				t.Fatalf("NewSSRFPolicy() error = %v", err)
			}
			if err := failingPolicy.Validate(context.Background(), mustURL(t, "https://example.com/hook")); !errors.Is(err, ErrEndpointRejected) {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
	client, err := NewSecureHTTPClient(policy, time.Second)
	if err != nil {
		t.Fatalf("NewSecureHTTPClient() error = %v", err)
	}
	transport := client.Transport.(*policyTransport).next.(*http.Transport)
	if transport.DialContext == nil || client.Timeout != time.Second {
		t.Fatal("NewSecureHTTPClient() did not preserve its transport bounds")
	}
	if secureDialerKeepAlive != 30*time.Second {
		t.Fatalf("secure dialer keepalive = %v", secureDialerKeepAlive)
	}
	for _, timeout := range []time.Duration{-1, 0} {
		if _, err := NewSecureHTTPClient(policy, timeout); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("NewSecureHTTPClient(%v) error = %v", timeout, err)
		}
	}
}

func TestRequestMessageAndAttemptObservationsPreserveSemanticBoundaries(t *testing.T) {
	t.Parallel()

	request := &http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/hook"}, Host: "example.com"}
	if message := requestMessage(request, nil, nil, "", ""); message.Path != "/hook" {
		t.Fatalf("requestMessage() path = %q", message.Path)
	}
	request.URL.Path = ""
	if message := requestMessage(request, nil, nil, "", ""); message.Path != "/" {
		t.Fatalf("requestMessage() empty path = %q", message.Path)
	}

	observer := &recordingObserver{}
	deliverer := &Deliverer{observer: observer}
	for _, attempt := range []DeliveryAttempt{
		{Classification: FailureRetryable},
		{Classification: FailureRetryable, StatusCode: http.StatusTooManyRequests},
		{Classification: FailureTerminal},
		{Classification: FailureTerminal, StatusCode: http.StatusBadRequest},
	} {
		deliverer.recordAttempt(context.Background(), &DeliveryResult{}, attempt)
	}
	events := observer.snapshot()
	want := []Reason{ReasonTransport, ReasonStatus, ReasonTransport, ReasonStatus}
	if len(events) != len(want) {
		t.Fatalf("observations = %#v", events)
	}
	for index, reason := range want {
		if events[index].Reason != reason {
			t.Fatalf("observation %d reason = %q, want %q", index, events[index].Reason, reason)
		}
	}
}

func validDeliveryConfig(t *testing.T) DeliveryConfig {
	t.Helper()

	signer, err := NewSigner(SignerConfig{Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: []byte("secret")}}})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}

	return DeliveryConfig{
		Client:           &responseDoer{response: response(http.StatusNoContent, http.NoBody)},
		Signer:           signer,
		EndpointPolicy:   EndpointPolicyFunc(func(context.Context, *url.URL) error { return nil }),
		Retry:            RetryPolicy{MaxAttempts: 2, BaseDelay: time.Second, MaxDelay: 2 * time.Second},
		IDGenerator:      func() (string, error) { return "id", nil },
		MaxRequestBytes:  1024,
		MaxResponseBytes: 1024,
		MaxFanOut:        2,
		HeaderLimits:     HeaderLimits{MaxSignatures: 1, MaxBytes: 512},
	}
}

package webhook

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestVerificationErrorMethodsAreNilSafe(t *testing.T) {
	t.Parallel()

	var nilFailure *VerificationError
	if nilFailure.Error() != "webhook verification failed" || nilFailure.Unwrap() != nil {
		t.Fatalf("nil verification failure methods were not safe")
	}
	failure := &VerificationError{}
	if failure.Error() != "webhook verification failed" || failure.Unwrap() != nil {
		t.Fatalf("empty verification failure methods were not safe")
	}
	failure.Kind = ErrReplay
	if failure.Error() != ErrReplay.Error() || !errors.Is(failure, ErrReplay) {
		t.Fatalf("typed verification failure = %v", failure)
	}
}

func TestCanonicalizeRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	valid := Message{Timestamp: time.Unix(1, 0), Nonce: "nonce", Method: "POST", Path: "/", Host: "example.com"}
	for name, test := range map[string]struct {
		message   Message
		algorithm Algorithm
		want      error
	}{
		"algorithm":          {message: valid, algorithm: "md5", want: ErrInvalidConfiguration},
		"timestamp":          {message: Message{Method: "POST"}, algorithm: SHA256, want: ErrInvalidTimestamp},
		"negative timestamp": {message: Message{Timestamp: time.Unix(-1, 0), Nonce: "nonce", Method: "POST"}, algorithm: SHA256, want: ErrInvalidTimestamp},
		"empty method":       {message: func() Message { value := valid; value.Method = ""; return value }(), algorithm: SHA256, want: ErrInvalidConfiguration},
		"method":             {message: func() Message { value := valid; value.Method = "POST\nX"; return value }(), algorithm: SHA256, want: ErrInvalidConfiguration},
		"query":              {message: func() Message { value := valid; value.RawQuery = "%"; return value }(), algorithm: SHA256, want: ErrInvalidConfiguration},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Canonicalize(test.message, "key", test.algorithm); !errors.Is(err, test.want) {
				t.Fatalf("Canonicalize() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSignerRejectsInvalidConfigurationAndInactiveKeys(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	configs := []SignerConfig{
		{Algorithm: "md5", Keys: []SigningKey{{ID: "key", Secret: []byte("x")}}},
		{Algorithm: SHA256},
		{Algorithm: SHA256, Keys: []SigningKey{{ID: "", Secret: []byte("x")}}},
		{Algorithm: SHA256, Keys: []SigningKey{{ID: "key"}}},
		{Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: []byte("x"), NotBefore: now, NotAfter: now.Add(-time.Second)}}},
	}
	for _, config := range configs {
		if _, err := NewSigner(config); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("NewSigner(%#v) error = %v", config, err)
		}
	}
	signer, err := NewSigner(SignerConfig{
		Algorithm: SHA256,
		Keys:      []SigningKey{{ID: "future", Secret: []byte("x"), NotBefore: now.Add(time.Hour)}},
		Clock:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	if _, err := signer.Sign(Message{}); !errors.Is(err, ErrNoActiveKey) {
		t.Fatalf("Sign() error = %v, want ErrNoActiveKey", err)
	}
	defaultClockSigner, err := NewSigner(SignerConfig{Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: []byte("x")}}})
	if err != nil {
		t.Fatalf("NewSigner() default clock error = %v", err)
	}
	if _, err := defaultClockSigner.Sign(Message{Method: "POST\n", Path: "/"}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("Sign() canonical error = %v", err)
	}
}

func TestSignerAndVerifierCopyKeyMaterial(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	secret := []byte("copy-me")
	signer, err := NewSigner(SignerConfig{Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: secret}}, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	verifier, err := NewVerifier(VerifierConfig{Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: secret}}, Clock: func() time.Time { return now }, Tolerance: time.Minute})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	copy(secret, []byte("changed"))
	message := Message{Timestamp: now, Method: "POST", Path: "/", Host: "example.com"}
	signatures, err := signer.Sign(message)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if _, err := verifier.Verify(message, signatures); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestVerifierRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	key := VerificationKey{ID: "key", Secret: []byte("secret")}
	store := &recordingReplayStore{recorded: true}
	configs := []VerifierConfig{
		{Algorithm: "md5", Keys: []VerificationKey{key}},
		{Algorithm: SHA256},
		{Algorithm: SHA256, Keys: []VerificationKey{key}, Tolerance: -1},
		{Algorithm: SHA256, Keys: []VerificationKey{{ID: "", Secret: []byte("x")}}},
		{Algorithm: SHA256, Keys: []VerificationKey{{ID: "key"}}},
		{Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: []byte("x"), NotBefore: time.Unix(2, 0), NotAfter: time.Unix(1, 0)}}},
		{Algorithm: SHA256, Keys: []VerificationKey{key, key}},
		{Algorithm: SHA256, Keys: []VerificationKey{key}, ReplayStore: store},
		{Algorithm: SHA256, Keys: []VerificationKey{key}, ReplayTTL: time.Minute, ReplayNamespace: "tenant"},
	}
	for _, config := range configs {
		if _, err := NewVerifier(config); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("NewVerifier(%#v) error = %v", config, err)
		}
	}
	if _, err := NewVerifier(VerifierConfig{Algorithm: SHA256, Keys: []VerificationKey{key}}); err != nil {
		t.Fatalf("NewVerifier() default clock error = %v", err)
	}
}

func TestVerifierSkipsMalformedAndInactiveSignaturesDeterministically(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	message := Message{Timestamp: now, Method: "POST", Path: "/", Host: "example.com"}
	key := []byte("secret")
	signer, _ := NewSigner(SignerConfig{Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: key}}, Clock: func() time.Time { return now }})
	valid, _ := signer.Sign(message)
	verifier, err := NewVerifier(VerifierConfig{
		Algorithm: SHA256,
		Keys: []VerificationKey{
			{ID: "key", Secret: key},
			{ID: "future", Secret: key, NotBefore: now.Add(time.Hour)},
			{ID: "revoked", Secret: key, Revoked: true},
		},
		Clock: func() time.Time { return now }, Tolerance: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	tests := map[string]Signature{
		"version":       {Version: "v2", Algorithm: SHA256, KeyID: "key", Timestamp: now, Nonce: "nonce"},
		"algorithm":     {Version: "v1", Algorithm: SHA512, KeyID: "key", Timestamp: now, Nonce: "nonce"},
		"timestamp":     {Version: "v1", Algorithm: SHA256, KeyID: "key", Nonce: "nonce"},
		"expired":       {Version: "v1", Algorithm: SHA256, KeyID: "key", Timestamp: now.Add(-time.Hour), Nonce: "nonce"},
		"message time":  {Version: "v1", Algorithm: SHA256, KeyID: "key", Timestamp: now.Add(time.Second), Nonce: "nonce"},
		"unknown key":   {Version: "v1", Algorithm: SHA256, KeyID: "unknown", Timestamp: now, Nonce: "nonce"},
		"future key":    {Version: "v1", Algorithm: SHA256, KeyID: "future", Timestamp: now, Nonce: "nonce"},
		"revoked key":   {Version: "v1", Algorithm: SHA256, KeyID: "revoked", Timestamp: now, Nonce: "nonce"},
		"bad encoding":  {Version: "v1", Algorithm: SHA256, KeyID: "key", Timestamp: now, Nonce: "nonce", Value: "%"},
		"missing nonce": {Version: "v1", Algorithm: SHA256, KeyID: "key", Timestamp: now},
	}
	for name, signature := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := verifier.Verify(message, []Signature{signature}); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("Verify() error = %v", err)
			}
		})
	}
	invalidQuery := message
	invalidQuery.RawQuery = "%"
	if _, err := verifier.Verify(invalidQuery, valid); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify() canonical error = %v", err)
	}
}

func TestCaptureBodySurfacesReadAndCloseFailures(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read failed")
	request := &http.Request{Body: &faultBody{readErr: readErr}, ContentLength: -1}
	if _, err := CaptureBody(request, 8); !errors.Is(err, ErrBodyRead) {
		t.Fatalf("CaptureBody() read error = %v", err)
	}
	request = &http.Request{Body: &faultBody{reader: bytes.NewReader([]byte("body")), closeErr: errors.New("close failed")}, ContentLength: 4}
	if _, err := CaptureBody(request, 8); !errors.Is(err, ErrBodyRead) {
		t.Fatalf("CaptureBody() close error = %v", err)
	}
	request = &http.Request{Body: &faultBody{closeErr: errors.New("close failed")}, ContentLength: 9}
	if _, err := CaptureBody(request, 8); !errors.Is(err, ErrBodyRead) {
		t.Fatalf("CaptureBody() rejected close error = %v", err)
	}
}

func TestHTTPHeaderAndRequestErrorPaths(t *testing.T) {
	t.Parallel()

	if err := SetSignatureHeaders(nil, nil); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("SetSignatureHeaders() error = %v", err)
	}
	if _, err := ParseSignatureHeaders(nil, HeaderLimits{}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("ParseSignatureHeaders() limits error = %v", err)
	}
	invalidSignatures := []Signature{
		{},
		{Version: "v1", Algorithm: SHA256, KeyID: "key", Timestamp: time.Unix(-1, 0), Nonce: "nonce", Value: "ubEWwKQcbLvNqyq4NDMHO4k5KX8euM7Z9nnpaZC2mD8"},
		{Version: "v1", Algorithm: SHA256, KeyID: "key", Timestamp: time.Unix(1, 0), Nonce: "nonce", Value: "%"},
		{Version: "v1", Algorithm: "md5", KeyID: "key", Timestamp: time.Unix(1, 0), Nonce: "nonce", Value: ""},
	}
	for _, signature := range invalidSignatures {
		if err := SetSignatureHeaders(make(http.Header), []Signature{signature}); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("SetSignatureHeaders(%#v) error = %v", signature, err)
		}
	}

	validValue := "ubEWwKQcbLvNqyq4NDMHO4k5KX8euM7Z9nnpaZC2mD8"
	malformed := []string{
		"v1;wrong=sha256;keyid=a2V5;timestamp=1;nonce=bm9uY2U;signature=" + validValue,
		"v1;algorithm=md5;keyid=a2V5;timestamp=1;nonce=bm9uY2U;signature=" + validValue,
		"v1;algorithm=sha256;wrong=a2V5;timestamp=1;nonce=bm9uY2U;signature=" + validValue,
		"v1;algorithm=sha256;keyid=_w;timestamp=1;nonce=bm9uY2U;signature=" + validValue,
		"v1;algorithm=sha256;keyid=a2V5;wrong=1;nonce=bm9uY2U;signature=" + validValue,
		"v1;algorithm=sha256;keyid=a2V5;timestamp=-1;nonce=bm9uY2U;signature=" + validValue,
		"v1;algorithm=sha256;keyid=a2V5;timestamp=x;nonce=bm9uY2U;signature=" + validValue,
		"v1;algorithm=sha256;keyid=a2V5;timestamp=1;wrong=bm9uY2U;signature=" + validValue,
		"v1;algorithm=sha256;keyid=a2V5;timestamp=1;nonce=%;signature=" + validValue,
		"v1;algorithm=sha256;keyid=a2V5;timestamp=1;nonce=bm9uY2U;wrong=" + validValue,
		"v1;algorithm=sha256;keyid=a2V5;timestamp=1;nonce=bm9uY2U;signature=%",
	}
	for _, value := range malformed {
		header := http.Header{SignatureHeader: {value}}
		if _, err := ParseSignatureHeaders(header, HeaderLimits{MaxSignatures: 1, MaxBytes: 512}); !errors.Is(err, ErrMalformedSignatureHeader) {
			t.Fatalf("ParseSignatureHeaders(%q) error = %v", value, err)
		}
	}

	signer, _ := NewSigner(SignerConfig{Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: []byte("key")}}})
	if _, _, err := signer.SignRequest(nil, RequestOptions{}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("SignRequest(nil) error = %v", err)
	}
	if _, _, err := signer.SignRequest(&http.Request{}, RequestOptions{}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("SignRequest(no URL) error = %v", err)
	}
	request := &http.Request{Method: "POST", URL: &url.URL{Path: "/"}, Body: io.NopCloser(bytes.NewReader([]byte("body"))), ContentLength: 4}
	if _, _, err := signer.SignRequest(request, RequestOptions{MaxBodyBytes: 1}); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("SignRequest() body error = %v", err)
	}
	request = &http.Request{
		Method: "POST", URL: &url.URL{Path: "/"}, Body: http.NoBody,
		Header: http.Header{"Content-Type": {"application/json", "text/plain"}},
	}
	if _, _, err := signer.SignRequest(request, RequestOptions{MaxBodyBytes: 1}); !errors.Is(err, ErrMalformedSignedHeader) {
		t.Fatalf("SignRequest() signed header error = %v", err)
	}
	for name, value := range map[string]string{
		"oversized":     string(bytes.Repeat([]byte("x"), maxFixedSignedHeaderBytes+1)),
		"invalid UTF-8": string([]byte{0xff}),
		"line break":    "application/json\r\nX-Injected: true",
	} {
		header := http.Header{"Content-Type": {value}}
		if _, _, err := fixedSignedHeaders(header); !errors.Is(err, ErrMalformedSignedHeader) {
			t.Fatalf("fixedSignedHeaders(%s) error = %v", name, err)
		}
	}
	request = &http.Request{Method: "POST", URL: &url.URL{Path: "/"}, Body: http.NoBody}
	if _, _, err := signer.SignRequest(request, RequestOptions{
		MaxBodyBytes: 1,
		HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 1},
	}); !errors.Is(err, ErrSignatureHeadersTooLarge) || request.Header.Get(SignatureHeader) != "" {
		t.Fatalf("SignRequest() header error = %v, header = %q", err, request.Header.Get(SignatureHeader))
	}
	factory, _ := hashFactory(SHA256)
	invalidSigner := &Signer{
		algorithm: SHA256, hash: factory, keys: []SigningKey{{Secret: []byte("key")}}, clock: time.Now,
		nonce: func() (string, error) { return "nonce", nil },
	}
	request = &http.Request{Method: "POST", URL: &url.URL{Path: "/"}, Body: http.NoBody}
	if _, _, err := invalidSigner.SignRequest(request, RequestOptions{MaxBodyBytes: 1, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 512}}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("SignRequest() invalid signer error = %v", err)
	}
}

func TestSignerRejectsNonceGenerationFailures(t *testing.T) {
	t.Parallel()

	base := SignerConfig{Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: []byte("secret")}}}
	for name, generator := range map[string]NonceGenerator{
		"error": func() (string, error) { return "", errors.New("entropy failed") },
		"empty": func() (string, error) { return "", nil },
		"large": func() (string, error) { return string(bytes.Repeat([]byte("x"), maxNonceBytes+1)), nil },
	} {
		t.Run(name, func(t *testing.T) {
			config := base
			config.NonceGenerator = generator
			signer, err := NewSigner(config)
			if err != nil {
				t.Fatalf("NewSigner() error = %v", err)
			}
			if _, err := signer.Sign(Message{Method: "POST", Path: "/"}); !errors.Is(err, ErrNonceGeneration) {
				t.Fatalf("Sign() error = %v", err)
			}
		})
	}
	signer, _ := NewSigner(base)
	signer.nonce = nil
	if _, err := signer.Sign(Message{Method: "POST", Path: "/"}); !errors.Is(err, ErrNonceGeneration) {
		t.Fatalf("Sign() nil generator error = %v", err)
	}
	if _, err := signer.Sign(Message{Nonce: string(bytes.Repeat([]byte("x"), maxNonceBytes+1)), Method: "POST", Path: "/"}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("Sign() invalid explicit nonce error = %v", err)
	}
	if _, err := generateNonce(&faultBody{readErr: errors.New("entropy failed")}); !errors.Is(err, ErrNonceGeneration) {
		t.Fatalf("generateNonce() error = %v", err)
	}
}

func TestVerifyRequestAndReplayErrorPaths(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key := []byte("secret")
	signer, _ := NewSigner(SignerConfig{Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: key}}, Clock: func() time.Time { return now }})
	store := &recordingReplayStore{recorded: true}
	verifier, _ := NewVerifier(VerifierConfig{
		Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: key}},
		Clock: func() time.Time { return now }, Tolerance: time.Minute,
		ReplayStore: store, ReplayTTL: time.Minute, ReplayNamespace: "tenant",
	})
	plain, _ := NewVerifier(VerifierConfig{Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: key}}, Clock: func() time.Time { return now }, Tolerance: time.Minute})

	if _, _, err := verifier.VerifyRequest(context.Background(), nil, RequestOptions{}); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("VerifyRequest(nil) error = %v", err)
	}
	unsigned := &http.Request{Method: "POST", URL: &url.URL{Path: "/"}, Header: make(http.Header), Body: http.NoBody}
	if _, _, err := verifier.VerifyRequest(context.Background(), unsigned, RequestOptions{HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 256}}); !errors.Is(err, ErrMalformedSignatureHeader) {
		t.Fatalf("VerifyRequest() header error = %v", err)
	}

	request, _ := http.NewRequest(http.MethodPost, "https://example.com/hook", bytes.NewReader([]byte("body")))
	_, _, _ = signer.SignRequest(request, RequestOptions{MaxBodyBytes: 8, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 512}})
	request.ContentLength = 4
	if _, _, err := verifier.VerifyRequest(context.Background(), request, RequestOptions{MaxBodyBytes: 1, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 512}}); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("VerifyRequest() body error = %v", err)
	}

	request, _ = http.NewRequest(http.MethodPost, "https://example.com/hook", bytes.NewReader([]byte("body")))
	_, _, _ = signer.SignRequest(request, RequestOptions{MaxBodyBytes: 8, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 512}})
	request.Body = io.NopCloser(bytes.NewReader([]byte("evil")))
	request.ContentLength = 4
	if _, _, err := verifier.VerifyRequest(context.Background(), request, RequestOptions{MaxBodyBytes: 8, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 512}}); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("VerifyRequest() authentication error = %v", err)
	}

	request, _ = http.NewRequest(http.MethodPost, "https://example.com/hook", bytes.NewReader([]byte("body")))
	_, _, _ = signer.SignRequest(request, RequestOptions{MaxBodyBytes: 8, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 512}})
	if _, _, err := verifier.VerifyRequest(context.Background(), request, RequestOptions{MaxBodyBytes: 8, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 512}}); !errors.Is(err, ErrMissingEventID) {
		t.Fatalf("VerifyRequest() missing extractor error = %v", err)
	}
	if _, _, err := verifier.VerifyRequest(context.Background(), request, RequestOptions{MaxBodyBytes: 8, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 512}, EventID: func(*http.Request, []byte) (string, error) { return "", errors.New("bad event") }}); !errors.Is(err, ErrMissingEventID) {
		t.Fatalf("VerifyRequest() extractor error = %v", err)
	}
	if _, err := verifier.VerifyAndRecord(context.Background(), Message{}, nil, "event"); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("VerifyAndRecord() verification error = %v", err)
	}
	message := Message{Timestamp: now, Method: "POST", Path: "/", Host: "example.com"}
	signatures, _ := signer.Sign(message)
	if _, err := plain.VerifyAndRecord(context.Background(), message, signatures, ""); err != nil {
		t.Fatalf("VerifyAndRecord() without replay store error = %v", err)
	}
	if err := verifier.recordReplay(context.Background(), Verification{KeyID: "key", Algorithm: SHA256}, ""); !errors.Is(err, ErrMissingEventID) {
		t.Fatalf("recordReplay() missing event error = %v", err)
	}
}

func TestSignerOrdersKeysWithEqualActivationByID(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	signer, _ := NewSigner(SignerConfig{Algorithm: SHA256, Keys: []SigningKey{
		{ID: "z", Secret: []byte("z"), NotBefore: now.Add(-time.Hour)},
		{ID: "a", Secret: []byte("a"), NotBefore: now.Add(-time.Hour)},
	}, Clock: func() time.Time { return now }})
	signatures, err := signer.Sign(Message{Timestamp: now, Method: "POST", Path: "/", Host: "example.com"})
	if err != nil || signatures[0].KeyID != "a" || signatures[1].KeyID != "z" {
		t.Fatalf("Sign() order = %#v, error = %v", signatures, err)
	}
}

func TestHeaderEventIDValidation(t *testing.T) {
	t.Parallel()

	if _, err := HeaderEventID("", 1)(nil, nil); !errors.Is(err, ErrMissingEventID) {
		t.Fatalf("HeaderEventID() config error = %v", err)
	}
	request := &http.Request{Header: http.Header{"X-Event-ID": {"one", "two"}}}
	if _, err := HeaderEventID("X-Event-ID", 8)(request, nil); !errors.Is(err, ErrMissingEventID) {
		t.Fatalf("HeaderEventID() duplicate error = %v", err)
	}
}

func TestObservationClassifiesPolicyTransportAndClockRegression(t *testing.T) {
	t.Parallel()

	if observationReason(ErrEndpointRejected) != ReasonPolicy || observationReason(ErrDeliveryFailed) != ReasonTransport || observationReason(errors.New("unknown")) != ReasonInternal {
		t.Fatal("observationReason() did not classify terminal failures")
	}
	if elapsed(func() time.Time { return time.Unix(1, 0) }, time.Unix(2, 0)) != 0 {
		t.Fatal("elapsed() did not clamp a regressing clock")
	}
	if _, err := sign("md5", []byte("secret"), []byte("message")); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("sign() error = %v", err)
	}
	if value, err := sign(SHA256, []byte("secret"), []byte("message")); err != nil || len(value) != 32 {
		t.Fatalf("sign(SHA256) bytes = %d, error = %v", len(value), err)
	}
}

type faultBody struct {
	reader   *bytes.Reader
	readErr  error
	closeErr error
}

func (b *faultBody) Read(buffer []byte) (int, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}
	if b.reader == nil {
		return 0, io.EOF
	}

	return b.reader.Read(buffer)
}

func (b *faultBody) Close() error { return b.closeErr }

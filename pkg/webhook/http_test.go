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

func TestSignatureHeadersRoundTripMultipleRotationSignatures(t *testing.T) {
	t.Parallel()

	timestamp := time.Unix(1_700_000_000, 0)
	want := []Signature{
		{Version: "v1", Algorithm: SHA256, KeyID: "new/key", Timestamp: timestamp, Nonce: "nonce-1", Value: "ubEWwKQcbLvNqyq4NDMHO4k5KX8euM7Z9nnpaZC2mD8"},
		{Version: "v1", Algorithm: SHA512, KeyID: "old", Timestamp: timestamp, Nonce: "nonce-1", Value: "tRxmI6q0Dn7LIha2ikR7n54zICwhT7fSvJ4EUxKIGq7XAr6jl9uM_WkTPrq3xy9yqRjY_l_Lt8LbDIQHEyGztA"},
	}
	header := make(http.Header)
	if err := SetSignatureHeaders(header, want); err != nil {
		t.Fatalf("SetSignatureHeaders() error = %v", err)
	}

	got, err := ParseSignatureHeaders(header, HeaderLimits{MaxSignatures: 2, MaxBytes: 512})
	if err != nil {
		t.Fatalf("ParseSignatureHeaders() error = %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("ParseSignatureHeaders() returned %d signatures", len(got))
	}
	for index := range want {
		if got[index].Version != want[index].Version ||
			got[index].Algorithm != want[index].Algorithm ||
			got[index].KeyID != want[index].KeyID ||
			!got[index].Timestamp.Equal(want[index].Timestamp) ||
			got[index].Nonce != want[index].Nonce ||
			got[index].Value != want[index].Value {
			t.Fatalf("signature %d = %#v, want %#v", index, got[index], want[index])
		}
	}
}

func TestParseSignatureHeadersRejectsMalformedOrAmbiguousInput(t *testing.T) {
	t.Parallel()

	valid := "v1;algorithm=sha256;keyid=a2V5;timestamp=1700000000;nonce=bm9uY2U;signature=ubEWwKQcbLvNqyq4NDMHO4k5KX8euM7Z9nnpaZC2mD8"
	tests := map[string][]string{
		"missing":              nil,
		"empty":                {""},
		"unknown version":      {"v2" + valid[2:]},
		"unknown algorithm":    {"v1;algorithm=md5" + valid[len("v1;algorithm=sha256"):]},
		"noncanonical time":    {"v1;algorithm=sha256;keyid=a2V5;timestamp=01700000000;signature=ubEWwKQcbLvNqyq4NDMHO4k5KX8euM7Z9nnpaZC2mD8"},
		"invalid key encoding": {"v1;algorithm=sha256;keyid=%;timestamp=1700000000;signature=ubEWwKQcbLvNqyq4NDMHO4k5KX8euM7Z9nnpaZC2mD8"},
		"missing nonce":        {"v1;algorithm=sha256;keyid=a2V5;timestamp=1700000000;signature=ubEWwKQcbLvNqyq4NDMHO4k5KX8euM7Z9nnpaZC2mD8"},
		"invalid nonce":        {"v1;algorithm=sha256;keyid=a2V5;timestamp=1700000000;nonce=%;signature=ubEWwKQcbLvNqyq4NDMHO4k5KX8euM7Z9nnpaZC2mD8"},
		"short signature":      {"v1;algorithm=sha256;keyid=a2V5;timestamp=1700000000;signature=YQ"},
		"duplicate key":        {valid, valid},
		"combined values":      {valid + "," + valid},
	}
	for name, values := range tests {
		name := name
		values := values
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			header := make(http.Header)
			header[SignatureHeader] = values
			if _, err := ParseSignatureHeaders(header, HeaderLimits{MaxSignatures: 4, MaxBytes: 512}); !errors.Is(err, ErrMalformedSignatureHeader) {
				t.Fatalf("ParseSignatureHeaders() error = %v, want ErrMalformedSignatureHeader", err)
			}
		})
	}
}

func TestParseSignatureHeadersAppliesLimitsBeforeDecoding(t *testing.T) {
	t.Parallel()

	header := http.Header{SignatureHeader: {"%%%%", "%%%%"}}
	if _, err := ParseSignatureHeaders(header, HeaderLimits{MaxSignatures: 1, MaxBytes: 100}); !errors.Is(err, ErrSignatureHeadersTooLarge) {
		t.Fatalf("signature count error = %v, want ErrSignatureHeadersTooLarge", err)
	}
	if _, err := ParseSignatureHeaders(header, HeaderLimits{MaxSignatures: 2, MaxBytes: 7}); !errors.Is(err, ErrSignatureHeadersTooLarge) {
		t.Fatalf("signature bytes error = %v, want ErrSignatureHeadersTooLarge", err)
	}
}

func TestSignAndVerifyRequestUsesRawBodyAndRestoresIt(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 987_654_321)
	signer, err := NewSigner(SignerConfig{
		Algorithm: SHA256,
		Keys:      []SigningKey{{ID: "key", Secret: []byte("secret")}},
		Clock:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	body := []byte("{\"exact\":true}\r\n")
	request := &http.Request{
		Method:        http.MethodPost,
		URL:           &url.URL{Path: "/hooks/orders", RawPath: "/hooks/%6Frders", RawQuery: "b=2&a=1"},
		Host:          "receiver.example",
		Header:        make(http.Header),
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
	if _, _, err := signer.SignRequest(request, RequestOptions{MaxBodyBytes: 1024, HeaderLimits: HeaderLimits{MaxSignatures: 2, MaxBytes: 512}}); err != nil {
		t.Fatalf("SignRequest() error = %v", err)
	}

	verifier, err := NewVerifier(VerifierConfig{
		Algorithm: SHA256,
		Keys:      []VerificationKey{{ID: "key", Secret: []byte("secret")}},
		Clock:     func() time.Time { return time.Unix(1_700_000_000, 0) },
		Tolerance: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	verification, captured, err := verifier.VerifyRequest(context.Background(), request, RequestOptions{
		MaxBodyBytes: 1024,
		HeaderLimits: HeaderLimits{MaxSignatures: 2, MaxBytes: 512},
	})
	if err != nil {
		t.Fatalf("VerifyRequest() error = %v", err)
	}
	if verification.KeyID != "key" || !bytes.Equal(captured, body) {
		t.Fatalf("VerifyRequest() = %#v, body = %q", verification, captured)
	}
	restored, err := io.ReadAll(request.Body)
	if err != nil || !bytes.Equal(restored, body) {
		t.Fatalf("restored request body = %q, error = %v", restored, err)
	}
}

func TestVerifyRequestRejectsMutationOfFixedSignedHeaders(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	for name, mutate := range map[string]func(http.Header){
		"content type": func(header http.Header) { header.Set("Content-Type", "text/plain") },
		"idempotency":  func(header http.Header) { header.Set(IdempotencyHeader, "other-event") },
	} {
		t.Run(name, func(t *testing.T) {
			request, _ := http.NewRequest(http.MethodPost, "https://example.com/hooks", bytes.NewBufferString("body"))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(IdempotencyHeader, "event-1")
			signer, _ := NewSigner(SignerConfig{
				Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: []byte("secret")}},
				Clock: func() time.Time { return now }, NonceGenerator: func() (string, error) { return "nonce", nil },
			})
			_, _, _ = signer.SignRequest(request, RequestOptions{MaxBodyBytes: 64, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 512}})
			mutate(request.Header)
			verifier, _ := NewVerifier(VerifierConfig{
				Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: []byte("secret")}},
				Clock: func() time.Time { return now }, Tolerance: time.Minute,
			})
			if _, _, err := verifier.VerifyRequest(context.Background(), request, RequestOptions{MaxBodyBytes: 64, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 512}}); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("VerifyRequest() error = %v", err)
			}
		})
	}
}

func TestVerifyRequestRejectsDuplicateSignedHeaderBeforeBodyRead(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	request := signedRequestFixture(t, now)
	request.Header[IdempotencyHeader] = []string{"event-1", "event-2"}
	body := &observedBody{reader: bytes.NewReader([]byte("body"))}
	request.Body = body
	request.ContentLength = 4
	verifier, _ := NewVerifier(VerifierConfig{
		Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: []byte("secret")}},
		Clock: func() time.Time { return now }, Tolerance: time.Minute,
	})
	if _, _, err := verifier.VerifyRequest(context.Background(), request, RequestOptions{MaxBodyBytes: 64, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 512}}); !errors.Is(err, ErrMalformedSignedHeader) {
		t.Fatalf("VerifyRequest() error = %v", err)
	}
	if body.reads != 0 {
		t.Fatalf("body reads = %d, want 0", body.reads)
	}
}

func TestVerifyRequestExtractsEventIDAfterAuthentication(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	store := &recordingReplayStore{recorded: true}
	request := signedRequestFixture(t, now)
	request.Header.Set("Webhook-Id", "event-123")
	verifier, err := NewVerifier(VerifierConfig{
		Algorithm:       SHA256,
		Keys:            []VerificationKey{{ID: "key", Secret: []byte("secret")}},
		Clock:           func() time.Time { return now },
		Tolerance:       time.Minute,
		ReplayStore:     store,
		ReplayTTL:       time.Hour,
		ReplayNamespace: "tenant",
	})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}

	_, _, err = verifier.VerifyRequest(context.Background(), request, RequestOptions{
		MaxBodyBytes: 64,
		HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 256},
		EventID:      HeaderEventID("Webhook-Id", 64),
	})
	if err != nil {
		t.Fatalf("VerifyRequest() error = %v", err)
	}
	if len(store.key) != 64 {
		t.Fatalf("stored replay key length = %d", len(store.key))
	}
}

func TestHeaderEventIDRejectsDuplicateAndOversizedValues(t *testing.T) {
	t.Parallel()

	extractor := HeaderEventID("Webhook-Id", 8)
	for name, values := range map[string][]string{
		"missing":   nil,
		"duplicate": {"one", "two"},
		"oversized": {"123456789"},
		"blank":     {""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			request := &http.Request{Header: http.Header{"Webhook-Id": values}}
			if _, err := extractor(request, nil); !errors.Is(err, ErrMissingEventID) {
				t.Fatalf("HeaderEventID() error = %v, want ErrMissingEventID", err)
			}
		})
	}
}

func signedRequestFixture(t *testing.T, now time.Time) *http.Request {
	t.Helper()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com/hooks", bytes.NewBufferString("body"))
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}
	signer, err := NewSigner(SignerConfig{
		Algorithm: SHA256,
		Keys:      []SigningKey{{ID: "key", Secret: []byte("secret")}},
		Clock:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	if _, _, err := signer.SignRequest(request, RequestOptions{MaxBodyBytes: 64, HeaderLimits: HeaderLimits{MaxSignatures: 1, MaxBytes: 256}}); err != nil {
		t.Fatalf("SignRequest() error = %v", err)
	}

	return request
}

package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"net/url"
	"testing"
	"time"
)

func FuzzParseSignatureHeaders(f *testing.F) {
	f.Add("v1;algorithm=sha256;keyid=a2V5;timestamp=1700000000;nonce=bm9uY2U;signature=ubEWwKQcbLvNqyq4NDMHO4k5KX8euM7Z9nnpaZC2mD8")
	f.Add("")
	f.Add("%%%%")
	f.Fuzz(func(t *testing.T, value string) {
		header := http.Header{SignatureHeader: {value}}
		signatures, err := ParseSignatureHeaders(header, HeaderLimits{MaxSignatures: 2, MaxBytes: 4096})
		if err == nil {
			roundTrip := make(http.Header)
			if err := SetSignatureHeaders(roundTrip, signatures); err != nil {
				t.Fatalf("valid parsed signature could not be formatted: %v", err)
			}
		}
	})
}

func FuzzCanonicalize(f *testing.F) {
	f.Add("POST", "/hooks", "a=1", "example.com", "application/json", "event-1", []byte("body"), "key", "nonce")
	f.Add("\n", "%", "%", "", "\r\n", "\xff", []byte{0xff}, "", "")
	f.Fuzz(func(t *testing.T, method, path, query, host, contentType, idempotencyKey string, body []byte, keyID, nonce string) {
		message := Message{
			Timestamp: time.Unix(1_700_000_000, 0), Method: method, Path: path,
			RawQuery: query, Host: host, ContentType: contentType,
			IdempotencyKey: idempotencyKey, Body: body, Nonce: nonce,
		}
		first, firstErr := Canonicalize(message, keyID, SHA256)
		second, secondErr := Canonicalize(message, keyID, SHA256)
		if (firstErr == nil) != (secondErr == nil) || string(first) != string(second) {
			t.Fatal("canonicalization is nondeterministic")
		}
	})
}

func FuzzTimestampVerification(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(1_700_000_000))
	f.Add(int64(-1))
	now := time.Unix(1_700_000_000, 0)
	verifier, err := NewVerifier(VerifierConfig{
		Algorithm: SHA256,
		Keys:      []VerificationKey{{ID: "key", Secret: []byte("secret")}},
		Clock:     func() time.Time { return now },
		Tolerance: time.Minute,
	})
	if err != nil {
		f.Fatalf("NewVerifier() error = %v", err)
	}
	f.Fuzz(func(t *testing.T, seconds int64) {
		message := Message{Timestamp: time.Unix(seconds, 0), Method: "POST", Path: "/", Host: "example.com"}
		signature := Signature{Version: "v1", Algorithm: SHA256, KeyID: "key", Timestamp: time.Unix(seconds, 0), Nonce: "nonce", Value: "AA"}
		_, _ = verifier.Verify(message, []Signature{signature})
	})
}

func FuzzDeliveryWire(f *testing.F) {
	f.Add([]byte(`{"version":"v1","endpoint":"https://example.com","body":"","event_id":"event"}`))
	f.Add([]byte(`{`))
	f.Fuzz(func(t *testing.T, encoded []byte) {
		delivery, err := UnmarshalDeliveryRequest(encoded, 4096)
		if err == nil {
			roundTrip, err := MarshalDeliveryRequest(delivery, 4096)
			if err != nil {
				t.Fatalf("decoded delivery could not be encoded: %v", err)
			}
			if _, err := UnmarshalDeliveryRequest(roundTrip, 4096); err != nil {
				t.Fatalf("round-trip delivery could not be decoded: %v", err)
			}
		}
	})
}

func FuzzEnvelope(f *testing.F) {
	f.Add("id", "type", "source", []byte(`{"valid":true}`))
	f.Add("", "", "", []byte(`{`))
	f.Fuzz(func(t *testing.T, id, eventType, source string, data []byte) {
		envelope := Envelope{ID: id, Type: eventType, Source: source, Time: time.Unix(1_700_000_000, 0), Data: data}
		encoded, err := envelope.MarshalJSON()
		if err == nil && !json.Valid(encoded) {
			t.Fatalf("MarshalJSON() emitted invalid JSON: %q", encoded)
		}
	})
}

func FuzzSSRFPolicy(f *testing.F) {
	f.Add("https://example.com/hook")
	f.Add("http://127.0.0.1/")
	f.Add(":")
	policy, err := NewSSRFPolicy(SSRFPolicyConfig{
		Resolver:     &staticResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}},
		MaxAddresses: 4,
	})
	if err != nil {
		f.Fatalf("NewSSRFPolicy() error = %v", err)
	}
	f.Fuzz(func(t *testing.T, rawURL string) {
		endpoint, err := url.Parse(rawURL)
		if err != nil {
			return
		}
		_ = policy.Validate(context.Background(), endpoint)
	})
}

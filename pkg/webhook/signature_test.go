package webhook

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestCanonicalizeProducesStableVersionedBytes(t *testing.T) {
	t.Parallel()

	message := Message{
		Timestamp:      time.Unix(1_700_000_000, 0),
		Nonce:          "nonce-1",
		Method:         "post",
		Path:           "/hooks/%2Forders",
		RawQuery:       "z=last&a=2&a=1&empty=",
		Host:           "EXAMPLE.com:443",
		ContentType:    "application/json",
		IdempotencyKey: "event-1",
		Body:           []byte("{\"amount\":10}\n"),
		Metadata: map[string]string{
			"tenant": "acme",
			"trace":  "abc 123",
		},
	}

	canonical, err := Canonicalize(message, "key-1", SHA256)
	if err != nil {
		t.Fatalf("Canonicalize() error = %v", err)
	}

	want := "webhook-v1\n" +
		"algorithm:sha256\n" +
		"timestamp:1700000000\n" +
		"nonce:bm9uY2UtMQ\n" +
		"key-id:a2V5LTE\n" +
		"method:post\n" +
		"path:L2hvb2tzLyUyRm9yZGVycw\n" +
		"query:YT0yJmE9MSZlbXB0eT0mej1sYXN0\n" +
		"host:ZXhhbXBsZS5jb206NDQz\n" +
		"content-type:YXBwbGljYXRpb24vanNvbg\n" +
		"idempotency-key:ZXZlbnQtMQ\n" +
		"body-sha256:7uLoFwkLsBnbnP6vIIYnhi8WZtNUwapoCmzCi-Rdh_w\n" +
		"metadata:ZEdWdVlXNTA9WVdOdFpRCmRISmhZMlU9WVdKaklERXlNdw\n"
	if string(canonical) != want {
		t.Fatalf("Canonicalize() = %q, want %q", canonical, want)
	}
}

func TestSignerAndVerifierSupportSHA256AndSHA512(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	message := Message{
		Timestamp: now,
		Method:    "POST",
		Path:      "/webhooks",
		Host:      "receiver.example",
		Body:      []byte("payload"),
	}

	for _, algorithm := range []Algorithm{SHA256, SHA512} {
		algorithm := algorithm
		t.Run(string(algorithm), func(t *testing.T) {
			t.Parallel()

			signer, err := NewSigner(SignerConfig{
				Algorithm: algorithm,
				Keys: []SigningKey{{
					ID:     "primary",
					Secret: []byte("correct horse battery staple"),
				}},
				Clock: func() time.Time { return now },
			})
			if err != nil {
				t.Fatalf("NewSigner() error = %v", err)
			}

			signatures, err := signer.Sign(message)
			if err != nil {
				t.Fatalf("Sign() error = %v", err)
			}
			if len(signatures) != 1 {
				t.Fatalf("Sign() returned %d signatures, want 1", len(signatures))
			}
			if _, err := base64.RawURLEncoding.DecodeString(signatures[0].Value); err != nil {
				t.Fatalf("signature is not unpadded base64url: %v", err)
			}

			verifier, err := NewVerifier(VerifierConfig{
				Algorithm: algorithm,
				Keys: []VerificationKey{{
					ID:     "primary",
					Secret: []byte("correct horse battery staple"),
				}},
				Clock:     func() time.Time { return now },
				Tolerance: 5 * time.Minute,
			})
			if err != nil {
				t.Fatalf("NewVerifier() error = %v", err)
			}

			verification, err := verifier.Verify(message, signatures)
			if err != nil {
				t.Fatalf("Verify() error = %v", err)
			}
			if verification.KeyID != "primary" || verification.Algorithm != algorithm {
				t.Fatalf("Verify() = %#v", verification)
			}
		})
	}
}

func TestSignerUsesOneInjectedNonceAcrossRotationSignatures(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	calls := 0
	signer, err := NewSigner(SignerConfig{
		Algorithm: SHA256,
		Keys: []SigningKey{
			{ID: "new", Secret: []byte("new-secret")},
			{ID: "old", Secret: []byte("old-secret")},
		},
		Clock: func() time.Time { return now },
		NonceGenerator: func() (string, error) {
			calls++
			return "deterministic-nonce", nil
		},
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	signatures, err := signer.Sign(Message{Method: "POST", Path: "/", Host: "example.com"})
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if calls != 1 || len(signatures) != 2 || signatures[0].Nonce != "deterministic-nonce" || signatures[1].Nonce != "deterministic-nonce" {
		t.Fatalf("calls = %d, signatures = %#v", calls, signatures)
	}
	verifier, _ := NewVerifier(VerifierConfig{
		Algorithm: SHA256,
		Keys: []VerificationKey{
			{ID: "new", Secret: []byte("new-secret")},
			{ID: "old", Secret: []byte("old-secret")},
		},
		Clock: func() time.Time { return now }, Tolerance: time.Minute,
	})
	verification, err := verifier.Verify(Message{Method: "POST", Path: "/", Host: "example.com"}, signatures)
	if err != nil || verification.Nonce != "deterministic-nonce" {
		t.Fatalf("Verify() = %#v, error = %v", verification, err)
	}
}

func TestVerifierRejectsModifiedExactBytes(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	message := Message{Timestamp: now, Method: "POST", Path: "/", Host: "example.com", Body: []byte("original")}
	key := SigningKey{ID: "key", Secret: []byte("secret")}
	signer, err := NewSigner(SignerConfig{Algorithm: SHA256, Keys: []SigningKey{key}, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	signatures, err := signer.Sign(message)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	verifier, err := NewVerifier(VerifierConfig{
		Algorithm: SHA256,
		Keys:      []VerificationKey{{ID: key.ID, Secret: key.Secret}},
		Clock:     func() time.Time { return now },
		Tolerance: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}

	message.Body = []byte("original\n")
	_, err = verifier.Verify(message, signatures)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify() error = %v, want ErrInvalidSignature", err)
	}

	var failure *VerificationError
	if !errors.As(err, &failure) {
		t.Fatalf("Verify() error type = %T, want *VerificationError", err)
	}
	if failure.SafeMessage() != "webhook verification failed" {
		t.Fatalf("SafeMessage() = %q", failure.SafeMessage())
	}
}

func TestVerifierBindsDuplicateQueryValueOrder(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	message := Message{Timestamp: now, Nonce: "nonce", Method: "POST", Path: "/", RawQuery: "a=first&a=second", Host: "example.com"}
	signer, _ := NewSigner(SignerConfig{
		Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: []byte("secret")}},
		Clock: func() time.Time { return now },
	})
	verifier, _ := NewVerifier(VerifierConfig{
		Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: []byte("secret")}},
		Clock: func() time.Time { return now }, Tolerance: time.Minute,
	})
	signatures, _ := signer.Sign(message)
	message.RawQuery = "a=second&a=first"
	if _, err := verifier.Verify(message, signatures); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify() error = %v, want ErrInvalidSignature", err)
	}
}

func TestRotationSignsAllActiveKeysAndAcceptsOverlap(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	keys := []SigningKey{
		{ID: "old", Secret: []byte("old-secret"), NotAfter: now.Add(time.Minute)},
		{ID: "new", Secret: []byte("new-secret"), NotBefore: now.Add(-time.Minute)},
		{ID: "future", Secret: []byte("future-secret"), NotBefore: now.Add(time.Hour)},
		{ID: "revoked", Secret: []byte("revoked-secret"), Revoked: true},
	}
	signer, err := NewSigner(SignerConfig{Algorithm: SHA256, Keys: keys, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}

	message := Message{Method: "POST", Path: "/", Host: "example.com", Body: []byte("body")}
	signatures, err := signer.Sign(message)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if len(signatures) != 2 || signatures[0].KeyID != "new" || signatures[1].KeyID != "old" {
		t.Fatalf("Sign() signatures = %#v, want new then old", signatures)
	}

	verifier, err := NewVerifier(VerifierConfig{
		Algorithm: SHA256,
		Keys: []VerificationKey{
			{ID: "old", Secret: []byte("old-secret"), NotAfter: now.Add(time.Minute)},
			{ID: "new", Secret: []byte("new-secret"), NotBefore: now.Add(-time.Minute)},
		},
		Clock:     func() time.Time { return now },
		Tolerance: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	if _, err := verifier.Verify(message, signatures[1:]); err != nil {
		t.Fatalf("Verify() old overlap signature error = %v", err)
	}
}

func TestSignerSelectsRotationKeyAtSignedTimestamp(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	historical := now.Add(-2 * time.Hour)
	signer, err := NewSigner(SignerConfig{
		Algorithm: SHA256,
		Keys: []SigningKey{
			{ID: "historical", Secret: []byte("old"), NotBefore: now.Add(-3 * time.Hour), NotAfter: now.Add(-time.Hour)},
			{ID: "current", Secret: []byte("new"), NotBefore: now.Add(-time.Hour)},
		},
		Clock: func() time.Time { return now }, NonceGenerator: func() (string, error) { return "nonce", nil },
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	signatures, err := signer.Sign(Message{Timestamp: historical, Method: "POST", Path: "/", Host: "example.com"})
	if err != nil || len(signatures) != 1 || signatures[0].KeyID != "historical" {
		t.Fatalf("Sign() signatures = %#v, error = %v", signatures, err)
	}
}

func TestVerifierRejectsMutationOfEverySignedComponent(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	message := Message{
		Timestamp: now, Method: "POST", Path: "/hooks", RawQuery: "a=1",
		Host: "example.com", ContentType: "application/json", IdempotencyKey: "event-1",
		Body: []byte("body"), Metadata: map[string]string{"tenant": "one"},
	}
	key := []byte("correct-key")
	signer, err := NewSigner(SignerConfig{
		Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: key}}, Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	signatures, err := signer.Sign(message)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	verifier, err := NewVerifier(VerifierConfig{
		Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: key}},
		Clock: func() time.Time { return now }, Tolerance: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}

	mutations := map[string]func(Message) Message{
		"nonce":        func(value Message) Message { value.Nonce = "other-nonce"; return value },
		"method":       func(value Message) Message { value.Method = "PUT"; return value },
		"method case":  func(value Message) Message { value.Method = "post"; return value },
		"path":         func(value Message) Message { value.Path = "/hooks/other"; return value },
		"query":        func(value Message) Message { value.RawQuery = "a=2"; return value },
		"host":         func(value Message) Message { value.Host = "other.example"; return value },
		"content type": func(value Message) Message { value.ContentType = "text/plain"; return value },
		"idempotency":  func(value Message) Message { value.IdempotencyKey = "event-2"; return value },
		"body":         func(value Message) Message { value.Body = []byte("Body"); return value },
		"metadata": func(value Message) Message {
			value.Metadata = map[string]string{"tenant": "two"}
			return value
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := verifier.Verify(mutate(message), signatures); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("Verify() error = %v, want ErrInvalidSignature", err)
			}
		})
	}

	wrongKeyVerifier, err := NewVerifier(VerifierConfig{
		Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: []byte("wrong-key")}},
		Clock: func() time.Time { return now }, Tolerance: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewVerifier() wrong key error = %v", err)
	}
	if _, err := wrongKeyVerifier.Verify(message, signatures); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("wrong-key Verify() error = %v", err)
	}

	mutatedSignature := append([]Signature(nil), signatures...)
	mutatedSignature[0].Value = "Ab" + mutatedSignature[0].Value[2:]
	if _, err := verifier.Verify(message, mutatedSignature); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("mutated signature Verify() error = %v", err)
	}
}

func TestVerifierTimestampToleranceBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key := []byte("key")
	verifier, err := NewVerifier(VerifierConfig{
		Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: key}},
		Clock: func() time.Time { return now }, Tolerance: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	for name, timestamp := range map[string]time.Time{
		"past boundary":   now.Add(-time.Minute),
		"future boundary": now.Add(time.Minute),
	} {
		t.Run(name, func(t *testing.T) {
			message := Message{Timestamp: timestamp, Method: "POST", Path: "/", Host: "example.com", Body: []byte("body")}
			signer, err := NewSigner(SignerConfig{Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: key}}, Clock: func() time.Time { return timestamp }})
			if err != nil {
				t.Fatalf("NewSigner() error = %v", err)
			}
			signatures, err := signer.Sign(message)
			if err != nil {
				t.Fatalf("Sign() error = %v", err)
			}
			if _, err := verifier.Verify(message, signatures); err != nil {
				t.Fatalf("Verify() boundary error = %v", err)
			}
		})
	}

	message := Message{Timestamp: now.Add(time.Minute + time.Second), Method: "POST", Path: "/", Host: "example.com", Body: []byte("body")}
	signer, _ := NewSigner(SignerConfig{Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: key}}, Clock: func() time.Time { return message.Timestamp }})
	signatures, _ := signer.Sign(message)
	if _, err := verifier.Verify(message, signatures); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify() outside tolerance error = %v", err)
	}
}

func TestVerifierComparesCallerTimestampAtProtocolSecondPrecision(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 987_654_321)
	message := Message{Timestamp: now, Method: "POST", Path: "/", Host: "example.com"}
	signer, _ := NewSigner(SignerConfig{
		Algorithm: SHA256, Keys: []SigningKey{{ID: "key", Secret: []byte("secret")}},
		Clock: func() time.Time { return now }, NonceGenerator: func() (string, error) { return "nonce", nil },
	})
	verifier, _ := NewVerifier(VerifierConfig{
		Algorithm: SHA256, Keys: []VerificationKey{{ID: "key", Secret: []byte("secret")}},
		Clock: func() time.Time { return now }, Tolerance: time.Minute,
	})
	signatures, _ := signer.Sign(message)
	if _, err := verifier.Verify(message, signatures); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

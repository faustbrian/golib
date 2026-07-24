package webhook

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestIndependentInteroperabilityVectors(t *testing.T) {
	t.Parallel()

	encoded, err := os.ReadFile("testdata/vectors/v1.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var fixture interoperabilityFixture
	if err := json.Unmarshal(encoded, &fixture); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if fixture.Version != "v1" || fixture.Generator != "python-stdlib" || len(fixture.Vectors) != 2 {
		t.Fatalf("fixture metadata = %#v", fixture)
	}
	for _, vector := range fixture.Vectors {
		vector := vector
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()
			body, err := base64.RawURLEncoding.DecodeString(vector.Body)
			if err != nil {
				t.Fatalf("body decode error = %v", err)
			}
			secret, err := base64.RawURLEncoding.DecodeString(vector.KeyMaterial)
			if err != nil {
				t.Fatalf("key decode error = %v", err)
			}
			message := Message{
				Timestamp: time.Unix(vector.Timestamp, 0), Method: vector.Method,
				Nonce: vector.Nonce,
				Path:  vector.Path, RawQuery: vector.RawQuery, Host: vector.Host,
				ContentType: vector.ContentType, IdempotencyKey: vector.IdempotencyKey,
				Body: body, Metadata: vector.Metadata,
			}
			canonical, err := Canonicalize(message, vector.KeyID, vector.Algorithm)
			if err != nil {
				t.Fatalf("Canonicalize() error = %v", err)
			}
			if got := base64.RawURLEncoding.EncodeToString(canonical); got != vector.Canonical {
				t.Fatalf("canonical = %q, want %q", got, vector.Canonical)
			}
			signer, err := NewSigner(SignerConfig{
				Algorithm: vector.Algorithm,
				Keys:      []SigningKey{{ID: vector.KeyID, Secret: secret}},
				Clock:     func() time.Time { return message.Timestamp },
			})
			if err != nil {
				t.Fatalf("NewSigner() error = %v", err)
			}
			signatures, err := signer.Sign(message)
			if err != nil {
				t.Fatalf("Sign() error = %v", err)
			}
			if len(signatures) != 1 || signatures[0].Value != vector.Signature {
				t.Fatalf("signatures = %#v, want %q", signatures, vector.Signature)
			}
		})
	}
}

type interoperabilityFixture struct {
	Version   string                   `json:"version"`
	Generator string                   `json:"generator"`
	Vectors   []interoperabilityVector `json:"vectors"`
}

type interoperabilityVector struct {
	Name           string            `json:"name"`
	Algorithm      Algorithm         `json:"algorithm"`
	KeyID          string            `json:"key_id"`
	KeyMaterial    string            `json:"key_material_base64url"`
	Timestamp      int64             `json:"timestamp"`
	Nonce          string            `json:"nonce"`
	Method         string            `json:"method"`
	Path           string            `json:"path"`
	RawQuery       string            `json:"raw_query"`
	Host           string            `json:"host"`
	ContentType    string            `json:"content_type"`
	IdempotencyKey string            `json:"idempotency_key"`
	Body           string            `json:"body_base64url"`
	Metadata       map[string]string `json:"metadata"`
	Canonical      string            `json:"canonical_base64url"`
	Signature      string            `json:"signature_base64url"`
}

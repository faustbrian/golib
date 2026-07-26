package idempotency_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/idempotency"
)

func TestNewKeyRequiresEveryIdentityPart(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		namespace string
		tenant    string
		operation string
		caller    string
		value     string
	}{
		"namespace": {tenant: "tenant", operation: "charge", caller: "api", value: "request"},
		"tenant":    {namespace: "billing", operation: "charge", caller: "api", value: "request"},
		"operation": {namespace: "billing", tenant: "tenant", caller: "api", value: "request"},
		"caller":    {namespace: "billing", tenant: "tenant", operation: "charge", value: "request"},
		"value":     {namespace: "billing", tenant: "tenant", operation: "charge", caller: "api"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := idempotency.NewKey(
				test.namespace,
				test.tenant,
				test.operation,
				test.caller,
				test.value,
			)

			assertReason(t, err, idempotency.ReasonInvalidKey)
		})
	}
}

func TestNewKeyRejectsOversizedParts(t *testing.T) {
	t.Parallel()

	_, err := idempotency.NewKey(
		strings.Repeat("n", idempotency.MaxKeyPartBytes+1),
		"tenant",
		"charge",
		"api",
		"request",
	)

	assertReason(t, err, idempotency.ReasonLimitExceeded)
}

func TestKeyPreservesIdentityParts(t *testing.T) {
	t.Parallel()

	key, err := idempotency.NewKey("billing", "tenant", "charge", "api", "request")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}

	if key.Namespace() != "billing" || key.Tenant() != "tenant" ||
		key.Operation() != "charge" || key.Caller() != "api" ||
		key.Value() != "request" {
		t.Fatalf("NewKey() did not preserve all identity parts: %#v", key)
	}
}

func TestFingerprintUsesAnExplicitVersion(t *testing.T) {
	t.Parallel()

	fingerprint, err := idempotency.NewFingerprint("v1", []byte("canonical request"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}

	if fingerprint.Version() != "v1" {
		t.Fatalf("Version() = %q, want v1", fingerprint.Version())
	}
	if len(fingerprint.Sum()) != 32 {
		t.Fatalf("len(Sum()) = %d, want 32", len(fingerprint.Sum()))
	}

	same, err := idempotency.NewFingerprint("v1", []byte("canonical request"))
	if err != nil {
		t.Fatalf("NewFingerprint() second error = %v", err)
	}
	otherVersion, err := idempotency.NewFingerprint("v2", []byte("canonical request"))
	if err != nil {
		t.Fatalf("NewFingerprint() other version error = %v", err)
	}

	if !fingerprint.Equal(same) {
		t.Fatal("Equal() = false for equal versioned content")
	}
	if fingerprint.Equal(otherVersion) {
		t.Fatal("Equal() = true for different fingerprint versions")
	}
}

func TestFingerprintRejectsMissingVersion(t *testing.T) {
	t.Parallel()

	_, err := idempotency.NewFingerprint("", []byte("canonical request"))

	assertReason(t, err, idempotency.ReasonInvalidFingerprint)
}

func TestFingerprintRejectsOversizedVersion(t *testing.T) {
	t.Parallel()

	version := strings.Repeat("v", idempotency.MaxFingerprintVersionBytes+1)
	_, err := idempotency.NewFingerprint(version, []byte("canonical request"))
	assertReason(t, err, idempotency.ReasonLimitExceeded)
	_, err = idempotency.NewFingerprintFromSum(version, make([]byte, 32))
	assertReason(t, err, idempotency.ReasonLimitExceeded)
}

func TestFingerprintCanBeReconstructedFromPersistedDigest(t *testing.T) {
	t.Parallel()

	original, err := idempotency.NewFingerprint("v1", []byte("canonical request"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	reconstructed, err := idempotency.NewFingerprintFromSum("v1", original.Sum())
	if err != nil {
		t.Fatalf("NewFingerprintFromSum() error = %v", err)
	}
	if !original.Equal(reconstructed) {
		t.Fatal("persisted digest did not reconstruct the fingerprint")
	}

	digest := original.Sum()
	reconstructed, err = idempotency.NewFingerprintFromSum("v1", digest)
	if err != nil {
		t.Fatalf("NewFingerprintFromSum() second error = %v", err)
	}
	digest[0] ^= 0xff
	if !original.Equal(reconstructed) {
		t.Fatal("fingerprint retained a mutable digest alias")
	}
}

func TestPersistedFingerprintRequiresVersionAndSHA256Digest(t *testing.T) {
	t.Parallel()

	_, err := idempotency.NewFingerprintFromSum("", make([]byte, 32))
	assertReason(t, err, idempotency.ReasonInvalidFingerprint)
	_, err = idempotency.NewFingerprintFromSum("v1", make([]byte, 31))
	assertReason(t, err, idempotency.ReasonInvalidFingerprint)
}

func TestErrorHasStableDiagnosticText(t *testing.T) {
	t.Parallel()

	err := &idempotency.Error{
		Reason: idempotency.ReasonInvalidKey,
		Field:  "namespace",
	}

	if got, want := err.Error(), "idempotency: invalid_key: namespace"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestRecordReturnsItsOwnershipProof(t *testing.T) {
	t.Parallel()

	key, err := idempotency.NewKey("billing", "tenant", "charge", "api", "request")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	record := idempotency.Record{Key: key, OwnerToken: "owner", FencingToken: 7}

	ownership := record.Ownership()
	if ownership.Key != key || ownership.OwnerToken != "owner" || ownership.FencingToken != 7 {
		t.Fatalf("Ownership() = %#v", ownership)
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

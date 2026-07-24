package idempotency_test

import (
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/idempotency"
)

func FuzzKeyAndFingerprintValidation(f *testing.F) {
	f.Add(
		"http", "tenant", "POST /widgets", "caller", "request-1",
		"json-v1", []byte(`{"amount":42}`), make([]byte, 32),
	)
	f.Add("", "", "", "", "", "", []byte{0xff}, []byte{0x00})
	f.Add(
		"namespace", "tenant", "operation", "caller", "value",
		strings.Repeat("v", idempotency.MaxFingerprintVersionBytes+1),
		[]byte("canonical"), make([]byte, 32),
	)

	f.Fuzz(func(
		t *testing.T,
		namespace string,
		tenant string,
		operation string,
		caller string,
		value string,
		version string,
		canonical []byte,
		persisted []byte,
	) {
		parts := []string{namespace, tenant, operation, caller, value}
		for _, part := range parts {
			if len(part) > idempotency.MaxKeyPartBytes+1 {
				return
			}
		}
		if len(version) > idempotency.MaxFingerprintVersionBytes+1 ||
			len(canonical) > 4096 || len(persisted) > 64 {
			return
		}

		key, err := idempotency.NewKey(namespace, tenant, operation, caller, value)
		validKey := true
		for _, part := range parts {
			validKey = validKey && part != "" && len(part) <= idempotency.MaxKeyPartBytes
		}
		if validKey != (err == nil) {
			t.Fatalf("NewKey() valid = %t, error = %v", validKey, err)
		}
		if err == nil && (key.Namespace() != namespace || key.Tenant() != tenant ||
			key.Operation() != operation || key.Caller() != caller || key.Value() != value) {
			t.Fatalf("NewKey() did not preserve all identity parts: %#v", key)
		}

		fingerprint, err := idempotency.NewFingerprint(version, canonical)
		validVersion := version != "" && len(version) <= idempotency.MaxFingerprintVersionBytes
		if validVersion != (err == nil) {
			t.Fatalf("NewFingerprint() version = %q, error = %v", version, err)
		}
		if err == nil && (fingerprint.Version() != version || len(fingerprint.Sum()) != 32) {
			t.Fatalf("NewFingerprint() = %#v", fingerprint)
		}

		reconstructed, err := idempotency.NewFingerprintFromSum(version, persisted)
		validPersisted := validVersion && len(persisted) == 32
		if validPersisted != (err == nil) {
			t.Fatalf("NewFingerprintFromSum() valid = %t, error = %v", validPersisted, err)
		}
		if err == nil && (reconstructed.Version() != version ||
			string(reconstructed.Sum()) != string(persisted)) {
			t.Fatalf("NewFingerprintFromSum() = %#v", reconstructed)
		}
	})
}

func FuzzFingerprintPolicyVersionsRemainDistinct(f *testing.F) {
	f.Add("jcs-v1", "jcs-v2", []byte(`{"amount":42}`))
	f.Add("same", "same", []byte{0xff, 0x00, 0x01})
	f.Add("", "v1", []byte("missing version"))
	f.Add(
		strings.Repeat("v", idempotency.MaxFingerprintVersionBytes+1),
		"v1",
		[]byte("oversized version"),
	)

	f.Fuzz(func(t *testing.T, firstVersion string, secondVersion string, canonical []byte) {
		if len(firstVersion) > idempotency.MaxFingerprintVersionBytes+1 ||
			len(secondVersion) > idempotency.MaxFingerprintVersionBytes+1 ||
			len(canonical) > 4096 {
			return
		}
		first, firstErr := idempotency.NewFingerprint(firstVersion, canonical)
		second, secondErr := idempotency.NewFingerprint(secondVersion, canonical)
		firstValid := firstVersion != "" && len(firstVersion) <= idempotency.MaxFingerprintVersionBytes
		secondValid := secondVersion != "" && len(secondVersion) <= idempotency.MaxFingerprintVersionBytes
		if firstValid != (firstErr == nil) || secondValid != (secondErr == nil) {
			t.Fatalf("version validity = (%t, %t), errors = (%v, %v)", firstValid, secondValid, firstErr, secondErr)
		}
		if !firstValid || !secondValid {
			return
		}
		if first.Equal(second) != (firstVersion == secondVersion) {
			t.Fatalf("Equal() did not preserve policy identity: %q, %q", firstVersion, secondVersion)
		}
		reconstructed, err := idempotency.NewFingerprintFromSum(secondVersion, first.Sum())
		if err != nil {
			t.Fatalf("NewFingerprintFromSum() error = %v", err)
		}
		if reconstructed.Equal(first) != (firstVersion == secondVersion) {
			t.Fatalf("persisted sum crossed policy versions: %q, %q", firstVersion, secondVersion)
		}
	})
}

package valkey

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

func TestRecordVersion1FixtureRemainsReadableAndWritable(t *testing.T) {
	t.Parallel()

	fixture := map[string]string{
		"schema":              "1",
		"namespace":           "orders",
		"tenant":              "tenant-secret",
		"operation":           "create",
		"caller":              "caller-secret",
		"key_value":           "request-secret",
		"fingerprint_version": "jcs-v1",
		"fingerprint_sum":     "0deeb8fa1dbbee4c0dbe7f5e3c9183940139f26d22797ee8ab07c00557a4c2ff",
		"state":               "completed",
		"owner_token":         "owner-secret",
		"fencing_token":       "7",
		"lease_expires_at_ms": "1784190660123",
		"heartbeat_at_ms":     "1784190601123",
		"attempt":             "3",
		"created_at_ms":       "1784190600123",
		"updated_at_ms":       "1784190602123",
		"completed_at_ms":     "1784190602123",
		"failed_at_ms":        "0",
		"abandoned_at_ms":     "0",
		"expired_at_ms":       "0",
		"result":              string([]byte{'{', 0, '}'}),
		"metadata":            `{"content-type":"application/json"}`,
	}
	want := testRecord(t)
	decoded, err := decodeRecord(fixture)
	if err != nil {
		t.Fatalf("decodeRecord(v1 fixture) error = %v", err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("decodeRecord(v1 fixture) = %#v, want %#v", decoded, want)
	}

	encoded, err := encodeRecord(decoded)
	if err != nil {
		t.Fatalf("encodeRecord(v1 fixture) error = %v", err)
	}
	if !reflect.DeepEqual(encoded, fixture) {
		t.Fatalf("encodeRecord(v1 fixture) = %#v", encoded)
	}
}

func TestRecordKeyIsOpaqueAndClusterSafe(t *testing.T) {
	t.Parallel()

	key := testKey(t, "request")
	encoded := recordKey("idempotency", key)

	pattern := regexp.MustCompile(`^idempotency:\{[0-9a-f]{64}\}$`)
	if !pattern.MatchString(encoded) {
		t.Fatalf("recordKey() = %q", encoded)
	}
	for _, secret := range []string{
		key.Namespace(), key.Tenant(), key.Operation(), key.Caller(), key.Value(),
	} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("recordKey() exposed identity part %q", secret)
		}
	}
	if encoded != recordKey("idempotency", key) {
		t.Fatal("recordKey() is not deterministic")
	}
	if encoded == recordKey("idempotency", testKey(t, "other")) {
		t.Fatal("recordKey() collided for different identities")
	}
}

func TestRecordCodecPreservesPersistedState(t *testing.T) {
	t.Parallel()

	record := testRecord(t)
	fields, err := encodeRecord(record)
	if err != nil {
		t.Fatalf("encodeRecord() error = %v", err)
	}
	decoded, err := decodeRecord(fields)
	if err != nil {
		t.Fatalf("decodeRecord() error = %v", err)
	}

	if decoded.Key != record.Key || !decoded.Fingerprint.Equal(record.Fingerprint) ||
		decoded.State != record.State || decoded.OwnerToken != record.OwnerToken ||
		decoded.FencingToken != record.FencingToken || decoded.Attempt != record.Attempt ||
		!decoded.LeaseExpiresAt.Equal(record.LeaseExpiresAt) ||
		!decoded.HeartbeatAt.Equal(record.HeartbeatAt) ||
		!decoded.CreatedAt.Equal(record.CreatedAt) || !decoded.UpdatedAt.Equal(record.UpdatedAt) ||
		!decoded.CompletedAt.Equal(record.CompletedAt) || !decoded.FailedAt.Equal(record.FailedAt) ||
		!decoded.AbandonedAt.Equal(record.AbandonedAt) || !decoded.ExpiredAt.Equal(record.ExpiredAt) ||
		string(decoded.Result) != string(record.Result) || decoded.Metadata["content-type"] != "application/json" {
		t.Fatalf("decodeRecord() = %#v, want %#v", decoded, record)
	}
}

func TestRecordCodecRejectsMalformedPersistedData(t *testing.T) {
	t.Parallel()

	valid, err := encodeRecord(testRecord(t))
	if err != nil {
		t.Fatalf("encodeRecord() error = %v", err)
	}
	tests := map[string]func(map[string]string){
		"schema":      func(fields map[string]string) { fields[fieldSchema] = "2" },
		"key":         func(fields map[string]string) { fields[fieldTenant] = "" },
		"fingerprint": func(fields map[string]string) { fields[fieldFingerprintSum] = "not-hex" },
		"fingerprint version": func(fields map[string]string) {
			fields[fieldFingerprintVersion] = strings.Repeat("v", idempotency.MaxFingerprintVersionBytes+1)
		},
		"short digest": func(fields map[string]string) { fields[fieldFingerprintSum] = "00" },
		"state":        func(fields map[string]string) { fields[fieldState] = "future" },
		"owner":        func(fields map[string]string) { fields[fieldOwnerToken] = "" },
		"owner too long": func(fields map[string]string) {
			fields[fieldOwnerToken] = strings.Repeat("o", idempotency.MaxOwnerTokenBytes+1)
		},
		"fence":        func(fields map[string]string) { fields[fieldFencingToken] = "NaN" },
		"zero fence":   func(fields map[string]string) { fields[fieldFencingToken] = "0" },
		"attempt":      func(fields map[string]string) { fields[fieldAttempt] = "-1" },
		"zero attempt": func(fields map[string]string) { fields[fieldAttempt] = "0" },
		"timestamp":    func(fields map[string]string) { fields[fieldUpdatedAt] = "later" },
		"metadata":     func(fields map[string]string) { fields[fieldMetadata] = "{" },
		"result": func(fields map[string]string) {
			fields[fieldResult] = strings.Repeat("x", idempotency.MaxResultBytes+1)
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fields := cloneFields(valid)
			mutate(fields)
			_, err := decodeRecord(fields)
			assertCodecReason(t, err, idempotency.ReasonInvalidPayload)
		})
	}
}

func TestRecordCodecRejectsOversizedMetadata(t *testing.T) {
	t.Parallel()

	record := testRecord(t)
	record.Metadata = make(map[string]string, idempotency.MaxMetadataEntries+1)
	for index := range idempotency.MaxMetadataEntries + 1 {
		record.Metadata[string(rune('a'+index))] = "value"
	}
	_, err := encodeRecord(record)
	assertCodecReason(t, err, idempotency.ReasonLimitExceeded)

	fields, err := encodeRecord(testRecord(t))
	if err != nil {
		t.Fatalf("encodeRecord() error = %v", err)
	}
	fields[fieldMetadata] = `{"key":"` + strings.Repeat("x", idempotency.MaxMetadataValueBytes+1) + `"}`
	_, err = decodeRecord(fields)
	assertCodecReason(t, err, idempotency.ReasonLimitExceeded)

	fields, err = encodeRecord(testRecord(t))
	if err != nil {
		t.Fatalf("encodeRecord() second error = %v", err)
	}
	fields[fieldMetadata] = `{"` + strings.Repeat("k", idempotency.MaxMetadataKeyBytes+1) + `":"value"}`
	_, err = decodeRecord(fields)
	assertCodecReason(t, err, idempotency.ReasonLimitExceeded)
}

func TestRecordEncoderRejectsInvalidAndOversizedValues(t *testing.T) {
	t.Parallel()

	t.Run("identity", func(t *testing.T) {
		t.Parallel()
		record := testRecord(t)
		record.FencingToken = 0
		_, err := encodeRecord(record)
		assertCodecReason(t, err, idempotency.ReasonInvalidPayload)
	})

	t.Run("result", func(t *testing.T) {
		t.Parallel()
		record := testRecord(t)
		record.Result = make([]byte, idempotency.MaxResultBytes+1)
		_, err := encodeRecord(record)
		assertCodecReason(t, err, idempotency.ReasonLimitExceeded)
	})

	t.Run("owner token", func(t *testing.T) {
		t.Parallel()
		record := testRecord(t)
		record.OwnerToken = strings.Repeat("o", idempotency.MaxOwnerTokenBytes+1)
		_, err := encodeRecord(record)
		assertCodecReason(t, err, idempotency.ReasonInvalidPayload)
	})

	t.Run("metadata key", func(t *testing.T) {
		t.Parallel()
		record := testRecord(t)
		record.Metadata = map[string]string{
			strings.Repeat("k", idempotency.MaxMetadataKeyBytes+1): "value",
		}
		_, err := encodeRecord(record)
		assertCodecReason(t, err, idempotency.ReasonLimitExceeded)
	})
}

func TestAcquireReplyIncludesAValidatedOutcomeAndRecord(t *testing.T) {
	t.Parallel()

	fields, err := encodeRecord(testRecord(t))
	if err != nil {
		t.Fatalf("encodeRecord() error = %v", err)
	}
	reply := []string{string(idempotency.OutcomeReplayed)}
	for key, value := range fields {
		reply = append(reply, key, value)
	}
	result, err := decodeAcquireReply(reply)
	if err != nil {
		t.Fatalf("decodeAcquireReply() error = %v", err)
	}
	if result.Outcome != idempotency.OutcomeReplayed || result.Record.State != idempotency.StateCompleted {
		t.Fatalf("decodeAcquireReply() = %#v", result)
	}

	for name, malformed := range map[string][]string{
		"empty":   nil,
		"outcome": {"unknown", fieldSchema, schemaVersion},
		"pairs":   {string(idempotency.OutcomeReplayed), fieldSchema},
		"record": {
			string(idempotency.OutcomeReplayed), fieldSchema, "future",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := decodeAcquireReply(malformed)
			assertCodecReason(t, err, idempotency.ReasonInvalidPayload)
		})
	}
}

func testKey(t testing.TB, value string) idempotency.Key {
	t.Helper()
	key, err := idempotency.NewKey("orders", "tenant-secret", "create", "caller-secret", value)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	return key
}

func testRecord(t testing.TB) idempotency.Record {
	t.Helper()
	fingerprint, err := idempotency.NewFingerprint("jcs-v1", []byte("canonical"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	start := time.Date(2026, 7, 16, 8, 30, 0, 123000000, time.UTC)
	return idempotency.Record{
		Key:            testKey(t, "request-secret"),
		Fingerprint:    fingerprint,
		State:          idempotency.StateCompleted,
		OwnerToken:     "owner-secret",
		FencingToken:   7,
		LeaseExpiresAt: start.Add(time.Minute),
		HeartbeatAt:    start.Add(time.Second),
		Attempt:        3,
		CreatedAt:      start,
		UpdatedAt:      start.Add(2 * time.Second),
		CompletedAt:    start.Add(2 * time.Second),
		FailedAt:       time.Time{},
		AbandonedAt:    time.Time{},
		ExpiredAt:      time.Time{},
		Result:         []byte{'{', 0, '}'},
		Metadata:       map[string]string{"content-type": "application/json"},
	}
}

func cloneFields(fields map[string]string) map[string]string {
	cloned := make(map[string]string, len(fields))
	for key, value := range fields {
		cloned[key] = value
	}
	return cloned
}

func assertCodecReason(t *testing.T, err error, reason idempotency.Reason) {
	t.Helper()
	var semanticError *idempotency.Error
	if !errors.As(err, &semanticError) {
		t.Fatalf("error = %v, want *idempotency.Error", err)
	}
	if semanticError.Reason != reason {
		t.Fatalf("reason = %q, want %q", semanticError.Reason, reason)
	}
}

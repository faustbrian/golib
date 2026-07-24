package postgres

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

const postgresRecordV1Fixture = `{"schema":1,"namespace":"postgres","tenant":"tenant","operation":"operation","caller":"caller","key_value":"key","fingerprint_version":"v1","fingerprint_sum":"H1i5FFsk0QjXrDiIcziz6jIpgzucHkGCUDQ/kHv9EEc=","state":"completed","owner_token":"owner","fencing_token":3,"lease_expires_at":"2023-11-14T22:14:20.123Z","heartbeat_at":"2023-11-14T22:13:20.123Z","attempt":2,"created_at":"2023-11-14T21:13:20.123Z","updated_at":"2023-11-14T22:13:20.123Z","completed_at":"2023-11-14T22:13:20.123Z","failed_at":"0001-01-01T00:00:00Z","abandoned_at":"0001-01-01T00:00:00Z","expired_at":"0001-01-01T00:00:00Z","result":"eyJvayI6dHJ1ZX0=","metadata":{"content-type":"application/json"}}`

func TestRecordVersion1FixtureRemainsReadableAndWritable(t *testing.T) {
	want := codecRecord(t)
	decoded, err := decodeRecord([]byte(postgresRecordV1Fixture))
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
	if !bytes.Equal(encoded, []byte(postgresRecordV1Fixture)) {
		t.Fatalf("encodeRecord(v1 fixture) = %s", encoded)
	}
}

func TestRecordCodecRoundTrip(t *testing.T) {
	record := codecRecord(t)
	encoded, err := encodeRecord(record)
	if err != nil {
		t.Fatalf("encodeRecord() error = %v", err)
	}
	decoded, err := decodeRecord(encoded)
	if err != nil {
		t.Fatalf("decodeRecord() error = %v", err)
	}
	if decoded.Key != record.Key || !decoded.Fingerprint.Equal(record.Fingerprint) ||
		decoded.State != record.State || decoded.OwnerToken != record.OwnerToken ||
		decoded.FencingToken != record.FencingToken || decoded.Attempt != record.Attempt ||
		!decoded.CreatedAt.Equal(record.CreatedAt) || !decoded.CompletedAt.Equal(record.CompletedAt) ||
		string(decoded.Result) != string(record.Result) || decoded.Metadata["content-type"] != "application/json" {
		t.Fatalf("decoded record = %#v", decoded)
	}
}

func TestRecordCodecRejectsMalformedPersistedData(t *testing.T) {
	valid, err := encodeRecord(codecRecord(t))
	if err != nil {
		t.Fatalf("encodeRecord() error = %v", err)
	}
	var base map[string]any
	if err := json.Unmarshal(valid, &base); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	tests := map[string]func(map[string]any){
		"schema":      func(value map[string]any) { value["schema"] = 2 },
		"namespace":   func(value map[string]any) { value["namespace"] = "" },
		"fingerprint": func(value map[string]any) { value["fingerprint_sum"] = "AA==" },
		"fingerprint version": func(value map[string]any) {
			value["fingerprint_version"] = strings.Repeat("v", idempotency.MaxFingerprintVersionBytes+1)
		},
		"state":   func(value map[string]any) { value["state"] = "future" },
		"owner":   func(value map[string]any) { value["owner_token"] = "" },
		"fence":   func(value map[string]any) { value["fencing_token"] = 0 },
		"attempt": func(value map[string]any) { value["attempt"] = 0 },
		"created": func(value map[string]any) { value["created_at"] = "0001-01-01T00:00:00Z" },
		"result":  func(value map[string]any) { value["result"] = strings.Repeat("x", idempotency.MaxResultBytes+1) },
		"metadata": func(value map[string]any) {
			value["metadata"] = map[string]string{
				strings.Repeat("x", idempotency.MaxMetadataKeyBytes+1): "invalid",
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			copyValue := make(map[string]any, len(base))
			for key, value := range base {
				copyValue[key] = value
			}
			mutate(copyValue)
			encoded, err := json.Marshal(copyValue)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if _, err := decodeRecord(encoded); err == nil {
				t.Fatal("decodeRecord() error = nil")
			}
		})
	}
	if _, err := decodeRecord([]byte("not-json")); err == nil {
		t.Fatal("decodeRecord() malformed JSON error = nil")
	}
}

func TestRecordEncoderRejectsInvalidAndOversizedValues(t *testing.T) {
	tests := map[string]func(*idempotency.Record){
		"key": func(record *idempotency.Record) { record.Key = idempotency.Key{} },
		"fingerprint": func(record *idempotency.Record) {
			record.Fingerprint = idempotency.Fingerprint{}
		},
		"invalid record": func(record *idempotency.Record) { record.State = "future" },
		"owner":          func(record *idempotency.Record) { record.OwnerToken = "" },
		"owner too long": func(record *idempotency.Record) {
			record.OwnerToken = strings.Repeat("x", idempotency.MaxOwnerTokenBytes+1)
		},
		"fence":     func(record *idempotency.Record) { record.FencingToken = 0 },
		"attempt":   func(record *idempotency.Record) { record.Attempt = 0 },
		"timestamp": func(record *idempotency.Record) { record.UpdatedAt = time.Time{} },
		"result": func(record *idempotency.Record) {
			record.Result = []byte(strings.Repeat("x", idempotency.MaxResultBytes+1))
		},
		"metadata entries": func(record *idempotency.Record) {
			record.Metadata = make(map[string]string, idempotency.MaxMetadataEntries+1)
			for index := 0; index <= idempotency.MaxMetadataEntries; index++ {
				record.Metadata[string(rune('a'+index))] = "value"
			}
		},
		"metadata key": func(record *idempotency.Record) {
			record.Metadata = map[string]string{
				strings.Repeat("x", idempotency.MaxMetadataKeyBytes+1): "value",
			}
		},
		"metadata value": func(record *idempotency.Record) {
			record.Metadata = map[string]string{
				"key": strings.Repeat("x", idempotency.MaxMetadataValueBytes+1),
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			record := codecRecord(t)
			mutate(&record)
			if _, err := encodeRecord(record); err == nil {
				t.Fatal("encodeRecord() error = nil")
			}
		})
	}
}

func TestRecordCodecPreservesNilMetadata(t *testing.T) {
	record := codecRecord(t)
	record.Metadata = nil
	encoded, err := encodeRecord(record)
	if err != nil {
		t.Fatalf("encodeRecord() error = %v", err)
	}
	decoded, err := decodeRecord(encoded)
	if err != nil || decoded.Metadata != nil {
		t.Fatalf("decodeRecord() = %#v, %v", decoded, err)
	}
}

func codecRecord(t testing.TB) idempotency.Record {
	t.Helper()
	key, err := idempotency.NewKey("postgres", "tenant", "operation", "caller", "key")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("request"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	now := time.Unix(1_700_000_000, 123_000_000).UTC()
	return idempotency.Record{
		Key: key, Fingerprint: fingerprint, State: idempotency.StateCompleted,
		OwnerToken: "owner", FencingToken: 3, LeaseExpiresAt: now.Add(time.Minute),
		HeartbeatAt: now, Attempt: 2, CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
		CompletedAt: now, Result: []byte(`{"ok":true}`),
		Metadata: map[string]string{"content-type": "application/json"},
	}
}

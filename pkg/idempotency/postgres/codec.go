package postgres

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

const recordSchema = 1

type persistedRecord struct {
	Schema         int               `json:"schema"`
	Namespace      string            `json:"namespace"`
	Tenant         string            `json:"tenant"`
	Operation      string            `json:"operation"`
	Caller         string            `json:"caller"`
	KeyValue       string            `json:"key_value"`
	FingerprintVer string            `json:"fingerprint_version"`
	FingerprintSum []byte            `json:"fingerprint_sum"`
	State          idempotency.State `json:"state"`
	OwnerToken     string            `json:"owner_token"`
	FencingToken   uint64            `json:"fencing_token"`
	LeaseExpiresAt time.Time         `json:"lease_expires_at"`
	HeartbeatAt    time.Time         `json:"heartbeat_at"`
	Attempt        uint64            `json:"attempt"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	CompletedAt    time.Time         `json:"completed_at,omitempty"`
	FailedAt       time.Time         `json:"failed_at,omitempty"`
	AbandonedAt    time.Time         `json:"abandoned_at,omitempty"`
	ExpiredAt      time.Time         `json:"expired_at,omitempty"`
	Result         []byte            `json:"result,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

func encodeRecord(record idempotency.Record) ([]byte, error) {
	if err := validateRecord(record); err != nil {
		return nil, err
	}
	persisted := persistedRecord{
		Schema:    recordSchema,
		Namespace: record.Key.Namespace(), Tenant: record.Key.Tenant(),
		Operation: record.Key.Operation(), Caller: record.Key.Caller(),
		KeyValue: record.Key.Value(), FingerprintVer: record.Fingerprint.Version(),
		FingerprintSum: record.Fingerprint.Sum(), State: record.State,
		OwnerToken: record.OwnerToken, FencingToken: record.FencingToken,
		LeaseExpiresAt: record.LeaseExpiresAt, HeartbeatAt: record.HeartbeatAt,
		Attempt: record.Attempt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
		CompletedAt: record.CompletedAt, FailedAt: record.FailedAt,
		AbandonedAt: record.AbandonedAt, ExpiredAt: record.ExpiredAt,
		Result: append([]byte(nil), record.Result...), Metadata: cloneMetadata(record.Metadata),
	}
	encoded, _ := json.Marshal(persisted)
	return encoded, nil
}

func decodeRecord(encoded []byte) (idempotency.Record, error) {
	var persisted persistedRecord
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		return idempotency.Record{}, payloadError("record", err)
	}
	if persisted.Schema != recordSchema {
		return idempotency.Record{}, payloadError("schema", nil)
	}
	key, err := idempotency.NewKey(
		persisted.Namespace, persisted.Tenant, persisted.Operation,
		persisted.Caller, persisted.KeyValue,
	)
	if err != nil {
		return idempotency.Record{}, payloadError("key", err)
	}
	fingerprint, err := idempotency.NewFingerprintFromSum(
		persisted.FingerprintVer, persisted.FingerprintSum,
	)
	if err != nil {
		return idempotency.Record{}, payloadError("fingerprint", err)
	}
	record := idempotency.Record{
		Key: key, Fingerprint: fingerprint, State: persisted.State,
		OwnerToken: persisted.OwnerToken, FencingToken: persisted.FencingToken,
		LeaseExpiresAt: persisted.LeaseExpiresAt, HeartbeatAt: persisted.HeartbeatAt,
		Attempt: persisted.Attempt, CreatedAt: persisted.CreatedAt, UpdatedAt: persisted.UpdatedAt,
		CompletedAt: persisted.CompletedAt, FailedAt: persisted.FailedAt,
		AbandonedAt: persisted.AbandonedAt, ExpiredAt: persisted.ExpiredAt,
		Result: append([]byte(nil), persisted.Result...), Metadata: cloneMetadata(persisted.Metadata),
	}
	if err := validateRecord(record); err != nil {
		return idempotency.Record{}, err
	}
	return record, nil
}

func validateRecord(record idempotency.Record) error {
	if _, err := idempotency.NewKey(
		record.Key.Namespace(), record.Key.Tenant(), record.Key.Operation(),
		record.Key.Caller(), record.Key.Value(),
	); err != nil {
		return payloadError("key", err)
	}
	if _, err := idempotency.NewFingerprintFromSum(
		record.Fingerprint.Version(), record.Fingerprint.Sum(),
	); err != nil {
		return payloadError("fingerprint", err)
	}
	switch record.State {
	case idempotency.StateAcquired, idempotency.StateRunning,
		idempotency.StateCompleted, idempotency.StateFailed,
		idempotency.StateExpired, idempotency.StateAbandoned:
	default:
		return payloadError("state", nil)
	}
	if record.OwnerToken == "" || len(record.OwnerToken) > idempotency.MaxOwnerTokenBytes {
		return payloadError("owner_token", nil)
	}
	if record.FencingToken == 0 {
		return payloadError("fencing_token", nil)
	}
	if record.Attempt == 0 {
		return payloadError("attempt", nil)
	}
	if record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() ||
		record.LeaseExpiresAt.IsZero() || record.HeartbeatAt.IsZero() {
		return payloadError("timestamp", nil)
	}
	if len(record.Result) > idempotency.MaxResultBytes {
		return &idempotency.Error{Reason: idempotency.ReasonLimitExceeded, Field: "result"}
	}
	if err := validateMetadata(record.Metadata); err != nil {
		return err
	}
	return nil
}

func validateMetadata(metadata map[string]string) error {
	if len(metadata) > idempotency.MaxMetadataEntries {
		return &idempotency.Error{Reason: idempotency.ReasonLimitExceeded, Field: "metadata"}
	}
	for key, value := range metadata {
		if len(key) > idempotency.MaxMetadataKeyBytes ||
			len(value) > idempotency.MaxMetadataValueBytes {
			return &idempotency.Error{Reason: idempotency.ReasonLimitExceeded, Field: "metadata"}
		}
	}
	return nil
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return nil
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func payloadError(field string, cause error) error {
	if cause == nil {
		cause = errors.New("invalid persisted idempotency record")
	}
	return &idempotency.Error{Reason: idempotency.ReasonInvalidPayload, Field: field, Cause: cause}
}

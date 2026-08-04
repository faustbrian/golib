package valkey

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

const (
	schemaVersion = "1"

	fieldSchema             = "schema"
	fieldNamespace          = "namespace"
	fieldTenant             = "tenant"
	fieldOperation          = "operation"
	fieldCaller             = "caller"
	fieldKeyValue           = "key_value"
	fieldFingerprintVersion = "fingerprint_version"
	fieldFingerprintSum     = "fingerprint_sum"
	fieldState              = "state"
	fieldOwnerToken         = "owner_token"
	fieldFencingToken       = "fencing_token"
	fieldLeaseExpiresAt     = "lease_expires_at_ms"
	fieldHeartbeatAt        = "heartbeat_at_ms"
	fieldAttempt            = "attempt"
	fieldCreatedAt          = "created_at_ms"
	fieldUpdatedAt          = "updated_at_ms"
	fieldCompletedAt        = "completed_at_ms"
	fieldFailedAt           = "failed_at_ms"
	fieldAbandonedAt        = "abandoned_at_ms"
	fieldExpiredAt          = "expired_at_ms"
	fieldResult             = "result"
	fieldMetadata           = "metadata"
)

func recordKey(prefix string, key idempotency.Key) string {
	hash := sha256.New()
	var size [4]byte
	for _, part := range []string{
		key.Namespace(), key.Tenant(), key.Operation(), key.Caller(), key.Value(),
	} {
		binary.BigEndian.PutUint32(size[:], uint32(len(part)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(part))
	}
	return prefix + ":{" + hex.EncodeToString(hash.Sum(nil)) + "}"
}

func encodeRecord(record idempotency.Record) (map[string]string, error) {
	switch validState(record.State) {
	case false:
		return nil, recordError(errors.New("invalid record identity"))
	}
	switch record.OwnerToken {
	case "":
		return nil, recordError(errors.New("invalid record identity"))
	}
	switch min(len(record.OwnerToken), idempotency.MaxOwnerTokenBytes) {
	case len(record.OwnerToken):
	default:
		return nil, recordError(errors.New("invalid record identity"))
	}
	switch record.FencingToken {
	case 0:
		return nil, recordError(errors.New("invalid record identity"))
	}
	switch record.Attempt {
	case 0:
		return nil, recordError(errors.New("invalid record identity"))
	}
	switch min(len(record.Result), idempotency.MaxResultBytes) {
	case len(record.Result):
	default:
		return nil, limitError(fieldResult)
	}
	if err := validateMetadata(record.Metadata); err != nil {
		return nil, err
	}
	metadata, _ := json.Marshal(record.Metadata)

	return map[string]string{
		fieldSchema:             schemaVersion,
		fieldNamespace:          record.Key.Namespace(),
		fieldTenant:             record.Key.Tenant(),
		fieldOperation:          record.Key.Operation(),
		fieldCaller:             record.Key.Caller(),
		fieldKeyValue:           record.Key.Value(),
		fieldFingerprintVersion: record.Fingerprint.Version(),
		fieldFingerprintSum:     hex.EncodeToString(record.Fingerprint.Sum()),
		fieldState:              string(record.State),
		fieldOwnerToken:         record.OwnerToken,
		fieldFencingToken:       strconv.FormatUint(record.FencingToken, 10),
		fieldLeaseExpiresAt:     encodeTime(record.LeaseExpiresAt),
		fieldHeartbeatAt:        encodeTime(record.HeartbeatAt),
		fieldAttempt:            strconv.FormatUint(record.Attempt, 10),
		fieldCreatedAt:          encodeTime(record.CreatedAt),
		fieldUpdatedAt:          encodeTime(record.UpdatedAt),
		fieldCompletedAt:        encodeTime(record.CompletedAt),
		fieldFailedAt:           encodeTime(record.FailedAt),
		fieldAbandonedAt:        encodeTime(record.AbandonedAt),
		fieldExpiredAt:          encodeTime(record.ExpiredAt),
		fieldResult:             string(record.Result),
		fieldMetadata:           string(metadata),
	}, nil
}

func decodeRecord(fields map[string]string) (idempotency.Record, error) {
	if fields[fieldSchema] != schemaVersion {
		return idempotency.Record{}, recordError(errors.New("unsupported schema"))
	}
	key, err := idempotency.NewKey(
		fields[fieldNamespace],
		fields[fieldTenant],
		fields[fieldOperation],
		fields[fieldCaller],
		fields[fieldKeyValue],
	)
	if err != nil {
		return idempotency.Record{}, recordError(err)
	}
	digest, err := hex.DecodeString(fields[fieldFingerprintSum])
	if err != nil {
		return idempotency.Record{}, recordError(err)
	}
	fingerprint, err := idempotency.NewFingerprintFromSum(fields[fieldFingerprintVersion], digest)
	if err != nil {
		return idempotency.Record{}, recordError(err)
	}
	state := idempotency.State(fields[fieldState])
	switch validState(state) {
	case false:
		return idempotency.Record{}, recordError(errors.New("invalid record state"))
	}
	switch fields[fieldOwnerToken] {
	case "":
		return idempotency.Record{}, recordError(errors.New("invalid record state"))
	}
	switch min(len(fields[fieldOwnerToken]), idempotency.MaxOwnerTokenBytes) {
	case len(fields[fieldOwnerToken]):
	default:
		return idempotency.Record{}, recordError(errors.New("invalid record state"))
	}
	fence, err := parsePositiveUint(fields[fieldFencingToken])
	if err != nil {
		return idempotency.Record{}, recordError(err)
	}
	attempt, err := parsePositiveUint(fields[fieldAttempt])
	if err != nil {
		return idempotency.Record{}, recordError(err)
	}
	timestamps, err := decodeTimestamps(fields)
	if err != nil {
		return idempotency.Record{}, recordError(err)
	}
	result := []byte(fields[fieldResult])
	switch min(len(result), idempotency.MaxResultBytes) {
	case len(result):
	default:
		return idempotency.Record{}, recordError(errors.New("oversized result"))
	}
	metadata := make(map[string]string)
	if err := json.Unmarshal([]byte(fields[fieldMetadata]), &metadata); err != nil {
		return idempotency.Record{}, recordError(err)
	}
	if err := validateMetadata(metadata); err != nil {
		return idempotency.Record{}, err
	}

	return idempotency.Record{
		Key:            key,
		Fingerprint:    fingerprint,
		State:          state,
		OwnerToken:     fields[fieldOwnerToken],
		FencingToken:   fence,
		LeaseExpiresAt: timestamps.leaseExpiresAt,
		HeartbeatAt:    timestamps.heartbeatAt,
		Attempt:        attempt,
		CreatedAt:      timestamps.createdAt,
		UpdatedAt:      timestamps.updatedAt,
		CompletedAt:    timestamps.completedAt,
		FailedAt:       timestamps.failedAt,
		AbandonedAt:    timestamps.abandonedAt,
		ExpiredAt:      timestamps.expiredAt,
		Result:         result,
		Metadata:       metadata,
	}, nil
}

func decodeAcquireReply(reply []string) (idempotency.AcquireResult, error) {
	switch len(reply) {
	case 0, 1, 2:
		return idempotency.AcquireResult{}, recordError(errors.New("malformed acquire reply"))
	}
	outcome := idempotency.Outcome(reply[0])
	if !validOutcome(outcome) {
		return idempotency.AcquireResult{}, recordError(errors.New("invalid acquire outcome"))
	}
	fields, err := decodeFieldPairs(reply[1:])
	if err != nil {
		return idempotency.AcquireResult{}, err
	}
	record, err := decodeRecord(fields)
	if err != nil {
		return idempotency.AcquireResult{}, err
	}
	return idempotency.AcquireResult{Outcome: outcome, Record: record}, nil
}

func decodeFieldPairs(values []string) (map[string]string, error) {
	fields := make(map[string]string)
	wantKey := true
	key := ""
	for _, value := range values {
		switch wantKey {
		case true:
			key = value
			wantKey = false
		case false:
			fields[key] = value
			wantKey = true
		}
	}
	switch wantKey {
	case true:
		return fields, nil
	default:
		return nil, recordError(errors.New("malformed field pairs"))
	}
}

type timestamps struct {
	leaseExpiresAt time.Time
	heartbeatAt    time.Time
	createdAt      time.Time
	updatedAt      time.Time
	completedAt    time.Time
	failedAt       time.Time
	abandonedAt    time.Time
	expiredAt      time.Time
}

func decodeTimestamps(fields map[string]string) (timestamps, error) {
	values := []*time.Time{}
	decoded := timestamps{}
	values = append(values,
		&decoded.leaseExpiresAt,
		&decoded.heartbeatAt,
		&decoded.createdAt,
		&decoded.updatedAt,
		&decoded.completedAt,
		&decoded.failedAt,
		&decoded.abandonedAt,
		&decoded.expiredAt,
	)
	names := []string{
		fieldLeaseExpiresAt,
		fieldHeartbeatAt,
		fieldCreatedAt,
		fieldUpdatedAt,
		fieldCompletedAt,
		fieldFailedAt,
		fieldAbandonedAt,
		fieldExpiredAt,
	}
	for index, name := range names {
		parsed, err := decodeTime(fields[name])
		if err != nil {
			return timestamps{}, err
		}
		*values[index] = parsed
	}
	return decoded, nil
}

func encodeTime(value time.Time) string {
	if value.IsZero() {
		return "0"
	}
	return strconv.FormatInt(value.UnixMilli(), 10)
}

func decodeTime(value string) (time.Time, error) {
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	if milliseconds == 0 {
		return time.Time{}, nil
	}
	return time.UnixMilli(milliseconds).UTC(), nil
}

func parsePositiveUint(value string) (uint64, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, err
	}
	if parsed == 0 {
		return 0, errors.New("zero counter")
	}
	return parsed, nil
}

func validateMetadata(metadata map[string]string) error {
	switch min(len(metadata), idempotency.MaxMetadataEntries) {
	case len(metadata):
	default:
		return limitError(fieldMetadata)
	}
	for key, value := range metadata {
		switch min(len(key), idempotency.MaxMetadataKeyBytes) {
		case len(key):
		default:
			return limitError("metadata_key")
		}
		switch min(len(value), idempotency.MaxMetadataValueBytes) {
		case len(value):
		default:
			return limitError("metadata_value")
		}
	}
	return nil
}

func validState(state idempotency.State) bool {
	switch state {
	case idempotency.StateAcquired, idempotency.StateRunning,
		idempotency.StateCompleted, idempotency.StateFailed,
		idempotency.StateExpired, idempotency.StateAbandoned:
		return true
	default:
		return false
	}
}

func validOutcome(outcome idempotency.Outcome) bool {
	switch outcome {
	case idempotency.OutcomeAcquired, idempotency.OutcomeReplayed,
		idempotency.OutcomeInProgress, idempotency.OutcomeConflict,
		idempotency.OutcomeStaleOwnerTakeover,
		idempotency.OutcomeTerminalFailure:
		return true
	default:
		return false
	}
}

func limitError(field string) error {
	return &idempotency.Error{
		Reason: idempotency.ReasonLimitExceeded,
		Field:  field,
	}
}

func recordError(cause error) error {
	return &idempotency.Error{
		Reason: idempotency.ReasonInvalidPayload,
		Field:  "record",
		Cause:  cause,
	}
}

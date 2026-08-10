package audit

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const canonicalSchemaVersion = 1

type canonicalActor struct {
	Kind                 ActorKind       `json:"kind"`
	ID                   string          `json:"id,omitempty"`
	AuthenticationMethod string          `json:"authentication_method,omitempty"`
	DelegatedBy          *canonicalActor `json:"delegated_by,omitempty"`
}

type canonicalSubject struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

type canonicalContext struct {
	TenantID      string `json:"tenant_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	TraceID       string `json:"trace_id,omitempty"`
	IdempotencyID string `json:"idempotency_id,omitempty"`
	SourceService string `json:"source_service,omitempty"`
	SourceVersion string `json:"source_version,omitempty"`
	Environment   string `json:"environment,omitempty"`
	NetworkOrigin string `json:"network_origin,omitempty"`
	UserAgent     string `json:"user_agent,omitempty"`
}

type canonicalChanges struct {
	NoChange bool              `json:"no_change"`
	Redacted bool              `json:"redacted,omitempty"`
	Before   map[string]string `json:"before,omitempty"`
	After    map[string]string `json:"after,omitempty"`
}

type canonicalPolicy struct {
	ID      string `json:"id,omitempty"`
	Version string `json:"version,omitempty"`
}

type canonicalIntegrity struct {
	Algorithm      IntegrityAlgorithm `json:"algorithm,omitempty"`
	Partition      string             `json:"partition,omitempty"`
	KeyID          string             `json:"key_id,omitempty"`
	Sequence       uint64             `json:"sequence,omitempty"`
	PreviousDigest string             `json:"previous_digest,omitempty"`
	Digest         string             `json:"digest,omitempty"`
}

type canonicalRecord struct {
	SchemaVersion int                `json:"schema_version"`
	ID            string             `json:"id"`
	OccurredAt    string             `json:"occurred_at"`
	RecordedAt    string             `json:"recorded_at"`
	Action        string             `json:"action"`
	Outcome       Outcome            `json:"outcome"`
	ReasonCode    string             `json:"reason_code,omitempty"`
	Description   string             `json:"description,omitempty"`
	Actor         canonicalActor     `json:"actor"`
	Subject       canonicalSubject   `json:"subject"`
	Context       canonicalContext   `json:"context"`
	Changes       canonicalChanges   `json:"changes"`
	Policy        canonicalPolicy    `json:"policy"`
	Attributes    map[string]string  `json:"attributes,omitempty"`
	Integrity     canonicalIntegrity `json:"integrity"`
}

// CanonicalJSON returns the versioned deterministic bytes used by integrity,
// persistence, export, and cross-adapter comparison. Struct field order is
// fixed and encoding/json sorts every map key lexicographically.
func CanonicalJSON(record Record) ([]byte, error) {
	return json.Marshal(toCanonical(record))
}

// ParseCanonicalJSON strictly decodes one versioned canonical record and
// reapplies all current size, identity, and privacy validation before taking
// ownership of its mutable fields.
func ParseCanonicalJSON(encoded []byte, limits Limits) (Record, error) {
	if err := limits.Validate(); err != nil {
		return Record{}, err
	}
	if len(encoded) == 0 || len(encoded) > limits.MaxRecordBytes {
		return Record{}, invalid("canonical_record", "exceeds byte limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var value canonicalRecord
	if err := decoder.Decode(&value); err != nil {
		return Record{}, fmt.Errorf("%w: decode canonical record", ErrInvalidArgument)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Record{}, err
	}
	if value.SchemaVersion != canonicalSchemaVersion {
		return Record{}, invalid("schema_version", "is unsupported")
	}
	occurredAt, err := time.Parse(canonicalTimeLayout, value.OccurredAt)
	if err != nil {
		return Record{}, invalid("occurred_at", "is malformed")
	}
	recordedAt, err := time.Parse(canonicalTimeLayout, value.RecordedAt)
	if err != nil {
		return Record{}, invalid("recorded_at", "is malformed")
	}
	previous, err := decodeDigest(value.Integrity.PreviousDigest, limits.MaxIntegrityBytes)
	if err != nil {
		return Record{}, err
	}
	digest, err := decodeDigest(value.Integrity.Digest, limits.MaxIntegrityBytes)
	if err != nil {
		return Record{}, err
	}
	input := RecordInput{
		OccurredAt: occurredAt, Action: value.Action, Outcome: value.Outcome,
		ReasonCode: value.ReasonCode, Description: value.Description,
		Actor:   actorInput(value.Actor),
		Subject: SubjectInput{Type: value.Subject.Type, ID: value.Subject.ID, Deleted: value.Subject.Deleted},
		Context: ContextInput{
			TenantID: value.Context.TenantID, CorrelationID: value.Context.CorrelationID,
			CausationID: value.Context.CausationID, RequestID: value.Context.RequestID,
			TraceID: value.Context.TraceID, IdempotencyID: value.Context.IdempotencyID,
			SourceService: value.Context.SourceService, SourceVersion: value.Context.SourceVersion,
			Environment: value.Context.Environment, NetworkOrigin: value.Context.NetworkOrigin,
			UserAgent: value.Context.UserAgent,
		},
		Changes:    ChangeSetInput{NoChange: value.Changes.NoChange, Redacted: value.Changes.Redacted, Before: value.Changes.Before, After: value.Changes.After},
		Policy:     PolicyMetadata{PolicyID: value.Policy.ID, Version: value.Policy.Version},
		Attributes: value.Attributes,
		Integrity: IntegrityInput{Algorithm: value.Integrity.Algorithm, Partition: value.Integrity.Partition,
			KeyID: value.Integrity.KeyID, Sequence: value.Integrity.Sequence,
			PreviousDigest: previous, Digest: digest},
	}
	if err := validateInput(value.ID, canonicalTime(recordedAt), input, limits); err != nil {
		return Record{}, err
	}
	record := recordFromInput(value.ID, canonicalTime(recordedAt), input)
	canonical, _ := CanonicalJSON(record)
	if !bytes.Equal(encoded, canonical) {
		return Record{}, invalid("canonical_record", "is not canonical")
	}
	return record, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	}
	return invalid("canonical_record", "contains trailing data")
}

func decodeDigest(value string, maximum int) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > maximum*2 {
		return nil, invalid("digest", "exceeds byte limit")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, invalid("digest", "is malformed")
	}
	return decoded, nil
}

func actorInput(value canonicalActor) ActorInput {
	result := ActorInput{Kind: value.Kind, ID: value.ID, AuthenticationMethod: value.AuthenticationMethod}
	if value.DelegatedBy != nil {
		delegated := actorInput(*value.DelegatedBy)
		result.DelegatedBy = &delegated
	}
	return result
}

func toCanonical(record Record) canonicalRecord {
	return canonicalRecord{
		SchemaVersion: canonicalSchemaVersion,
		ID:            record.id,
		OccurredAt:    record.occurredAt.Format(canonicalTimeLayout),
		RecordedAt:    record.recordedAt.Format(canonicalTimeLayout),
		Action:        record.action,
		Outcome:       record.outcome,
		ReasonCode:    record.reasonCode,
		Description:   record.description,
		Actor:         canonicalizeActor(record.actor),
		Subject:       canonicalSubject{record.subject.resourceType, record.subject.id, record.subject.deleted},
		Context: canonicalContext{
			record.context.tenantID, record.context.correlationID, record.context.causationID,
			record.context.requestID, record.context.traceID, record.context.idempotencyID,
			record.context.sourceService, record.context.sourceVersion, record.context.environment,
			record.context.networkOrigin, record.context.userAgent,
		},
		Changes: canonicalChanges{
			NoChange: record.changes.noChange,
			Redacted: record.changes.redacted,
			Before:   cloneMap(record.changes.before),
			After:    cloneMap(record.changes.after),
		},
		Policy:     canonicalPolicy{record.policy.PolicyID, record.policy.Version},
		Attributes: cloneMap(record.attributes),
		Integrity: canonicalIntegrity{
			record.integrity.algorithm,
			record.integrity.partition,
			record.integrity.keyID,
			record.integrity.sequence,
			hex.EncodeToString(record.integrity.previousDigest),
			hex.EncodeToString(record.integrity.digest),
		},
	}
}

func canonicalizeActor(actor Actor) canonicalActor {
	result := canonicalActor{
		Kind:                 actor.kind,
		ID:                   actor.id,
		AuthenticationMethod: actor.authenticationMethod,
	}
	if actor.delegatedBy != nil {
		delegated := canonicalizeActor(*actor.delegatedBy)
		result.DelegatedBy = &delegated
	}
	return result
}

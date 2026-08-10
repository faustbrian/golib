package search

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	ErrBulkLimit         = errors.New("search: bulk operation exceeds item or byte limit")
	ErrTenantMismatch    = errors.New("search: bulk operation crosses tenant boundary")
	ErrInvalidOperation  = errors.New("search: invalid write operation")
	ErrInvalidBulkResult = errors.New("search: invalid bulk result")
)

// WriteAction identifies a document mutation.
type WriteAction string

const (
	ActionIndex  WriteAction = "index"
	ActionUpdate WriteAction = "update"
	ActionUpsert WriteAction = "upsert"
	ActionDelete WriteAction = "delete"
)

// RefreshPolicy makes post-write visibility explicit.
type RefreshPolicy string

const (
	RefreshNone      RefreshPolicy = "none"
	RefreshWaitFor   RefreshPolicy = "wait_for"
	RefreshImmediate RefreshPolicy = "immediate"
)

// WriteOperation is a source-versioned mutation. Source is absent only for a
// delete. Index/update/upsert sources remain caller-independent copies.
type WriteOperation struct {
	Action  WriteAction
	Tenant  string
	Index   string
	ID      string
	Version uint64
	Source  json.RawMessage
}

func IndexDocument(document Document) WriteOperation {
	return operationFromDocument(ActionIndex, document)
}
func UpdateDocument(document Document) WriteOperation {
	return operationFromDocument(ActionUpdate, document)
}
func UpsertDocument(document Document) WriteOperation {
	return operationFromDocument(ActionUpsert, document)
}
func DeleteDocument(tenant, index, id string, version uint64) WriteOperation {
	return WriteOperation{Action: ActionDelete, Tenant: tenant, Index: index, ID: id, Version: version}
}

func operationFromDocument(action WriteAction, document Document) WriteOperation {
	return WriteOperation{Action: action, Tenant: document.Tenant, Index: document.Index, ID: document.ID, Version: document.Version, Source: append(json.RawMessage(nil), document.Source...)}
}

// BulkRequest is one bounded, single-tenant write unit. A backend may partially
// apply it; callers must inspect every returned item.
type BulkRequest struct {
	Operations []WriteOperation
	Refresh    RefreshPolicy
}

// Validate checks external versioning, tenant isolation, and encoded source
// bounds before network execution.
func (r BulkRequest) Validate(capabilities Capabilities, limits Limits) error {
	if limits.Validate() != nil {
		return ErrBulkLimit
	}
	if !capabilities.ExternalVersion || !capabilities.BulkPartialOutcomes {
		return ErrUnsupported
	}
	if len(r.Operations) == 0 || len(r.Operations) > limits.MaxBulkItems {
		return ErrBulkLimit
	}
	if r.Refresh != RefreshNone && r.Refresh != RefreshWaitFor && r.Refresh != RefreshImmediate {
		return ErrInvalidOperation
	}
	tenant := r.Operations[0].Tenant
	remainingBytes := limits.MaxBulkBytes
	for _, operation := range r.Operations {
		if operation.Action == ActionUpdate && !capabilities.UpdateExisting {
			return unsupported("update existing")
		}
		if operation.Tenant != tenant {
			return ErrTenantMismatch
		}
		if err := operation.validate(limits); err != nil {
			return err
		}
		for _, size := range []int{len(operation.Tenant), len(operation.Index), len(operation.ID), len(operation.Source), 64} {
			if size > remainingBytes {
				return ErrBulkLimit
			}
			remainingBytes -= size
		}
	}
	return nil
}

func (o WriteOperation) validate(limits Limits) error {
	if o.Tenant == "" || len(o.Tenant) > limits.MaxTenantBytes || o.Index == "" || len(o.Index) > limits.MaxIndexBytes ||
		o.ID == "" || len(o.ID) > limits.MaxIDBytes || o.Version == 0 {
		return ErrInvalidOperation
	}
	switch o.Action {
	case ActionIndex, ActionUpdate, ActionUpsert:
		trimmed := bytesTrimSpace(o.Source)
		if len(o.Source) == 0 || len(o.Source) > limits.MaxSourceBytes || len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' || !json.Valid(trimmed) {
			return ErrInvalidOperation
		}
	case ActionDelete:
		if len(o.Source) != 0 {
			return ErrInvalidOperation
		}
	default:
		return ErrInvalidOperation
	}
	return nil
}

func bytesTrimSpace(value []byte) []byte {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\n' || value[start] == '\r' || value[start] == '\t') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\n' || value[end-1] == '\r' || value[end-1] == '\t') {
		end--
	}
	return value[start:end]
}

// OutcomeState classifies each write without flattening partial or ambiguous
// bulk execution into a Boolean.
type OutcomeState string

const (
	OutcomeApplied         OutcomeState = "applied"
	OutcomeNotFound        OutcomeState = "not_found"
	OutcomeVersionConflict OutcomeState = "version_conflict"
	OutcomeRejected        OutcomeState = "rejected"
	OutcomeFailed          OutcomeState = "failed"
	OutcomeUnknown         OutcomeState = "unknown"
)

type ItemOutcome struct {
	Position   int
	ID         string
	Action     WriteAction
	State      OutcomeState
	Version    uint64
	Code       string
	Diagnostic string
	Retryable  bool
}

type BulkResult struct{ items []ItemOutcome }

// NewBulkResult validates positional attribution and preserves every outcome.
func NewBulkResult(items []ItemOutcome) (BulkResult, error) {
	if len(items) == 0 {
		return BulkResult{}, ErrInvalidBulkResult
	}
	copyItems := append([]ItemOutcome(nil), items...)
	for position, item := range copyItems {
		if item.Position != position || item.ID == "" || !validAction(item.Action) || !validOutcome(item.State) {
			return BulkResult{}, ErrInvalidBulkResult
		}
	}
	return BulkResult{items: copyItems}, nil
}

func (r BulkResult) Items() []ItemOutcome { return append([]ItemOutcome(nil), r.items...) }
func (r BulkResult) Partial() bool {
	for _, item := range r.items {
		if item.State != OutcomeApplied {
			return true
		}
	}
	return false
}
func (r BulkResult) HasUnknown() bool {
	for _, item := range r.items {
		if item.State == OutcomeUnknown {
			return true
		}
	}
	return false
}

func validAction(action WriteAction) bool {
	return action == ActionIndex || action == ActionUpdate || action == ActionUpsert || action == ActionDelete
}
func validOutcome(state OutcomeState) bool {
	return state == OutcomeApplied || state == OutcomeNotFound || state == OutcomeVersionConflict || state == OutcomeRejected || state == OutcomeFailed || state == OutcomeUnknown
}

// Indexer is the context-aware document write boundary implemented by adapters.
type Indexer interface {
	Write(context.Context, WriteOperation, RefreshPolicy) (ItemOutcome, error)
	Bulk(context.Context, BulkRequest) (BulkResult, error)
}

package search

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

var ErrInvalidProjectionEvent = errors.New("search: invalid projection event")

type ProjectionKind string

const (
	ProjectionUpsert ProjectionKind = "upsert"
	ProjectionDelete ProjectionKind = "delete"
)

// ProjectionEvent is a deterministic outbox/event boundary. Idempotency is
// provided by its stable external version and caller-owned outbox key, not by
// claiming exactly-once delivery.
type ProjectionEvent struct {
	tenant, index, id string
	version           uint64
	kind              ProjectionKind
	source            json.RawMessage
	idempotencyKey    string
}

func NewProjectionEvent(tenant, index, id string, version uint64, kind ProjectionKind, source json.RawMessage, idempotencyKey string, limits Limits) (ProjectionEvent, error) {
	if limits.Validate() != nil || idempotencyKey == "" || len(idempotencyKey) > limits.MaxIDBytes ||
		!utf8.ValidString(tenant) || !utf8.ValidString(index) || !utf8.ValidString(id) || !utf8.ValidString(idempotencyKey) {
		return ProjectionEvent{}, ErrInvalidProjectionEvent
	}
	switch kind {
	case ProjectionUpsert:
		document, err := NewDocument(tenant, index, id, version, source, limits)
		if err != nil {
			return ProjectionEvent{}, errors.Join(ErrInvalidProjectionEvent, err)
		}
		return ProjectionEvent{tenant: tenant, index: index, id: id, version: version, kind: kind, source: document.Source, idempotencyKey: idempotencyKey}, nil
	case ProjectionDelete:
		if len(source) != 0 || tenant == "" || index == "" || id == "" || version == 0 || len(tenant) > limits.MaxTenantBytes || len(index) > limits.MaxIndexBytes || len(id) > limits.MaxIDBytes {
			return ProjectionEvent{}, ErrInvalidProjectionEvent
		}
		return ProjectionEvent{tenant: tenant, index: index, id: id, version: version, kind: kind, idempotencyKey: idempotencyKey}, nil
	default:
		return ProjectionEvent{}, ErrInvalidProjectionEvent
	}
}

func (event ProjectionEvent) IdempotencyKey() string { return event.idempotencyKey }
func (event ProjectionEvent) Operation() WriteOperation {
	if event.kind == ProjectionDelete {
		return DeleteDocument(event.tenant, event.index, event.id, event.version)
	}
	return WriteOperation{Action: ActionUpsert, Tenant: event.tenant, Index: event.index, ID: event.id, Version: event.version, Source: append(json.RawMessage(nil), event.source...)}
}

// ProjectionOutbox is implemented by an application's source-of-truth
// transaction boundary. The search package never opens or owns that
// transaction.
type ProjectionOutbox interface {
	Enqueue(context.Context, ProjectionEvent) error
}

// ProjectionConsumer applies at-least-once events through external versions.
// It returns conflicts and unknown outcomes to the caller for policy decisions.
type ProjectionConsumer struct{ indexer Indexer }

func NewProjectionConsumer(indexer Indexer) (*ProjectionConsumer, error) {
	if indexer == nil {
		return nil, ErrInvalidProjectionEvent
	}
	return &ProjectionConsumer{indexer: indexer}, nil
}
func (consumer *ProjectionConsumer) Handle(ctx context.Context, event ProjectionEvent, refresh RefreshPolicy) (ItemOutcome, error) {
	return consumer.indexer.Write(ctx, event.Operation(), refresh)
}

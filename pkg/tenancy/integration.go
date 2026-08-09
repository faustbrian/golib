package tenancy

import (
	"context"
	"encoding/binary"
	"errors"
)

// ErrInvalidIntegration reports an unknown or incomplete integration boundary.
var ErrInvalidIntegration = errors.New("tenancy: invalid integration")

// Boundary names a first-party propagation and namespace integration.
type Boundary string

const (
	// BoundaryQueue identifies queued deliveries and retries.
	BoundaryQueue Boundary = "queue"
	// BoundaryOutbox identifies transactional outbox records.
	BoundaryOutbox Boundary = "outbox"
	// BoundaryKafka identifies Kafka records.
	BoundaryKafka Boundary = "kafka"
	// BoundaryCloudEvents identifies CloudEvents attributes and extensions.
	BoundaryCloudEvents Boundary = "cloudevents"
	// BoundaryAudit identifies audit records.
	BoundaryAudit Boundary = "audit"
	// BoundaryCorrelation identifies correlation integration metadata.
	BoundaryCorrelation Boundary = "correlation"
	// BoundaryIdempotency identifies idempotency records.
	BoundaryIdempotency Boundary = "idempotency"
	// BoundaryCache identifies cache keys.
	BoundaryCache Boundary = "cache"
	// BoundaryRateLimit identifies rate-limit keys.
	BoundaryRateLimit Boundary = "rate-limit"
	// BoundarySearch identifies search indexes and documents.
	BoundarySearch Boundary = "search"
	// BoundaryScheduler identifies scheduled executions.
	BoundaryScheduler Boundary = "scheduler"
	// BoundaryWorkflow identifies workflow executions.
	BoundaryWorkflow Boundary = "workflow"
	// BoundaryEventSourcing identifies event streams and aggregates.
	BoundaryEventSourcing Boundary = "event-sourcing"
	// BoundaryTelemetry identifies telemetry attributes and links.
	BoundaryTelemetry Boundary = "telemetry"
)

// Integration gives queue, outbox, Kafka, CloudEvents, audit, correlation,
// idempotency, cache, rate-limit, search, scheduler, workflow, event-sourcing,
// and telemetry adapters one small explicit contract.
type Integration struct {
	boundary Boundary
	codec    *PropagationCodec
}

// NewIntegration validates a semantic boundary and propagation policy.
func NewIntegration(boundary Boundary, options PropagationOptions) (*Integration, error) {
	if !boundary.valid() {
		return nil, ErrInvalidIntegration
	}
	codec, err := NewPropagationCodec(options)
	if err != nil {
		return nil, ErrInvalidIntegration
	}
	return &Integration{boundary: boundary, codec: codec}, nil
}

// Boundary returns the immutable semantic integration name.
func (integration *Integration) Boundary() Boundary {
	if integration == nil {
		return ""
	}
	return integration.boundary
}

// Send injects the tenant-bound scope required from ctx.
func (integration *Integration) Send(ctx context.Context, carrier Carrier) error {
	if !integration.valid() {
		return ErrInvalidIntegration
	}
	return integration.codec.InjectFromContext(carrier, ctx)
}

// Receive accepts tenant metadata only after trusted is explicitly supplied.
func (integration *Integration) Receive(
	ctx context.Context,
	carrier Carrier,
	trusted bool,
) (context.Context, error) {
	if !integration.valid() {
		return nil, ErrInvalidIntegration
	}
	return integration.codec.Accept(ctx, carrier, trusted)
}

// Key creates a boundary-separated opaque namespace for scope and logicalKey.
func (integration *Integration) Key(
	encoder *NamespaceEncoder,
	scope Scope,
	logicalKey string,
) (string, error) {
	if !integration.valid() {
		return "", ErrInvalidIntegration
	}
	if logicalKey == "" {
		return "", ErrInvalidNamespaceInput
	}
	return encoder.Encode(
		scope,
		integration.boundary.namespaceDomain(),
		integrationKey(integration.boundary, logicalKey),
	)
}

func (integration *Integration) valid() bool {
	return integration != nil && integration.codec != nil && integration.boundary.valid()
}

func (boundary Boundary) valid() bool {
	switch boundary {
	case BoundaryQueue, BoundaryOutbox, BoundaryKafka, BoundaryCloudEvents,
		BoundaryAudit, BoundaryCorrelation, BoundaryIdempotency, BoundaryCache,
		BoundaryRateLimit, BoundarySearch, BoundaryScheduler, BoundaryWorkflow,
		BoundaryEventSourcing, BoundaryTelemetry:
		return true
	default:
		return false
	}
}

func (boundary Boundary) namespaceDomain() NamespaceDomain {
	switch boundary {
	case BoundaryQueue:
		return NamespaceQueue
	case BoundaryIdempotency:
		return NamespaceIdempotency
	case BoundaryCache:
		return NamespaceCache
	case BoundaryRateLimit:
		return NamespaceRateLimit
	case BoundarySearch:
		return NamespaceSearch
	case BoundaryScheduler:
		return NamespaceScheduler
	case BoundaryWorkflow, BoundaryCorrelation:
		return NamespaceWorkflow
	case BoundaryTelemetry, BoundaryAudit:
		return NamespaceTelemetry
	default:
		return NamespaceEvent
	}
}

func integrationKey(boundary Boundary, logicalKey string) string {
	var result []byte
	result = appendLengthPrefixed(result, string(boundary))
	return string(appendLengthPrefixed(result, logicalKey))
}

func appendLengthPrefixed(target []byte, value string) []byte {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	target = append(target, size[:]...)
	return append(target, value...)
}

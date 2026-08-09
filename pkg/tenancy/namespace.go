package tenancy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
)

const (
	minimumNamespaceKeyBytes = 32
	maximumNamespaceKeyBytes = 1024
	maximumNamespacePart     = 4096
	namespaceVersion         = "tenancy-namespace-v1"
)

var (
	// ErrInvalidNamespaceKey reports a missing, weak, or oversized HMAC key.
	ErrInvalidNamespaceKey = errors.New("tenancy: invalid namespace key")
	// ErrInvalidNamespaceInput reports invalid scope, domain, or logical key.
	ErrInvalidNamespaceInput = errors.New("tenancy: invalid namespace input")
)

// NamespaceDomain separates independently owned isolation key spaces.
type NamespaceDomain string

const (
	// NamespaceCache isolates cache entries.
	NamespaceCache NamespaceDomain = "cache"
	// NamespaceIdempotency isolates idempotency records.
	NamespaceIdempotency NamespaceDomain = "idempotency"
	// NamespaceRateLimit isolates rate-limit counters.
	NamespaceRateLimit NamespaceDomain = "rate-limit"
	// NamespaceSearch isolates search indexes and documents.
	NamespaceSearch NamespaceDomain = "search"
	// NamespaceQueue isolates queue routing and deduplication keys.
	NamespaceQueue NamespaceDomain = "queue"
	// NamespaceScheduler isolates scheduled work.
	NamespaceScheduler NamespaceDomain = "scheduler"
	// NamespaceEvent isolates event streams and event identities.
	NamespaceEvent NamespaceDomain = "event"
	// NamespaceWorkflow isolates workflow executions.
	NamespaceWorkflow NamespaceDomain = "workflow"
	// NamespaceTelemetry produces opaque telemetry attributes.
	NamespaceTelemetry NamespaceDomain = "telemetry"
)

// NamespaceEncoder creates opaque, collision-resistant, versioned keys. It
// owns a copy of its HMAC key and is safe for concurrent use.
type NamespaceEncoder struct {
	key []byte
}

// NewNamespaceEncoder validates and copies a secret HMAC key.
func NewNamespaceEncoder(key []byte) (*NamespaceEncoder, error) {
	if len(key) < minimumNamespaceKeyBytes || len(key) > maximumNamespaceKeyBytes {
		return nil, ErrInvalidNamespaceKey
	}
	return &NamespaceEncoder{key: append([]byte(nil), key...)}, nil
}

// Encode composes scope, domain, and key with length-delimited HMAC input. Raw
// tenant IDs and logical keys never appear in the returned namespace.
func (encoder *NamespaceEncoder) Encode(scope Scope, domain NamespaceDomain, key string) (string, error) {
	if encoder == nil || len(encoder.key) < minimumNamespaceKeyBytes ||
		!scope.Valid() || !domain.valid() || len(key) == 0 || len(key) > maximumNamespacePart {
		return "", ErrInvalidNamespaceInput
	}
	digest := hmac.New(sha256.New, encoder.key)
	writeNamespacePart(digest, namespaceVersion)
	digest.Write([]byte{byte(scope.Kind())})
	writeNamespacePart(digest, scope.TenantID().Value())
	writeNamespacePart(digest, string(domain))
	writeNamespacePart(digest, key)
	return "tn1_" + base64.RawURLEncoding.EncodeToString(digest.Sum(nil)), nil
}

func (domain NamespaceDomain) valid() bool {
	switch domain {
	case NamespaceCache, NamespaceIdempotency, NamespaceRateLimit,
		NamespaceSearch, NamespaceQueue, NamespaceScheduler, NamespaceEvent,
		NamespaceWorkflow, NamespaceTelemetry:
		return true
	default:
		return false
	}
}

type namespaceWriter interface {
	Write([]byte) (int, error)
}

func writeNamespacePart(writer namespaceWriter, value string) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = writer.Write(size[:])
	_, _ = writer.Write([]byte(value))
}

package search

import (
	"errors"
	"time"
)

var ErrInvalidLimits = errors.New("search: limits must be positive")

// Limits bounds caller-controlled identifiers, payloads, traversal, and
// response decoding. Adapters may advertise stricter limits.
type Limits struct {
	MaxTenantBytes    int
	MaxIndexBytes     int
	MaxIDBytes        int
	MaxSourceBytes    int
	MaxQueryBytes     int
	MaxBulkItems      int
	MaxBulkBytes      int
	MaxPageItems      int
	MaxPages          int
	MaxResultBytes    int64
	MaxCursorDuration time.Duration
	MaxQueryDepth     int
	MaxQueryClauses   int
	MaxJSONDepth      int
	MaxJSONNodes      int
}

// DefaultLimits returns conservative bounded defaults.
func DefaultLimits() Limits {
	return Limits{
		MaxTenantBytes:    128,
		MaxIndexBytes:     255,
		MaxIDBytes:        512,
		MaxSourceBytes:    1 << 20,
		MaxQueryBytes:     1 << 20,
		MaxBulkItems:      500,
		MaxBulkBytes:      5 << 20,
		MaxPageItems:      1_000,
		MaxPages:          1_000,
		MaxResultBytes:    10 << 20,
		MaxCursorDuration: 15 * time.Minute,
		MaxQueryDepth:     32,
		MaxQueryClauses:   1_024,
		MaxJSONDepth:      32,
		MaxJSONNodes:      4_096,
	}
}

// Validate rejects configurations that leave a resource unbounded.
func (l Limits) Validate() error {
	if l.MaxTenantBytes <= 0 || l.MaxIndexBytes <= 0 || l.MaxIDBytes <= 0 ||
		l.MaxSourceBytes <= 0 || l.MaxQueryBytes <= 0 || l.MaxBulkItems <= 0 || l.MaxBulkBytes <= 0 ||
		l.MaxPageItems <= 0 || l.MaxPages <= 0 || l.MaxResultBytes <= 0 ||
		l.MaxCursorDuration <= 0 ||
		l.MaxQueryDepth <= 0 || l.MaxQueryClauses <= 0 || l.MaxJSONDepth <= 0 ||
		l.MaxJSONNodes <= 0 || l.MaxPages > int(^uint(0)>>1)/l.MaxPageItems {
		return ErrInvalidLimits
	}

	return nil
}

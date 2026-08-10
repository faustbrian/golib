package search

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalidResult = errors.New("search: invalid backend result")

type TotalRelation string

const (
	TotalExact      TotalRelation = "eq"
	TotalLowerBound TotalRelation = "gte"
)

type Total struct {
	Value    uint64
	Relation TotalRelation
}

// Hit owns all returned bytes and slices. Score is backend-provided relevance
// and is intentionally not claimed to be portable across adapters.
type Hit struct {
	Index      string
	ID         string
	Version    uint64
	Score      *float64
	Source     json.RawMessage
	SortValues []json.RawMessage
	Highlights map[string][]string
}

type Failure struct {
	Scope, Code, Message string
	Retryable            bool
}
type ShardDiagnostics struct{ Total, Successful, Skipped, Failed int }
type Diagnostics struct {
	Backend   string
	RequestID string
	Took      time.Duration
	TimedOut  bool
	Partial   bool
	Shards    ShardDiagnostics
	Failures  []Failure
	Warnings  []string
}

// Result retains partial-result diagnostics rather than converting a successful
// HTTP status into an unconditional success claim.
type Result struct {
	hits         []Hit
	total        Total
	aggregations map[string]json.RawMessage
	suggestions  map[string]json.RawMessage
	diagnostics  Diagnostics
	nextCursor   string
}

func NewResult(hits []Hit, total Total, aggregations, suggestions map[string]json.RawMessage, diagnostics Diagnostics, nextCursor string) (Result, error) {
	if total.Relation != TotalExact && total.Relation != TotalLowerBound || total.Value < uint64(len(hits)) {
		return Result{}, ErrInvalidResult
	}
	copyHits := make([]Hit, len(hits))
	for index, hit := range hits {
		if hit.Index == "" || hit.ID == "" || hit.Version == 0 || hit.Score != nil && (math.IsNaN(*hit.Score) || math.IsInf(*hit.Score, 0)) {
			return Result{}, ErrInvalidResult
		}
		if len(hit.Source) > 0 && !json.Valid(hit.Source) {
			return Result{}, ErrInvalidResult
		}
		for _, value := range hit.SortValues {
			if !json.Valid(value) {
				return Result{}, ErrInvalidResult
			}
		}
		copyHits[index] = cloneHit(hit)
	}
	if !validRawResultMap(aggregations) || !validRawResultMap(suggestions) || !validDiagnostics(diagnostics) {
		return Result{}, ErrInvalidResult
	}

	return Result{
		hits: copyHits, total: total, aggregations: cloneRawMap(aggregations), suggestions: cloneRawMap(suggestions),
		diagnostics: cloneDiagnostics(diagnostics), nextCursor: nextCursor,
	}, nil
}

func validRawResultMap(values map[string]json.RawMessage) bool {
	for name, value := range values {
		if !validField(name) || !json.Valid(value) {
			return false
		}
	}
	return true
}

func validDiagnostics(value Diagnostics) bool {
	shards := value.Shards
	if value.Took < 0 || !validDiagnosticIdentifier(value.Backend, MaxFieldNameBytes, true) ||
		!validDiagnosticIdentifier(value.RequestID, DefaultLimits().MaxIDBytes, false) ||
		shards.Total < 0 || shards.Successful < 0 || shards.Skipped < 0 || shards.Failed < 0 ||
		shards.Successful > shards.Total || shards.Skipped > shards.Total-shards.Successful ||
		shards.Failed != shards.Total-shards.Successful-shards.Skipped ||
		(value.TimedOut || shards.Failed > 0 || len(value.Failures) > 0) && !value.Partial {
		return false
	}
	for _, failure := range value.Failures {
		if !validDiagnosticIdentifier(failure.Scope, MaxFieldNameBytes, true) ||
			!validDiagnosticIdentifier(failure.Code, MaxFieldNameBytes, true) || !validDiagnosticText(failure.Message) {
			return false
		}
	}
	for _, warning := range value.Warnings {
		if !validDiagnosticText(warning) {
			return false
		}
	}
	return true
}

func validDiagnosticIdentifier(value string, maximumBytes int, required bool) bool {
	if value == "" {
		return !required
	}
	return len(value) <= maximumBytes && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

func validDiagnosticText(value string) bool {
	return len(value) <= DefaultLimits().MaxSourceBytes && utf8.ValidString(value) && !strings.ContainsRune(value, '\x00')
}

func (r Result) Hits() []Hit {
	result := make([]Hit, len(r.hits))
	for index, hit := range r.hits {
		result[index] = cloneHit(hit)
	}
	return result
}
func (r Result) Total() Total                             { return r.total }
func (r Result) Aggregations() map[string]json.RawMessage { return cloneRawMap(r.aggregations) }
func (r Result) Suggestions() map[string]json.RawMessage  { return cloneRawMap(r.suggestions) }
func (r Result) Diagnostics() Diagnostics                 { return cloneDiagnostics(r.diagnostics) }
func (r Result) NextCursor() string                       { return r.nextCursor }

func cloneHit(hit Hit) Hit {
	hit.Source = append(json.RawMessage(nil), hit.Source...)
	hit.SortValues = cloneRawMessages(hit.SortValues)
	if hit.Score != nil {
		score := *hit.Score
		hit.Score = &score
	}
	if hit.Highlights != nil {
		result := make(map[string][]string, len(hit.Highlights))
		for field, fragments := range hit.Highlights {
			result[field] = append([]string(nil), fragments...)
		}
		hit.Highlights = result
	}
	return hit
}

func cloneRawMap(values map[string]json.RawMessage) map[string]json.RawMessage {
	if values == nil {
		return nil
	}
	result := make(map[string]json.RawMessage, len(values))
	for name, value := range values {
		result[name] = append(json.RawMessage(nil), value...)
	}
	return result
}

func cloneDiagnostics(value Diagnostics) Diagnostics {
	value.Failures = append([]Failure(nil), value.Failures...)
	value.Warnings = append([]string(nil), value.Warnings...)
	return value
}

// Searcher is the context-aware query boundary implemented by adapters.
type Searcher interface {
	Capabilities(context.Context) (Capabilities, error)
	Search(context.Context, Request) (Result, error)
}

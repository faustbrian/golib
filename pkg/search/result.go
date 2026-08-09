package search

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"
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
	if total.Relation != "" && total.Relation != TotalExact && total.Relation != TotalLowerBound {
		return Result{}, ErrInvalidResult
	}
	copyHits := make([]Hit, len(hits))
	for index, hit := range hits {
		if hit.Index == "" || hit.ID == "" || hit.Score != nil && (math.IsNaN(*hit.Score) || math.IsInf(*hit.Score, 0)) {
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
	if diagnostics.Took < 0 || diagnostics.Shards.Total < 0 || diagnostics.Shards.Successful < 0 ||
		diagnostics.Shards.Skipped < 0 || diagnostics.Shards.Failed < 0 ||
		diagnostics.Shards.Total != 0 && diagnostics.Shards.Successful+diagnostics.Shards.Skipped+diagnostics.Shards.Failed != diagnostics.Shards.Total {
		return Result{}, ErrInvalidResult
	}

	return Result{
		hits: copyHits, total: total, aggregations: cloneRawMap(aggregations), suggestions: cloneRawMap(suggestions),
		diagnostics: cloneDiagnostics(diagnostics), nextCursor: nextCursor,
	}, nil
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

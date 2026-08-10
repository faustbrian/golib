package search

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidQuery = errors.New("search: invalid query")
	ErrUnsupported  = errors.New("search: unsupported capability")
	ErrUnstableSort = errors.New("search: cursor pagination requires a final _id sort")
	ErrPageLimit    = errors.New("search: pagination limit exceeded")
)

// Capabilities declares adapter behavior. False means the adapter must reject
// the feature before network execution rather than silently degrading it.
type Capabilities struct {
	Boolean, Term, FullText, Prefix, Range, Exists, Geo  bool
	Cursor, PointInTime, Offset                          bool
	Projection, Highlight, Aggregation, Suggestion       bool
	ExternalVersion, UpdateExisting, BulkPartialOutcomes bool
	Lifecycle, Templates, RawExtensions                  bool
}

// AllCapabilities returns the complete shared capability set, primarily for
// conformance tests. Production adapters should report only proven features.
func AllCapabilities() Capabilities {
	return Capabilities{
		Boolean: true, Term: true, FullText: true, Prefix: true, Range: true,
		Exists: true, Geo: true, Cursor: true, PointInTime: true, Offset: true,
		Projection: true, Highlight: true, Aggregation: true, Suggestion: true,
		ExternalVersion: true, UpdateExisting: true, BulkPartialOutcomes: true, Lifecycle: true,
		Templates: true, RawExtensions: true,
	}
}

// Query is a typed query node supported by capability-aware adapters.
type Query interface{ queryNode() }

type MatchAllQuery struct{}

func (MatchAllQuery) queryNode() {}

type BoolQuery struct {
	Must, Should, Filter, MustNot []Query
	MinimumShouldMatch            int
}

func (BoolQuery) queryNode() {}

type TermQuery struct {
	Field string
	Value Value
}

func (TermQuery) queryNode() {}

type FullTextQuery struct {
	Fields                 []string
	Text, Analyzer, Locale string
}

func (FullTextQuery) queryNode() {}

type PrefixQuery struct{ Field, Prefix string }

func (PrefixQuery) queryNode() {}

type RangeQuery struct {
	Field            string
	GT, GTE, LT, LTE *Value
}

func (RangeQuery) queryNode() {}

type ExistsQuery struct{ Field string }

func (ExistsQuery) queryNode() {}

type GeoDistanceQuery struct {
	Field      string
	Origin     GeoPoint
	DistanceKM Value
}

func (GeoDistanceQuery) queryNode() {}

// RawExtensionQuery carries one bounded backend-specific query object. Adapter
// identifies the only adapter allowed to interpret Payload. Applications must
// authorize construction of these nodes and must not pass unrestricted caller
// JSON through this type.
type RawExtensionQuery struct {
	Adapter string
	Payload json.RawMessage
}

func (RawExtensionQuery) queryNode() {}

// SortDirection defines ascending or descending ordering.
type SortDirection string

const (
	Ascending  SortDirection = "asc"
	Descending SortDirection = "desc"
)

// MissingPlacement defines where missing sort values appear.
type MissingPlacement string

const (
	MissingDefault MissingPlacement = ""
	MissingFirst   MissingPlacement = "_first"
	MissingLast    MissingPlacement = "_last"
)

type Sort struct {
	Field     string
	Direction SortDirection
	Missing   MissingPlacement
}

type CursorPage struct {
	Size      int
	Cursor    string
	KeepAlive time.Duration
}
type OffsetPage struct{ Size, Offset int }

type Projection struct{ Includes, Excludes []string }
type Highlight struct {
	FragmentSize, MaxFragments int
	PreTag, PostTag            string
}

type Aggregation interface{ aggregationNode() }
type TermsAggregation struct {
	Field string
	Size  int
}

func (TermsAggregation) aggregationNode() {}

type RangeBucket struct {
	Key      string
	From, To *Value
}
type RangeAggregation struct {
	Field   string
	Buckets []RangeBucket
}

func (RangeAggregation) aggregationNode() {}

type Suggestion interface{ suggestionNode() }
type PrefixSuggestion struct {
	Field, Text string
	Size        int
}

func (PrefixSuggestion) suggestionNode() {}

// Request contains a backend-neutral typed search operation.
type Request struct {
	Tenant, Index string
	Query         Query
	Sort          []Sort
	Page          any
	Projection    Projection
	Highlights    map[string]Highlight
	Aggregations  map[string]Aggregation
	Suggestions   map[string]Suggestion
}

// Validate rejects malformed, unbounded, or unsupported requests before an
// adapter performs I/O.
func (r Request) Validate(capabilities Capabilities, limits Limits) error {
	if limits.Validate() != nil {
		return ErrInvalidQuery
	}
	if r.Tenant == "" || len(r.Tenant) > limits.MaxTenantBytes ||
		r.Index == "" || len(r.Index) > limits.MaxIndexBytes {
		return ErrInvalidQuery
	}
	if r.Query == nil {
		return ErrInvalidQuery
	}
	if !requestInputsWithinBudget(r, limits) {
		return ErrInvalidQuery
	}
	clauses := 0
	if err := validateQuery(r.Query, capabilities, limits, 1, &clauses); err != nil {
		return err
	}
	if err := validateSort(r.Sort); err != nil {
		return err
	}
	switch page := r.Page.(type) {
	case CursorPage:
		if !capabilities.Cursor || !capabilities.PointInTime {
			return unsupported("cursor and point-in-time")
		}
		if page.Size <= 0 || page.Size > limits.MaxPageItems || page.KeepAlive <= 0 || page.KeepAlive > limits.MaxCursorDuration {
			return ErrPageLimit
		}
		if len(r.Sort) == 0 || r.Sort[len(r.Sort)-1].Field != "_id" {
			return ErrUnstableSort
		}
	case OffsetPage:
		if !capabilities.Offset {
			return unsupported("offset")
		}
		maximumItems := limits.MaxPages * limits.MaxPageItems
		if page.Size <= 0 || page.Size > limits.MaxPageItems || page.Offset < 0 ||
			page.Offset > maximumItems-page.Size {
			return ErrPageLimit
		}
	default:
		return ErrPageLimit
	}
	if (len(r.Projection.Includes) > 0 || len(r.Projection.Excludes) > 0) && !capabilities.Projection {
		return unsupported("source projection")
	}
	for _, fields := range [][]string{r.Projection.Includes, r.Projection.Excludes} {
		for _, field := range fields {
			if !validField(field) {
				return ErrInvalidQuery
			}
		}
	}
	if len(r.Highlights) > 0 && !capabilities.Highlight {
		return unsupported("highlight")
	}
	for field, highlight := range r.Highlights {
		if !validField(field) || highlight.FragmentSize <= 0 || highlight.FragmentSize > limits.MaxSourceBytes ||
			highlight.MaxFragments <= 0 || highlight.MaxFragments > limits.MaxPageItems {
			return ErrInvalidQuery
		}
	}
	if len(r.Aggregations) > 0 && !capabilities.Aggregation {
		return unsupported("aggregation")
	}
	for name, aggregation := range r.Aggregations {
		if !validField(name) || !validAggregation(aggregation, limits) {
			return ErrInvalidQuery
		}
	}
	if len(r.Suggestions) > 0 && !capabilities.Suggestion {
		return unsupported("suggestion")
	}
	for name, suggestion := range r.Suggestions {
		if !validField(name) || !validSuggestion(suggestion, limits) {
			return ErrInvalidQuery
		}
	}
	encoded, err := requestFingerprintPayload(r)
	if err != nil || len(encoded) > limits.MaxQueryBytes {
		return ErrInvalidQuery
	}

	return nil
}

func validateQuery(query Query, capabilities Capabilities, limits Limits, depth int, clauses *int) error {
	*clauses++
	if depth > limits.MaxQueryDepth || *clauses > limits.MaxQueryClauses {
		return ErrInvalidQuery
	}
	switch node := query.(type) {
	case MatchAllQuery:
		return nil
	case BoolQuery:
		if !capabilities.Boolean {
			return unsupported("boolean")
		}
		if len(node.Must) == 0 && len(node.Should) == 0 && len(node.Filter) == 0 && len(node.MustNot) == 0 ||
			node.MinimumShouldMatch < 0 || node.MinimumShouldMatch > len(node.Should) {
			return ErrInvalidQuery
		}
		for _, children := range [][]Query{node.Must, node.Should, node.Filter, node.MustNot} {
			for _, child := range children {
				if child == nil {
					return ErrInvalidQuery
				}
				if err := validateQuery(child, capabilities, limits, depth+1, clauses); err != nil {
					return err
				}
			}
		}
		return nil
	case TermQuery:
		if !capabilities.Term {
			return unsupported("term")
		}
		if !validField(node.Field) || !validTermValue(node.Value) {
			return ErrInvalidQuery
		}
	case FullTextQuery:
		if !capabilities.FullText {
			return unsupported("full text")
		}
		if len(node.Fields) == 0 || strings.TrimSpace(node.Text) == "" {
			return ErrInvalidQuery
		}
		for _, field := range node.Fields {
			if !validFullTextField(field) {
				return ErrInvalidQuery
			}
		}
	case PrefixQuery:
		if !capabilities.Prefix {
			return unsupported("prefix")
		}
		if !validField(node.Field) || node.Prefix == "" {
			return ErrInvalidQuery
		}
	case RangeQuery:
		if !capabilities.Range {
			return unsupported("range")
		}
		if !validField(node.Field) || node.GT == nil && node.GTE == nil && node.LT == nil && node.LTE == nil ||
			node.GT != nil && node.GTE != nil || node.LT != nil && node.LTE != nil {
			return ErrInvalidQuery
		}
		for _, bound := range []*Value{node.GT, node.GTE, node.LT, node.LTE} {
			if bound != nil && !validRangeValue(*bound) {
				return ErrInvalidQuery
			}
		}
	case ExistsQuery:
		if !capabilities.Exists {
			return unsupported("exists")
		}
		if !validField(node.Field) {
			return ErrInvalidQuery
		}
	case GeoDistanceQuery:
		if !capabilities.Geo {
			return unsupported("geo")
		}
		if !validField(node.Field) || math.IsNaN(node.Origin.Latitude) || math.IsInf(node.Origin.Latitude, 0) ||
			node.Origin.Latitude < -90 || node.Origin.Latitude > 90 || math.IsNaN(node.Origin.Longitude) ||
			math.IsInf(node.Origin.Longitude, 0) || node.Origin.Longitude < -180 || node.Origin.Longitude > 180 || node.DistanceKM.kind != KindNumber {
			return ErrInvalidQuery
		}
		distance, err := strconv.ParseFloat(node.DistanceKM.text, 64)
		if err != nil || distance <= 0 {
			return ErrInvalidQuery
		}
	case RawExtensionQuery:
		if !capabilities.RawExtensions {
			return unsupported("raw extension")
		}
		if !validField(node.Adapter) || len(node.Payload) == 0 || len(node.Payload) > limits.MaxSourceBytes {
			return ErrInvalidQuery
		}
		if _, err := canonicalJSONObject(node.Payload); err != nil {
			return ErrInvalidQuery
		}
	default:
		return ErrInvalidQuery
	}
	return nil
}

func validateSort(sorts []Sort) error {
	for _, sort := range sorts {
		if !validField(sort.Field) || sort.Direction != Ascending && sort.Direction != Descending ||
			sort.Missing != MissingDefault && sort.Missing != MissingFirst && sort.Missing != MissingLast {
			return ErrInvalidQuery
		}
	}
	return nil
}

func validAggregation(aggregation Aggregation, limits Limits) bool {
	switch value := aggregation.(type) {
	case TermsAggregation:
		return validField(value.Field) && value.Size > 0 && value.Size <= limits.MaxPageItems
	case RangeAggregation:
		if !validField(value.Field) || len(value.Buckets) == 0 || len(value.Buckets) > limits.MaxQueryClauses {
			return false
		}
		for _, bucket := range value.Buckets {
			if bucket.Key == "" || bucket.From == nil && bucket.To == nil ||
				bucket.From != nil && !validRangeValue(*bucket.From) || bucket.To != nil && !validRangeValue(*bucket.To) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func validSuggestion(suggestion Suggestion, limits Limits) bool {
	value, ok := suggestion.(PrefixSuggestion)
	return ok && validField(value.Field) && value.Text != "" && value.Size > 0 && value.Size <= limits.MaxPageItems
}

func validField(field string) bool {
	if field == "" || len(field) > MaxFieldNameBytes || strings.ContainsAny(field, "^\x00\r\n") {
		return false
	}
	return true
}

func validFullTextField(field string) bool {
	if len(field) > MaxFieldNameBytes {
		return false
	}
	base, boost, found := strings.Cut(field, "^")
	if !found {
		return validField(field)
	}
	value, err := strconv.ParseFloat(boost, 64)
	if !validField(base) || err != nil {
		return false
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return false
	}
	return true
}

type requestInputBudget struct {
	bytes, nodes, maxCollection int
}

func requestInputsWithinBudget(request Request, limits Limits) bool {
	budget := requestInputBudget{bytes: limits.MaxQueryBytes, nodes: limits.MaxQueryClauses, maxCollection: limits.MaxQueryClauses}
	if !budget.consume(len(request.Tenant)) {
		return false
	}
	if !budget.consume(len(request.Index)) {
		return false
	}
	if !budget.query(request.Query, limits.MaxQueryDepth) {
		return false
	}
	for _, count := range []int{len(request.Sort), len(request.Projection.Includes), len(request.Projection.Excludes), len(request.Highlights), len(request.Aggregations), len(request.Suggestions)} {
		if count > limits.MaxQueryClauses {
			return false
		}
	}
	for _, sort := range request.Sort {
		if !budget.consume(len(sort.Field)) {
			return false
		}
	}
	if page, ok := request.Page.(CursorPage); ok {
		if !budget.consume(len(page.Cursor)) {
			return false
		}
	}
	for _, fields := range [][]string{request.Projection.Includes, request.Projection.Excludes} {
		for _, field := range fields {
			if !budget.consume(len(field)) {
				return false
			}
		}
	}
	for field, highlight := range request.Highlights {
		if !budget.consume(len(field)) {
			return false
		}
		if !budget.consume(len(highlight.PreTag)) {
			return false
		}
		if !budget.consume(len(highlight.PostTag)) {
			return false
		}
	}
	for name, aggregation := range request.Aggregations {
		if !budget.consume(len(name)) {
			return false
		}
		if !budget.aggregation(aggregation) {
			return false
		}
	}
	for name, suggestion := range request.Suggestions {
		if !budget.consume(len(name)) {
			return false
		}
		if !budget.suggestion(suggestion) {
			return false
		}
	}
	return true
}

func (b *requestInputBudget) consume(size int) bool {
	if size > b.bytes {
		return false
	}
	b.bytes -= size
	return true
}

func (b *requestInputBudget) node() bool {
	if b.nodes <= 0 {
		return false
	}
	b.nodes--
	return true
}

func (b *requestInputBudget) query(query Query, depth int) bool {
	if query == nil {
		return false
	}
	if depth <= 0 {
		return false
	}
	if !b.node() {
		return false
	}
	switch value := query.(type) {
	case MatchAllQuery:
		return true
	case BoolQuery:
		for _, children := range [][]Query{value.Must, value.Should, value.Filter, value.MustNot} {
			for _, child := range children {
				if !b.query(child, depth-1) {
					return false
				}
			}
		}
		return true
	case TermQuery:
		if !b.consume(len(value.Field)) {
			return false
		}
		return b.value(value.Value)
	case FullTextQuery:
		if len(value.Fields) > b.maxCollection {
			return false
		}
		if !b.consume(len(value.Text)) {
			return false
		}
		if !b.consume(len(value.Analyzer)) {
			return false
		}
		if !b.consume(len(value.Locale)) {
			return false
		}
		for _, field := range value.Fields {
			if !b.consume(len(field)) {
				return false
			}
		}
		return true
	case PrefixQuery:
		if !b.consume(len(value.Field)) {
			return false
		}
		return b.consume(len(value.Prefix))
	case RangeQuery:
		if !b.consume(len(value.Field)) {
			return false
		}
		for _, bound := range []*Value{value.GT, value.GTE, value.LT, value.LTE} {
			if bound != nil && !b.value(*bound) {
				return false
			}
		}
		return true
	case ExistsQuery:
		return b.consume(len(value.Field))
	case GeoDistanceQuery:
		if !b.consume(len(value.Field)) {
			return false
		}
		return b.value(value.DistanceKM)
	case RawExtensionQuery:
		if !b.consume(len(value.Adapter)) {
			return false
		}
		return b.consume(len(value.Payload))
	default:
		return false
	}
}

func (b *requestInputBudget) value(value Value) bool {
	switch value.kind {
	case KindString, KindNumber, KindTime:
		return b.consume(len(value.text))
	case KindNull, KindBool, KindGeo:
		return true
	default:
		return false
	}
}

func (b *requestInputBudget) aggregation(aggregation Aggregation) bool {
	switch value := aggregation.(type) {
	case TermsAggregation:
		return b.consume(len(value.Field))
	case RangeAggregation:
		if !b.consume(len(value.Field)) {
			return false
		}
		for _, bucket := range value.Buckets {
			if !b.consume(len(bucket.Key)) {
				return false
			}
			for _, bound := range []*Value{bucket.From, bucket.To} {
				if bound != nil && !b.value(*bound) {
					return false
				}
			}
		}
		return true
	default:
		return false
	}
}

func (b *requestInputBudget) suggestion(suggestion Suggestion) bool {
	value, ok := suggestion.(PrefixSuggestion)
	if !ok {
		return false
	}
	if !b.consume(len(value.Field)) {
		return false
	}
	return b.consume(len(value.Text))
}

func validTermValue(value Value) bool {
	switch value.kind {
	case KindString, KindNumber, KindBool, KindTime:
		return true
	default:
		return false
	}
}

func validRangeValue(value Value) bool {
	switch value.kind {
	case KindString, KindNumber, KindTime:
		return true
	default:
		return false
	}
}

func unsupported(name string) error { return fmt.Errorf("%w: %s", ErrUnsupported, name) }

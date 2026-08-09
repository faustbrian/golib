package search

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// RequestFingerprint returns a deterministic SHA-256 binding for query
// behavior. The opaque cursor itself is intentionally excluded so every page
// in one traversal has the same binding.
func RequestFingerprint(request Request, limits Limits) (string, error) {
	if limits.Validate() != nil || !requestInputsWithinBudget(request, limits) {
		return "", ErrInvalidQuery
	}
	encoded, err := requestFingerprintPayload(request)
	if err != nil || len(encoded) > limits.MaxQueryBytes {
		return "", ErrInvalidQuery
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func requestFingerprintPayload(request Request) ([]byte, error) {
	model := map[string]any{
		"tenant":       request.Tenant,
		"index":        request.Index,
		"query":        queryFingerprintModel(request.Query),
		"sort":         request.Sort,
		"page":         pageFingerprintModel(request.Page),
		"projection":   request.Projection,
		"highlights":   request.Highlights,
		"aggregations": aggregationFingerprintModel(request.Aggregations),
		"suggestions":  suggestionFingerprintModel(request.Suggestions),
	}
	return json.Marshal(model)
}

func queryFingerprintModel(query Query) any {
	switch node := query.(type) {
	case MatchAllQuery:
		return map[string]any{"type": "match_all"}
	case BoolQuery:
		return map[string]any{"type": "bool", "must": queryModels(node.Must), "should": queryModels(node.Should), "filter": queryModels(node.Filter), "must_not": queryModels(node.MustNot), "minimum_should_match": node.MinimumShouldMatch}
	case TermQuery:
		return map[string]any{"type": "term", "field": node.Field, "value": node.Value}
	case FullTextQuery:
		return map[string]any{"type": "full_text", "fields": node.Fields, "text": node.Text, "analyzer": node.Analyzer, "locale": node.Locale}
	case PrefixQuery:
		return map[string]any{"type": "prefix", "field": node.Field, "prefix": node.Prefix}
	case RangeQuery:
		return map[string]any{"type": "range", "field": node.Field, "gt": node.GT, "gte": node.GTE, "lt": node.LT, "lte": node.LTE}
	case ExistsQuery:
		return map[string]any{"type": "exists", "field": node.Field}
	case GeoDistanceQuery:
		return map[string]any{"type": "geo_distance", "field": node.Field, "origin": node.Origin, "distance_km": node.DistanceKM}
	case RawExtensionQuery:
		return map[string]any{"type": "raw_extension", "adapter": node.Adapter, "payload": node.Payload}
	default:
		return map[string]any{"type": "invalid"}
	}
}

func queryModels(queries []Query) []any {
	if queries == nil {
		return nil
	}
	result := make([]any, len(queries))
	for index, query := range queries {
		result[index] = queryFingerprintModel(query)
	}
	return result
}

func pageFingerprintModel(page any) any {
	switch value := page.(type) {
	case CursorPage:
		return map[string]any{"type": "cursor", "size": value.Size, "keep_alive_ns": value.KeepAlive.Nanoseconds()}
	case OffsetPage:
		return map[string]any{"type": "offset", "size": value.Size, "offset": value.Offset}
	default:
		return map[string]any{"type": "invalid"}
	}
}

func aggregationFingerprintModel(values map[string]Aggregation) map[string]any {
	if values == nil {
		return nil
	}
	result := make(map[string]any, len(values))
	for name, aggregation := range values {
		switch value := aggregation.(type) {
		case TermsAggregation:
			result[name] = map[string]any{"type": "terms", "field": value.Field, "size": value.Size}
		case RangeAggregation:
			result[name] = map[string]any{"type": "range", "field": value.Field, "buckets": value.Buckets}
		default:
			result[name] = map[string]any{"type": "invalid"}
		}
	}
	return result
}

func suggestionFingerprintModel(values map[string]Suggestion) map[string]any {
	if values == nil {
		return nil
	}
	result := make(map[string]any, len(values))
	for name, suggestion := range values {
		switch value := suggestion.(type) {
		case PrefixSuggestion:
			result[name] = map[string]any{"type": "prefix", "field": value.Field, "text": value.Text, "size": value.Size}
		default:
			result[name] = map[string]any{"type": "invalid"}
		}
	}
	return result
}

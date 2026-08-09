package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

var (
	// ErrSearchDisabled identifies tenant search attempted without the explicit
	// resolver, bounds, and cursor ownership required by SearchConfig.
	ErrSearchDisabled = errors.New("search/opensearch: search is not configured")
	// ErrUnsafeIndexTarget identifies a resolver result that cannot safely be
	// used as one OpenSearch index or alias path segment.
	ErrUnsafeIndexTarget = errors.New("search/opensearch: resolved index target is unsafe")
)

var indexTargetPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,254}$`)
var indexFingerprintPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)
var analyzerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// IndexAccess lets a resolver enforce distinct read and write aliases.
type IndexAccess string

const (
	IndexRead  IndexAccess = "read"
	IndexWrite IndexAccess = "write"
)

// IndexTarget is an opaque physical alias plus the immutable index-definition
// fingerprint to which cursors are bound. Name must not contain raw tenant
// labels unless the caller has explicitly determined that disclosure is safe.
type IndexTarget struct {
	Name        string
	Fingerprint string
}

// IndexResolver maps a tenant and logical index to a least-privilege physical
// alias. Implementations must be concurrency-safe and must not return a target
// from another tenant.
type IndexResolver interface {
	Resolve(context.Context, string, string, IndexAccess) (IndexTarget, error)
}

// IndexResolverFunc adapts a function to IndexResolver.
type IndexResolverFunc func(context.Context, string, string, IndexAccess) (IndexTarget, error)

func (resolve IndexResolverFunc) Resolve(ctx context.Context, tenant, index string, access IndexAccess) (IndexTarget, error) {
	return resolve(ctx, tenant, index, access)
}

// SearchConfig owns the complete typed-search boundary. Raw OpenSearch DSL is
// intentionally not accepted by this configuration.
type SearchConfig struct {
	Limits          search.Limits
	CursorCodec     *search.CursorCodec
	Resolver        IndexResolver
	Clock           func() time.Time
	LocaleAnalyzers map[string]string
}

// Capabilities reports only the shared features translated by this adapter.
func (c *Client) Capabilities(ctx context.Context) (search.Capabilities, error) {
	if ctx == nil {
		return search.Capabilities{}, ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return search.Capabilities{}, cancelledFailure(OperationSearch, err)
	}
	if err := c.begin(); err != nil {
		return search.Capabilities{}, err
	}
	if c.search == nil {
		return search.Capabilities{}, ErrSearchDisabled
	}
	return search.Capabilities{
		Boolean: true, Term: true, FullText: true, Prefix: true, Range: true,
		Exists: true, Geo: true, Cursor: true, PointInTime: true, Offset: true,
		Projection: true, Highlight: true, Aggregation: true, Suggestion: true,
		ExternalVersion: true, BulkPartialOutcomes: true, Lifecycle: c.lifecycle != nil, Templates: c.lifecycle != nil,
		RawExtensions: true,
	}, nil
}

// Search performs one bounded typed search. Cursor pages own a PIT until the
// final empty/short page or an error closes it; successful continuation moves
// that ownership into the signed cursor.
func (c *Client) Search(ctx context.Context, request search.Request) (result search.Result, err error) {
	capabilities, err := c.Capabilities(ctx)
	if err != nil {
		return search.Result{}, err
	}
	if err := request.Validate(capabilities, c.search.Limits); err != nil {
		return search.Result{}, err
	}
	target, err := c.search.Resolver.Resolve(ctx, request.Tenant, request.Index, IndexRead)
	if err != nil {
		return search.Result{}, ErrUnsafeIndexTarget
	}
	if !validIndexTarget(target) {
		return search.Result{}, ErrUnsafeIndexTarget
	}
	fingerprint, _ := search.RequestFingerprint(request, c.search.Limits)
	binding := search.CursorBinding{
		Tenant: request.Tenant, Index: request.Index,
		QueryFingerprint: fingerprint, IndexFingerprint: target.Fingerprint,
	}

	var state search.CursorState
	var ownedPIT bool
	switch page := request.Page.(type) {
	case search.CursorPage:
		if page.Cursor == "" {
			state.PointInTime, err = c.createPIT(ctx, target.Name, page.KeepAlive)
			ownedPIT = err == nil
		} else {
			state, err = c.search.CursorCodec.Decode(page.Cursor, binding, c.search.Limits)
			ownedPIT = err == nil
		}
		if err != nil {
			return search.Result{}, err
		}
	}
	if ownedPIT {
		defer func() {
			if ownedPIT {
				if cleanupErr := c.deletePIT(context.WithoutCancel(ctx), state.PointInTime); cleanupErr != nil {
					err = errors.Join(err, cleanupErr)
				}
			}
		}()
	}

	body, err := encodeSearchRequest(request, state, c.search.LocaleAnalyzers)
	if err != nil {
		return search.Result{}, err
	}
	searchPath := "/_search"
	if _, offset := request.Page.(search.OffsetPage); offset {
		searchPath = "/" + target.Name + "/_search"
	}
	responseBody, err := c.execute(ctx, OperationSearch, http.MethodPost, searchPath, body, http.StatusOK)
	if err != nil {
		return search.Result{}, err
	}
	if int64(len(responseBody)) > c.search.Limits.MaxResultBytes {
		return search.Result{}, malformedFailure(OperationSearch, ErrResponseTooLarge)
	}
	decoded, err := decodeSearchResponse(responseBody)
	if err != nil {
		return search.Result{}, malformedFailure(OperationSearch, err)
	}
	if decoded.PITID != "" {
		state.PointInTime = decoded.PITID
	}

	nextCursor := ""
	pageSize := searchPageSize(request.Page)
	if len(decoded.Hits) > pageSize {
		return search.Result{}, malformedFailure(OperationSearch, ErrMalformedResponse)
	}
	if ownedPIT && len(decoded.Hits) == pageSize {
		last := decoded.Hits[len(decoded.Hits)-1]
		maximumItems := c.search.Limits.MaxPages * c.search.Limits.MaxPageItems
		responseBytes := int64(len(responseBody))
		if state.Page >= c.search.Limits.MaxPages || state.Items > maximumItems-len(decoded.Hits) ||
			state.Bytes > c.search.Limits.MaxResultBytes-responseBytes {
			return search.Result{}, search.ErrPageLimit
		}
		state.SortValues = last.SortValues
		state.Page++
		state.Items += len(decoded.Hits)
		state.Bytes += responseBytes
		page := request.Page.(search.CursorPage)
		if state.ExpiresAt.IsZero() {
			state.ExpiresAt = c.search.Clock().Add(page.KeepAlive)
		}
		nextCursor, err = c.search.CursorCodec.Encode(binding, state)
		if err != nil {
			return search.Result{}, err
		}
		ownedPIT = false
	}
	if ownedPIT {
		ownedPIT = false
		if err := c.deletePIT(context.WithoutCancel(ctx), state.PointInTime); err != nil {
			return search.Result{}, err
		}
	}

	result, err = search.NewResult(
		decoded.Hits, decoded.Total, decoded.Aggregations, decoded.Suggestions,
		decoded.Diagnostics, nextCursor,
	)
	if err != nil {
		return search.Result{}, malformedFailure(OperationSearch, err)
	}
	return result, nil
}

func validIndexTarget(target IndexTarget) bool {
	return indexTargetPattern.MatchString(target.Name) && target.Name != "." && target.Name != ".." &&
		indexFingerprintPattern.MatchString(target.Fingerprint)
}

func searchPageSize(page any) int {
	switch value := page.(type) {
	case search.CursorPage:
		return value.Size
	case search.OffsetPage:
		return value.Size
	default:
		return 0
	}
}

func (c *Client) createPIT(ctx context.Context, index string, keepAlive time.Duration) (string, error) {
	path := "/" + index + "/_search/point_in_time?keep_alive=" + fmt.Sprintf("%dms", keepAlive.Milliseconds())
	body, err := c.execute(ctx, OperationCreatePIT, http.MethodPost, path, nil, http.StatusOK, http.StatusCreated)
	if err != nil {
		return "", err
	}
	var response struct {
		PITID string `json:"pit_id"`
	}
	if json.Unmarshal(body, &response) != nil {
		return "", malformedFailure(OperationCreatePIT, ErrMalformedResponse)
	}
	if response.PITID == "" {
		return "", malformedFailure(OperationCreatePIT, ErrMalformedResponse)
	}
	return response.PITID, nil
}

func (c *Client) deletePIT(ctx context.Context, id string) error {
	body, _ := json.Marshal(map[string]string{"pit_id": id})
	_, err := c.execute(ctx, OperationDeletePIT, http.MethodDelete, "/_search/point_in_time", body, http.StatusOK)
	return err
}

func (c *Client) execute(ctx context.Context, operation Operation, method, path string, body []byte, accepted ...int) ([]byte, error) {
	return c.executeContent(ctx, operation, method, path, body, "application/json", accepted...)
}

func (c *Client) executeContent(ctx context.Context, operation Operation, method, path string, body []byte, contentType string, accepted ...int) (responseBytes []byte, err error) {
	if ctx == nil {
		return nil, ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, cancelledFailure(operation, err)
	}
	if err := c.begin(); err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(withOperation(ctx, operation), c.timeout)
	defer cancel()
	request, _ := http.NewRequestWithContext(requestCtx, method, path, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", contentType)
	}
	response, transportErr := c.client.Stream(request)
	if response == nil {
		return nil, transportFailure(operation, transportErr)
	}
	defer func() { _ = response.Body.Close() }()
	if transportErr != nil {
		return nil, transportFailure(operation, transportErr)
	}
	responseBody, err := readBounded(response.Body, c.maximumResponseBytes)
	if err != nil {
		return nil, malformedFailure(operation, err)
	}
	for _, status := range accepted {
		if response.StatusCode == status {
			return responseBody, nil
		}
	}
	return nil, responseFailure(operation, response.StatusCode, responseBody)
}

func encodeSearchRequest(request search.Request, state search.CursorState, localeAnalyzers map[string]string) ([]byte, error) {
	query, err := encodeQuery(request.Query, localeAnalyzers)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"query": query, "size": searchPageSize(request.Page), "version": true}
	if page, ok := request.Page.(search.OffsetPage); ok {
		body["from"] = page.Offset
	} else {
		page := request.Page.(search.CursorPage)
		body["pit"] = map[string]any{"id": state.PointInTime, "keep_alive": fmt.Sprintf("%dms", page.KeepAlive.Milliseconds())}
		if len(state.SortValues) > 0 {
			body["search_after"] = state.SortValues
		}
	}
	if len(request.Sort) > 0 {
		sorts := make([]any, len(request.Sort))
		for index, item := range request.Sort {
			options := map[string]any{"order": item.Direction}
			if item.Missing != search.MissingDefault {
				options["missing"] = item.Missing
			}
			sorts[index] = map[string]any{item.Field: options}
		}
		body["sort"] = sorts
	}
	if len(request.Projection.Includes) != 0 {
		body["_source"] = map[string]any{"includes": request.Projection.Includes, "excludes": request.Projection.Excludes}
	} else if len(request.Projection.Excludes) != 0 {
		body["_source"] = map[string]any{"includes": request.Projection.Includes, "excludes": request.Projection.Excludes}
	}
	if len(request.Highlights) > 0 {
		fields := make(map[string]any, len(request.Highlights))
		for field, item := range request.Highlights {
			value := map[string]any{"fragment_size": item.FragmentSize, "number_of_fragments": item.MaxFragments}
			if item.PreTag != "" {
				value["pre_tags"] = []string{item.PreTag}
			}
			if item.PostTag != "" {
				value["post_tags"] = []string{item.PostTag}
			}
			fields[field] = value
		}
		body["highlight"] = map[string]any{"fields": fields}
	}
	if len(request.Aggregations) > 0 {
		body["aggs"] = encodeAggregations(request.Aggregations)
	}
	if len(request.Suggestions) > 0 {
		body["suggest"] = encodeSuggestions(request.Suggestions)
	}
	return json.Marshal(body)
}

func encodeQuery(query search.Query, localeAnalyzers map[string]string) (any, error) {
	switch node := query.(type) {
	case search.MatchAllQuery:
		return map[string]any{"match_all": map[string]any{}}, nil
	case search.BoolQuery:
		value := map[string]any{}
		clauses := []struct {
			name     string
			children []search.Query
		}{
			{name: "must", children: node.Must},
			{name: "should", children: node.Should},
			{name: "filter", children: node.Filter},
			{name: "must_not", children: node.MustNot},
		}
		for _, clause := range clauses {
			name, children := clause.name, clause.children
			if len(children) == 0 {
				continue
			}
			encoded := make([]any, len(children))
			for index, child := range children {
				item, err := encodeQuery(child, localeAnalyzers)
				if err != nil {
					return nil, err
				}
				encoded[index] = item
			}
			value[name] = encoded
		}
		if node.MinimumShouldMatch > 0 {
			value["minimum_should_match"] = node.MinimumShouldMatch
		}
		return map[string]any{"bool": value}, nil
	case search.TermQuery:
		return map[string]any{"term": map[string]any{node.Field: node.Value}}, nil
	case search.FullTextQuery:
		value := map[string]any{"query": node.Text, "fields": node.Fields}
		analyzer := node.Analyzer
		if analyzer != "" && !analyzerPattern.MatchString(analyzer) {
			return nil, search.ErrInvalidQuery
		}
		if node.Locale != "" {
			localized, exists := localeAnalyzers[node.Locale]
			if !exists || analyzer != "" && analyzer != localized {
				return nil, search.ErrUnsupported
			}
			analyzer = localized
		}
		if analyzer != "" {
			value["analyzer"] = analyzer
		}
		return map[string]any{"multi_match": value}, nil
	case search.PrefixQuery:
		return map[string]any{"prefix": map[string]any{node.Field: map[string]any{"value": node.Prefix}}}, nil
	case search.RangeQuery:
		bounds := map[string]any{}
		for name, value := range map[string]*search.Value{"gt": node.GT, "gte": node.GTE, "lt": node.LT, "lte": node.LTE} {
			if value != nil {
				bounds[name] = *value
			}
		}
		return map[string]any{"range": map[string]any{node.Field: bounds}}, nil
	case search.ExistsQuery:
		return map[string]any{"exists": map[string]any{"field": node.Field}}, nil
	case search.GeoDistanceQuery:
		distance, _ := json.Marshal(node.DistanceKM)
		return map[string]any{"geo_distance": map[string]any{"distance": string(distance) + "km", node.Field: map[string]float64{"lat": node.Origin.Latitude, "lon": node.Origin.Longitude}}}, nil
	case search.RawExtensionQuery:
		if node.Adapter != "opensearch" {
			return nil, search.ErrUnsupported
		}
		decoder := json.NewDecoder(bytes.NewReader(node.Payload))
		decoder.UseNumber()
		var value map[string]any
		if err := decoder.Decode(&value); err != nil || value == nil {
			return nil, search.ErrInvalidQuery
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			return nil, search.ErrInvalidQuery
		}
		return value, nil
	default:
		return nil, search.ErrInvalidQuery
	}
}

func encodeAggregations(values map[string]search.Aggregation) map[string]any {
	result := make(map[string]any, len(values))
	for name, aggregation := range values {
		switch value := aggregation.(type) {
		case search.TermsAggregation:
			result[name] = map[string]any{"terms": map[string]any{"field": value.Field, "size": value.Size}}
		case search.RangeAggregation:
			ranges := make([]any, len(value.Buckets))
			for index, bucket := range value.Buckets {
				item := map[string]any{"key": bucket.Key}
				if bucket.From != nil {
					item["from"] = *bucket.From
				}
				if bucket.To != nil {
					item["to"] = *bucket.To
				}
				ranges[index] = item
			}
			result[name] = map[string]any{"range": map[string]any{"field": value.Field, "ranges": ranges}}
		}
	}
	return result
}

func encodeSuggestions(values map[string]search.Suggestion) map[string]any {
	result := make(map[string]any, len(values))
	for name, suggestion := range values {
		value := suggestion.(search.PrefixSuggestion)
		result[name] = map[string]any{"prefix": value.Text, "completion": map[string]any{"field": value.Field, "size": value.Size, "skip_duplicates": true}}
	}
	return result
}

type decodedSearch struct {
	Hits         []search.Hit
	Total        search.Total
	Aggregations map[string]json.RawMessage
	Suggestions  map[string]json.RawMessage
	Diagnostics  search.Diagnostics
	PITID        string
}

func decodeSearchResponse(body []byte) (decodedSearch, error) {
	var payload struct {
		Took     int64  `json:"took"`
		TimedOut bool   `json:"timed_out"`
		PITID    string `json:"pit_id"`
		Shards   struct {
			Total, Successful, Skipped, Failed int
			Failures                           []struct {
				Index  string `json:"index"`
				Reason struct {
					Type string `json:"type"`
				} `json:"reason"`
			} `json:"failures"`
		} `json:"_shards"`
		Hits struct {
			Total search.Total `json:"total"`
			Hits  []struct {
				Index     string              `json:"_index"`
				ID        string              `json:"_id"`
				Version   uint64              `json:"_version"`
				Score     *float64            `json:"_score"`
				Source    json.RawMessage     `json:"_source"`
				Sort      []json.RawMessage   `json:"sort"`
				Highlight map[string][]string `json:"highlight"`
			} `json:"hits"`
		} `json:"hits"`
		Aggregations map[string]json.RawMessage `json:"aggregations"`
		Suggest      map[string]json.RawMessage `json:"suggest"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&payload); err != nil {
		return decodedSearch{}, ErrMalformedResponse
	}
	if trailingJSON(decoder) {
		return decodedSearch{}, ErrMalformedResponse
	}
	if payload.Took < 0 || payload.Took > math.MaxInt64/int64(time.Millisecond) {
		return decodedSearch{}, ErrMalformedResponse
	}
	if payload.Shards.Total < 0 {
		return decodedSearch{}, ErrMalformedResponse
	}
	if payload.Shards.Successful < 0 {
		return decodedSearch{}, ErrMalformedResponse
	}
	if payload.Shards.Skipped < 0 {
		return decodedSearch{}, ErrMalformedResponse
	}
	if payload.Shards.Failed < 0 {
		return decodedSearch{}, ErrMalformedResponse
	}
	if payload.Hits.Total.Relation != search.TotalExact && payload.Hits.Total.Relation != search.TotalLowerBound {
		return decodedSearch{}, ErrMalformedResponse
	}
	if payload.Hits.Total.Value < uint64(len(payload.Hits.Hits)) {
		return decodedSearch{}, ErrMalformedResponse
	}
	hits := make([]search.Hit, len(payload.Hits.Hits))
	for index, hit := range payload.Hits.Hits {
		if hit.Version == 0 {
			return decodedSearch{}, ErrMalformedResponse
		}
		hits[index] = search.Hit{Index: hit.Index, ID: hit.ID, Version: hit.Version, Score: hit.Score, Source: hit.Source, SortValues: hit.Sort, Highlights: hit.Highlight}
	}
	failures := make([]search.Failure, 0, len(payload.Shards.Failures))
	for _, failure := range payload.Shards.Failures {
		code := failure.Reason.Type
		if !safeErrorCode(code) {
			code = "unknown"
		}
		failures = append(failures, search.Failure{Scope: "shard", Code: code, Retryable: false})
	}
	partial := payload.TimedOut || payload.Shards.Failed > 0
	diagnostics := search.Diagnostics{Backend: "opensearch", Took: time.Duration(payload.Took) * time.Millisecond, TimedOut: payload.TimedOut, Partial: partial,
		Shards: search.ShardDiagnostics{Total: payload.Shards.Total, Successful: payload.Shards.Successful, Skipped: payload.Shards.Skipped, Failed: payload.Shards.Failed}, Failures: failures}
	return decodedSearch{Hits: hits, Total: payload.Hits.Total, Aggregations: payload.Aggregations, Suggestions: payload.Suggest, Diagnostics: diagnostics, PITID: payload.PITID}, nil
}

func trailingJSON(decoder *json.Decoder) bool {
	var extra any
	return decoder.Decode(&extra) != io.EOF
}

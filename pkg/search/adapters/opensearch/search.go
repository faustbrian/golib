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
	"strings"
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
	// ErrSearchDenied identifies a query or source-field scope rejected before
	// index resolution. The underlying policy error is intentionally hidden.
	ErrSearchDenied = errors.New("search/opensearch: search authorization denied")
	// ErrWriteDisabled identifies a write attempted without a durable source
	// guard capable of rejecting stale versions after backend tombstone expiry.
	ErrWriteDisabled = errors.New("search/opensearch: writes are not configured")
	// ErrWriteDenied identifies a source guard rejection before index resolution.
	// The underlying policy or storage error is intentionally hidden.
	ErrWriteDenied = errors.New("search/opensearch: write authorization denied")
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

// IndexTarget binds one opaque read/write alias to the exact tenant-owned
// physical generation permitted in backend responses. Fingerprint is the
// immutable index definition to which cursors are bound. Neither name may
// contain raw tenant labels unless the caller has explicitly determined that
// disclosure is safe.
type IndexTarget struct {
	Name, PhysicalName string
	Fingerprint        string
}

// IndexResolver maps a tenant and logical index to a least-privilege request
// alias plus the exact physical generation permitted in backend responses.
// Implementations must be concurrency-safe, must not return a target from
// another tenant, and must update the physical generation inside the same
// application write fence that protects an alias cutover.
type IndexResolver interface {
	Resolve(context.Context, string, string, IndexAccess) (IndexTarget, error)
}

// IndexResolverFunc adapts a function to IndexResolver.
type IndexResolverFunc func(context.Context, string, string, IndexAccess) (IndexTarget, error)

func (resolve IndexResolverFunc) Resolve(ctx context.Context, tenant, index string, access IndexAccess) (IndexTarget, error) {
	return resolve(ctx, tenant, index, access)
}

// SearchAuthorization exposes the complete caller intent to the application
// policy before physical index resolution. Query contains every typed or raw
// query node; result-shaping fields are copied so policies can authorize field
// disclosure, filtering, sorting, aggregations, suggestions, and highlights.
type SearchAuthorization struct {
	Tenant, Index string
	Query         search.Query
	Sort          []search.Sort
	Pagination    SearchPaginationAuthorization
	Projection    search.Projection
	Highlights    map[string]search.Highlight
	Aggregations  map[string]search.Aggregation
	Suggestions   map[string]search.Suggestion
	FullSource    bool
}

// SearchPaginationKind identifies the bounded traversal shape presented to a
// search authorization policy without disclosing an opaque cursor token.
type SearchPaginationKind string

const (
	SearchPaginationCursor SearchPaginationKind = "cursor"
	SearchPaginationOffset SearchPaginationKind = "offset"
)

// SearchPaginationAuthorization exposes search cost and continuation intent to
// policy. Cursor bytes remain private because binding is verified separately.
type SearchPaginationAuthorization struct {
	Kind         SearchPaginationKind
	Size         int
	Offset       int
	KeepAlive    time.Duration
	Continuation bool
}

// SearchAuthorizer approves one complete logical search intent. It must be
// concurrency-safe and must treat raw-extension payloads as untrusted data.
type SearchAuthorizer interface {
	AuthorizeSearch(context.Context, SearchAuthorization) error
}

// SearchAuthorizerFunc adapts a function to SearchAuthorizer.
type SearchAuthorizerFunc func(context.Context, SearchAuthorization) error

func (authorize SearchAuthorizerFunc) AuthorizeSearch(ctx context.Context, request SearchAuthorization) error {
	return authorize(ctx, request)
}

// SearchConfig owns the complete typed-search boundary. Raw OpenSearch DSL is
// intentionally not accepted by this configuration.
type SearchConfig struct {
	Limits      search.Limits
	CursorCodec *search.CursorCodec
	Resolver    IndexResolver
	Authorizer  SearchAuthorizer
	WriteGuard  WriteGuard
	// MaximumOpenPointInTimes bounds PITs created by one client and retained in
	// returned cursors. Zero selects DefaultMaximumOpenPointInTimes.
	MaximumOpenPointInTimes int
	// Deprecated: CursorCodec owns cursor time. Clock is retained for source
	// compatibility and is ignored.
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
	writesEnabled := c.search.WriteGuard != nil
	lifecycleMutationsEnabled := c.lifecycle != nil && c.lifecycle.MutationGuard != nil
	return search.Capabilities{
		Boolean: true, Term: true, FullText: true, Prefix: true, Range: true,
		Exists: true, Geo: true, Cursor: true, PointInTime: true, Offset: true,
		Projection: true, Highlight: true, Aggregation: true, Suggestion: true,
		ExternalVersion: writesEnabled, BulkPartialOutcomes: writesEnabled, Lifecycle: lifecycleMutationsEnabled, Templates: c.lifecycle != nil,
		RawExtensions: c.search.Authorizer != nil,
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
	request = cloneExecutionRequest(request)
	if err := c.authorizeSearch(ctx, request); err != nil {
		return search.Result{}, err
	}
	if _, err := encodeQuery(request.Query, c.search.LocaleAnalyzers); err != nil {
		return search.Result{}, err
	}
	target, err := c.resolveIndexTarget(ctx, OperationSearch, request.Tenant, request.Index, IndexRead)
	if err != nil {
		return search.Result{}, err
	}
	fingerprint, _ := search.RequestFingerprint(request, c.search.Limits)
	binding := search.CursorBinding{
		Tenant: request.Tenant, Index: request.Index,
		QueryFingerprint: fingerprint, IndexFingerprint: target.Fingerprint,
	}

	var state search.CursorState
	var ownedPIT bool
	var pitLease *pointInTimeLease
	switch page := request.Page.(type) {
	case search.CursorPage:
		if page.Cursor == "" {
			leaseExpiry, deadlineErr := c.search.CursorCodec.Deadline(page.KeepAlive)
			if deadlineErr != nil {
				return search.Result{}, deadlineErr
			}
			pitLease, err = c.pits.reserve(leaseExpiry)
			if err == nil {
				state.PointInTime, err = c.createPIT(ctx, target.Name, page.KeepAlive)
			}
			if err == nil {
				state.ExpiresAt = leaseExpiry
				// A successful create response transfers backend PIT ownership even
				// when concurrent client closure prevents local tracker binding.
				ownedPIT = true
				if bindErr := c.pits.bind(pitLease, state.PointInTime, state.ExpiresAt); bindErr != nil {
					if errors.Is(bindErr, errPointInTimeIDConflict) {
						// The identifier is already owned by another live cursor. Deleting
						// it would destroy that cursor rather than clean up this failed bind.
						ownedPIT = false
						err = malformedFailure(OperationCreatePIT, ErrMalformedResponse)
					} else {
						err = bindErr
					}
				}
			}
		} else {
			state, err = c.search.CursorCodec.Decode(page.Cursor, binding, c.search.Limits)
			if err == nil {
				remaining, remainingErr := c.search.CursorCodec.Remaining(state.ExpiresAt)
				remaining = remaining.Truncate(time.Millisecond)
				if remainingErr != nil || remaining < time.Millisecond {
					err = search.ErrCursorExpired
				} else {
					page.KeepAlive = min(page.KeepAlive, remaining)
					request.Page = page
					pitLease, err = c.pits.acquire(state.PointInTime, state.ExpiresAt)
					ownedPIT = err == nil
				}
			}
		}
	}
	if ownedPIT {
		defer func() {
			if ownedPIT {
				if cleanupErr := c.deletePIT(context.WithoutCancel(ctx), state.PointInTime); cleanupErr != nil {
					c.transport.telemetry.signal(ctx, OperationDeletePIT, TelemetryPITCleanupFailure)
					err = errors.Join(err, cleanupErr)
				} else {
					c.pits.release(pitLease)
				}
			}
		}()
	}
	if err != nil {
		if !ownedPIT {
			c.pits.release(pitLease)
		}
		return search.Result{}, err
	}

	// Search preflights this identical immutable request before resolving the
	// physical target or creating a PIT, so encoding cannot fail here.
	body, _ := encodeSearchRequest(request, state, c.search.LocaleAnalyzers)
	searchPath := "/_search"
	if _, offset := request.Page.(search.OffsetPage); offset {
		searchPath = "/" + target.Name + "/_search"
	}
	responseLimit := min(c.maximumResponseBytes, c.search.Limits.MaxResultBytes)
	responseBody, err := c.executeBounded(ctx, OperationSearch, http.MethodPost, searchPath, body, responseLimit, http.StatusOK)
	if err != nil {
		if _, cursorPage := request.Page.(search.CursorPage); cursorPage {
			err = classifyPITSearchFailure(err)
		}
		return search.Result{}, err
	}
	decoded, err := decodeSearchResponse(responseBody)
	if decoded.PITID != "" {
		if len(decoded.PITID) > c.search.Limits.MaxQueryBytes || strings.ContainsAny(decoded.PITID, "\x00\r\n") {
			return search.Result{}, malformedFailure(OperationSearch, ErrMalformedResponse)
		}
		if !c.pits.rotate(pitLease, state.PointInTime, decoded.PITID) {
			return search.Result{}, malformedFailure(OperationSearch, ErrMalformedResponse)
		}
		state.PointInTime = decoded.PITID
	}
	if err != nil {
		return search.Result{}, malformedFailure(OperationSearch, err)
	}
	if err := validateSearchResponseContract(request, target.PhysicalName, decoded, c.search.Limits); err != nil {
		return search.Result{}, malformedFailure(OperationSearch, err)
	}
	for index := range decoded.Hits {
		decoded.Hits[index].Index = request.Index
	}
	if decoded.Diagnostics.Partial {
		c.transport.telemetry.signal(ctx, OperationSearch, TelemetryPartialSearch)
		if _, cursorPage := request.Page.(search.CursorPage); cursorPage {
			return search.Result{}, ErrPartialResults
		}
	}

	nextCursor := ""
	pageSize := searchPageSize(request.Page)
	if len(decoded.Hits) > pageSize {
		return search.Result{}, malformedFailure(OperationSearch, ErrMalformedResponse)
	}
	responseBytes := int64(len(responseBody))
	maximumItems := c.search.Limits.MaxPages * c.search.Limits.MaxPageItems
	if ownedPIT {
		if state.Items > maximumItems-len(decoded.Hits) {
			return search.Result{}, search.ErrPageLimit
		}
		if state.Bytes > c.search.Limits.MaxResultBytes-responseBytes {
			return search.Result{}, search.ErrPageLimit
		}
	}
	validated, validationErr := search.NewResult(
		decoded.Hits, decoded.Total, decoded.Aggregations, decoded.Suggestions,
		decoded.Diagnostics, "",
	)
	if validationErr != nil {
		return search.Result{}, malformedFailure(OperationSearch, validationErr)
	}
	if ownedPIT && len(decoded.Hits) == pageSize {
		last := decoded.Hits[len(decoded.Hits)-1]
		if state.Page >= c.search.Limits.MaxPages-1 {
			return search.Result{}, search.ErrPageLimit
		}
		if state.Items >= maximumItems-len(decoded.Hits) {
			return search.Result{}, search.ErrPageLimit
		}
		if state.Bytes >= c.search.Limits.MaxResultBytes-responseBytes {
			return search.Result{}, search.ErrPageLimit
		}
		state.SortValues = last.SortValues
		state.Page++
		state.Items += len(decoded.Hits)
		state.Bytes += responseBytes
		nextCursor, err = c.search.CursorCodec.Encode(binding, state)
		if err != nil {
			return search.Result{}, err
		}
		// The identical payload was validated above; NewResult does not validate
		// the opaque cursor added here, so this reconstruction cannot fail.
		result, _ = search.NewResult(
			decoded.Hits, decoded.Total, decoded.Aggregations, decoded.Suggestions,
			decoded.Diagnostics, nextCursor,
		)
		c.pits.yield(pitLease)
		ownedPIT = false
		return result, nil
	}
	if ownedPIT {
		ownedPIT = false
		if err := c.deletePIT(context.WithoutCancel(ctx), state.PointInTime); err != nil {
			c.transport.telemetry.signal(ctx, OperationDeletePIT, TelemetryPITCleanupFailure)
			return search.Result{}, err
		}
		c.pits.release(pitLease)
	}
	return validated, nil
}

// PointInTimeSnapshot reports this client's bounded PIT ownership without
// exposing tenant, index, query, or backend PIT identifiers.
func (c *Client) PointInTimeSnapshot() PointInTimeSnapshot {
	if c == nil {
		return PointInTimeSnapshot{}
	}
	return c.pits.snapshot()
}

func validateSearchResponseContract(request search.Request, physicalIndex string, response decodedSearch, limits search.Limits) error {
	if _, offset := request.Page.(search.OffsetPage); offset && response.PITID != "" {
		return ErrMalformedResponse
	}
	if !sameResponseKeys(response.Aggregations, request.Aggregations) || !sameResponseKeys(response.Suggestions, request.Suggestions) {
		return ErrMalformedResponse
	}
	for _, hit := range response.Hits {
		if !validPhysicalName(hit.Index) || hit.Index != physicalIndex || len(hit.ID) > limits.MaxIDBytes || len(hit.Source) > limits.MaxSourceBytes ||
			len(hit.SortValues) != len(request.Sort) {
			return ErrMalformedResponse
		}
		if len(hit.Source) > 0 {
			if _, err := search.NewDocument(request.Tenant, request.Index, hit.ID, hit.Version, hit.Source, limits); err != nil {
				return ErrMalformedResponse
			}
			if !sourceWithinProjection(hit.Source, request.Projection) {
				return ErrMalformedResponse
			}
		}
		for _, value := range hit.SortValues {
			if len(value) > limits.MaxQueryBytes {
				return ErrMalformedResponse
			}
		}
		if len(hit.Highlights) > len(request.Highlights) {
			return ErrMalformedResponse
		}
		for field, fragments := range hit.Highlights {
			requested, exists := request.Highlights[field]
			if !exists || len(fragments) > requested.MaxFragments {
				return ErrMalformedResponse
			}
			for _, fragment := range fragments {
				if len(fragment) > limits.MaxSourceBytes {
					return ErrMalformedResponse
				}
			}
		}
	}
	return nil
}

func sourceWithinProjection(source json.RawMessage, projection search.Projection) bool {
	if len(projection.Includes) == 0 && len(projection.Excludes) == 0 {
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()
	var value map[string]any
	if decoder.Decode(&value) != nil || value == nil || decoder.Decode(&struct{}{}) != io.EOF {
		return false
	}
	return projectedValueAllowed(value, "", projection)
}

func projectedValueAllowed(value any, prefix string, projection search.Projection) bool {
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 && prefix != "" {
			return projectionAllowsPath(prefix, projection)
		}
		for name, child := range typed {
			field := name
			if prefix != "" {
				field = prefix + "." + name
			}
			if !projectedValueAllowed(child, field, projection) {
				return false
			}
		}
		return true
	case []any:
		if len(typed) == 0 {
			return projectionAllowsPath(prefix, projection)
		}
		for _, child := range typed {
			if !projectedValueAllowed(child, prefix, projection) {
				return false
			}
		}
		return true
	default:
		return projectionAllowsPath(prefix, projection)
	}
}

func projectionAllowsPath(field string, projection search.Projection) bool {
	for _, pattern := range projection.Excludes {
		if projectionPatternMatches(pattern, field) {
			return false
		}
	}
	if len(projection.Includes) == 0 {
		return true
	}
	for _, pattern := range projection.Includes {
		if projectionPatternMatches(pattern, field) {
			return true
		}
	}
	return false
}

func projectionPatternMatches(pattern, field string) bool {
	if pattern == field || strings.HasPrefix(field, pattern+".") {
		return true
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return false
	}
	position := 0
	if parts[0] != "" {
		if !strings.HasPrefix(field, parts[0]) {
			return false
		}
		position = len(parts[0])
	}
	for _, part := range parts[1 : len(parts)-1] {
		if part == "" {
			continue
		}
		next := strings.Index(field[position:], part)
		if next < 0 {
			return false
		}
		position += next + len(part)
	}
	last := parts[len(parts)-1]
	return last == "" || len(field)-len(last) >= position && strings.HasSuffix(field, last)
}

func sameResponseKeys[A, B any](response map[string]A, requested map[string]B) bool {
	if len(response) != len(requested) {
		return false
	}
	for name := range response {
		if _, exists := requested[name]; !exists {
			return false
		}
	}
	return true
}

func cloneExecutionRequest(request search.Request) search.Request {
	request.Query = cloneAuthorizationQuery(request.Query)
	request.Sort = append([]search.Sort(nil), request.Sort...)
	request.Projection = search.Projection{
		Includes: append([]string(nil), request.Projection.Includes...),
		Excludes: append([]string(nil), request.Projection.Excludes...),
	}
	request.Highlights = cloneMap(request.Highlights)
	request.Aggregations = cloneAuthorizationAggregations(request.Aggregations)
	request.Suggestions = cloneMap(request.Suggestions)
	return request
}

func (c *Client) authorizeSearch(ctx context.Context, request search.Request) error {
	if err := ctx.Err(); err != nil {
		return cancelledFailure(OperationSearch, err)
	}
	if c.search.Authorizer == nil {
		return ErrSearchDenied
	}
	intent := SearchAuthorization{
		Tenant: request.Tenant, Index: request.Index, Query: cloneAuthorizationQuery(request.Query),
		Sort:       append([]search.Sort(nil), request.Sort...),
		Pagination: searchPaginationAuthorization(request.Page),
		Projection: search.Projection{
			Includes: append([]string(nil), request.Projection.Includes...),
			Excludes: append([]string(nil), request.Projection.Excludes...),
		},
		Highlights:   cloneMap(request.Highlights),
		Aggregations: cloneAuthorizationAggregations(request.Aggregations),
		Suggestions:  cloneMap(request.Suggestions),
		FullSource:   len(request.Projection.Includes) == 0 && len(request.Projection.Excludes) == 0,
	}
	if err := c.search.Authorizer.AuthorizeSearch(ctx, intent); err != nil {
		return sanitizedCallbackFailure(OperationSearch, ErrSearchDenied, err)
	}
	if err := ctx.Err(); err != nil {
		return cancelledFailure(OperationSearch, err)
	}
	return nil
}

func (c *Client) resolveIndexTarget(ctx context.Context, operation Operation, tenant, index string, access IndexAccess) (IndexTarget, error) {
	if err := ctx.Err(); err != nil {
		return IndexTarget{}, cancelledFailure(operation, err)
	}
	target, err := c.search.Resolver.Resolve(ctx, tenant, index, access)
	if err != nil {
		return IndexTarget{}, sanitizedCallbackFailure(operation, ErrUnsafeIndexTarget, err)
	}
	if err := ctx.Err(); err != nil {
		return IndexTarget{}, cancelledFailure(operation, err)
	}
	if !validIndexTarget(target) {
		return IndexTarget{}, ErrUnsafeIndexTarget
	}
	return target, nil
}

func searchPaginationAuthorization(page any) SearchPaginationAuthorization {
	switch value := page.(type) {
	case search.CursorPage:
		return SearchPaginationAuthorization{
			Kind: SearchPaginationCursor, Size: value.Size, KeepAlive: value.KeepAlive,
			Continuation: value.Cursor != "",
		}
	case search.OffsetPage:
		return SearchPaginationAuthorization{Kind: SearchPaginationOffset, Size: value.Size, Offset: value.Offset}
	default:
		return SearchPaginationAuthorization{}
	}
}

func cloneAuthorizationQuery(query search.Query) search.Query {
	switch node := query.(type) {
	case search.BoolQuery:
		node.Must = cloneAuthorizationQueries(node.Must)
		node.Should = cloneAuthorizationQueries(node.Should)
		node.Filter = cloneAuthorizationQueries(node.Filter)
		node.MustNot = cloneAuthorizationQueries(node.MustNot)
		return node
	case search.FullTextQuery:
		node.Fields = append([]string(nil), node.Fields...)
		return node
	case search.RangeQuery:
		node.GT = cloneAuthorizationValue(node.GT)
		node.GTE = cloneAuthorizationValue(node.GTE)
		node.LT = cloneAuthorizationValue(node.LT)
		node.LTE = cloneAuthorizationValue(node.LTE)
		return node
	case search.RawExtensionQuery:
		node.Payload = append(json.RawMessage(nil), node.Payload...)
		return node
	default:
		return query
	}
}

func cloneAuthorizationQueries(queries []search.Query) []search.Query {
	if queries == nil {
		return nil
	}
	cloned := make([]search.Query, len(queries))
	for index, query := range queries {
		cloned[index] = cloneAuthorizationQuery(query)
	}
	return cloned
}

func cloneAuthorizationValue(value *search.Value) *search.Value {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneAuthorizationAggregations(source map[string]search.Aggregation) map[string]search.Aggregation {
	if source == nil {
		return nil
	}
	result := make(map[string]search.Aggregation, len(source))
	for name, aggregation := range source {
		switch value := aggregation.(type) {
		case search.RangeAggregation:
			value.Buckets = append([]search.RangeBucket(nil), value.Buckets...)
			for index := range value.Buckets {
				value.Buckets[index].From = cloneAuthorizationValue(value.Buckets[index].From)
				value.Buckets[index].To = cloneAuthorizationValue(value.Buckets[index].To)
			}
			result[name] = value
		default:
			result[name] = aggregation
		}
	}
	return result
}

func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	if source == nil {
		return nil
	}
	result := make(map[K]V, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func validIndexTarget(target IndexTarget) bool {
	return indexTargetPattern.MatchString(target.Name) && target.Name != "." && target.Name != ".." &&
		indexTargetPattern.MatchString(target.PhysicalName) && target.PhysicalName != "." && target.PhysicalName != ".." &&
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
	body, err := c.executeMutation(ctx, OperationCreatePIT, http.MethodPost, path, nil, http.StatusOK, http.StatusCreated)
	if err != nil {
		return "", err
	}
	var response struct {
		PITID string `json:"pit_id"`
	}
	if json.Unmarshal(body, &response) != nil {
		return "", unknownMalformedFailure(OperationCreatePIT, ErrMalformedResponse)
	}
	if response.PITID == "" || len(response.PITID) > c.search.Limits.MaxQueryBytes || strings.ContainsAny(response.PITID, "\x00\r\n") {
		return "", unknownMalformedFailure(OperationCreatePIT, ErrMalformedResponse)
	}
	return response.PITID, nil
}

func (c *Client) deletePIT(ctx context.Context, id string) error {
	body, _ := json.Marshal(map[string]string{"pit_id": id})
	responseBody, err := c.executeMutation(ctx, OperationDeletePIT, http.MethodDelete, "/_search/point_in_time", body, http.StatusOK)
	if err != nil {
		return err
	}
	var response struct {
		PITs []struct {
			PITID      string `json:"pit_id"`
			Successful bool   `json:"successful"`
		} `json:"pits"`
	}
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	if decoder.Decode(&response) != nil || trailingJSON(decoder) || len(response.PITs) != 1 ||
		response.PITs[0].PITID != id || !response.PITs[0].Successful {
		return unknownMalformedFailure(OperationDeletePIT, ErrMalformedResponse)
	}
	return nil
}

func (c *Client) execute(ctx context.Context, operation Operation, method, path string, body []byte, accepted ...int) ([]byte, error) {
	return c.executeContent(ctx, operation, method, path, body, "application/json", accepted...)
}

func (c *Client) executeBounded(ctx context.Context, operation Operation, method, path string, body []byte, maximumResponseBytes int64, accepted ...int) (responseBytes []byte, err error) {
	responseBytes, _, err = c.executeContentWithStatusBounded(ctx, operation, method, path, body, "application/json", false, maximumResponseBytes, accepted...)
	return responseBytes, err
}

func (c *Client) executeContent(ctx context.Context, operation Operation, method, path string, body []byte, contentType string, accepted ...int) (responseBytes []byte, err error) {
	responseBytes, _, err = c.executeContentWithStatus(ctx, operation, method, path, body, contentType, false, accepted...)
	return responseBytes, err
}

func (c *Client) executeMutation(ctx context.Context, operation Operation, method, path string, body []byte, accepted ...int) ([]byte, error) {
	responseBytes, _, err := c.executeMutationWithStatus(ctx, operation, method, path, body, accepted...)
	return responseBytes, err
}

func (c *Client) executeMutationWithStatus(ctx context.Context, operation Operation, method, path string, body []byte, accepted ...int) ([]byte, int, error) {
	return c.executeContentWithStatus(ctx, operation, method, path, body, "application/json", true, accepted...)
}

func (c *Client) executeContentWithStatus(ctx context.Context, operation Operation, method, path string, body []byte, contentType string, ambiguousOnDispatch bool, accepted ...int) (responseBytes []byte, status int, err error) {
	return c.executeContentWithStatusBounded(ctx, operation, method, path, body, contentType, ambiguousOnDispatch, c.maximumResponseBytes, accepted...)
}

func (c *Client) executeContentWithStatusBounded(ctx context.Context, operation Operation, method, path string, body []byte, contentType string, ambiguousOnDispatch bool, maximumResponseBytes int64, accepted ...int) (responseBytes []byte, status int, err error) {
	if ctx == nil {
		return nil, 0, ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, cancelledFailure(operation, err)
	}
	if err := c.begin(); err != nil {
		return nil, 0, err
	}
	requestCtx, cancel := context.WithTimeout(withOperation(ctx, operation), c.timeout)
	defer cancel()
	request, requestErr := http.NewRequestWithContext(requestCtx, method, path, bytes.NewReader(body))
	if requestErr != nil {
		return nil, 0, malformedFailure(operation, requestErr)
	}
	if body != nil {
		request.Header.Set("Content-Type", contentType)
	}
	response, transportErr := c.client.Stream(request)
	if response == nil || transportErr != nil {
		responseStatus := 0
		if response != nil {
			responseStatus = response.StatusCode
			_ = response.Body.Close()
		}
		if ambiguousOnDispatch {
			return nil, responseStatus, unknownTransportFailure(operation, transportErr)
		}
		return nil, responseStatus, transportFailure(operation, transportErr)
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := readBounded(response.Body, maximumResponseBytes)
	if err != nil {
		if ambiguousOnDispatch {
			return nil, response.StatusCode, unknownMalformedFailure(operation, err)
		}
		return nil, response.StatusCode, malformedFailure(operation, err)
	}
	for _, status := range accepted {
		if response.StatusCode == status {
			return responseBody, response.StatusCode, nil
		}
	}
	return nil, response.StatusCode, responseFailure(operation, response.StatusCode, responseBody)
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
			field := item.Field
			if field == search.DocumentIDSortField {
				field = "_id"
			}
			sorts[index] = map[string]any{field: options}
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
		if node.MinimumShouldMatch > 0 || len(node.Should) > 0 && len(node.Must) == 0 && len(node.Filter) == 0 {
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
	type responseShards struct {
		Total      *int `json:"total"`
		Successful *int `json:"successful"`
		Skipped    *int `json:"skipped"`
		Failed     *int `json:"failed"`
		Failures   []struct {
			Index  string `json:"index"`
			Reason struct {
				Type string `json:"type"`
			} `json:"reason"`
		} `json:"failures"`
	}
	type responseHit struct {
		Index     string              `json:"_index"`
		ID        string              `json:"_id"`
		Version   uint64              `json:"_version"`
		Score     *float64            `json:"_score"`
		Source    json.RawMessage     `json:"_source"`
		Sort      []json.RawMessage   `json:"sort"`
		Highlight map[string][]string `json:"highlight"`
	}
	type responseHits struct {
		Total *struct {
			Value    *uint64               `json:"value"`
			Relation *search.TotalRelation `json:"relation"`
		} `json:"total"`
		Hits []responseHit `json:"hits"`
	}
	var payload struct {
		Took         *int64                     `json:"took"`
		TimedOut     *bool                      `json:"timed_out"`
		PITID        string                     `json:"pit_id"`
		Shards       *responseShards            `json:"_shards"`
		Hits         *responseHits              `json:"hits"`
		Aggregations map[string]json.RawMessage `json:"aggregations"`
		Suggest      map[string]json.RawMessage `json:"suggest"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&payload); err != nil {
		return decodedSearch{}, ErrMalformedResponse
	}
	invalid := decodedSearch{PITID: payload.PITID}
	if trailingJSON(decoder) {
		return invalid, ErrMalformedResponse
	}
	if payload.Took == nil || payload.TimedOut == nil || payload.Shards == nil ||
		payload.Shards.Total == nil || payload.Shards.Successful == nil ||
		payload.Shards.Skipped == nil || payload.Shards.Failed == nil ||
		payload.Hits == nil || payload.Hits.Total == nil || payload.Hits.Total.Value == nil ||
		payload.Hits.Total.Relation == nil || payload.Hits.Hits == nil {
		return invalid, ErrMalformedResponse
	}
	took := *payload.Took
	timedOut := *payload.TimedOut
	shardTotal := *payload.Shards.Total
	shardSuccessful := *payload.Shards.Successful
	shardSkipped := *payload.Shards.Skipped
	shardFailed := *payload.Shards.Failed
	if took < 0 || took > math.MaxInt64/int64(time.Millisecond) {
		return invalid, ErrMalformedResponse
	}
	if shardTotal < 0 {
		return invalid, ErrMalformedResponse
	}
	if shardSuccessful < 0 {
		return invalid, ErrMalformedResponse
	}
	if shardSkipped < 0 {
		return invalid, ErrMalformedResponse
	}
	if shardFailed < 0 {
		return invalid, ErrMalformedResponse
	}
	total := search.Total{Value: *payload.Hits.Total.Value, Relation: *payload.Hits.Total.Relation}
	if total.Relation != search.TotalExact && total.Relation != search.TotalLowerBound {
		return invalid, ErrMalformedResponse
	}
	if total.Value < uint64(len(payload.Hits.Hits)) {
		return invalid, ErrMalformedResponse
	}
	hits := make([]search.Hit, len(payload.Hits.Hits))
	for index, hit := range payload.Hits.Hits {
		if hit.Index == "" || hit.ID == "" || hit.Version == 0 {
			return invalid, ErrMalformedResponse
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
	partial := timedOut || shardFailed > 0
	diagnostics := search.Diagnostics{Backend: "opensearch", Took: time.Duration(took) * time.Millisecond, TimedOut: timedOut, Partial: partial,
		Shards: search.ShardDiagnostics{Total: shardTotal, Successful: shardSuccessful, Skipped: shardSkipped, Failed: shardFailed}, Failures: failures}
	return decodedSearch{Hits: hits, Total: total, Aggregations: payload.Aggregations, Suggestions: payload.Suggest, Diagnostics: diagnostics, PITID: payload.PITID}, nil
}

func trailingJSON(decoder *json.Decoder) bool {
	var extra any
	return decoder.Decode(&extra) != io.EOF
}

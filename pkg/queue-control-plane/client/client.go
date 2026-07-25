// Package client provides a typed bounded administrative API client.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	controlkubernetes "github.com/faustbrian/golib/pkg/queue-control-plane/kubernetes"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

const defaultMaxResponseBytes int64 = 4 << 20

const (
	// APIKeyIDHeader carries the non-secret static key identifier accepted by
	// the deployable control-plane server.
	APIKeyIDHeader = "X-Queue-Control-Key-ID" //nolint:gosec // A protocol header name, not a credential.
	// APIKeySecretHeader carries the static key credential accepted by the
	// deployable control-plane server.
	APIKeySecretHeader = "X-Queue-Control-Key" //nolint:gosec // A protocol header name, not a credential.
)

var (
	ErrInvalidConfiguration = errors.New("control-plane client: invalid configuration")
	ErrInvalidRequest       = errors.New("control-plane client: invalid request")
	ErrInvalidToken         = errors.New("control-plane client: invalid bearer token")
	ErrInvalidAPIKey        = errors.New("control-plane client: invalid API key")
	ErrResponseTooLarge     = errors.New("control-plane client: response too large")
)

// TokenSource acquires a bearer token for one request context.
type TokenSource interface {
	Token(context.Context) (string, error)
}

// APIKeySource acquires a static API-key identifier and secret for one request
// context.
type APIKeySource interface {
	APIKey(context.Context) (string, string, error)
}

// Config defines the API endpoint, authentication, and response bounds.
type Config struct {
	BaseURL          string
	HTTPClient       *http.Client
	Tokens           TokenSource
	APIKeys          APIKeySource
	MaxResponseBytes int64
}

// WorkerQuery contains bounded worker-list filters.
type WorkerQuery struct {
	Limit uint32
	After string
	State fleet.State
	Queue string
}

// WorkloadQuery contains bounded Kubernetes pagination.
type WorkloadQuery struct {
	Limit    int64
	Continue string
}

// AuditQuery contains bounded audit-history pagination.
type AuditQuery struct {
	After uint64
	Limit uint32
}

// CommandQuery contains bounded command-history pagination.
type CommandQuery struct {
	Cursor string
	Limit  uint32
}

// QueueQuery contains bounded queue-status pagination.
type QueueQuery struct {
	Cursor string
	Limit  uint32
}

// RecordQuery contains bounded failure and dead-letter list controls.
type RecordQuery struct {
	Cursor    string
	Limit     uint32
	Search    string
	Sort      queue.SortField
	Direction queue.SortDirection
}

// RecordInspectOptions controls independently privileged record fields.
type RecordInspectOptions struct {
	Payload           queue.PayloadVisibility
	RevealDiagnostics bool
}

// APIError is a stable non-success administrative response.
type APIError struct {
	Status int
	Code   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("control-plane API: status %d code %s", e.Status, e.Code)
}

// Client calls the versioned administrative API.
type Client struct {
	baseURL          *url.URL
	httpClient       *http.Client
	tokens           TokenSource
	apiKeys          APIKeySource
	maxResponseBytes int64
}

// New creates a typed administrative API client.
func New(config Config) (*Client, error) {
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil || baseURL.Host == "" ||
		(baseURL.Scheme != "http" && baseURL.Scheme != "https") ||
		baseURL.User != nil || (baseURL.Path != "" && baseURL.Path != "/") ||
		baseURL.RawQuery != "" || baseURL.Fragment != "" ||
		(nilInterface(config.Tokens) == nilInterface(config.APIKeys)) ||
		config.MaxResponseBytes < 0 {
		return nil, ErrInvalidConfiguration
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	httpClient := *config.HTTPClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if config.MaxResponseBytes == 0 {
		config.MaxResponseBytes = defaultMaxResponseBytes
	}
	baseURL.Path = ""

	return &Client{
		baseURL:          baseURL,
		httpClient:       &httpClient,
		tokens:           config.Tokens,
		apiKeys:          config.APIKeys,
		maxResponseBytes: config.MaxResponseBytes,
	}, nil
}

// ExecuteCommand submits one actor-free command request for a tenant.
func (c *Client) ExecuteCommand(
	ctx context.Context,
	tenant string,
	command apihttp.CommandRequest,
) (controlplane.CommandResult, error) {
	if strings.TrimSpace(tenant) == "" {
		return controlplane.CommandResult{}, ErrInvalidRequest
	}
	endpoint := c.baseURL.JoinPath("v1", "tenants", tenant, "commands")
	var result controlplane.CommandResult
	if err := c.do(ctx, http.MethodPost, endpoint, command, &result); err != nil {
		return controlplane.CommandResult{}, err
	}

	return result, nil
}

// GetCommand returns one tenant-scoped durable command outcome.
func (c *Client) GetCommand(
	ctx context.Context,
	tenant string,
	key string,
) (controlplane.CommandResult, error) {
	if invalidIdentity(tenant) || invalidIdentity(key) {
		return controlplane.CommandResult{}, ErrInvalidRequest
	}
	endpoint := c.baseURL.JoinPath("v1", "tenants", tenant, "commands", key)
	var result controlplane.CommandResult
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &result); err != nil {
		return controlplane.CommandResult{}, err
	}

	return result, nil
}

// ListCommands returns one bounded tenant command-history page.
func (c *Client) ListCommands(
	ctx context.Context,
	tenant string,
	query CommandQuery,
) (apihttp.CommandHistoryPage, error) {
	if invalidIdentity(tenant) || query.Limit > apihttp.MaxCommandPageSize ||
		len(query.Cursor) > apihttp.MaxCommandCursorBytes {
		return apihttp.CommandHistoryPage{}, ErrInvalidRequest
	}
	endpoint := c.baseURL.JoinPath("v1", "tenants", tenant, "commands")
	values := endpoint.Query()
	if query.Cursor != "" {
		values.Set("cursor", query.Cursor)
	}
	if query.Limit > 0 {
		values.Set("limit", strconv.FormatUint(uint64(query.Limit), 10))
	}
	endpoint.RawQuery = values.Encode()

	var page apihttp.CommandHistoryPage
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &page); err != nil {
		return apihttp.CommandHistoryPage{}, err
	}

	return page, nil
}

// ListWorkers returns one bounded tenant worker page.
func (c *Client) ListWorkers(
	ctx context.Context,
	tenant string,
	query WorkerQuery,
) (apihttp.WorkerPage, error) {
	if strings.TrimSpace(tenant) == "" || query.Limit > apihttp.MaxWorkerPageSize {
		return apihttp.WorkerPage{}, ErrInvalidRequest
	}
	endpoint := c.baseURL.JoinPath("v1", "tenants", tenant, "workers")
	values := endpoint.Query()
	if query.Limit > 0 {
		values.Set("limit", strconv.FormatUint(uint64(query.Limit), 10))
	}
	if query.After != "" {
		values.Set("after", query.After)
	}
	if query.State != "" {
		values.Set("state", string(query.State))
	}
	if query.Queue != "" {
		values.Set("queue", query.Queue)
	}
	endpoint.RawQuery = values.Encode()

	var page apihttp.WorkerPage
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &page); err != nil {
		return apihttp.WorkerPage{}, err
	}

	return page, nil
}

// ListQueues returns one bounded tenant queue-status page.
func (c *Client) ListQueues(
	ctx context.Context,
	tenant string,
	query QueueQuery,
) (apihttp.QueuePage, error) {
	if invalidIdentity(tenant) || query.Limit > queue.MaxStatusPageSize ||
		len(query.Cursor) > queue.MaxCursorBytes {
		return apihttp.QueuePage{}, ErrInvalidRequest
	}
	endpoint := c.baseURL.JoinPath("v1", "tenants", tenant, "queues")
	values := endpoint.Query()
	if query.Cursor != "" {
		values.Set("cursor", query.Cursor)
	}
	if query.Limit > 0 {
		values.Set("limit", strconv.FormatUint(uint64(query.Limit), 10))
	}
	endpoint.RawQuery = values.Encode()

	var page apihttp.QueuePage
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &page); err != nil {
		return apihttp.QueuePage{}, err
	}

	return page, nil
}

// ListWorkloads returns one bounded tenant workload page.
func (c *Client) ListWorkloads(
	ctx context.Context,
	tenant string,
	query WorkloadQuery,
) (controlkubernetes.Page, error) {
	if invalidIdentity(tenant) || query.Limit < 0 || query.Limit > controlkubernetes.MaxPageSize ||
		len(query.Continue) > controlkubernetes.MaxContinueTokenBytes {
		return controlkubernetes.Page{}, ErrInvalidRequest
	}
	endpoint := c.baseURL.JoinPath("v1", "tenants", tenant, "workloads")
	values := endpoint.Query()
	if query.Limit > 0 {
		values.Set("limit", strconv.FormatInt(query.Limit, 10))
	}
	if query.Continue != "" {
		values.Set("continue", query.Continue)
	}
	endpoint.RawQuery = values.Encode()

	var page controlkubernetes.Page
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &page); err != nil {
		return controlkubernetes.Page{}, err
	}

	return page, nil
}

// ListAudit returns one bounded tenant audit-history page.
func (c *Client) ListAudit(
	ctx context.Context,
	tenant string,
	query AuditQuery,
) (apihttp.AuditPage, error) {
	if strings.TrimSpace(tenant) == "" || query.Limit > apihttp.MaxAuditPageSize {
		return apihttp.AuditPage{}, ErrInvalidRequest
	}
	endpoint := c.baseURL.JoinPath("v1", "tenants", tenant, "audit")
	values := endpoint.Query()
	if query.After > 0 {
		values.Set("after", strconv.FormatUint(query.After, 10))
	}
	if query.Limit > 0 {
		values.Set("limit", strconv.FormatUint(uint64(query.Limit), 10))
	}
	endpoint.RawQuery = values.Encode()

	var page apihttp.AuditPage
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &page); err != nil {
		return apihttp.AuditPage{}, err
	}

	return page, nil
}

// ListFailures returns one bounded tenant failure page.
func (c *Client) ListFailures(
	ctx context.Context,
	tenant string,
	query RecordQuery,
) (apihttp.RecordPage, error) {
	return c.listRecords(ctx, tenant, "failures", query)
}

// ListDeadLetters returns one bounded tenant dead-letter page.
func (c *Client) ListDeadLetters(
	ctx context.Context,
	tenant string,
	query RecordQuery,
) (apihttp.RecordPage, error) {
	return c.listRecords(ctx, tenant, "dead-letters", query)
}

// InspectFailure returns one tenant failure at the requested visibility.
func (c *Client) InspectFailure(
	ctx context.Context,
	tenant string,
	id string,
	visibility queue.PayloadVisibility,
) (apihttp.Record, error) {
	return c.InspectFailureWithOptions(ctx, tenant, id, RecordInspectOptions{Payload: visibility})
}

// InspectFailureWithOptions returns a failure with explicit privileged fields.
func (c *Client) InspectFailureWithOptions(
	ctx context.Context,
	tenant string,
	id string,
	options RecordInspectOptions,
) (apihttp.Record, error) {
	return c.inspectRecord(ctx, tenant, "failures", id, options)
}

// InspectDeadLetter returns one tenant dead letter at the requested visibility.
func (c *Client) InspectDeadLetter(
	ctx context.Context,
	tenant string,
	id string,
	visibility queue.PayloadVisibility,
) (apihttp.Record, error) {
	return c.InspectDeadLetterWithOptions(ctx, tenant, id, RecordInspectOptions{Payload: visibility})
}

// InspectDeadLetterWithOptions returns a dead letter with explicit privileged fields.
func (c *Client) InspectDeadLetterWithOptions(
	ctx context.Context,
	tenant string,
	id string,
	options RecordInspectOptions,
) (apihttp.Record, error) {
	return c.inspectRecord(ctx, tenant, "dead-letters", id, options)
}

func (c *Client) listRecords(
	ctx context.Context,
	tenant string,
	collection string,
	query RecordQuery,
) (apihttp.RecordPage, error) {
	if invalidIdentity(tenant) || !validRecordQuery(query) {
		return apihttp.RecordPage{}, ErrInvalidRequest
	}
	endpoint := c.baseURL.JoinPath("v1", "tenants", tenant, collection)
	values := endpoint.Query()
	if query.Cursor != "" {
		values.Set("cursor", query.Cursor)
	}
	if query.Limit > 0 {
		values.Set("limit", strconv.FormatUint(uint64(query.Limit), 10))
	}
	if query.Search != "" {
		values.Set("search", query.Search)
	}
	if query.Sort != "" {
		values.Set("sort", string(query.Sort))
	}
	if query.Direction != "" {
		values.Set("direction", string(query.Direction))
	}
	endpoint.RawQuery = values.Encode()

	var page apihttp.RecordPage
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &page); err != nil {
		return apihttp.RecordPage{}, err
	}

	return page, nil
}

func (c *Client) inspectRecord(
	ctx context.Context,
	tenant string,
	collection string,
	id string,
	options RecordInspectOptions,
) (apihttp.Record, error) {
	if invalidIdentity(tenant) || invalidIdentity(id) || !validPayloadVisibility(options.Payload) {
		return apihttp.Record{}, ErrInvalidRequest
	}
	endpoint := c.baseURL.JoinPath("v1", "tenants", tenant, collection, id)
	if options.Payload != queue.PayloadHidden || options.RevealDiagnostics {
		values := endpoint.Query()
		if options.Payload != queue.PayloadHidden {
			values.Set("payload", string(options.Payload))
		}
		if options.RevealDiagnostics {
			values.Set("diagnostics", "revealed")
		}
		endpoint.RawQuery = values.Encode()
	}
	var record apihttp.Record
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &record); err != nil {
		return apihttp.Record{}, err
	}

	return record, nil
}

func validRecordQuery(query RecordQuery) bool {
	if query.Limit > queue.MaxPageSize || len(query.Cursor) > queue.MaxCursorBytes ||
		len(query.Search) > queue.MaxSearchBytes {
		return false
	}
	if query.Sort != "" && query.Sort != queue.SortOccurredAt &&
		query.Sort != queue.SortQueue && query.Sort != queue.SortAttempts {
		return false
	}

	return query.Direction == "" || query.Direction == queue.SortAscending ||
		query.Direction == queue.SortDescending
}

func validPayloadVisibility(visibility queue.PayloadVisibility) bool {
	return visibility == queue.PayloadHidden || visibility == queue.PayloadRedacted ||
		visibility == queue.PayloadRevealed
}

func (c *Client) do(
	ctx context.Context,
	method string,
	endpoint *url.URL,
	input any,
	output any,
) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("control-plane client: encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return fmt.Errorf("control-plane client: create request: %w", err)
	}
	if err := c.authenticate(ctx, request); err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("control-plane client: request: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	encoded, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("control-plane client: read response: %w", err)
	}
	if int64(len(encoded)) > c.maxResponseBytes {
		return ErrResponseTooLarge
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		problem := apihttp.Problem{Code: "http_error"}
		_ = json.Unmarshal(encoded, &problem)
		return &APIError{Status: response.StatusCode, Code: problem.Code}
	}
	if err := json.Unmarshal(encoded, output); err != nil {
		return fmt.Errorf("control-plane client: decode response: %w", err)
	}

	return nil
}

func (c *Client) authenticate(ctx context.Context, request *http.Request) error {
	if !nilInterface(c.apiKeys) {
		id, secret, err := c.apiKeys.APIKey(ctx)
		if err != nil {
			return err
		}
		if strings.TrimSpace(id) == "" || strings.TrimSpace(secret) == "" {
			return ErrInvalidAPIKey
		}
		request.Header.Set(APIKeyIDHeader, id)
		request.Header.Set(APIKeySecretHeader, secret)

		return nil
	}

	token, err := c.tokens.Token(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return ErrInvalidToken
	}
	request.Header.Set("Authorization", "Bearer "+token)

	return nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	return reflected.Kind() == reflect.Pointer && reflected.IsNil()
}

func invalidIdentity(value string) bool {
	return strings.TrimSpace(value) == "" || len(value) > controlplane.MaxIdentityBytes
}

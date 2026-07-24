// Package managementhttp transports queue management contracts over a
// bounded authenticated HTTP boundary.
package managementhttp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
)

const (
	defaultMaxResponseBytes int64 = 4 << 20
	maxTokenBytes                 = 4_096
)

var (
	ErrInvalidConfiguration = errors.New("management HTTP: invalid configuration")
	ErrInvalidRequest       = errors.New("management HTTP: invalid request")
	ErrInvalidResponse      = errors.New("management HTTP: invalid response")
	ErrResponseTooLarge     = errors.New("management HTTP: response too large")
	ErrRemoteFailure        = errors.New("management HTTP: remote failure")
)

// HandlerConfig provides the worker-side management services and shared
// transport credential.
type HandlerConfig struct {
	Token      string
	Status     management.StatusReader
	Controller management.Controller
	Records    management.RecordReader
}

// ClientConfig provides the worker endpoint, credential, and response bound.
type ClientConfig struct {
	BaseURL          string
	Token            string
	HTTPClient       *http.Client
	MaxResponseBytes int64
}

// Client implements remote management contracts.
type Client struct {
	baseURL          *url.URL
	token            string
	httpClient       *http.Client
	maxResponseBytes int64
}

type handler struct {
	token      string
	status     management.StatusReader
	controller management.Controller
	records    management.RecordReader
}

type problem struct {
	Code string `json:"code"`
}

type protocolVersion struct {
	Major uint16 `json:"major"`
	Minor uint16 `json:"minor"`
}

type workerStatus struct {
	ID           string                  `json:"id"`
	Version      string                  `json:"version"`
	StartedAt    time.Time               `json:"started_at"`
	HeartbeatAt  time.Time               `json:"heartbeat_at"`
	Queues       []string                `json:"queues"`
	Concurrency  uint32                  `json:"concurrency"`
	State        management.WorkerState  `json:"state"`
	CurrentJobs  uint32                  `json:"current_jobs"`
	DrainStatus  management.DrainState   `json:"drain_status"`
	Backend      string                  `json:"backend"`
	Protocol     protocolVersion         `json:"protocol"`
	Capabilities []management.Capability `json:"capabilities"`
}

type workerStatusPage struct {
	Items      []workerStatus `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type measurement[T any] struct {
	Value     T    `json:"value"`
	Supported bool `json:"supported"`
}

type queueMetrics struct {
	Depth            measurement[int64]         `json:"depth"`
	Lag              measurement[int64]         `json:"lag"`
	Pending          measurement[int64]         `json:"pending"`
	OldestAge        measurement[time.Duration] `json:"oldest_age"`
	Throughput       measurement[float64]       `json:"throughput"`
	Runtime          measurement[time.Duration] `json:"runtime"`
	Succeeded        measurement[uint64]        `json:"succeeded"`
	Failed           measurement[uint64]        `json:"failed"`
	Retried          measurement[uint64]        `json:"retried"`
	Reclaimed        measurement[uint64]        `json:"reclaimed"`
	DeadLettered     measurement[uint64]        `json:"dead_lettered"`
	SettlementErrors measurement[uint64]        `json:"settlement_errors"`
}

type queueStatus struct {
	Backend    string       `json:"backend"`
	Queue      string       `json:"queue"`
	ObservedAt time.Time    `json:"observed_at"`
	Metrics    queueMetrics `json:"metrics"`
}

type queueStatusPage struct {
	Items      []queueStatus `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// NewHandler creates a worker-side management HTTP handler.
func NewHandler(config HandlerConfig) (http.Handler, error) {
	statusMissing := nilInterface(config.Status)
	controllerMissing := nilInterface(config.Controller)
	recordsMissing := nilInterface(config.Records)
	if invalidToken(config.Token) || (statusMissing && controllerMissing && recordsMissing) {
		return nil, ErrInvalidConfiguration
	}
	h := &handler{
		token: config.Token, status: config.Status, controller: config.Controller,
		records: config.Records,
	}
	router := http.NewServeMux()
	if !statusMissing {
		router.HandleFunc("GET /v1/status/workers", h.listWorkers)
		router.HandleFunc("GET /v1/status/queues", h.listQueues)
	}
	if !controllerMissing {
		router.HandleFunc("POST /v1/commands", h.executeCommand)
	}
	if !recordsMissing {
		router.HandleFunc("GET /v1/records/failures", h.listFailures)
		router.HandleFunc("GET /v1/records/dead-letters", h.listDeadLetters)
		router.HandleFunc("GET /v1/records/failures/{id}", h.inspectFailure)
		router.HandleFunc("GET /v1/records/dead-letters/{id}", h.inspectDeadLetter)
	}

	return h.authenticate(router), nil
}

// NewClient creates a bounded management HTTP client.
func NewClient(config ClientConfig) (*Client, error) {
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil || baseURL.Host == "" ||
		(baseURL.Scheme != "http" && baseURL.Scheme != "https") ||
		baseURL.User != nil || (baseURL.Path != "" && baseURL.Path != "/") ||
		baseURL.RawQuery != "" || baseURL.Fragment != "" || invalidToken(config.Token) ||
		config.MaxResponseBytes < 0 {
		return nil, ErrInvalidConfiguration
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if config.MaxResponseBytes == 0 {
		config.MaxResponseBytes = defaultMaxResponseBytes
	}
	baseURL.Path = ""

	return &Client{
		baseURL: baseURL, token: config.Token, httpClient: config.HTTPClient,
		maxResponseBytes: config.MaxResponseBytes,
	}, nil
}

// ListWorkers returns one remote bounded worker-status page.
func (c *Client) ListWorkers(
	ctx context.Context,
	request management.StatusPageRequest,
) (management.WorkerStatusPage, error) {
	var wirePage workerStatusPage
	if err := c.listStatus(ctx, "workers", request, &wirePage); err != nil {
		return management.WorkerStatusPage{}, err
	}
	page := managementWorkerPage(wirePage)
	if err := page.Validate(); err != nil {
		return management.WorkerStatusPage{}, ErrInvalidResponse
	}

	return page, nil
}

// ListQueues returns one remote bounded queue-status page.
func (c *Client) ListQueues(
	ctx context.Context,
	request management.StatusPageRequest,
) (management.QueueStatusPage, error) {
	var wirePage queueStatusPage
	if err := c.listStatus(ctx, "queues", request, &wirePage); err != nil {
		return management.QueueStatusPage{}, err
	}
	page := managementQueuePage(wirePage)
	if err := page.Validate(); err != nil {
		return management.QueueStatusPage{}, ErrInvalidResponse
	}

	return page, nil
}

func (c *Client) listStatus(
	ctx context.Context,
	collection string,
	request management.StatusPageRequest,
	output any,
) error {
	if err := request.Validate(); err != nil {
		return ErrInvalidRequest
	}
	endpoint := c.baseURL.JoinPath("v1", "status", collection)
	values := endpoint.Query()
	values.Set("limit", strconv.FormatUint(uint64(request.Limit), 10))
	if request.Cursor != "" {
		values.Set("cursor", request.Cursor)
	}
	endpoint.RawQuery = values.Encode()

	// #nosec G704 -- the operator-supplied base URL is validated at construction,
	// and all subsequent path and query components come from bounded contracts.
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return ErrInvalidRequest
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.token)
	// #nosec G704 -- management endpoints are explicit operator configuration,
	// not request-derived destinations or redirects selected by an API caller.
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return ErrRemoteFailure
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return ErrRemoteFailure
	}
	limited := io.LimitReader(response.Body, c.maxResponseBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return ErrRemoteFailure
	}
	if int64(len(data)) > c.maxResponseBytes {
		return ErrResponseTooLarge
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return ErrInvalidResponse
	}
	if err := ensureEOF(decoder); err != nil {
		return ErrInvalidResponse
	}

	return nil
}

func (h *handler) listWorkers(writer http.ResponseWriter, request *http.Request) {
	pageRequest, ok := statusPageRequest(request.URL.Query())
	if !ok {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	page, err := h.status.ListWorkers(request.Context(), pageRequest)
	if err != nil || page.Validate() != nil {
		writeProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(writer, transportWorkerPage(page))
}

func (h *handler) listQueues(writer http.ResponseWriter, request *http.Request) {
	pageRequest, ok := statusPageRequest(request.URL.Query())
	if !ok {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	page, err := h.status.ListQueues(request.Context(), pageRequest)
	if err != nil || page.Validate() != nil {
		writeProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(writer, transportQueuePage(page))
}

func transportWorkerPage(page management.WorkerStatusPage) workerStatusPage {
	result := workerStatusPage{
		Items: make([]workerStatus, 0, len(page.Items)), NextCursor: page.NextCursor,
	}
	for _, item := range page.Items {
		result.Items = append(result.Items, workerStatus{
			ID: item.ID, Version: item.Version, StartedAt: item.StartedAt,
			HeartbeatAt: item.HeartbeatAt, Queues: append([]string(nil), item.Queues...),
			Concurrency: item.Concurrency, State: item.State, CurrentJobs: item.CurrentJobs,
			DrainStatus: item.DrainStatus, Backend: item.Backend,
			Protocol:     protocolVersion{Major: item.Protocol.Major, Minor: item.Protocol.Minor},
			Capabilities: append([]management.Capability(nil), item.Capabilities...),
		})
	}

	return result
}

func managementWorkerPage(page workerStatusPage) management.WorkerStatusPage {
	result := management.WorkerStatusPage{
		Items: make([]management.WorkerStatus, 0, len(page.Items)), NextCursor: page.NextCursor,
	}
	for _, item := range page.Items {
		result.Items = append(result.Items, management.WorkerStatus{
			ID: item.ID, Version: item.Version, StartedAt: item.StartedAt,
			HeartbeatAt: item.HeartbeatAt, Queues: append([]string(nil), item.Queues...),
			Concurrency: item.Concurrency, State: item.State, CurrentJobs: item.CurrentJobs,
			DrainStatus: item.DrainStatus, Backend: item.Backend,
			Protocol:     management.ProtocolVersion{Major: item.Protocol.Major, Minor: item.Protocol.Minor},
			Capabilities: append([]management.Capability(nil), item.Capabilities...),
		})
	}

	return result
}

func transportQueuePage(page management.QueueStatusPage) queueStatusPage {
	result := queueStatusPage{
		Items: make([]queueStatus, 0, len(page.Items)), NextCursor: page.NextCursor,
	}
	for _, item := range page.Items {
		result.Items = append(result.Items, queueStatus{
			Backend: item.Backend, Queue: item.Queue, ObservedAt: item.ObservedAt,
			Metrics: transportQueueMetrics(item.Metrics),
		})
	}

	return result
}

func managementQueuePage(page queueStatusPage) management.QueueStatusPage {
	result := management.QueueStatusPage{
		Items: make([]management.QueueStatus, 0, len(page.Items)), NextCursor: page.NextCursor,
	}
	for _, item := range page.Items {
		result.Items = append(result.Items, management.QueueStatus{
			Backend: item.Backend, Queue: item.Queue, ObservedAt: item.ObservedAt,
			Metrics: managementQueueMetrics(item.Metrics),
		})
	}

	return result
}

func transportQueueMetrics(metrics management.QueueMetrics) queueMetrics {
	return queueMetrics{
		Depth: measured(metrics.Depth), Lag: measured(metrics.Lag),
		Pending: measured(metrics.Pending), OldestAge: measured(metrics.OldestAge),
		Throughput: measured(metrics.Throughput), Runtime: measured(metrics.Runtime),
		Succeeded: measured(metrics.Succeeded), Failed: measured(metrics.Failed),
		Retried: measured(metrics.Retried), Reclaimed: measured(metrics.Reclaimed),
		DeadLettered:     measured(metrics.DeadLettered),
		SettlementErrors: measured(metrics.SettlementErrors),
	}
}

func managementQueueMetrics(metrics queueMetrics) management.QueueMetrics {
	return management.QueueMetrics{
		Depth: managementMeasured(metrics.Depth), Lag: managementMeasured(metrics.Lag),
		Pending: managementMeasured(metrics.Pending), OldestAge: managementMeasured(metrics.OldestAge),
		Throughput: managementMeasured(metrics.Throughput), Runtime: managementMeasured(metrics.Runtime),
		Succeeded: managementMeasured(metrics.Succeeded), Failed: managementMeasured(metrics.Failed),
		Retried: managementMeasured(metrics.Retried), Reclaimed: managementMeasured(metrics.Reclaimed),
		DeadLettered:     managementMeasured(metrics.DeadLettered),
		SettlementErrors: managementMeasured(metrics.SettlementErrors),
	}
}

func measured[T any](value management.Measurement[T]) measurement[T] {
	return measurement[T]{Value: value.Value, Supported: value.Supported}
}

func managementMeasured[T any](value measurement[T]) management.Measurement[T] {
	return management.Measurement[T]{Value: value.Value, Supported: value.Supported}
}

func (h *handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		provided, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if !found || len(provided) != len(h.token) ||
			subtle.ConstantTimeCompare([]byte(provided), []byte(h.token)) != 1 {
			writeProblem(writer, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func statusPageRequest(values url.Values) (management.StatusPageRequest, bool) {
	for key, entries := range values {
		if (key != "cursor" && key != "limit") || len(entries) != 1 {
			return management.StatusPageRequest{}, false
		}
	}
	request := management.StatusPageRequest{}
	rawLimit, exists := values["limit"]
	if !exists || rawLimit[0] == "" {
		return management.StatusPageRequest{}, false
	}
	limit, err := strconv.ParseUint(rawLimit[0], 10, 32)
	if err != nil {
		return management.StatusPageRequest{}, false
	}
	request.Limit = uint32(limit)
	if rawCursor, exists := values["cursor"]; exists {
		if rawCursor[0] == "" {
			return management.StatusPageRequest{}, false
		}
		request.Cursor = rawCursor[0]
	}
	if err := request.Validate(); err != nil {
		return management.StatusPageRequest{}, false
	}

	return request, true
}

func writeProblem(writer http.ResponseWriter, status int, code string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(problem{Code: code})
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(writer).Encode(value)
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalidResponse
	}

	return nil
}

func invalidToken(token string) bool {
	return strings.TrimSpace(token) == "" || len(token) > maxTokenBytes
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

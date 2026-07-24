package apihttp

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

const defaultQueuePageSize uint32 = 100

// Measurement distinguishes an honestly measured zero from unsupported data.
type Measurement[T any] struct {
	Value     T    `json:"value"`
	Supported bool `json:"supported"`
}

// QueuePage is one bounded page of queue observations.
type QueuePage struct {
	Queues     []Queue `json:"queues"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

// Queue is a backend-neutral logical queue observation.
type Queue struct {
	Backend    string       `json:"backend"`
	Name       string       `json:"name"`
	ObservedAt time.Time    `json:"observed_at"`
	Metrics    QueueMetrics `json:"metrics"`
}

// QueueMetrics contains supported gauges and monotonic lifecycle counters.
type QueueMetrics struct {
	Depth            Measurement[int64]   `json:"depth"`
	Lag              Measurement[int64]   `json:"lag"`
	Pending          Measurement[int64]   `json:"pending"`
	OldestAgeSeconds Measurement[float64] `json:"oldest_age_seconds"`
	Throughput       Measurement[float64] `json:"throughput"`
	RuntimeSeconds   Measurement[float64] `json:"runtime_seconds"`
	Succeeded        Measurement[uint64]  `json:"succeeded"`
	Failed           Measurement[uint64]  `json:"failed"`
	Retried          Measurement[uint64]  `json:"retried"`
	Reclaimed        Measurement[uint64]  `json:"reclaimed"`
	DeadLettered     Measurement[uint64]  `json:"dead_lettered"`
	SettlementErrors Measurement[uint64]  `json:"settlement_errors"`
}

func (h *handler) listQueues(writer http.ResponseWriter, request *http.Request) {
	principal, ok := authentication.PrincipalFromContext(request.Context())
	if !ok || principal.IsAnonymous() {
		writeProblem(writer, http.StatusUnauthorized, "unauthenticated")
		return
	}
	tenant := request.PathValue("tenant")
	if !validIdentity(tenant) {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := h.viewer.Authorize(
		request.Context(), tenant, principal.Subject(), controlplane.PermissionView,
		controlplane.Target{Kind: controlplane.TargetQueue, Name: "queues"},
	); err != nil {
		writeCommandError(writer, err)
		return
	}
	query, err := parseQueueQuery(request.URL.Query())
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	page, err := h.queues.ListQueues(request.Context(), tenant, query)
	if err != nil {
		writeProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(writer, http.StatusOK, queuePage(page))
}

func parseQueueQuery(values url.Values) (queue.StatusPageRequest, error) {
	request := queue.StatusPageRequest{Limit: defaultQueuePageSize}
	allowed := map[string]bool{"cursor": true, "limit": true}
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return queue.StatusPageRequest{}, ErrInvalidConfiguration
		}
	}
	if raw, exists := values["cursor"]; exists {
		if raw[0] == "" {
			return queue.StatusPageRequest{}, ErrInvalidConfiguration
		}
		request.Cursor = raw[0]
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return queue.StatusPageRequest{}, ErrInvalidConfiguration
		}
		request.Limit = uint32(limit)
	} else if _, exists := values["limit"]; exists {
		return queue.StatusPageRequest{}, ErrInvalidConfiguration
	}
	if err := request.Validate(); err != nil {
		return queue.StatusPageRequest{}, ErrInvalidConfiguration
	}

	return request, nil
}

func queuePage(page queue.QueueStatusPage) QueuePage {
	result := QueuePage{Queues: make([]Queue, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, status := range page.Items {
		result.Queues = append(result.Queues, queueModel(status))
	}

	return result
}

func queueModel(status queue.QueueStatus) Queue {
	return Queue{
		Backend: status.Backend, Name: status.Queue, ObservedAt: status.ObservedAt.UTC(),
		Metrics: QueueMetrics{
			Depth:            measured(status.Metrics.Depth),
			Lag:              measured(status.Metrics.Lag),
			Pending:          measured(status.Metrics.Pending),
			OldestAgeSeconds: measuredDuration(status.Metrics.OldestAge),
			Throughput:       measured(status.Metrics.Throughput),
			RuntimeSeconds:   measuredDuration(status.Metrics.Runtime),
			Succeeded:        measured(status.Metrics.Succeeded),
			Failed:           measured(status.Metrics.Failed),
			Retried:          measured(status.Metrics.Retried),
			Reclaimed:        measured(status.Metrics.Reclaimed),
			DeadLettered:     measured(status.Metrics.DeadLettered),
			SettlementErrors: measured(status.Metrics.SettlementErrors),
		},
	}
}

func measured[T any](measurement queue.Measurement[T]) Measurement[T] {
	if !measurement.Supported {
		return Measurement[T]{}
	}

	return Measurement[T]{Value: measurement.Value, Supported: true}
}

func measuredDuration(measurement queue.Measurement[time.Duration]) Measurement[float64] {
	if !measurement.Supported {
		return Measurement[float64]{}
	}

	return Measurement[float64]{Value: measurement.Value.Seconds(), Supported: true}
}

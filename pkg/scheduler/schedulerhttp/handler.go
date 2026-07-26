// Package schedulerhttp provides bounded scheduler inspection and recovery endpoints.
package schedulerhttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/lease"
)

const maxRequestBytes = 4 << 10

// ErrInvalidDependencies reports a missing registry or lease store.
var ErrInvalidDependencies = errors.New("scheduler http: registry and lease store are required")

// Schedule is the bounded public inspection view of a schedule.
type Schedule struct {
	Name               string                    `json:"name"`
	Task               string                    `json:"task"`
	Expression         string                    `json:"expression"`
	Timezone           string                    `json:"timezone"`
	Enabled            bool                      `json:"enabled"`
	Environments       []string                  `json:"environments,omitempty"`
	MissedRunPolicy    scheduler.MissedRunPolicy `json:"missed_run_policy"`
	MaxCatchUp         int                       `json:"max_catch_up"`
	OverlapPolicy      scheduler.OverlapPolicy   `json:"overlap_policy"`
	OnOneServer        bool                      `json:"on_one_server"`
	WithoutOverlapping bool                      `json:"without_overlapping"`
	Metadata           map[string]string         `json:"metadata,omitempty"`
}

// Handler exposes bounded scheduler inspection and fenced recovery endpoints.
type Handler struct {
	registry *scheduler.Registry
	leases   lease.Store
	mux      *http.ServeMux
}

// New constructs a scheduler HTTP handler with no arbitrary execution surface.
func New(registry *scheduler.Registry, leases lease.Store) (*Handler, error) {
	if registry == nil || leases == nil {
		return nil, ErrInvalidDependencies
	}
	handler := &Handler{registry: registry, leases: leases, mux: http.NewServeMux()}
	handler.mux.HandleFunc("GET /v1/schedules", handler.list)
	handler.mux.HandleFunc("GET /v1/validate", handler.validate)
	handler.mux.HandleFunc("GET /v1/schedules/{name}/next", handler.next)
	handler.mux.HandleFunc("GET /v1/schedules/{name}/due", handler.due)
	handler.mux.HandleFunc("GET /v1/schedules/{name}/test", handler.testSchedule)
	handler.mux.HandleFunc("POST /v1/recover", handler.recover)
	return handler, nil
}

func (handler *Handler) validate(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{
		"valid":     true,
		"schedules": len(handler.registry.Schedules()),
	})
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	handler.mux.ServeHTTP(response, request)
}

func (handler *Handler) list(response http.ResponseWriter, _ *http.Request) {
	schedules := handler.registry.Schedules()
	result := make([]Schedule, len(schedules))
	for index, schedule := range schedules {
		result[index] = scheduleView(schedule)
	}
	writeJSON(response, http.StatusOK, result)
}

func (handler *Handler) next(response http.ResponseWriter, request *http.Request) {
	after, err := parseTime(request, "after")
	if err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}
	next, err := handler.registry.Next(request.PathValue("name"), after)
	if err != nil {
		writeSchedulerError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]time.Time{"next": next})
}

func (handler *Handler) due(response http.ResponseWriter, request *http.Request) {
	after, err := parseTime(request, "after")
	if err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}
	through, err := parseTime(request, "through")
	if err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}
	occurrences, err := handler.registry.Due(request.PathValue("name"), after, through)
	if err != nil {
		writeSchedulerError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, occurrences)
}

func (handler *Handler) testSchedule(response http.ResponseWriter, request *http.Request) {
	at, err := parseTime(request, "at")
	if err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}
	occurrences, err := handler.registry.Due(request.PathValue("name"), at.Add(-time.Minute), at)
	if err != nil {
		writeSchedulerError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"due":         len(occurrences) > 0,
		"occurrences": occurrences,
	})
}

func (handler *Handler) recover(response http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBytes)
	var command struct {
		Key          string `json:"key"`
		FencingToken uint64 `json:"fencing_token"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&command); err != nil || command.Key == "" || command.FencingToken == 0 {
		writeError(response, http.StatusBadRequest, errors.New("invalid recovery command"))
		return
	}
	if err := handler.leases.Recover(request.Context(), command.Key, command.FencingToken); err != nil {
		writeLeaseError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func scheduleView(schedule scheduler.Schedule) Schedule {
	return Schedule{
		Name: schedule.Name, Task: schedule.Task, Expression: schedule.Expression,
		Timezone: schedule.Timezone, Enabled: schedule.Enabled,
		Environments:    append([]string(nil), schedule.Environments...),
		MissedRunPolicy: schedule.MissedRunPolicy, MaxCatchUp: schedule.MaxCatchUp,
		OverlapPolicy: schedule.OverlapPolicy, OnOneServer: schedule.OnOneServer,
		WithoutOverlapping: schedule.WithoutOverlapping,
		Metadata:           schedule.Metadata,
	}
}

func parseTime(request *http.Request, field string) (time.Time, error) {
	value := request.URL.Query().Get(field)
	if value == "" {
		return time.Time{}, errors.New("missing " + field)
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errors.New("invalid " + field)
	}
	return parsed, nil
}

func writeSchedulerError(response http.ResponseWriter, err error) {
	if errors.Is(err, scheduler.ErrScheduleNotFound) {
		writeError(response, http.StatusNotFound, err)
		return
	}
	writeError(response, http.StatusUnprocessableEntity, err)
}

func writeLeaseError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, lease.ErrNotFound):
		writeError(response, http.StatusNotFound, err)
	case errors.Is(err, lease.ErrStaleOwner):
		writeError(response, http.StatusConflict, err)
	default:
		writeError(response, http.StatusServiceUnavailable, err)
	}
}

func writeError(response http.ResponseWriter, status int, err error) {
	writeJSON(response, status, map[string]string{"error": err.Error()})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

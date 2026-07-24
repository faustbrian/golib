package schedulerhttp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/lease"
	"github.com/faustbrian/golib/pkg/scheduler/schedulerhttp"
)

type errorStore struct{ recoverErr error }

func (*errorStore) Acquire(context.Context, string, string, time.Duration, time.Time) (lease.Lease, error) {
	return lease.Lease{}, nil
}
func (*errorStore) Heartbeat(context.Context, lease.Lease, time.Duration, time.Time) (lease.Lease, error) {
	return lease.Lease{}, nil
}
func (*errorStore) Release(context.Context, lease.Lease) error           { return nil }
func (*errorStore) Inspect(context.Context, string) (lease.Lease, error) { return lease.Lease{}, nil }
func (store *errorStore) Recover(context.Context, string, uint64) error  { return store.recoverErr }
func (*errorStore) Capabilities() lease.Capabilities                     { return lease.Capabilities{} }

func TestHandlerMapsScheduleAndTimeErrors(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule("report", "task", scheduler.Daily())
	registry, _ := scheduler.Compile(schedule)
	handler, _ := schedulerhttp.New(registry, &errorStore{})
	tests := []struct {
		path string
		want int
	}{
		{"/v1/schedules/report/next", http.StatusBadRequest},
		{"/v1/schedules/missing/next?after=2026-01-01T00:00:00Z", http.StatusNotFound},
		{"/v1/schedules/report/due?after=bad&through=2026-01-01T00:00:00Z", http.StatusBadRequest},
		{"/v1/schedules/report/due?after=2026-01-01T00:00:00Z&through=bad", http.StatusBadRequest},
		{"/v1/schedules/missing/due?after=2026-01-01T00:00:00Z&through=2026-01-02T00:00:00Z", http.StatusNotFound},
		{"/v1/schedules/report/test", http.StatusBadRequest},
		{"/v1/schedules/report/test?at=bad", http.StatusBadRequest},
		{"/v1/schedules/missing/test?at=2026-01-01T00:00:00Z", http.StatusNotFound},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.want {
			t.Fatalf("GET %s = %d, want %d", test.path, response.Code, test.want)
		}
	}

	invalid := schedule
	invalid.MissedRunPolicy = scheduler.MissedRunPolicy(255)
	invalidRegistry, _ := scheduler.Compile(invalid)
	invalidHandler, _ := schedulerhttp.New(invalidRegistry, &errorStore{})
	response := httptest.NewRecorder()
	invalidHandler.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/v1/schedules/report/due?after=2026-01-01T00:00:00Z&through=2026-01-02T00:00:00Z",
		nil,
	))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid policy status = %d", response.Code)
	}
}

func TestHandlerMapsRecoveryErrorsAndMalformedBodies(t *testing.T) {
	t.Parallel()

	registry, _ := scheduler.Compile()
	tests := []struct {
		err  error
		want int
	}{
		{lease.ErrNotFound, http.StatusNotFound},
		{lease.ErrStaleOwner, http.StatusConflict},
		{errors.New("backend"), http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		handler, _ := schedulerhttp.New(registry, &errorStore{recoverErr: test.err})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(
			http.MethodPost, "/v1/recover", strings.NewReader(`{"key":"key","fencing_token":1}`),
		))
		if response.Code != test.want {
			t.Fatalf("recover %v status = %d, want %d", test.err, response.Code, test.want)
		}
	}
	for _, body := range []string{`{"key":"key","fencing_token":1,"extra":true}`, strings.Repeat("x", 5<<10)} {
		handler, _ := schedulerhttp.New(registry, &errorStore{})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/recover", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("malformed recovery status = %d", response.Code)
		}
	}
}

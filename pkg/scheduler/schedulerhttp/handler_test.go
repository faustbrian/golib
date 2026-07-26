package schedulerhttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
	"github.com/faustbrian/golib/pkg/scheduler/schedulerhttp"
)

func TestHandlerListsSchedulesAndCalculatesRuns(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule("report", "reports.generate", scheduler.Daily())
	registry, _ := scheduler.Compile(schedule)
	handler, err := schedulerhttp.New(registry, memory.New())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/schedules", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", response.Code, response.Body.String())
	}
	var schedules []schedulerhttp.Schedule
	if err := json.Unmarshal(response.Body.Bytes(), &schedules); err != nil {
		t.Fatalf("decode schedules error = %v", err)
	}
	if len(schedules) != 1 || schedules[0].Name != "report" || schedules[0].Expression != "0 0 * * *" {
		t.Fatalf("schedules = %+v", schedules)
	}
	validation := httptest.NewRecorder()
	handler.ServeHTTP(validation, httptest.NewRequest(http.MethodGet, "/v1/validate", nil))
	if validation.Code != http.StatusOK || !contains(validation.Body.String(), `"valid":true`) {
		t.Fatalf("validate response = %d %s", validation.Code, validation.Body.String())
	}

	next := httptest.NewRequest(http.MethodGet, "/v1/schedules/report/next?after=2026-01-01T00:00:00Z", nil)
	nextResponse := httptest.NewRecorder()
	handler.ServeHTTP(nextResponse, next)
	if nextResponse.Code != http.StatusOK || !contains(nextResponse.Body.String(), "2026-01-02T00:00:00Z") {
		t.Fatalf("next response = %d %s", nextResponse.Code, nextResponse.Body.String())
	}

	due := httptest.NewRequest(http.MethodGet, "/v1/schedules/report/due?after=2026-01-01T00:00:00Z&through=2026-01-02T00:00:00Z", nil)
	dueResponse := httptest.NewRecorder()
	handler.ServeHTTP(dueResponse, due)
	if dueResponse.Code != http.StatusOK || !contains(dueResponse.Body.String(), "2026-01-02T00:00:00Z") {
		t.Fatalf("due response = %d %s", dueResponse.Code, dueResponse.Body.String())
	}

	testResponse := httptest.NewRecorder()
	handler.ServeHTTP(testResponse, httptest.NewRequest(
		http.MethodGet,
		"/v1/schedules/report/test?at=2026-01-02T00:00:00Z",
		nil,
	))
	if testResponse.Code != http.StatusOK || !contains(testResponse.Body.String(), `"due":true`) {
		t.Fatalf("test response = %d %s", testResponse.Code, testResponse.Body.String())
	}
}

func TestHandlerRecoversLeaseWithFence(t *testing.T) {
	t.Parallel()

	store := memory.New()
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	owned, _ := store.Acquire(context.Background(), "task:report", "replica-a", time.Minute, now)
	registry, _ := scheduler.Compile()
	handler, _ := schedulerhttp.New(registry, store)
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/recover",
		strings.NewReader(`{"key":"task:report","fencing_token":1}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("recover response = %d %s", response.Code, response.Body.String())
	}
	if _, err := store.Inspect(context.Background(), owned.Key); err == nil {
		t.Fatal("lease still exists after recovery")
	}
}

func TestHandlerRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	if _, err := schedulerhttp.New(nil, nil); err == nil {
		t.Fatal("New(nil) error = nil")
	}
	registry, _ := scheduler.Compile()
	handler, _ := schedulerhttp.New(registry, memory.New())
	tests := []struct {
		method string
		path   string
		body   string
		want   int
	}{
		{http.MethodPost, "/v1/schedules", "", http.StatusMethodNotAllowed},
		{http.MethodGet, "/v1/schedules/missing/next?after=bad", "", http.StatusBadRequest},
		{http.MethodPost, "/v1/recover", "{}", http.StatusBadRequest},
		{http.MethodGet, "/shell", "", http.StatusNotFound},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("%s %s status = %d, want %d", test.method, test.path, response.Code, test.want)
		}
	}
}

func contains(value, substring string) bool {
	return strings.Contains(value, substring)
}

package managementhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
)

func FuzzHandlerCommandBody(f *testing.F) {
	f.Add([]byte(commandJSON(validCommand())))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"id":`))
	handler, err := NewHandler(HandlerConfig{
		Token: "transport-secret", Controller: fuzzController{},
	})
	if err != nil {
		f.Fatalf("NewHandler() error = %v", err)
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > int(maxCommandRequestBytes)+1 {
			return
		}
		request := httptest.NewRequest(
			http.MethodPost, "/v1/commands", strings.NewReader(string(body)),
		)
		request.Header.Set("Authorization", "Bearer transport-secret")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		switch response.Code {
		case http.StatusOK, http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		default:
			t.Fatalf("unexpected status %d for %q", response.Code, body)
		}
		if response.Body.Len() > 4<<10 || strings.Contains(response.Body.String(), "transport-secret") {
			t.Fatalf("unsafe response length=%d body=%q", response.Body.Len(), response.Body.String())
		}
	})
}

type fuzzController struct{}

func (fuzzController) Execute(
	_ context.Context,
	command management.Command,
) (management.CommandResult, error) {
	return management.CommandResult{
		CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
		WorkerID: "worker-1", Protocol: command.Protocol,
		Status: management.CommandAcknowledged, CompletedAt: time.Now().UTC(),
	}, nil
}

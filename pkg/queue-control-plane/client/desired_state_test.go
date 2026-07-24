package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestClientReadsValidatedDesiredStateAndBuildsTenantReader(t *testing.T) {
	t.Parallel()

	want := queue.DesiredRecord{
		Target: queue.Target{Kind: queue.TargetQueue, Name: "critical"},
		State:  queue.DesiredPaused, Revision: 3,
		ChangedAt: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC),
		CommandID: "pause-critical-3",
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet ||
			request.URL.Path != "/v1/tenants/tenant-1/desired-state/queue/critical" ||
			request.Header.Get(APIKeyIDHeader) != "worker-1" ||
			request.Header.Get(APIKeySecretHeader) != "secret-123" {
			t.Errorf("request = %s %s headers %v", request.Method, request.URL.Path, request.Header)
		}
		_ = json.NewEncoder(writer).Encode(want)
	}))
	defer server.Close()
	api, err := New(Config{
		BaseURL: server.URL,
		APIKeys: &apiKeySourceStub{id: "worker-1", secret: "secret-123"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	record, err := api.GetDesiredState(context.Background(), "tenant-1", want.Target)
	if err != nil || record != want {
		t.Fatalf("GetDesiredState() = (%+v, %v)", record, err)
	}
	reader, err := api.DesiredStateReader("tenant-1")
	if err != nil {
		t.Fatalf("DesiredStateReader() error = %v", err)
	}
	record, err = reader.GetDesiredState(context.Background(), want.Target)
	if err != nil || record != want {
		t.Fatalf("reader.GetDesiredState() = (%+v, %v)", record, err)
	}
}

func TestClientDesiredStateFailsClosed(t *testing.T) {
	t.Parallel()

	validTarget := queue.Target{Kind: queue.TargetQueue, Name: "critical"}
	authErr := errors.New("credential unavailable")
	invalidRecord := queue.DesiredRecord{
		Target: validTarget, State: queue.DesiredPaused, Revision: 1,
		ChangedAt: time.Unix(1, 0).UTC(), CommandID: "pause-1",
	}
	tests := map[string]struct {
		tenant  string
		target  queue.Target
		record  queue.DesiredRecord
		authErr error
		wantErr error
		calls   int
	}{
		"invalid tenant":  {target: validTarget, wantErr: ErrInvalidRequest},
		"invalid kind":    {tenant: "tenant-1", target: queue.Target{Kind: queue.TargetFailure, Name: "failure-1"}, wantErr: ErrInvalidRequest},
		"invalid name":    {tenant: "tenant-1", target: queue.Target{Kind: queue.TargetQueue}, wantErr: ErrInvalidRequest},
		"request failure": {tenant: "tenant-1", target: validTarget, authErr: authErr, wantErr: authErr},
		"invalid output":  {tenant: "tenant-1", target: validTarget, wantErr: ErrInvalidResponse, calls: 1},
		"mismatched output": {
			tenant: "tenant-1", target: validTarget, calls: 1,
			record: func() queue.DesiredRecord {
				record := invalidRecord
				record.Target.Name = "other"
				return record
			}(),
			wantErr: ErrInvalidResponse,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				calls++
				_ = json.NewEncoder(writer).Encode(tt.record)
			}))
			defer server.Close()
			api, err := New(Config{
				BaseURL: server.URL,
				Tokens:  &tokenSourceStub{token: "token", err: tt.authErr},
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, err = api.GetDesiredState(context.Background(), tt.tenant, tt.target)
			if !errors.Is(err, tt.wantErr) || calls != tt.calls {
				t.Fatalf("GetDesiredState() error = %v, calls = %d", err, calls)
			}
		})
	}
	if _, err := (&Client{}).DesiredStateReader(""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("DesiredStateReader(invalid) error = %v", err)
	}
}

package apihttp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
)

func BenchmarkWorkerAPIThousandWorkerMaximumPage(b *testing.B) {
	observedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	queues := make([]string, fleet.MaxQueuesPerWorker)
	for index := range queues {
		queues[index] = fmt.Sprintf("queue-%02d", index)
	}
	workers := make([]fleet.WorkerSnapshot, MaxWorkerPageSize)
	for index := range workers {
		workers[index] = fleet.WorkerSnapshot{
			Heartbeat: workerHeartbeat(
				"tenant-1", fmt.Sprintf("worker-%04d", index), observedAt, queues,
			),
			State: fleet.StateRunning,
		}
	}
	handler, err := NewHandler(Config{
		Commands:   &commandExecutorStub{},
		Workers:    &workerSourceStub{snapshot: fleet.RegistrySnapshot{Workers: workers}},
		Viewer:     &viewerStub{},
		Now:        func() time.Time { return observedAt },
		StaleAfter: time.Minute,
		Protocol: fleet.ProtocolRange{
			Minimum: fleet.ProtocolVersion{Major: 1},
			Maximum: fleet.ProtocolVersion{Major: 1},
		},
		WorkerCapabilities: []fleet.Capability{fleet.CapabilityDrain},
	})
	if err != nil {
		b.Fatalf("NewHandler() error = %v", err)
	}
	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{
		Subject: "operator-1", Method: "bearer",
	})
	if err != nil {
		b.Fatalf("NewPrincipal() error = %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		request := httptest.NewRequest(
			http.MethodGet, "/v1/tenants/tenant-1/workers?limit=1000", nil,
		)
		request = request.WithContext(
			authentication.ContextWithPrincipal(request.Context(), principal),
		)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			b.Fatalf("status = %d, want 200", response.Code)
		}
	}
}

package fleet

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHeartbeatValidateRejectsMalformedWorkerStatus(t *testing.T) {
	t.Parallel()

	valid := validHeartbeat("tenant-1", "worker-1", time.Unix(2, 0))
	tests := map[string]struct {
		mutate func(*Heartbeat)
		field  string
	}{
		"tenant":                    {mutate: func(value *Heartbeat) { value.TenantID = "" }, field: "tenant_id"},
		"worker":                    {mutate: func(value *Heartbeat) { value.WorkerID = "" }, field: "worker_id"},
		"version":                   {mutate: func(value *Heartbeat) { value.Version = "" }, field: "version"},
		"started":                   {mutate: func(value *Heartbeat) { value.StartedAt = time.Time{} }, field: "started_at"},
		"started after observation": {mutate: func(value *Heartbeat) { value.StartedAt = value.ObservedAt.Add(time.Second) }, field: "started_at"},
		"observed":                  {mutate: func(value *Heartbeat) { value.ObservedAt = time.Time{} }, field: "observed_at"},
		"queues":                    {mutate: func(value *Heartbeat) { value.Queues = make([]string, MaxQueuesPerWorker+1) }, field: "queues"},
		"queue":                     {mutate: func(value *Heartbeat) { value.Queues = []string{""} }, field: "queues[0]"},
		"concurrency":               {mutate: func(value *Heartbeat) { value.Concurrency = 0 }, field: "concurrency"},
		"concurrency limit":         {mutate: func(value *Heartbeat) { value.Concurrency = MaxWorkerConcurrency + 1 }, field: "concurrency"},
		"current jobs":              {mutate: func(value *Heartbeat) { value.CurrentJobs = value.Concurrency + 1 }, field: "current_jobs"},
		"state":                     {mutate: func(value *Heartbeat) { value.State = StateUnknown }, field: "state"},
		"drain":                     {mutate: func(value *Heartbeat) { value.DrainStatus = DrainState("lost") }, field: "drain_status"},
		"backend":                   {mutate: func(value *Heartbeat) { value.Backend = strings.Repeat("x", MaxIdentityBytes+1) }, field: "backend"},
		"protocol":                  {mutate: func(value *Heartbeat) { value.Protocol = ProtocolVersion{} }, field: "protocol"},
		"capabilities":              {mutate: func(value *Heartbeat) { value.Capabilities = make([]Capability, MaxCapabilitiesPerWorker+1) }, field: "capabilities"},
		"capability":                {mutate: func(value *Heartbeat) { value.Capabilities = []Capability{""} }, field: "capabilities[0]"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			heartbeat := valid
			tt.mutate(&heartbeat)
			err := heartbeat.Validate()
			var validationError *HeartbeatValidationError
			if !errors.As(err, &validationError) || validationError.Field != tt.field {
				t.Fatalf("Validate() error = %v, want field %s", err, tt.field)
			}
		})
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got := (&HeartbeatValidationError{Field: "state", Problem: "is unsupported"}).Error(); got != "state: is unsupported" {
		t.Fatalf("HeartbeatValidationError.Error() = %q", got)
	}
}

func validHeartbeat(tenant string, worker string, observedAt time.Time) Heartbeat {
	return Heartbeat{
		TenantID: tenant, WorkerID: worker, Version: "1.0.0",
		StartedAt: observedAt.Add(-time.Hour), ObservedAt: observedAt,
		Queues: []string{"critical"}, Concurrency: 4, State: StateRunning,
		CurrentJobs: 1, DrainStatus: DrainNotRequested, Backend: "redis",
		Protocol: ProtocolVersion{Major: 1}, Capabilities: []Capability{CapabilityDrain},
	}
}

func TestHeartbeatEffectiveState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	staleAfter := 30 * time.Second

	tests := map[string]struct {
		heartbeat Heartbeat
		want      State
	}{
		"fresh heartbeat preserves reported state": {
			heartbeat: Heartbeat{State: StateRunning, ObservedAt: now.Add(-time.Second)},
			want:      StateRunning,
		},
		"expired heartbeat is stale": {
			heartbeat: Heartbeat{State: StateRunning, ObservedAt: now.Add(-staleAfter)},
			want:      StateStale,
		},
		"missing timestamp is unknown": {
			heartbeat: Heartbeat{State: StateRunning},
			want:      StateUnknown,
		},
		"future heartbeat is unknown": {
			heartbeat: Heartbeat{State: StateRunning, ObservedAt: now.Add(time.Second)},
			want:      StateUnknown,
		},
		"unrecognized reported state is unknown": {
			heartbeat: Heartbeat{State: State("wedged"), ObservedAt: now.Add(-time.Second)},
			want:      StateUnknown,
		},
		"derived stale state cannot be reported as healthy": {
			heartbeat: Heartbeat{State: StateStale, ObservedAt: now.Add(-time.Second)},
			want:      StateUnknown,
		},
		"derived unknown state remains unknown": {
			heartbeat: Heartbeat{State: StateUnknown, ObservedAt: now.Add(-time.Second)},
			want:      StateUnknown,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := tt.heartbeat.EffectiveState(now, staleAfter); got != tt.want {
				t.Fatalf("EffectiveState() = %q, want %q", got, tt.want)
			}
		})
	}
}

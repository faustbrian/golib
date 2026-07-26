package schedulercli_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/lease"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
	"github.com/faustbrian/golib/pkg/scheduler/schedulercli"
)

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write") }

type failingStore struct{ err error }

func (*failingStore) Acquire(context.Context, string, string, time.Duration, time.Time) (lease.Lease, error) {
	return lease.Lease{}, nil
}
func (*failingStore) Heartbeat(context.Context, lease.Lease, time.Duration, time.Time) (lease.Lease, error) {
	return lease.Lease{}, nil
}
func (*failingStore) Release(context.Context, lease.Lease) error           { return nil }
func (*failingStore) Inspect(context.Context, string) (lease.Lease, error) { return lease.Lease{}, nil }
func (store *failingStore) Recover(context.Context, string, uint64) error  { return store.err }
func (*failingStore) Capabilities() lease.Capabilities                     { return lease.Capabilities{} }

func TestCLIUsageAndRuntimeFailures(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule("report", "task", scheduler.Daily())
	registry, _ := scheduler.Compile(schedule)
	store := memory.New()
	tests := []struct {
		args []string
		want int
	}{
		{nil, 2},
		{[]string{"next", "--unknown"}, 2},
		{[]string{"next", "--name", "report", "--after", "bad"}, 2},
		{[]string{"next", "--name", "missing", "--after", "2026-01-01T00:00:00Z"}, 1},
		{[]string{"due", "--unknown"}, 2},
		{[]string{"due", "--name", "report", "--after", "bad", "--through", "2026-01-01T00:00:00Z"}, 2},
		{[]string{"due", "--name", "report", "--after", "2026-01-01T00:00:00Z", "--through", "bad"}, 2},
		{[]string{"due", "--name", "missing", "--after", "2026-01-01T00:00:00Z", "--through", "2026-01-02T00:00:00Z"}, 1},
		{[]string{"test", "--unknown"}, 2},
		{[]string{"test", "--name", "report", "--at", "bad"}, 2},
		{[]string{"test", "--name", "missing", "--at", "2026-01-01T00:00:00Z"}, 1},
		{[]string{"recover", "--unknown"}, 2},
		{[]string{"recover", "--key", "key", "--token", "bad"}, 2},
		{[]string{"recover", "--key", "key", "--token", "0"}, 2},
	}
	for _, test := range tests {
		var stdout, stderr bytes.Buffer
		if code := schedulercli.Run(context.Background(), test.args, &stdout, &stderr, registry, store); code != test.want {
			t.Fatalf("Run(%v) = %d, want %d; stderr=%q", test.args, code, test.want, stderr.String())
		}
	}
}

func TestCLIReportsRecoveryAndWriterFailures(t *testing.T) {
	t.Parallel()

	registry, _ := scheduler.Compile()
	backend := errors.New("backend")
	var stderr bytes.Buffer
	code := schedulercli.Run(
		context.Background(), []string{"recover", "--key", "key", "--token", "1"},
		&bytes.Buffer{}, &stderr, registry, &failingStore{err: backend},
	)
	if code != 1 {
		t.Fatalf("Run(recovery failure) = %d", code)
	}
	if code := schedulercli.Run(context.Background(), []string{"list"}, errorWriter{}, &bytes.Buffer{}, registry, memory.New()); code != 1 {
		t.Fatalf("Run(writer failure) = %d", code)
	}
}

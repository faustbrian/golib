package schedulercli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
	"github.com/faustbrian/golib/pkg/scheduler/schedulercli"
)

func TestCLIInspectionCommands(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule("report", "reports.generate", scheduler.Daily())
	registry, _ := scheduler.Compile(schedule)
	store := memory.New()
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"list"}, `"name":"report"`},
		{[]string{"validate"}, `"valid":true`},
		{[]string{"next", "--name", "report", "--after", "2026-01-01T00:00:00Z"}, "2026-01-02T00:00:00Z"},
		{[]string{"due", "--name", "report", "--after", "2026-01-01T00:00:00Z", "--through", "2026-01-02T00:00:00Z"}, "2026-01-02T00:00:00Z"},
		{[]string{"test", "--name", "report", "--at", "2026-01-02T00:00:00Z"}, `"due":true`},
	}
	for _, test := range tests {
		var stdout, stderr bytes.Buffer
		code := schedulercli.Run(context.Background(), test.args, &stdout, &stderr, registry, store)
		if code != 0 || !strings.Contains(stdout.String(), test.want) {
			t.Fatalf("Run(%v) = %d, stdout %q, stderr %q", test.args, code, stdout.String(), stderr.String())
		}
	}
}

func TestCLIRecoversLeaseAndReportsInvalidInput(t *testing.T) {
	t.Parallel()

	store := memory.New()
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	owned, _ := store.Acquire(context.Background(), "task:report", "owner", time.Minute, now)
	registry, _ := scheduler.Compile()
	var stdout, stderr bytes.Buffer
	code := schedulercli.Run(
		context.Background(),
		[]string{"unlock", "--key", owned.Key, "--token", "1"},
		&stdout,
		&stderr,
		registry,
		store,
	)
	if code != 0 {
		t.Fatalf("unlock code = %d, stderr = %q", code, stderr.String())
	}
	if _, err := store.Inspect(context.Background(), owned.Key); err == nil {
		t.Fatal("lease still exists")
	}

	stdout.Reset()
	stderr.Reset()
	code = schedulercli.Run(context.Background(), []string{"unknown"}, &stdout, &stderr, registry, store)
	if code != 2 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("unknown code = %d, stderr = %q", code, stderr.String())
	}
}

func TestCLIRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	if code := schedulercli.Run(context.Background(), nil, &bytes.Buffer{}, &bytes.Buffer{}, nil, nil); code != 2 {
		t.Fatalf("Run(nil) = %d, want 2", code)
	}
}

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

	schedule, _ := scheduler.NewSchedule(
		"report", "reports.generate", scheduler.Daily(),
		scheduler.WithFridays(), scheduler.WithBetween("0:00", "17:00"),
	)
	registry, _ := scheduler.Compile(schedule)
	store := memory.New()
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"list"}, `"days_of_week":[5]`},
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

	registry, _ := scheduler.Compile()
	store := memory.New()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	for name, invoke := range map[string]func() int{
		"registry": func() int {
			return schedulercli.Run(context.Background(), nil, stdout, stderr, nil, store)
		},
		"leases": func() int {
			return schedulercli.Run(context.Background(), nil, stdout, stderr, registry, nil)
		},
		"stdout": func() int {
			return schedulercli.Run(context.Background(), nil, nil, stderr, registry, store)
		},
		"stderr": func() int {
			return schedulercli.Run(context.Background(), nil, stdout, nil, registry, store)
		},
	} {
		if code := invoke(); code != 2 {
			t.Fatalf("Run(nil %s) = %d, want 2", name, code)
		}
	}
}

func TestCLITestReportsFalseOutsideABoundary(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule("report", "task", scheduler.Daily())
	registry, _ := scheduler.Compile(schedule)
	var stdout, stderr bytes.Buffer
	code := schedulercli.Run(
		context.Background(),
		[]string{"test", "--name", "report", "--at", "2026-01-02T00:01:00Z"},
		&stdout,
		&stderr,
		registry,
		memory.New(),
	)
	if code != 0 || !strings.Contains(stdout.String(), `"due":false`) {
		t.Fatalf("Run(test not due) = %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
}

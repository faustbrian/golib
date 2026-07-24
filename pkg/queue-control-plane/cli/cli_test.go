package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/client"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	controlkubernetes "github.com/faustbrian/golib/pkg/queue-control-plane/kubernetes"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestRunnerExecutesEveryMutation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 16, 15, 0, 0, 0, time.UTC)
	tests := map[string]struct {
		args  []string
		check func(*testing.T, apihttp.CommandRequest)
	}{
		"pause":      {args: []string{"pause", "--target-kind", "queue", "--target", "critical"}},
		"resume":     {args: []string{"resume", "--target-kind", "worker_group", "--target", "payments"}},
		"drain":      {args: []string{"drain", "--target-kind", "worker_group", "--target", "payments"}},
		"terminate":  {args: []string{"terminate", "--target-kind", "worker", "--target", "worker-1"}},
		"retry":      {args: []string{"retry", "--target-kind", "failure", "--target", "failure-1"}},
		"delete":     {args: []string{"delete", "--target-kind", "dead_letter", "--target", "dead-1"}},
		"purge":      {args: []string{"purge", "--target-kind", "queue", "--target", "critical", "--confirm"}},
		"bulk_retry": {args: []string{"bulk-retry", "--target-kind", "failure", "--target", "failed", "--confirm", "--limit", "25"}, check: checkSelection},
		"replay":     {args: []string{"replay", "--target-kind", "dead_letter", "--target", "dead-1", "--confirm", "--destination", "recovery", "--replay-policy", "reject_duplicate"}, check: checkReplay},
		"scale":      {args: []string{"scale", "--target-kind", "workload", "--target", "payments", "--replicas", "5"}, check: checkScale},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			api := &apiStub{result: controlplane.CommandResult{IdempotencyKey: "request-1", TenantID: "tenant-1", Status: controlplane.CommandAccepted}}
			var output bytes.Buffer
			runner := Runner{Client: api, Stdout: &output, Stderr: &bytes.Buffer{}, Now: func() time.Time { return now }}
			args := append(tt.args, "--tenant", "tenant-1", "--idempotency-key", "request-1", "--reason", "incident response")
			if exit := runner.Run(context.Background(), args); exit != ExitOK {
				t.Fatalf("Run() = %d, want %d", exit, ExitOK)
			}
			if api.command.Action != controlplane.Action(name) || api.command.RequestedAt != now {
				t.Fatalf("command = %+v, want %s at %s", api.command, name, now)
			}
			if tt.check != nil {
				tt.check(t, api.command)
			}
			if output.String() == "" {
				t.Fatal("stdout is empty, want JSON result")
			}
		})
	}
}

func TestRunnerListsWorkers(t *testing.T) {
	t.Parallel()

	api := &apiStub{workers: apihttp.WorkerPage{Workers: []apihttp.Worker{{TenantID: "tenant-1", WorkerID: "worker-1"}}}}
	var output bytes.Buffer
	runner := Runner{Client: api, Stdout: &output, Stderr: &bytes.Buffer{}}
	exit := runner.Run(context.Background(), []string{
		"workers", "list", "--tenant", "tenant-1", "--limit", "25", "--after", "worker-0", "--state", "stale", "--queue", "critical",
	})
	if exit != ExitOK {
		t.Fatalf("Run() = %d, want %d", exit, ExitOK)
	}
	if api.workerQuery != (client.WorkerQuery{Limit: 25, After: "worker-0", State: fleet.StateStale, Queue: "critical"}) {
		t.Fatalf("worker query = %+v", api.workerQuery)
	}
	if output.String() == "" {
		t.Fatal("stdout is empty, want JSON page")
	}
}

func TestRunnerSupportsSafeHumanOutput(t *testing.T) {
	t.Parallel()

	api := &apiStub{workers: apihttp.WorkerPage{
		Workers: []apihttp.Worker{{TenantID: "tenant-1", WorkerID: "worker-\x1b[31m"}},
	}}
	var output bytes.Buffer
	exit := (Runner{Client: api, Stdout: &output, Stderr: &bytes.Buffer{}}).Run(
		context.Background(),
		[]string{"--output", "human", "workers", "list", "--tenant", "tenant-1"},
	)
	if exit != ExitOK || !strings.Contains(output.String(), "\n  \"workers\"") ||
		strings.Contains(output.String(), "\x1b") || !strings.Contains(output.String(), `\u001b`) {
		t.Fatalf("human output = exit %d, %q", exit, output.String())
	}
}

func TestRunnerRejectsInvalidOutputMode(t *testing.T) {
	t.Parallel()

	for name, args := range map[string][]string{
		"missing value": {"--output"},
		"unknown value": {"--output", "yaml", "workers", "list"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			api := &apiStub{}
			var stderr bytes.Buffer
			exit := (Runner{Client: api, Stdout: &bytes.Buffer{}, Stderr: &stderr}).Run(
				context.Background(), args,
			)
			if exit != ExitUsage || api.workerQuery != (client.WorkerQuery{}) || stderr.String() == "" {
				t.Fatalf("Run() = %d, query %+v, stderr %q", exit, api.workerQuery, stderr.String())
			}
		})
	}
}

func TestRunnerReportsTruthfulRetentionStatus(t *testing.T) {
	t.Parallel()

	api := &apiStub{workers: apihttp.WorkerPage{
		Workers: []apihttp.Worker{{
			TenantID: "tenant-1", WorkerID: "worker-1", Backend: "redis-streams",
			Queues:       []string{"critical"},
			Capabilities: []fleet.Capability{fleet.CapabilityRetentionCount},
			Compatibility: fleet.Compatibility{
				State:   fleet.CompatibilityCompatible,
				Enabled: []fleet.Capability{fleet.CapabilityRetentionCount},
			},
		}},
		NextCursor: "worker-1",
	}}
	var output bytes.Buffer
	exit := (Runner{Client: api, Stdout: &output, Stderr: &bytes.Buffer{}}).Run(
		context.Background(),
		[]string{
			"retention", "status", "--tenant", "tenant-1", "--limit", "25",
			"--after", "worker-0", "--queue", "critical",
		},
	)
	if exit != ExitOK || api.workerQuery != (client.WorkerQuery{
		Limit: 25, After: "worker-0", Queue: "critical",
	}) {
		t.Fatalf("retention status = exit %d, query %+v", exit, api.workerQuery)
	}
	for _, fragment := range []string{
		`"worker_id":"worker-1"`,
		`"mode":"count","configured":true,"negotiated":true,"limit_known":false`,
		`"mode":"time","configured":false,"negotiated":false,"limit_known":false`,
		`"mode":"bytes","configured":false,"negotiated":false,"limit_known":false`,
		`"next_cursor":"worker-1"`,
	} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("retention output %q does not contain %q", output.String(), fragment)
		}
	}
}

func TestRunnerListsQueues(t *testing.T) {
	t.Parallel()

	api := &apiStub{queues: apihttp.QueuePage{
		Queues: []apihttp.Queue{{Name: "critical"}}, NextCursor: "next",
	}}
	var output bytes.Buffer
	exit := (Runner{Client: api, Stdout: &output, Stderr: &bytes.Buffer{}}).Run(
		context.Background(),
		[]string{"queues", "list", "--tenant", "tenant-1", "--limit", "25", "--cursor", "current"},
	)
	if exit != ExitOK || api.queueQuery != (client.QueueQuery{Limit: 25, Cursor: "current"}) ||
		!strings.Contains(output.String(), `"next_cursor":"next"`) {
		t.Fatalf("queues list = exit %d, query %+v, output %q", exit, api.queueQuery, output.String())
	}
}

func TestRunnerFailsClosedWhenQueueCapabilityIsUnavailable(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	exit := (Runner{
		Client: legacyAPIStub{}, Stdout: &bytes.Buffer{}, Stderr: &stderr,
	}).Run(context.Background(), []string{"queues", "list", "--tenant", "tenant-1"})
	if exit != ExitFailure || !strings.Contains(stderr.String(), "queue status is unavailable") {
		t.Fatalf("Run() = %d, stderr %q", exit, stderr.String())
	}
}

func TestRunnerListsWorkloads(t *testing.T) {
	t.Parallel()

	api := &apiStub{workloads: controlkubernetes.Page{
		Items: []controlkubernetes.Status{{Name: "billing-workers"}},
	}}
	var output bytes.Buffer
	runner := Runner{Client: api, Stdout: &output, Stderr: &bytes.Buffer{}}
	exit := runner.Run(context.Background(), []string{
		"workloads", "list", "--tenant", "tenant-1", "--limit", "25", "--continue", "current/page",
	})
	if exit != ExitOK {
		t.Fatalf("Run() = %d, want %d", exit, ExitOK)
	}
	if api.workloadQuery != (client.WorkloadQuery{Limit: 25, Continue: "current/page"}) {
		t.Fatalf("workload query = %#v", api.workloadQuery)
	}
	if output.String() == "" {
		t.Fatal("stdout is empty, want JSON page")
	}
}

func TestRunnerListsAuditHistory(t *testing.T) {
	t.Parallel()

	api := &apiStub{audit: apihttp.AuditPage{NextSequence: 5}}
	var output bytes.Buffer
	runner := Runner{Client: api, Stdout: &output, Stderr: &bytes.Buffer{}}
	exit := runner.Run(context.Background(), []string{
		"audit", "list", "--tenant", "tenant-1", "--after", "4", "--limit", "25",
	})
	if exit != ExitOK {
		t.Fatalf("Run() = %d, want %d", exit, ExitOK)
	}
	if api.auditQuery != (client.AuditQuery{After: 4, Limit: 25}) {
		t.Fatalf("audit query = %+v", api.auditQuery)
	}
	if output.String() == "" {
		t.Fatal("stdout is empty, want JSON page")
	}
}

func TestRunnerGetsCommandResult(t *testing.T) {
	t.Parallel()

	api := &apiStub{result: controlplane.CommandResult{Status: controlplane.CommandAccepted}}
	var output bytes.Buffer
	exit := (Runner{Client: api, Stdout: &output, Stderr: &bytes.Buffer{}}).Run(
		context.Background(),
		[]string{"command", "get", "--tenant", "tenant-1", "--idempotency-key", "request-1"},
	)
	if exit != ExitOK {
		t.Fatalf("Run() = %d, want %d", exit, ExitOK)
	}
	if api.commandTenant != "tenant-1" || api.commandKey != "request-1" || output.String() == "" {
		t.Fatalf("GetCommand() = (%q, %q), output %q", api.commandTenant, api.commandKey, output.String())
	}
}

func TestRunnerListsCommandHistory(t *testing.T) {
	t.Parallel()

	api := &apiStub{commands: apihttp.CommandHistoryPage{NextCursor: "next-page"}}
	var output bytes.Buffer
	exit := (Runner{Client: api, Stdout: &output, Stderr: &bytes.Buffer{}}).Run(
		context.Background(),
		[]string{
			"command", "list", "--tenant", "tenant-1", "--cursor", "current-page", "--limit", "25",
		},
	)
	if exit != ExitOK {
		t.Fatalf("Run() = %d, want %d", exit, ExitOK)
	}
	if api.commandTenant != "tenant-1" ||
		api.commandQuery != (client.CommandQuery{Cursor: "current-page", Limit: 25}) ||
		!strings.Contains(output.String(), `"next_cursor":"next-page"`) {
		t.Fatalf("ListCommands() = (%q, %+v), output %q", api.commandTenant, api.commandQuery, output.String())
	}
}

func TestRunnerListsAndInspectsFailureRecords(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"failures", "dead-letters"} {
		api := &apiStub{records: apihttp.RecordPage{NextCursor: "next"}}
		var output bytes.Buffer
		exit := (Runner{Client: api, Stdout: &output, Stderr: &bytes.Buffer{}}).Run(
			context.Background(),
			[]string{
				command, "list", "--tenant", "tenant-1", "--cursor", "current",
				"--limit", "25", "--search", "critical", "--sort", "queue",
				"--direction", "asc",
			},
		)
		if exit != ExitOK || api.recordQuery != (client.RecordQuery{
			Cursor: "current", Limit: 25, Search: "critical",
			Sort: queue.SortQueue, Direction: queue.SortAscending,
		}) || !strings.Contains(output.String(), `"next_cursor":"next"`) {
			t.Fatalf("%s list = exit %d, query %+v, output %q", command, exit, api.recordQuery, output.String())
		}

		output.Reset()
		exit = (Runner{Client: api, Stdout: &output, Stderr: &bytes.Buffer{}}).Run(
			context.Background(),
			[]string{
				command, "get", "--tenant", "tenant-1", "--id", "failure-1",
				"--payload", "revealed", "--diagnostics",
			},
		)
		if exit != ExitOK || api.recordID != "failure-1" ||
			api.recordVisibility != queue.PayloadRevealed || !api.recordDiagnostics {
			t.Fatalf("%s get = exit %d, id %q, visibility %q", command, exit, api.recordID, api.recordVisibility)
		}
		if command == "failures" {
			exit = (Runner{Client: api, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}).Run(
				context.Background(),
				[]string{command, "get", "--tenant", "tenant-1", "--id", "failure-1", "--payload", "redacted"},
			)
			if exit != ExitOK || api.recordVisibility != queue.PayloadRedacted {
				t.Fatalf("redacted get = exit %d, visibility %q", exit, api.recordVisibility)
			}
		}
	}
}

func TestRunnerFailsClosedWhenRecordCapabilityIsUnavailable(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	exit := (Runner{
		Client: legacyAPIStub{}, Stdout: &bytes.Buffer{}, Stderr: &stderr,
	}).Run(context.Background(), []string{"failures", "list", "--tenant", "tenant-1"})
	if exit != ExitFailure || !strings.Contains(stderr.String(), "record access is unavailable") {
		t.Fatalf("Run() = %d, stderr %q", exit, stderr.String())
	}
}

func TestRunnerFailsClosedWhenCommandHistoryCapabilityIsUnavailable(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	exit := (Runner{
		Client: legacyAPIStub{}, Stdout: &bytes.Buffer{}, Stderr: &stderr,
	}).Run(context.Background(), []string{"command", "list", "--tenant", "tenant-1"})
	if exit != ExitFailure || !strings.Contains(stderr.String(), "command history is unavailable") {
		t.Fatalf("Run() = %d, stderr %q", exit, stderr.String())
	}
}

func TestRunnerRejectsInvalidUsageWithoutCallingAPI(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"missing command":          {},
		"unknown command":          {"unknown"},
		"missing worker action":    {"workers"},
		"unknown worker action":    {"workers", "show"},
		"worker flag":              {"workers", "list", "--unknown"},
		"worker argument":          {"workers", "list", "extra"},
		"worker tenant":            {"workers", "list"},
		"worker limit":             {"workers", "list", "--tenant", "tenant-1", "--limit", "1001"},
		"missing retention action": {"retention"},
		"unknown retention action": {"retention", "configure"},
		"retention tenant":         {"retention", "status"},
		"retention limit":          {"retention", "status", "--tenant", "tenant-1", "--limit", "1001"},
		"missing queue action":     {"queues"},
		"unknown queue action":     {"queues", "show"},
		"queue flag":               {"queues", "list", "--unknown"},
		"queue argument":           {"queues", "list", "extra"},
		"queue tenant":             {"queues", "list"},
		"queue limit":              {"queues", "list", "--tenant", "tenant-1", "--limit", "201"},
		"queue cursor": {
			"queues", "list", "--tenant", "tenant-1", "--cursor",
			strings.Repeat("x", queue.MaxCursorBytes+1),
		},
		"missing workload action": {"workloads"},
		"unknown workload action": {"workloads", "show"},
		"workload flag":           {"workloads", "list", "--unknown"},
		"workload argument":       {"workloads", "list", "extra"},
		"workload tenant":         {"workloads", "list"},
		"negative workload limit": {"workloads", "list", "--tenant", "tenant-1", "--limit", "-1"},
		"workload limit":          {"workloads", "list", "--tenant", "tenant-1", "--limit", "501"},
		"workload continuation": {
			"workloads", "list", "--tenant", "tenant-1", "--continue",
			strings.Repeat("x", controlkubernetes.MaxContinueTokenBytes+1),
		},
		"missing audit action":   {"audit"},
		"unknown audit action":   {"audit", "show"},
		"audit flag":             {"audit", "list", "--unknown"},
		"audit argument":         {"audit", "list", "extra"},
		"audit tenant":           {"audit", "list"},
		"audit limit":            {"audit", "list", "--tenant", "tenant-1", "--limit", "1001"},
		"missing command action": {"command"},
		"unknown command action": {"command", "show"},
		"command list flag":      {"command", "list", "--unknown"},
		"command list argument":  {"command", "list", "--tenant", "tenant-1", "extra"},
		"command list tenant":    {"command", "list"},
		"command list limit":     {"command", "list", "--tenant", "tenant-1", "--limit", "1001"},
		"command list cursor": {
			"command", "list", "--tenant", "tenant-1", "--cursor",
			strings.Repeat("x", apihttp.MaxCommandCursorBytes+1),
		},
		"command flag":           {"command", "get", "--unknown"},
		"command argument":       {"command", "get", "extra"},
		"command tenant":         {"command", "get", "--idempotency-key", "request-1"},
		"command key":            {"command", "get", "--tenant", "tenant-1"},
		"missing failure action": {"failures"},
		"unknown failure action": {"failures", "show"},
		"record list flag":       {"failures", "list", "--unknown"},
		"record list argument":   {"failures", "list", "extra"},
		"record list tenant":     {"failures", "list"},
		"record list limit":      {"failures", "list", "--tenant", "tenant-1", "--limit", "201"},
		"record list cursor": {
			"failures", "list", "--tenant", "tenant-1", "--cursor",
			strings.Repeat("x", queue.MaxCursorBytes+1),
		},
		"record list search": {
			"failures", "list", "--tenant", "tenant-1", "--search",
			strings.Repeat("x", queue.MaxSearchBytes+1),
		},
		"record list sort":      {"failures", "list", "--tenant", "tenant-1", "--sort", "payload"},
		"record list direction": {"failures", "list", "--tenant", "tenant-1", "--direction", "sideways"},
		"record get flag":       {"dead-letters", "get", "--unknown"},
		"record get argument":   {"dead-letters", "get", "extra"},
		"record get tenant":     {"dead-letters", "get", "--id", "dead-1"},
		"record get id":         {"dead-letters", "get", "--tenant", "tenant-1"},
		"record get payload":    {"dead-letters", "get", "--tenant", "tenant-1", "--id", "dead-1", "--payload", "raw"},
		"mutation flag":         {"pause", "--unknown"},
		"mutation argument":     {"pause", "extra"},
		"mutation envelope":     {"pause", "--tenant", "tenant-1"},
		"bulk retry overflow": {
			"bulk-retry", "--tenant", "tenant-1", "--idempotency-key", "request-1",
			"--reason", "maintenance", "--target-kind", "failure", "--target", "failed",
			"--confirm", "--limit", "1001",
		},
		"scale overflow": {
			"scale", "--tenant", "tenant-1", "--idempotency-key", "request-1",
			"--reason", "maintenance", "--target-kind", "workload", "--target", "workers",
			"--replicas", "10001",
		},
		"requested time": {"pause", "--tenant", "tenant-1", "--idempotency-key", "request-1", "--reason", "maintenance",
			"--target-kind", "queue", "--target", "critical", "--requested-at", "yesterday"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			api := &apiStub{}
			var stderr bytes.Buffer
			exit := (Runner{Client: api, Stdout: &bytes.Buffer{}, Stderr: &stderr}).Run(context.Background(), args)
			if exit != ExitUsage {
				t.Fatalf("Run() = %d, want %d", exit, ExitUsage)
			}
			if api.command.Action != "" || api.workerQuery != (client.WorkerQuery{}) ||
				api.workloadQuery != (client.WorkloadQuery{}) {
				t.Fatal("invalid usage reached the API")
			}
			if stderr.String() == "" {
				t.Fatal("stderr is empty")
			}
		})
	}
}

func TestRunnerReportsAPIAndOutputFailures(t *testing.T) {
	t.Parallel()

	apiErr := errors.New("API unavailable")
	validMutation := []string{"pause", "--tenant", "tenant-1", "--idempotency-key", "request-1", "--reason", "maintenance", "--target-kind", "queue", "--target", "critical"}
	tests := map[string]struct {
		api    *apiStub
		args   []string
		stdout *errorWriter
	}{
		"mutation API":  {api: &apiStub{err: apiErr}, args: validMutation},
		"worker API":    {api: &apiStub{err: apiErr}, args: []string{"workers", "list", "--tenant", "tenant-1"}},
		"retention API": {api: &apiStub{err: apiErr}, args: []string{"retention", "status", "--tenant", "tenant-1"}},
		"queue API":     {api: &apiStub{err: apiErr}, args: []string{"queues", "list", "--tenant", "tenant-1"}},
		"workload API":  {api: &apiStub{err: apiErr}, args: []string{"workloads", "list", "--tenant", "tenant-1"}},
		"audit API":     {api: &apiStub{err: apiErr}, args: []string{"audit", "list", "--tenant", "tenant-1"}},
		"command API":   {api: &apiStub{err: apiErr}, args: []string{"command", "get", "--tenant", "tenant-1", "--idempotency-key", "request-1"}},
		"command list API": {
			api: &apiStub{err: apiErr}, args: []string{"command", "list", "--tenant", "tenant-1"},
		},
		"record list API": {
			api: &apiStub{err: apiErr}, args: []string{"failures", "list", "--tenant", "tenant-1"},
		},
		"record get API": {
			api: &apiStub{err: apiErr}, args: []string{"dead-letters", "get", "--tenant", "tenant-1", "--id", "dead-1"},
		},
		"mutation output": {
			api: &apiStub{result: controlplane.CommandResult{Status: controlplane.CommandAccepted}}, args: validMutation,
			stdout: &errorWriter{err: errors.New("output closed")},
		},
		"worker output": {
			api: &apiStub{}, args: []string{"workers", "list", "--tenant", "tenant-1"},
			stdout: &errorWriter{err: errors.New("output closed")},
		},
		"retention output": {
			api: &apiStub{}, args: []string{"retention", "status", "--tenant", "tenant-1"},
			stdout: &errorWriter{err: errors.New("output closed")},
		},
		"queue output": {
			api: &apiStub{}, args: []string{"queues", "list", "--tenant", "tenant-1"},
			stdout: &errorWriter{err: errors.New("output closed")},
		},
		"workload output": {
			api: &apiStub{}, args: []string{"workloads", "list", "--tenant", "tenant-1"},
			stdout: &errorWriter{err: errors.New("output closed")},
		},
		"audit output": {
			api: &apiStub{}, args: []string{"audit", "list", "--tenant", "tenant-1"},
			stdout: &errorWriter{err: errors.New("output closed")},
		},
		"command output": {
			api: &apiStub{}, args: []string{"command", "get", "--tenant", "tenant-1", "--idempotency-key", "request-1"},
			stdout: &errorWriter{err: errors.New("output closed")},
		},
		"command list output": {
			api: &apiStub{}, args: []string{"command", "list", "--tenant", "tenant-1"},
			stdout: &errorWriter{err: errors.New("output closed")},
		},
		"record list output": {
			api: &apiStub{}, args: []string{"failures", "list", "--tenant", "tenant-1"},
			stdout: &errorWriter{err: errors.New("output closed")},
		},
		"record get output": {
			api: &apiStub{}, args: []string{"dead-letters", "get", "--tenant", "tenant-1", "--id", "dead-1"},
			stdout: &errorWriter{err: errors.New("output closed")},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			var stdout interface{ Write([]byte) (int, error) } = &output
			if tt.stdout != nil {
				stdout = tt.stdout
			}
			var stderr bytes.Buffer
			exit := (Runner{Client: tt.api, Stdout: stdout, Stderr: &stderr}).Run(context.Background(), tt.args)
			if exit != ExitFailure {
				t.Fatalf("Run() = %d, want %d", exit, ExitFailure)
			}
			if stderr.String() == "" {
				t.Fatal("stderr is empty")
			}
		})
	}
}

func TestRunnerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	var typedNil *apiStub
	valid := &apiStub{}
	for _, runner := range []Runner{
		{},
		{Client: typedNil, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}},
		{Client: valid, Stderr: &bytes.Buffer{}},
		{Client: valid, Stdout: &bytes.Buffer{}},
	} {
		if exit := runner.Run(context.Background(), nil); exit != ExitFailure {
			t.Fatalf("Run() = %d, want %d", exit, ExitFailure)
		}
	}
}

func TestRunnerAcceptsExplicitRequestedTime(t *testing.T) {
	t.Parallel()

	api := &apiStub{}
	runner := Runner{Client: api, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	exit := runner.Run(context.Background(), []string{
		"pause", "--tenant", "tenant-1", "--idempotency-key", "request-1", "--reason", "maintenance",
		"--target-kind", "queue", "--target", "critical", "--requested-at", "2026-07-16T15:00:00.123456Z",
	})
	if exit != ExitOK {
		t.Fatalf("Run() = %d, want %d", exit, ExitOK)
	}
	want := time.Date(2026, time.July, 16, 15, 0, 0, 123456000, time.UTC)
	if !api.command.RequestedAt.Equal(want) {
		t.Fatalf("RequestedAt = %s, want %s", api.command.RequestedAt, want)
	}
}

func checkSelection(t *testing.T, command apihttp.CommandRequest) {
	t.Helper()
	if command.Selection == nil || command.Selection.Limit != 25 {
		t.Fatalf("selection = %+v, want 25", command.Selection)
	}
}

func checkReplay(t *testing.T, command apihttp.CommandRequest) {
	t.Helper()
	if command.Replay == nil || command.Replay.Destination != "recovery" || command.Replay.IdempotencyPolicy != controlplane.ReplayRejectDuplicate {
		t.Fatalf("replay = %+v, want recovery reject_duplicate", command.Replay)
	}
}

func checkScale(t *testing.T, command apihttp.CommandRequest) {
	t.Helper()
	if command.Scale == nil || command.Scale.Replicas != 5 {
		t.Fatalf("scale = %+v, want 5", command.Scale)
	}
}

type apiStub struct {
	result            controlplane.CommandResult
	commands          apihttp.CommandHistoryPage
	workers           apihttp.WorkerPage
	queues            apihttp.QueuePage
	workloads         controlkubernetes.Page
	audit             apihttp.AuditPage
	err               error
	command           apihttp.CommandRequest
	workerQuery       client.WorkerQuery
	queueQuery        client.QueueQuery
	workloadQuery     client.WorkloadQuery
	auditQuery        client.AuditQuery
	commandTenant     string
	commandKey        string
	commandQuery      client.CommandQuery
	records           apihttp.RecordPage
	record            apihttp.Record
	recordQuery       client.RecordQuery
	recordID          string
	recordVisibility  queue.PayloadVisibility
	recordDiagnostics bool
}

type errorWriter struct {
	err error
}

type legacyAPIStub struct{}

func (legacyAPIStub) ExecuteCommand(
	context.Context,
	string,
	apihttp.CommandRequest,
) (controlplane.CommandResult, error) {
	return controlplane.CommandResult{}, nil
}

func (legacyAPIStub) ListWorkers(
	context.Context,
	string,
	client.WorkerQuery,
) (apihttp.WorkerPage, error) {
	return apihttp.WorkerPage{}, nil
}

func (legacyAPIStub) ListWorkloads(
	context.Context,
	string,
	client.WorkloadQuery,
) (controlkubernetes.Page, error) {
	return controlkubernetes.Page{}, nil
}

func (legacyAPIStub) ListAudit(
	context.Context,
	string,
	client.AuditQuery,
) (apihttp.AuditPage, error) {
	return apihttp.AuditPage{}, nil
}

func (legacyAPIStub) GetCommand(
	context.Context,
	string,
	string,
) (controlplane.CommandResult, error) {
	return controlplane.CommandResult{}, nil
}

func (w *errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (s *apiStub) ExecuteCommand(_ context.Context, _ string, command apihttp.CommandRequest) (controlplane.CommandResult, error) {
	s.command = command
	return s.result, s.err
}

func (s *apiStub) ListWorkers(_ context.Context, _ string, query client.WorkerQuery) (apihttp.WorkerPage, error) {
	s.workerQuery = query
	return s.workers, s.err
}

func (s *apiStub) ListQueues(_ context.Context, _ string, query client.QueueQuery) (apihttp.QueuePage, error) {
	s.queueQuery = query
	return s.queues, s.err
}

func (s *apiStub) ListWorkloads(_ context.Context, _ string, query client.WorkloadQuery) (controlkubernetes.Page, error) {
	s.workloadQuery = query
	return s.workloads, s.err
}

func (s *apiStub) ListAudit(_ context.Context, _ string, query client.AuditQuery) (apihttp.AuditPage, error) {
	s.auditQuery = query
	return s.audit, s.err
}

func (s *apiStub) GetCommand(_ context.Context, tenant string, key string) (controlplane.CommandResult, error) {
	s.commandTenant = tenant
	s.commandKey = key
	return s.result, s.err
}

func (s *apiStub) ListCommands(
	_ context.Context,
	tenant string,
	query client.CommandQuery,
) (apihttp.CommandHistoryPage, error) {
	s.commandTenant = tenant
	s.commandQuery = query

	return s.commands, s.err
}

func (s *apiStub) ListFailures(
	_ context.Context,
	_ string,
	query client.RecordQuery,
) (apihttp.RecordPage, error) {
	s.recordQuery = query

	return s.records, s.err
}

func (s *apiStub) ListDeadLetters(
	_ context.Context,
	_ string,
	query client.RecordQuery,
) (apihttp.RecordPage, error) {
	s.recordQuery = query

	return s.records, s.err
}

func (s *apiStub) InspectFailureWithOptions(
	_ context.Context,
	_ string,
	id string,
	options client.RecordInspectOptions,
) (apihttp.Record, error) {
	s.recordID = id
	s.recordVisibility = options.Payload
	s.recordDiagnostics = options.RevealDiagnostics

	return s.record, s.err
}

func (s *apiStub) InspectDeadLetterWithOptions(
	_ context.Context,
	_ string,
	id string,
	options client.RecordInspectOptions,
) (apihttp.Record, error) {
	s.recordID = id
	s.recordVisibility = options.Payload
	s.recordDiagnostics = options.RevealDiagnostics

	return s.record, s.err
}

// Package cli implements the administrative command-line workflow.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/client"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	controlkubernetes "github.com/faustbrian/golib/pkg/queue-control-plane/kubernetes"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

const (
	// ExitOK reports a successful administrative operation.
	ExitOK = 0
	// ExitFailure reports an API or output failure.
	ExitFailure = 1
	// ExitUsage reports invalid command-line input.
	ExitUsage = 2
)

var errUsage = errors.New("invalid command usage")

type outputFormat string

const (
	outputJSON  outputFormat = "json"
	outputHuman outputFormat = "human"
)

// API is the bounded administrative surface consumed by the CLI.
type API interface {
	ExecuteCommand(context.Context, string, apihttp.CommandRequest) (controlplane.CommandResult, error)
	ListWorkers(context.Context, string, client.WorkerQuery) (apihttp.WorkerPage, error)
	ListWorkloads(context.Context, string, client.WorkloadQuery) (controlkubernetes.Page, error)
	ListAudit(context.Context, string, client.AuditQuery) (apihttp.AuditPage, error)
	GetCommand(context.Context, string, string) (controlplane.CommandResult, error)
}

type commandHistoryAPI interface {
	ListCommands(context.Context, string, client.CommandQuery) (apihttp.CommandHistoryPage, error)
}

type recordAPI interface {
	ListFailures(context.Context, string, client.RecordQuery) (apihttp.RecordPage, error)
	ListDeadLetters(context.Context, string, client.RecordQuery) (apihttp.RecordPage, error)
	InspectFailureWithOptions(context.Context, string, string, client.RecordInspectOptions) (apihttp.Record, error)
	InspectDeadLetterWithOptions(context.Context, string, string, client.RecordInspectOptions) (apihttp.Record, error)
}

type queueAPI interface {
	ListQueues(context.Context, string, client.QueueQuery) (apihttp.QueuePage, error)
}

// Runner executes CLI arguments against an administrative API client.
type Runner struct {
	Client API
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
	output outputFormat
}

// Run executes one diagnostic or mutating command and returns a process code.
func (r Runner) Run(ctx context.Context, args []string) int {
	if nilInterface(r.Client) || r.Stdout == nil || r.Stderr == nil {
		return ExitFailure
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	var valid bool
	r.output, args, valid = parseOutputFormat(args)
	if !valid {
		r.writeError(errUsage)
		return ExitUsage
	}
	if len(args) == 0 {
		r.writeError(errUsage)
		return ExitUsage
	}

	var err error
	switch args[0] {
	case "workers":
		err = r.runWorkers(ctx, args[1:])
	case "queues":
		err = r.runQueues(ctx, args[1:])
	case "workloads":
		err = r.runWorkloads(ctx, args[1:])
	case "audit":
		err = r.runAudit(ctx, args[1:])
	case "command":
		err = r.runCommand(ctx, args[1:])
	case "retention":
		err = r.runRetention(ctx, args[1:])
	case "failures", "dead-letters":
		err = r.runRecords(ctx, args[0], args[1:])
	default:
		err = r.runMutation(ctx, args[0], args[1:])
	}
	if err == nil {
		return ExitOK
	}
	r.writeError(err)
	if errors.Is(err, errUsage) {
		return ExitUsage
	}

	return ExitFailure
}

type retentionPage struct {
	Workers    []retentionStatus `json:"workers"`
	Rejected   uint64            `json:"rejected"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

type retentionStatus struct {
	WorkerID      string                   `json:"worker_id"`
	Backend       string                   `json:"backend"`
	Queues        []string                 `json:"queues"`
	Compatibility fleet.CompatibilityState `json:"compatibility"`
	Modes         []retentionModeStatus    `json:"modes"`
}

type retentionModeStatus struct {
	Mode       string `json:"mode"`
	Configured bool   `json:"configured"`
	Negotiated bool   `json:"negotiated"`
	LimitKnown bool   `json:"limit_known"`
}

func (r Runner) runRetention(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "status" {
		return errUsage
	}
	flags := flag.NewFlagSet("retention status", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	tenant := flags.String("tenant", "", "tenant identifier")
	limit := flags.Uint("limit", 0, "worker page size")
	after := flags.String("after", "", "worker cursor")
	queueName := flags.String("queue", "", "queue filter")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 ||
		strings.TrimSpace(*tenant) == "" || len(*tenant) > controlplane.MaxIdentityBytes ||
		*limit > uint(apihttp.MaxWorkerPageSize) ||
		len(*after) > controlplane.MaxIdentityBytes ||
		len(*queueName) > controlplane.MaxIdentityBytes {
		return errUsage
	}
	page, err := r.Client.ListWorkers(ctx, *tenant, client.WorkerQuery{
		Limit: uint32(*limit), After: *after, Queue: *queueName,
	})
	if err != nil {
		return err
	}

	status := retentionPage{
		Workers:  make([]retentionStatus, 0, len(page.Workers)),
		Rejected: page.Rejected, NextCursor: page.NextCursor,
	}
	for _, worker := range page.Workers {
		status.Workers = append(status.Workers, workerRetentionStatus(worker))
	}

	return r.writeOutput(status)
}

func workerRetentionStatus(worker apihttp.Worker) retentionStatus {
	modes := []struct {
		name       string
		capability fleet.Capability
	}{
		{name: "count", capability: fleet.CapabilityRetentionCount},
		{name: "time", capability: fleet.CapabilityRetentionTime},
		{name: "bytes", capability: fleet.CapabilityRetentionBytes},
	}
	status := retentionStatus{
		WorkerID: worker.WorkerID, Backend: worker.Backend,
		Queues:        append([]string(nil), worker.Queues...),
		Compatibility: worker.Compatibility.State,
		Modes:         make([]retentionModeStatus, 0, len(modes)),
	}
	for _, mode := range modes {
		status.Modes = append(status.Modes, retentionModeStatus{
			Mode:       mode.name,
			Configured: containsCapability(worker.Capabilities, mode.capability),
			Negotiated: containsCapability(worker.Compatibility.Enabled, mode.capability),
			LimitKnown: false,
		})
	}

	return status
}

func containsCapability(capabilities []fleet.Capability, want fleet.Capability) bool {
	for _, capability := range capabilities {
		if capability == want {
			return true
		}
	}

	return false
}

func (r Runner) runQueues(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return errUsage
	}
	queues, ok := r.Client.(queueAPI)
	if !ok {
		return errors.New("queue status is unavailable")
	}
	flags := flag.NewFlagSet("queues list", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	tenant := flags.String("tenant", "", "tenant identifier")
	cursor := flags.String("cursor", "", "opaque queue cursor")
	limit := flags.Uint("limit", 0, "page size")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 ||
		strings.TrimSpace(*tenant) == "" || *limit > uint(queue.MaxStatusPageSize) ||
		len(*cursor) > queue.MaxCursorBytes {
		return errUsage
	}
	page, err := queues.ListQueues(ctx, *tenant, client.QueueQuery{
		Cursor: *cursor,
		Limit:  uint32(*limit),
	})
	if err != nil {
		return err
	}

	return r.writeOutput(page)
}

func (r Runner) runRecords(
	ctx context.Context,
	collection string,
	args []string,
) error {
	if len(args) == 0 || (args[0] != "list" && args[0] != "get") {
		return errUsage
	}
	records, ok := r.Client.(recordAPI)
	if !ok {
		return errors.New("record access is unavailable")
	}
	if args[0] == "list" {
		return r.runRecordList(ctx, records, collection, args[1:])
	}

	return r.runRecordGet(ctx, records, collection, args[1:])
}

func (r Runner) runRecordList(
	ctx context.Context,
	records recordAPI,
	collection string,
	args []string,
) error {
	flags := flag.NewFlagSet(collection+" list", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	tenant := flags.String("tenant", "", "tenant identifier")
	cursor := flags.String("cursor", "", "opaque record cursor")
	limit := flags.Uint("limit", 0, "page size")
	search := flags.String("search", "", "bounded record search")
	sortField := flags.String("sort", "", "occurred_at, queue, or attempts")
	direction := flags.String("direction", "", "asc or desc")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 ||
		strings.TrimSpace(*tenant) == "" || *limit > uint(queue.MaxPageSize) ||
		len(*cursor) > queue.MaxCursorBytes || len(*search) > queue.MaxSearchBytes ||
		!validRecordSort(queue.SortField(*sortField)) ||
		!validRecordDirection(queue.SortDirection(*direction)) {
		return errUsage
	}
	query := client.RecordQuery{
		Cursor: *cursor, Limit: uint32(*limit), Search: *search,
		Sort: queue.SortField(*sortField), Direction: queue.SortDirection(*direction),
	}
	var page apihttp.RecordPage
	var err error
	if collection == "failures" {
		page, err = records.ListFailures(ctx, *tenant, query)
	} else {
		page, err = records.ListDeadLetters(ctx, *tenant, query)
	}
	if err != nil {
		return err
	}

	return r.writeOutput(page)
}

func (r Runner) runRecordGet(
	ctx context.Context,
	records recordAPI,
	collection string,
	args []string,
) error {
	flags := flag.NewFlagSet(collection+" get", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	tenant := flags.String("tenant", "", "tenant identifier")
	id := flags.String("id", "", "failure or dead-letter identifier")
	payload := flags.String("payload", "hidden", "hidden, redacted, or revealed")
	diagnostics := flags.Bool("diagnostics", false, "reveal privileged diagnostics")
	if err := flags.Parse(args); err != nil {
		return errUsage
	}
	visibility, valid := cliPayloadVisibility(*payload)
	if flags.NArg() != 0 || strings.TrimSpace(*tenant) == "" ||
		strings.TrimSpace(*id) == "" || len(*id) > controlplane.MaxIdentityBytes || !valid {
		return errUsage
	}
	var record apihttp.Record
	var err error
	options := client.RecordInspectOptions{
		Payload: visibility, RevealDiagnostics: *diagnostics,
	}
	if collection == "failures" {
		record, err = records.InspectFailureWithOptions(ctx, *tenant, *id, options)
	} else {
		record, err = records.InspectDeadLetterWithOptions(ctx, *tenant, *id, options)
	}
	if err != nil {
		return err
	}

	return r.writeOutput(record)
}

func validRecordSort(field queue.SortField) bool {
	return field == "" || field == queue.SortOccurredAt || field == queue.SortQueue ||
		field == queue.SortAttempts
}

func validRecordDirection(direction queue.SortDirection) bool {
	return direction == "" || direction == queue.SortAscending ||
		direction == queue.SortDescending
}

func cliPayloadVisibility(value string) (queue.PayloadVisibility, bool) {
	switch value {
	case "hidden":
		return queue.PayloadHidden, true
	case string(queue.PayloadRedacted):
		return queue.PayloadRedacted, true
	case string(queue.PayloadRevealed):
		return queue.PayloadRevealed, true
	default:
		return queue.PayloadHidden, false
	}
}

func (r Runner) runWorkloads(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return errUsage
	}
	flags := flag.NewFlagSet("workloads list", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	tenant := flags.String("tenant", "", "tenant identifier")
	limit := flags.Int64("limit", 0, "page size")
	continueToken := flags.String("continue", "", "Kubernetes continuation token")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 ||
		strings.TrimSpace(*tenant) == "" || *limit < 0 || *limit > controlkubernetes.MaxPageSize ||
		len(*continueToken) > controlkubernetes.MaxContinueTokenBytes {
		return errUsage
	}
	page, err := r.Client.ListWorkloads(ctx, *tenant, client.WorkloadQuery{
		Limit:    *limit,
		Continue: *continueToken,
	})
	if err != nil {
		return err
	}

	return r.writeOutput(page)
}

func (r Runner) runCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	if args[0] == "list" {
		flags := flag.NewFlagSet("command list", flag.ContinueOnError)
		flags.SetOutput(r.Stderr)
		tenant := flags.String("tenant", "", "tenant identifier")
		cursor := flags.String("cursor", "", "opaque command cursor")
		limit := flags.Uint("limit", 0, "page size")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 ||
			strings.TrimSpace(*tenant) == "" || *limit > uint(apihttp.MaxCommandPageSize) ||
			len(*cursor) > apihttp.MaxCommandCursorBytes {
			return errUsage
		}
		history, ok := r.Client.(commandHistoryAPI)
		if !ok {
			return errors.New("command history is unavailable")
		}
		page, err := history.ListCommands(ctx, *tenant, client.CommandQuery{
			Cursor: *cursor,
			Limit:  uint32(*limit),
		})
		if err != nil {
			return err
		}

		return r.writeOutput(page)
	}
	if args[0] != "get" {
		return errUsage
	}
	flags := flag.NewFlagSet("command get", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	tenant := flags.String("tenant", "", "tenant identifier")
	key := flags.String("idempotency-key", "", "idempotency key")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 ||
		strings.TrimSpace(*tenant) == "" || strings.TrimSpace(*key) == "" {
		return errUsage
	}
	result, err := r.Client.GetCommand(ctx, *tenant, *key)
	if err != nil {
		return err
	}

	return r.writeOutput(result)
}

func (r Runner) runAudit(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return errUsage
	}
	flags := flag.NewFlagSet("audit list", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	tenant := flags.String("tenant", "", "tenant identifier")
	after := flags.Uint64("after", 0, "sequence cursor")
	limit := flags.Uint("limit", 0, "page size")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 ||
		strings.TrimSpace(*tenant) == "" || *limit > uint(apihttp.MaxAuditPageSize) {
		return errUsage
	}
	page, err := r.Client.ListAudit(ctx, *tenant, client.AuditQuery{
		After: *after,
		Limit: uint32(*limit),
	})
	if err != nil {
		return err
	}

	return r.writeOutput(page)
}

func (r Runner) runWorkers(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return errUsage
	}
	flags := flag.NewFlagSet("workers list", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	tenant := flags.String("tenant", "", "tenant identifier")
	limit := flags.Uint("limit", 0, "page size")
	after := flags.String("after", "", "worker cursor")
	state := flags.String("state", "", "worker state")
	queue := flags.String("queue", "", "queue filter")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || strings.TrimSpace(*tenant) == "" || *limit > uint(apihttp.MaxWorkerPageSize) {
		return errUsage
	}
	page, err := r.Client.ListWorkers(ctx, *tenant, client.WorkerQuery{
		Limit: uint32(*limit),
		After: *after,
		State: fleet.State(*state),
		Queue: *queue,
	})
	if err != nil {
		return err
	}

	return r.writeOutput(page)
}

func (r Runner) runMutation(ctx context.Context, name string, args []string) error {
	action, ok := mutationAction(name)
	if !ok {
		return errUsage
	}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	tenant := flags.String("tenant", "", "tenant identifier")
	key := flags.String("idempotency-key", "", "idempotency key")
	reason := flags.String("reason", "", "audit reason")
	targetKind := flags.String("target-kind", "", "target kind")
	target := flags.String("target", "", "target name")
	requestedAt := flags.String("requested-at", "", "RFC3339 request time")
	confirmed := flags.Bool("confirm", false, "confirm destructive operation")
	limit := flags.Uint("limit", 0, "bulk selection limit")
	destination := flags.String("destination", "", "replay destination")
	replayPolicy := flags.String("replay-policy", "", "replay idempotency policy")
	replicas := flags.Uint("replicas", 0, "desired workload replicas")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errUsage
	}
	at := r.Now().UTC()
	if *requestedAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, *requestedAt)
		if err != nil {
			return errUsage
		}
		at = parsed
	}
	request := apihttp.CommandRequest{
		IdempotencyKey: *key,
		Reason:         *reason,
		Action:         action,
		Target: apihttp.TargetRequest{
			Kind: controlplane.TargetKind(*targetKind),
			Name: *target,
		},
		RequestedAt: at,
		Confirmed:   *confirmed,
	}
	if action == controlplane.ActionBulkRetry {
		if *limit > uint(controlplane.MaxBulkSelection) {
			return errUsage
		}
		request.Selection = &apihttp.SelectionRequest{Limit: uint32(*limit)}
	}
	if action == controlplane.ActionReplay {
		request.Replay = &apihttp.ReplayRequest{
			Destination:       *destination,
			IdempotencyPolicy: controlplane.ReplayPolicy(*replayPolicy),
		}
	}
	if action == controlplane.ActionScale {
		if *replicas > uint(controlplane.MaxScaleReplicas) {
			return errUsage
		}
		request.Scale = &apihttp.ScaleRequest{Replicas: uint32(*replicas)}
	}
	if err := validateRequest(*tenant, request); err != nil {
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	result, err := r.Client.ExecuteCommand(ctx, *tenant, request)
	if err != nil {
		return err
	}

	return r.writeOutput(result)
}

func validateRequest(tenant string, request apihttp.CommandRequest) error {
	command := controlplane.Command{
		IdempotencyKey: request.IdempotencyKey,
		TenantID:       tenant,
		Actor:          "authenticated-api-actor",
		Reason:         request.Reason,
		Action:         request.Action,
		Target: controlplane.Target{
			Kind: request.Target.Kind,
			Name: request.Target.Name,
		},
		RequestedAt: request.RequestedAt,
		Confirmed:   request.Confirmed,
	}
	if request.Selection != nil {
		command.Selection = &controlplane.Selection{Limit: request.Selection.Limit}
	}
	if request.Replay != nil {
		command.Replay = &controlplane.Replay{
			Destination:       request.Replay.Destination,
			IdempotencyPolicy: request.Replay.IdempotencyPolicy,
		}
	}
	if request.Scale != nil {
		command.Scale = &controlplane.Scale{Replicas: request.Scale.Replicas}
	}

	return command.Validate()
}

func mutationAction(name string) (controlplane.Action, bool) {
	action := controlplane.Action(strings.ReplaceAll(name, "-", "_"))
	switch action {
	case controlplane.ActionPause, controlplane.ActionResume,
		controlplane.ActionDrain, controlplane.ActionTerminate,
		controlplane.ActionRetry, controlplane.ActionBulkRetry,
		controlplane.ActionDelete, controlplane.ActionPurge,
		controlplane.ActionReplay, controlplane.ActionScale:
		return action, true
	default:
		return "", false
	}
}

func parseOutputFormat(args []string) (outputFormat, []string, bool) {
	if len(args) == 0 || args[0] != "--output" {
		return outputJSON, args, true
	}
	if len(args) < 2 {
		return "", nil, false
	}
	format := outputFormat(args[1])
	if format != outputJSON && format != outputHuman {
		return "", nil, false
	}

	return format, args[2:], true
}

func (r Runner) writeOutput(value any) error {
	encoder := json.NewEncoder(r.Stdout)
	encoder.SetEscapeHTML(true)
	if r.output == outputHuman {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(value)
}

func (r Runner) writeError(err error) {
	_, _ = fmt.Fprintln(r.Stderr, err)
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	return reflected.Kind() == reflect.Pointer && reflected.IsNil()
}

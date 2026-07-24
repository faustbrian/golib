// Package schedulercli provides bounded scheduler inspection and recovery commands.
package schedulercli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/lease"
)

// Run executes one bounded scheduler control command and returns an exit code.
func Run(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	registry *scheduler.Registry,
	leases lease.Store,
) int {
	if registry == nil || leases == nil || stdout == nil || stderr == nil {
		return 2
	}
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "missing command")
		return 2
	}
	switch args[0] {
	case "list":
		return list(stdout, registry)
	case "validate":
		return encode(stdout, map[string]any{"valid": true, "schedules": len(registry.Schedules())})
	case "next":
		return next(args[1:], stdout, stderr, registry)
	case "due":
		return due(args[1:], stdout, stderr, registry)
	case "test":
		return test(args[1:], stdout, stderr, registry)
	case "unlock", "recover":
		return recoverLease(ctx, args[1:], stdout, stderr, leases)
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}

func list(stdout io.Writer, registry *scheduler.Registry) int {
	schedules := registry.Schedules()
	views := make([]map[string]any, len(schedules))
	for index, schedule := range schedules {
		views[index] = map[string]any{
			"name": schedule.Name, "task": schedule.Task,
			"expression": schedule.Expression, "timezone": schedule.Timezone,
			"enabled": schedule.Enabled, "on_one_server": schedule.OnOneServer,
			"without_overlapping": schedule.WithoutOverlapping,
		}
	}
	return encode(stdout, views)
}

func next(args []string, stdout, stderr io.Writer, registry *scheduler.Registry) int {
	flags := flag.NewFlagSet("next", flag.ContinueOnError)
	flags.SetOutput(stderr)
	name := flags.String("name", "", "schedule name")
	afterValue := flags.String("after", "", "RFC3339 instant")
	if flags.Parse(args) != nil || *name == "" {
		return 2
	}
	after, err := time.Parse(time.RFC3339Nano, *afterValue)
	if err != nil {
		return report(stderr, err, 2)
	}
	instant, err := registry.Next(*name, after)
	if err != nil {
		return report(stderr, err, 1)
	}
	return encode(stdout, map[string]time.Time{"next": instant})
}

func due(args []string, stdout, stderr io.Writer, registry *scheduler.Registry) int {
	flags := flag.NewFlagSet("due", flag.ContinueOnError)
	flags.SetOutput(stderr)
	name := flags.String("name", "", "schedule name")
	afterValue := flags.String("after", "", "RFC3339 instant")
	throughValue := flags.String("through", "", "RFC3339 instant")
	if flags.Parse(args) != nil || *name == "" {
		return 2
	}
	after, err := time.Parse(time.RFC3339Nano, *afterValue)
	if err != nil {
		return report(stderr, err, 2)
	}
	through, err := time.Parse(time.RFC3339Nano, *throughValue)
	if err != nil {
		return report(stderr, err, 2)
	}
	occurrences, err := registry.Due(*name, after, through)
	if err != nil {
		return report(stderr, err, 1)
	}
	return encode(stdout, occurrences)
}

func test(args []string, stdout, stderr io.Writer, registry *scheduler.Registry) int {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.SetOutput(stderr)
	name := flags.String("name", "", "schedule name")
	atValue := flags.String("at", "", "RFC3339 instant")
	if flags.Parse(args) != nil || *name == "" {
		return 2
	}
	at, err := time.Parse(time.RFC3339Nano, *atValue)
	if err != nil {
		return report(stderr, err, 2)
	}
	occurrences, err := registry.Due(*name, at.Add(-time.Minute), at)
	if err != nil {
		return report(stderr, err, 1)
	}
	return encode(stdout, map[string]any{"due": len(occurrences) > 0, "occurrences": occurrences})
}

func recoverLease(ctx context.Context, args []string, stdout, stderr io.Writer, leases lease.Store) int {
	flags := flag.NewFlagSet("recover", flag.ContinueOnError)
	flags.SetOutput(stderr)
	key := flags.String("key", "", "lease key")
	tokenValue := flags.String("token", "", "fencing token")
	if flags.Parse(args) != nil || *key == "" {
		return 2
	}
	token, err := strconv.ParseUint(*tokenValue, 10, 64)
	if err != nil || token == 0 {
		return report(stderr, errors.New("token must be a positive integer"), 2)
	}
	if err := leases.Recover(ctx, *key, token); err != nil {
		return report(stderr, err, 1)
	}
	return encode(stdout, map[string]bool{"recovered": true})
}

func encode(writer io.Writer, value any) int {
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		return 1
	}
	return 0
}

func report(writer io.Writer, err error, code int) int {
	_, _ = fmt.Fprintln(writer, err)
	return code
}

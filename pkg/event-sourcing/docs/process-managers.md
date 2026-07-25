# Process managers

Process managers coordinate reactions across aggregate boundaries. The package
keeps planning separate from execution: it does not include a command bus,
workflow engine, scheduler, queue, or hidden process-state store.

## Explicit planning

`processmanager.Manager[Command]` accepts one typed `Planner` and returns an
ordered `PlanResult[Command]`. The application owns the command type and the
executor:

```go
manager, err := processmanager.New(processmanager.Config[SendEmail]{
	Name:        "welcome-email",
	Replay:      processmanager.RejectReplay,
	MaxCommands: 1,
	Planner: func(
		ctx context.Context,
		delivery eventsourcing.Delivery,
	) ([]SendEmail, error) {
		return []SendEmail{planWelcomeEmail(delivery.Message())}, nil
	},
})
```

Planning never executes a command. `PlanResult` retains the triggering message
identifier and delivery mode for application idempotency and diagnostics.
Command slice ownership is defensive, while command values themselves must be
application-owned immutable values.

## Replay and duplicates

`RejectReplay` is the safe zero-value policy. A replay delivery returns
`ErrReplayRejected` before invoking the planner. `AllowReplay` must be selected
explicitly and still only permits planning; it does not execute effects.

Live delivery, retries, and at-least-once transports can repeat the same
message. The package does not claim exactly-once planning or execution.
Applications should key durable process state and command deduplication by the
triggering message ID.

## Bounds and failures

Every manager requires a non-zero command limit no greater than
`MaxPlannedCommands`. Empty plans are valid. Plans above the configured bound
fail without returning a partial command slice.

Planner errors preserve their cause through `PlannerError` while redacting the
diagnostic string. Planner panics are contained as `ErrPlannerPanic`; panic
values are not retained or reported.

Durable process-state composition, duplicate suppression, retries, and
executor conformance remain application or adapter responsibilities. The
compatibility matrix therefore marks process managers as partial.

# Process managers

Process managers coordinate reactions across aggregate boundaries. The package
keeps planning separate from execution: it does not include a command bus,
workflow engine, scheduler, queue, or hidden process-state store.

## Explicit planning

The [runnable process-manager example](../process_manager_example_test.go)
constructs a live delivery and derives one application-owned command without
executing it.

`processmanager.Manager[Command]` accepts one typed `Planner` and returns an
ordered `PlanResult[Command]`. The application owns the command type and the
executor:

```go
accountOpenedName, err := eventsourcing.NewEventName("account.opened")
if err != nil {
	return err
}
manager, err := processmanager.New(processmanager.Config[SendEmail]{
	Name:        "welcome-email",
	Replay:      processmanager.RejectReplay,
	EventNames:  []eventsourcing.EventName{accountOpenedName},
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
`Accepted` reports whether the stable event name matched the constructor-owned
allowlist and the planner ran. An unmatched valid delivery succeeds with its
message identity and mode, `Accepted() == false`, and no commands.
`CommandCount` reports the bounded plan size without copying commands. Command
slice ownership from `Commands` is defensive, while command values themselves
must be application-owned immutable values.

## Event acceptance and process correlation

EventSauce process managers are ordinary message consumers that select event
payload types in consumer code. The Go manager makes that routing contract
constructor data: every manager must register one to
`processmanager.MaxAcceptedEventNames` explicit stable event names. Empty,
duplicate, zero, or oversized registrations fail construction. The allowlist is
copied and does not depend on Go type names, reflection, or planner behavior.

Event-name acceptance is not process-instance correlation. The application
must derive and validate its process key from explicit event data, aggregate
identity, correlation ID, or another stable domain identifier. It must persist
that key with process state and isolate tenants before planning or execution.
The library does not infer correlation from message order or route unrelated
instances into shared mutable state.

## Replay and duplicates

`RejectReplay` is the safe zero-value policy. A replay delivery returns
`ErrReplayRejected` before invoking the planner. `AllowReplay` must be selected
explicitly and still only permits planning; it does not execute effects.

Live delivery, retries, and at-least-once transports can repeat the same
message. The package does not claim exactly-once planning or execution.
Applications should key durable process state and command deduplication by the
triggering message ID.

The package tests deliver the same message twice and use `PlanResult`'s
message ID to prove one application-owned command execution. That in-memory
set demonstrates the integration contract, not crash-safe deduplication. A
production executor must persist its processed-message state and command
effects atomically where its storage permits that composition.

## Bounds and failures

Every manager requires a non-zero command limit no greater than
`MaxPlannedCommands`. Empty plans are valid. Plans above the configured bound
fail without returning a partial command slice.

Planner errors preserve their cause through `PlannerError` while redacting the
diagnostic string. Planner panics are contained as `ErrPlannerPanic`; panic
values are not retained or reported.

## Scenario testing

The `eventtest.CheckProcessManagerScenario` helper runs the public planning
contract with a persisted live or replay delivery. It verifies triggering
message identity, delivery mode, accepted or ignored event names, ordered
commands, expected error categories, and the absence of partial output on
failure. Applications supply command equality, so the helper does not use
reflection or format command state.

Durable process-state composition, retry policy, and executor conformance
remain application or adapter responsibilities. The compatibility matrix marks
the EventSauce process-manager planning outcome implemented; that status does
not claim a command bus, workflow engine, or exactly-once execution.

The optional `gotelemetry.WrapProcessManager` adapter observes planning latency,
outcome, delivery mode, and successful command count using a bounded static
manager name. It never records event or command data and never executes the
returned plan.

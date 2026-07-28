# Service lifecycle integration

Package `schedulerservice` connects an explicit scheduler runner to the
`service` lifecycle. It does not define schedules, select a lease backend,
choose retry policy, or own business commands.

## Construction and ownership

The caller supplies a compiled `scheduler.Registry`, a concrete `lease.Store`,
an `scheduler.Executor`, correlation factory, runner options, and any facility
components. `New` constructs and exposes the concrete `*scheduler.Runner`.

Facility ownership is unchanged by the adapter. A facility closes only when
its supplied `service.Component` transfers that responsibility. Shared pools,
clients, and stores should therefore use components that do not close them.
Invalid registry, lease, executor, correlation, codec, or runner configuration
fails during plan construction, before component startup.

```go
adapter, err := schedulerservice.New(schedulerservice.Options{
    Name:        "scheduler",
    Registry:    registry,
    Leases:      leases,
    Executor:    executor,
    Correlation: build.Correlation,
    RunnerOptions: []scheduler.RunnerOption{
        scheduler.WithOwner(build.Identity.Instance),
    },
    Facilities: []service.Component{leaseComponent, queueComponent},
})
if err != nil {
    return service.Plan{}, err
}

return adapter.Plan(), nil
```

## Startup, drain, and shutdown

Facilities start in declaration order before the scheduler task begins. During
shutdown, `service` cancels and joins the runner task first. The adapter's
final lifecycle component then calls `Runner.Drain`, which rejects new ticks
and joins active decisions, executions, and callbacks. Facility components
close afterward in reverse declaration order.

The service shutdown context bounds `Drain`. If its deadline expires, the
context error remains observable and the runner remains in draining state. A
later `Drain` call is safe and can finish joining retained executions.
Facilities are not closed by the adapter after a failed partial startup because
the scheduler task never started; normal service rollback remains responsible
for every facility that did start.

The adapter provides no readiness check. A schedule role becomes ready after
its declared facilities start. Applications may add readiness only for
dependencies required to accept new work. Retry and recovery remain in the
scheduler, queue, lease, or application layer that owns them.

## Correlation

Each scheduled occurrence uses the existing `correlation/schedule` adapter.
The default `CorrelationIndependent` mode creates a new correlation ID and
request ID for every occurrence.

`CorrelationTrustedMetadata` is an explicit application choice for schedules
that continue a workflow whose correlation fields were deliberately encoded
in schedule metadata. It preserves the trusted correlation ID, creates a new
request ID, and uses the prior request ID as causation. Do not select this mode
for metadata that crosses an unauthenticated boundary.

# fault-injection

`fault-injection` is a deterministic, concurrency-safe, bounded toolkit for
exercising failure paths in Go tests and explicitly wired controlled
experiments. Its zero value is disabled, configuration is copied and validated
before use, and it has no environment-variable activation path, background
worker, unbounded history, or remote control surface.

It is not a mocking framework, production chaos control plane, Kubernetes
operator, broker simulator, or substitute for a real network proxy.

## Quick start

```go
injector, err := faultinject.New(faultinject.Config{Rules: []faultinject.Rule{{
    ID:          "second-call",
    Scope:       faultinject.BoundaryFunction,
    Activation:  faultinject.Active,
    Maximum:     1,
    Terminal:    faultinject.Continue,
    Observation: faultinject.Suppress,
    Schedule:    faultinject.Nth(2),
    Faults: []faultinject.Fault{
        faultinject.ErrorFault(faultinject.PhaseBefore, ErrUnavailable),
    },
}}})
if err != nil {
    return err
}

value, err := faultinject.Run(ctx, injector,
    faultinject.Metadata{Boundary: faultinject.BoundaryFunction}, operation)
```

Construct the injector inside the test or experiment composition root and pass
it explicitly to adapters. A nil or zero `Injector` delegates directly and
cannot become active later.

## Deterministic model

- Rules are ordered by `Order`, then stable `ID`.
- Mutex acquisition defines concurrent evaluation order. Record event
  `Sequence` values when replaying a concurrent campaign.
- `Nth`, `Every`, finite/repeating `Sequence`, and seeded `Probability`
  schedules are deterministic for the same eligible-call order.
- Probability uses an explicit seed and a deterministic call sequence; an
  unseeded probability-only mode does not exist.
- `Maximum` bounds rule activations. `MaxRules`, `MaxFaultsPerDecision`,
  `MaxLatency`, and `MaxBytes` bound configuration, work, and retained data.
- `Reset` starts a new generation. Decisions already returned retain their old
  generation and remain valid.
- Synchronous observers run after selection and outside engine locks. They
  cannot veto or rewrite a fault.

See [API](docs/api.md) and [deterministic recipes](docs/operations.md).

## Faults and adapters

The core model supports injected errors, bounded latency, cancellation,
deadline expiry, bounded safe-string panics, byte drop/truncation/duplication/
reordering/corruption, short IO, temporary and permanent network failures,
reset, half-close, and stream interruption.

Adapters cover:

- generic `Run` execution;
- `http.RoundTripper` and caller-owned response bodies;
- `io.Reader` and `io.Writer`;
- `net.Conn`, context dialers, and listeners;
- `fs.FS` open/read boundaries; and
- context sleepers and timer factories.

Every adapter defines its partial-result, close, and ownership behavior in
[adapter contracts](docs/adapters.md). During-operation faults are observable
in-process boundary simulations, not packet-, kernel-, broker-, or scheduler-
level failures.

## Controlled runtime experiments

Tests should use `Injector` directly. A runtime integration must additionally
use `Runtime`, which requires an explicit injector, authorizer, exact boundary
allowlist, expiry, maximum evaluation budget, audit sink, and terminal emergency
disable. It fails closed on authorization or clock failure and never consults
the environment. See [security](docs/safety.md).

## Kubernetes and infrastructure scope

An in-process injector affects one process in one pod. Independent pod-local
seeds do not implement a fleet percentage. Replica selection, blast radius,
rollout coordination, and disruption are owned by an external orchestrator.
See [Kubernetes semantics](docs/kubernetes.md) and the
[Toxiproxy comparison](docs/comparison.md).

## Adoption and tradeoffs

Use this module when deterministic call- or adapter-boundary behavior is the
contract under test. Prefer a direct test double for a single isolated return
value. Use Toxiproxy or another infrastructure tool when TCP proxy or real
network behavior matters. Use a cluster experiment system when pod selection
or fleet blast radius matters.

Dependency-heavy database, cache, queue, Kafka, object-storage, and RPC
integrations belong in nested modules or downstream repositories so the root
production package remains standard-library-only. The root module uses only a
test-scoped leak detector. See [extension guidance](docs/extension.md).

## Verification

From the repository root:

```sh
make inventory
make check MODULES=pkg/fault-injection
make ci-changed BASE=<revision>
```

The module includes exact statement coverage, deterministic golden schedules,
race/stress coverage, fuzz targets, adapter contract tests, and benchmarks.
Repository gates add mutation, API compatibility, documentation, security,
supply-chain, and clean-consumer checks.

## References

- [Failsafe-Go policy composition and events](https://failsafe-go.dev/)
- [goresilience chaos behavior](https://pkg.go.dev/github.com/slok/goresilience)
- [Toxiproxy fault model](https://github.com/Shopify/toxiproxy)
- [Go `net/http` contracts](https://pkg.go.dev/net/http)
- [Go `io` contracts](https://pkg.go.dev/io)
- [Go context](https://pkg.go.dev/context)
- [Kubernetes pod lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/)

See also [FAQ](docs/faq.md), [security policy](SECURITY.md), and
[performance methodology](docs/performance.md), and [changelog](CHANGELOG.md).

## License

MIT. See [LICENSE](LICENSE).

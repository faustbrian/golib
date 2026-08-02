# Kubernetes semantics

Executors are immutable and reusable inside a pod. The built-in budget and all
focused stateful policies are pod-local unless a package explicitly documents
a distributed backend.

- Scaling from one to `R` replicas multiplies local concurrency and
  rolling-window limits by at most `R`.
- A new pod starts with cold policy and budget state.
- During a rolling update, old and new policy revisions may make different
  decisions; compatible error and event contracts must remain stable.
- HPA signals can form feedback loops when local rejection lowers latency or
  CPU. Observe admitted, rejected, in-flight, and downstream load together.
- On SIGTERM, stop new request admission, let logical scopes finish within the
  termination grace period, then close local resources. This package does not
  supervise draining.
- Abrupt process loss drops local accounting and cannot undo side effects.
  Idempotency remains an application or protocol requirement.

Dependency degradation belongs in readiness only when the service cannot
safely serve any traffic. It must not be reported as process liveness failure.

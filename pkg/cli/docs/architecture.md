# Architecture and lifecycle

The application composition root creates mutable `Command` definitions and
calls `Compile` once. Compilation validates names, aliases, layouts, groups,
inheritance, cycles, reused nodes and bindings, and configured resource limits.
It publishes an immutable graph. Every `Run` builds fresh Cobra state below
`internal/engine`; no parser state is shared between invocations.

Handlers receive caller-owned `context.Context`, invocation-local typed
`Input`, explicit `IO`, and bounded `Output`. Dependencies are supplied through
constructors or closures. Framework metadata never contains invocation values,
so middleware cannot observe secrets by default.

## Lifecycle

| Phase | Runs after failure? |
| --- | --- |
| graph construction and compilation | execution does not begin on failure |
| argv selection and raw parsing | conversion and later phases do not run |
| typed conversion and option groups | validation and later phases do not run |
| application validation | middleware and cleanup do not run |
| middleware entry | may short-circuit; cleanup still runs |
| pre-run hooks | handler and post-run stop on failure; cleanup runs |
| handler | post-run stops on failure; cleanup runs |
| post-run hooks | later post-run hooks stop; cleanup runs |
| cleanup | runs once in reverse registration order with a bounded context |
| rendering and exit selection | rendering failure is retained as output error |

Cleanup uses `context.WithoutCancel` over the last lifecycle context plus a
bounded timeout. It cannot erase the primary failure. Multiple failures use
`errors.Join`, preserving deterministic primary-first `errors.Is` and
`errors.As` behavior.

Middleware is explicit and ordered. It receives `CommandMetadata` and `Next`,
not input values. Logging, correlation, tracing, timing, audit, and deliberate
panic recovery can therefore be composed without a mandatory backend.
`Next` is single-use and valid only while its middleware callback is active.
The runtime waits for a continuation that started concurrently before cleanup,
and rejects a retained continuation invoked after the callback returns.

The core does not recover panics by default. Ordinary usage, validation, and
command failures must be returned. An application that deliberately adds panic
recovery should do so in middleware and must redact its diagnostics.

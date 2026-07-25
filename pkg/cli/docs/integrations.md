# Optional ecosystem composition

Integrations remain application-owned or dependency-isolated nested modules.
Core imports only Cobra and its small parsing dependency boundary.

- `config`: load and validate configuration before `Run`, then capture typed
  configuration or services in command constructors. Environment variables do
  not become implicit flags.
- `validation`: call a validator from `WithValidation` and translate its
  application-safe result without exposing secret rejected values.
- `log`: add middleware that observes safe `CommandMetadata`. Route logs to
  stderr only when that does not violate JSON mode, or use a separate backend.
- `telemetry`: start spans in middleware, pass a derived context to `Next`,
  and record kind and status. Input values are absent by default.
- `correlation`: derive and propagate correlation through middleware rather
  than a package global.
- `prompts`: prompt only when `Invocation.Interactive()` is true. The prompt
  package remains optional and must not be imported by core.

No adapter should register commands globally, reflect over controller methods,
resolve dependencies from a service locator, or replace explicit constructor
injection. Material integrations belong in nested modules so they cannot expand
the core dependency graph accidentally.

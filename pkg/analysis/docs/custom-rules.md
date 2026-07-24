# Designing custom rules

Prefer organization configuration for package names, layers, adapters, typed
sinks, constructors, and exceptions. Forking a generic analyzer only to embed a
repository path makes reusable analysis harder to govern.

A new reusable rule belongs in `analyzers/<rule>` and exposes stable metadata,
an unconfigured analyzer, and typed construction when policy is required. Its
diagnostic identifies one semantic violation and starts advisory. Resolve Go
objects and types instead of matching text; bound all traversal and fact data.

Use `analysistest` for rule fixtures and add the analyzer to
`analysistestkit` for no-panic fuzzing and aggregate performance measurement.
Integrate metadata through `policy.Builtin` so `golib-analysis rules`, JSON, SARIF,
suppressions, exceptions, and compatibility checks share one identity.

Repository-only checks may live outside this project and consume the public
`analysis` models, but they should follow the same overlap review, exact
diagnostic, suppression, mutation, determinism, and corpus-evidence standards.
Do not execute target code or arbitrary configuration to extend analysis.

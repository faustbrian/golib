# Contributing

Open an issue before making a public contract or dependency change. Keep the
core independent of command dispatch and full-screen TUI types. New behavior
needs deterministic tests, hostile-path coverage, documentation, and a
changelog entry.

Use Go 1.26.5 and verify the module independently:

```sh
GOWORK=off make check
GOWORK=off make fuzz
GOWORK=off make benchmark
```

Commits use conventional subjects and explain why in the body. Never commit
real secrets, developer terminal captures containing secrets, generated fuzz
failures without review, or unrelated sibling-library changes.

# Contributing

Use Go 1.26.5, keep public values immutable, and preserve the distinction
between instants, civil dates, local times, and fixed elapsed durations.

Behavior changes start with a failing test. Mathematical changes require an
example or property that would fail under the realistic defect. Every
production statement must remain covered, but coverage alone is insufficient:
run properties, fuzz smoke, race, and mutation gates as appropriate.

```sh
make tools
make check
make nilaway
make mutation
make vuln
```

Do not add clocks, schedulers, natural-language parsing, timezone databases,
business calendars, or rendering to core. New parsing and variable-output APIs
must accept or document resource limits. Public API changes require migration
and changelog entries.

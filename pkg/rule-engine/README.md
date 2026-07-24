# rule-engine

`rule-engine` is a deterministic, typed, inspectable engine for evaluating
facts and propositions. It compiles immutable execution plans, evaluates them
concurrently without hidden I/O, supports bounded forward chaining, and emits
redacted diagnostics and explanations.

It is intentionally not an authorization system, feature-flag service,
validator, workflow engine, database query layer, or action runner. Those
products may adapt its decisions while retaining their own fail-closed and
domain semantics.

```go
country := ruleengine.MustPath("shipment", "country")
set := ruleengine.RuleSet{ID: "routing", Rules: []ruleengine.Rule{{
    ID: "finland",
    When: ruleengine.Compare(ruleengine.OpEqual,
        ruleengine.Variable(country),
        ruleengine.Literal(ruleengine.String("FI"))),
}}}
plan, diagnostics, err := ruleengine.NewCompiler(
    ruleengine.DefaultLimits(),
).Compile(context.Background(), set)
```

See the executable [package example](example_test.go), the
[quick start](docs/quickstart.md), and the [JSON AST fixture](jsonast/testdata/location-routing.json).

## Guarantees

- Missing and null are distinct typed values; there is no truthiness or
  implicit coercion.
- Priorities sort descending and equal priorities sort by rule ID ascending.
- Logical operands evaluate left to right and short-circuit deterministically.
- Compilation rejects duplicate IDs, unknown operators, incompatible literal
  types, dependency cycles, non-finite floats, unsafe regexes, and every
  configured bound violation.
- Evaluation bounds time, iterations, derived facts, explanations, and errors.
- Canonical JSON and SHA-256 hashes are stable for equivalent definitions.
- Built-in plans and contexts are immutable and safe for concurrent reuse.

## Documentation

- [Model](docs/model.md)
- [Operators](docs/operators.md)
- [Types and coercion](docs/types-and-coercion.md)
- [Compilation](docs/compilation.md)
- [Evaluation](docs/evaluation.md)
- [Rule sets](docs/rule-sets.md)
- [Extensions](docs/extensions.md)
- [JSON AST](docs/json-ast.md)
- [Limits](docs/limits.md)
- [Security](docs/security.md)
- [Performance](docs/performance.md)
- [Migration](docs/migration.md)
- [Integration](docs/integration.md)
- [Cookbook](docs/cookbook.md)
- [FAQ](docs/faq.md)
- [Compatibility](docs/compatibility.md)

## Verification

`make check` runs formatting, module hygiene, vet, static analysis, lint,
tests, meaningful 100% production coverage, race tests, fuzz smoke tests,
mutation tests, benchmarks, documentation checks, API compatibility,
security policy checks, vulnerability scanning, and workflow validation.

The module requires Go 1.26.5 and has no runtime dependencies.
Exact decimal, temporal-period, and measurement adapters live in isolated
nested modules described in the [extension guide](docs/extensions.md), so core
consumers do not inherit their dependency graphs.

## License

MIT. See [LICENSE](LICENSE).

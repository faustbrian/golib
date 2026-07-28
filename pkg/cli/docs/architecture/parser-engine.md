# Parser engine decision

Status: amended on 2026-07-28. The initial Cobra adapter decision from
2026-07-22 is superseded before `v1.0.0`.

`cli` owns a dependency-free parser under `internal/engine`. Its public command
model, typed values, errors, lifecycle, output, completion, and documentation
contracts remain unchanged. Applications never receive or construct parser
objects.

## Evidence that changed the decision

The initial adapter selected Cobra v1.10.2 and PFlag v1.0.9 because they covered
nested commands, inherited options, assigned and attached values, short-option
groups, help, version, and `--` behavior. The module always translated their
mutable state and diagnostics into owned contracts.

The service-platform process benchmark later proved that this otherwise
internal dependency added 558,016 bytes to a stripped cohesive service binary.
The frozen platform budget permits at most 262,144 bytes above equivalent
low-level composition. The adapter therefore made an independently verified
public adoption budget impossible even though its types remained private.

The complete argv contract already had deterministic unit, process, fuzz, and
mutation evidence. Replacing the adapter with a small owned parser reduced the
runtime dependency graph while retaining executable conformance coverage for:

- nested command names and aliases;
- persistent and command-local options;
- long assigned and separate values;
- attached short values and combined boolean shorthands;
- repeated options and explicit empty values;
- interspersed options and `--`;
- negative numeric positionals and digit shorthands;
- help and version actions; and
- stable unknown-option and missing-value classifications.

## Alternatives retained as comparison evidence

The benchmark-only nested module continues to compare equivalent behavior
against Cobra, `urfave/cli`, Kong, and the standard `flag` package. Those
dependencies do not enter the core module graph. Fang remains unsuitable for
core because it expands the Cobra dependency with terminal presentation
packages, while reflection-driven Kong composition conflicts with explicit
command registration.

## Boundary rules

- Core has no runtime module dependencies.
- Parsing uses only immutable compiled metadata and invocation-local state.
- Applications pass already-tokenized `[]string`; no parser receives a shell
  command string.
- Internal failures are translated into stable owned error kinds.
- Help, suggestions, completion, typed conversion, validation, and lifecycle
  behavior remain owned outside the raw parser.
- Parser changes require differential argv tests, fuzzing, generated-output
  drift review, performance evidence, and a changelog entry.

External parser objects remain outside the compatibility contract. Adding a
parser dependency again requires a new decision with measured binary, latency,
allocation, security, maintenance, and supply-chain evidence.

# Parser engine decision

Status: accepted on 2026-07-22.

`cli` uses Cobra as an internal token parsing and command-tree engine. Its
public model, values, errors, execution lifecycle, output, completion, and
documentation contracts remain owned by this module. Application code does not
receive or construct Cobra commands.

## Compared releases

The comparison used the latest stable module versions reported by the public Go
module proxy on 2026-07-22. Source and release links are pinned so the decision
can be reproduced when dependencies are reviewed.

| Candidate | Stable release | Go version | Runtime dependency shape | Relevant behavior |
| --- | --- | --- | --- | --- |
| [Cobra](https://github.com/spf13/cobra/tree/v1.10.2) | v1.10.2 | 1.15 | `pflag` plus optional documentation helpers | Nested commands, POSIX flags, inherited flags, suggestions, help, and Bash, Zsh, Fish, and PowerShell completion |
| [`urfave/cli`](https://github.com/urfave/cli/tree/v3.10.1) | v3.10.1 | 1.22 | No non-test library dependency required by its source | Nested commands, owned flag parser, lifecycle hooks, help, and shell completion |
| [Kong](https://github.com/alecthomas/kong/tree/v1.16.0) | v1.16.0 | 1.20 | Reflection-based schema parser with small supporting modules | Strong typed decoding and validation, nested commands, help, and completion |
| [`flag`](https://pkg.go.dev/flag) | Go 1.26.5 | Go toolchain | Standard library | Independent flag sets and basic typed values; parsing stops at the first positional token and command trees are application-owned |
| [Fang](https://github.com/charmbracelet/fang/tree/v1.0.0) | v1.0.0 | 1.24.2 | Cobra plus terminal styling, width, color, and rendering modules | Polished Cobra help, errors, version display, man pages, and shell completion |

Dependency shape describes the implementation architecture, not every module
listed for tests or tooling. Release dates and module declarations are retained
by the [Go module proxy](https://proxy.golang.org/). The candidate repositories,
module files, documentation, release histories, licenses, and current issue and
pull-request activity were inspected as maintenance and supply-chain evidence.

## Decision drivers

### Maintenance and compatibility

Cobra has a long v1 compatibility line, a December 2025 stable release, broad
production use, active maintenance, and conservative Go requirements. Its
command and flag behavior is mature enough that `cli` does not need to
rebuild token parsing. Pinning the exact release and keeping it internal bounds
upstream behavioral drift.

`urfave/cli` is active and its v3 handler accepts `context.Context`, but its v3
line recently made material public-model and parsing changes from v2. Replacing
one public framework model with another would not improve the ownership
boundary.

Kong provides excellent typed decoding, but reflection over application structs
is its central composition mechanism. That conflicts with explicit command
registration and the requirement that typed input remain inspectable without
reflection-driven discovery.

The standard `flag` package is the smallest and most auditable option, but it
does not provide a command graph, interspersed options, short-option groups,
suggestions, or completion. Building and maintaining those token semantics here
would duplicate mature parser work and enlarge this module's security surface.

Fang is a credible modern presentation layer, but it is itself Cobra-based and
materially expands terminal and styling dependencies. `cli` requires a
headless-safe output contract and deliberately leaves rich terminal interaction
to `prompts`, so Fang is not a safer core engine.

### Parsing and generation

Cobra and `pflag` cover the required raw syntaxes: assigned long options,
separate values, short options, combined boolean shorthands, `--`, repeated
options, interspersed options, and inherited flags. `cli` owns four-shell
completion generation because upstream templates evaluate reconstructed command
strings. The owned templates pass token arrays directly without evaluation.
`cli` still owns and tests every promised semantic because upstream defaults
alone do not distinguish omitted, explicit, defaulted, and secret values or
provide the required lifecycle and errors.

### Performance

The initial choice is based on capability and boundary safety, not marketing
benchmarks. The repository benchmark suite compares construction and steady
state dispatch against the pinned Cobra, `urfave/cli`, Kong, and `flag`
versions using equivalent trees, argv, validation, and discarded output.
Performance regressions are evidence for revisiting the adapter, not permission
to weaken public behavior.

## Boundary rules

- Cobra and `pflag` types remain below `internal/engine`.
- A fresh engine tree is created from immutable compiled metadata for each
  invocation; mutable parser state is never shared between runners.
- Cobra errors, output, usage rendering, and completion directives are
  translated into owned contracts before crossing the boundary.
- Applications pass an already-tokenized `[]string`; no engine path receives a
  shell command string.
- Upgrading Cobra requires parsing differential tests, generated-output drift
  checks, benchmarks, vulnerability review, and a changelog entry.

## Rejected permanent coupling

Exposing `*cobra.Command`, `*pflag.FlagSet`, or callbacks that accept either
type would make engine replacement a consumer migration. Such APIs are outside
the compatibility contract even if an internal adapter uses them.

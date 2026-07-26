# Goal: `cli`

## Objective

Build `cli` as a production-grade open source Go command framework for
constructing explicit, testable, composable command-line applications with the
developer experience expected from mature systems such as Symfony Console and
Laravel Artisan, without importing their service-container magic, reflection-
driven discovery, or framework-global state.

The package MUST standardize command trees, typed arguments and options,
validation, lifecycle execution, cancellation, output modes, help, completion,
documentation generation, errors, and exit codes. It MUST work equally well
for local developer tools, distributable binaries, CI jobs, containers, ECS
one-off tasks, migration jobs, importers, backfills, diagnostics, and repair
commands.

## Product Position

`cli` owns:

- explicit command and subcommand registration;
- typed positional arguments, flags, aliases, defaults, and validation;
- command help, usage, examples, version output, and deprecation metadata;
- deterministic parsing and dispatch;
- context propagation, cancellation, signal handling, and shutdown semantics;
- stable error classification and exit-code mapping;
- human, quiet, and machine-readable output contracts;
- command lifecycle middleware and observability integration points;
- shell completion and command-reference generation; and
- deterministic test harnesses for command execution.

It MUST NOT own:

- application dependency injection or a service container;
- reflection-based command discovery, controller injection, or model binding;
- configuration loading, secret retrieval, logging backends, or telemetry
  exporters;
- scheduling, queues, migrations, imports, or business workflows themselves;
- interactive prompt rendering, terminal forms, or full-screen TUIs;
- remote command execution, SSH access, daemon supervision, or process
  orchestration; or
- package-global command registries, mutable defaults, or hidden goroutines.

Interactive UX belongs to `prompts`. `cli` MUST remain fully useful
without it and MUST NOT require an interactive terminal.

## Dependency Strategy

Use Cobra as the initial parsing and command-tree engine unless implementation-
time evidence demonstrates a better maintained and materially safer choice.
The package MUST own its public contracts and MUST NOT expose Cobra types as
required application-facing APIs. Cobra-specific translation SHOULD remain in
an internal implementation or explicit adapter so a future engine change does
not force consumers to rewrite commands.

Before implementation, compare current stable versions of:

- `spf13/cobra`;
- `urfave/cli`;
- `alecthomas/kong`;
- the standard library `flag` package; and
- any current credible successor.

Record the decision using maintenance, dependency weight, compatibility,
parsing semantics, completion, documentation, performance, and supply-chain
evidence. Do not rebuild mature token parsing merely to avoid a dependency,
but do not make a third-party command object the permanent public contract.

The core module path MUST be:

`github.com/faustbrian/golib/pkg/cli`

Optional integrations that materially increase dependencies SHOULD use nested
modules. The core MUST keep a small, auditable dependency graph.

## Command Model

Provide an explicit command model supporting:

- stable name, aliases, summary, description, examples, and documentation
  links;
- arbitrarily nested subcommands with deterministic registration order;
- hidden, experimental, deprecated, and replacement-command metadata;
- typed positional arguments with required, optional, repeated, and remainder
  cardinalities;
- typed boolean, string, integer, unsigned integer, floating-point, duration,
  time, enum, string-slice, and key/value options;
- short and long option names, persistent/inherited options, defaults, and
  mutually exclusive or jointly required groups;
- exact distinction between omitted, explicitly empty, zero, and defaulted
  values where meaningful;
- command-level validation and cross-field validation;
- pre-run, run, post-run, and cleanup phases with precise failure semantics;
- command-local dependencies supplied through constructors or explicit
  closures; and
- immutable compiled command metadata safe for concurrent documentation and
  completion reads.

Command construction MUST reject duplicate names, aliases, options, ambiguous
argument layouts, invalid inheritance, cycles, and unreachable commands before
execution. Registration MUST be explicit in the application composition root.

Do not treat environment variables as implicit flags. Applications MAY use
`config` before command execution and pass typed configuration explicitly.

## Handler Contract

The public execution contract MUST center on `context.Context` and explicit IO.
Handlers MUST receive already parsed and validated input rather than reaching
back into global parser state.

The design MUST provide:

- one unambiguous invocation per parse;
- typed access without stringly typed map lookups as the primary API;
- exact preservation of caller context and cancellation cause;
- explicit `stdin`, `stdout`, and `stderr` ownership;
- no process exit from library code;
- no direct signal registration in reusable core components;
- no hidden reads from the process environment or current working directory;
- no panic-based handling of ordinary input or command errors; and
- a top-level runner that can translate terminal results to an exit status.

If generics are used for typed input, the design MUST remain comprehensible,
discoverable in documentation, and compatible with command composition. Avoid
generic machinery that makes simple commands harder to write or inspect.

## Parsing Semantics

Document and test exact behavior for:

- `--name=value`, `--name value`, short flags, combined short flags, and `--`;
- negative numbers, values beginning with a hyphen, empty values, and repeated
  options;
- option placement before and after positional arguments;
- inherited options and shadowing;
- unknown commands, unknown options, suggestions, and typo thresholds;
- Unicode command text, invalid UTF-8, control characters, and normalization;
- platform path syntax and Windows drive-letter boundaries;
- maximum argument count and cumulative input size;
- shell-provided token boundaries without attempting to reimplement a shell;
  and
- compatibility implications when parsing behavior changes.

Never execute shell interpolation, expand variables, glob paths, or split a
caller-provided string implicitly. An argv slice is already tokenized input.

## Execution Lifecycle

Define a deterministic lifecycle for:

1. command-tree construction and validation;
2. argv parsing and command selection;
3. typed value conversion;
4. input and cross-field validation;
5. lifecycle middleware entry;
6. pre-run behavior;
7. handler execution;
8. post-run behavior;
9. cleanup; and
10. error rendering and exit-code selection.

Specify exactly which later phases run after each earlier phase fails. Cleanup
MUST run where resources may have been acquired and MUST not erase the primary
failure. Multiple failures require deterministic composition compatible with
`errors.Is` and `errors.As`.

Middleware MUST be explicit, ordered, and protocol-neutral. It SHOULD support
logging, telemetry, correlation, timing, panic recovery, and audit adapters
without making any adapter mandatory. Middleware MUST be able to observe safe
command metadata without receiving secret values by default.

## Context, Signals, And Shutdown

- Every execution MUST propagate `context.Context` to the handler.
- A reusable runner MUST accept caller-owned context and IO.
- An application-level helper MAY translate configured operating-system
  signals into cancellation, but signal ownership and restoration MUST be
  explicit.
- Repeated signals MAY support graceful then forced termination only through a
  documented application-level policy.
- Cancellation MUST produce a stable exit classification without hiding a more
  specific command error.
- Middleware and cleanup MUST honor bounded shutdown contexts.
- Tests MUST not rely on delivering real process signals.

The package MUST NOT call `os.Exit` except in an explicitly named executable
boundary helper whose behavior is impossible to invoke accidentally in tests.
Prefer returning an integer status from the runner and leaving `os.Exit` to
`main`.

## Errors And Exit Codes

Provide stable typed errors and classifications for:

- help and version requests;
- unknown command and unknown option;
- malformed or missing argument/option values;
- validation failure;
- command usage failure;
- cancellation and deadline expiration;
- command execution failure;
- internal framework failure; and
- output or cleanup failure.

Define a documented, configurable exit-code policy. Defaults MUST distinguish
success, usage failure, command failure, cancellation, and internal failure
without pretending one universal operating-system convention exists. Exit
codes MUST be bounded to portable process semantics and MUST remain stable
under SemVer.

Errors MUST support `errors.Is` and `errors.As`, retain causes, avoid duplicate
rendering, and never include secrets merely because a secret-bearing option was
invalid. Library code MUST return errors rather than print and return the same
error unless a renderer explicitly owns presentation.

## Output Contracts

Treat output as an API, especially for automation.

- Separate `stdout` from `stderr` consistently.
- Support human, JSON, quiet, and no-color modes through explicit policy.
- `--json` output MUST be valid, versioned where necessary, deterministic, and
  free of progress animation, ANSI control sequences, and incidental logs.
- Structured output MUST define success and error envelopes without exposing
  Go implementation details.
- `--quiet` MUST suppress informational output but not change execution.
- Color and terminal decoration MUST be disabled when unsupported, redirected,
  forbidden by policy, or explicitly disabled.
- Width-sensitive rendering MUST degrade safely for pipes and narrow terminals.
- Broken pipes and partial writer failures MUST have explicit, testable
  semantics.

The core MAY provide small table, list, status, and key/value output helpers,
but MUST NOT become a terminal UI framework. Rich interaction and animation
belong to `prompts`.

## Non-Interactive And Container Operation

Every command MUST declare whether interaction is optional, required, or
forbidden. The runtime MUST expose an explicit non-interactive mode equivalent
in intent to `--no-interaction`.

- Non-interactive execution MUST never wait for terminal input unexpectedly.
- Missing required input MUST fail before side effects.
- Defaults used in non-interactive mode MUST be explicit and documented.
- JSON, quiet, piped, CI, and non-TTY operation MUST imply no animated output.
- Commands intended for ECS tasks MUST operate with immutable argv, env-backed
  configuration supplied through `config`, logs on stderr, and meaningful
  exit status.
- Production documentation MUST prefer ECS tasks, schedulers, workers, or
  deployment jobs over shell access into running service containers.

`prompts` MAY be composed by an application when interaction is allowed,
but `cli` MUST define enough capability metadata to prevent accidental
prompting in headless environments.

## Help, Completion, And Documentation

Generate consistent help and usage from the command model:

- root and nested command help;
- positional and option descriptions, defaults, requirements, and examples;
- inherited option provenance;
- aliases, deprecations, replacements, and related commands;
- stable ordering and width-aware formatting;
- plain-text, Markdown, and machine-readable command manifests;
- Bash, Zsh, Fish, and PowerShell completion; and
- completion installation instructions without mutating shell configuration.

Completion MUST be side-effect free by default, bounded, cancellation aware,
and safe for hostile partial input. Dynamic completion MUST use explicit
providers and MUST NOT perform network or database access unless the
application deliberately supplies such a provider. Completion failures MUST
not leak secrets or corrupt the invoking shell.

Generated command documentation MUST be reproducible and checked for drift in
CI. The public README and docs MUST let a new consumer build, test, package,
and deploy a command without reading implementation source.

## Testing Foundation

Provide first-class test support for:

- isolated argv, environment, working-directory, clock, signal, and IO seams;
- invocation without mutating `os.Args` or process-global streams;
- stdout, stderr, exit status, selected command, typed input, and error
  assertions;
- golden help, completion, Markdown, JSON, and error output;
- simulated cancellation and signal behavior;
- writer errors, short writes, broken pipes, invalid UTF-8, and hostile argv;
- middleware ordering and cleanup; and
- test-safe secret redaction assertions.

Tests MUST be parallel-safe by default. Helpers MUST register cleanup and MUST
not leave modified environment variables, working directories, signals,
terminal state, or global registries behind.

Meaningful 100% production statement coverage is REQUIRED. Coverage MUST come
from behaviorally useful assertions over success, failure, boundary,
concurrency, cancellation, and cleanup paths. Tests written only to execute a
line without validating its contract do not satisfy this goal.

## Security And Safety

- Secret-bearing arguments and options MUST be explicitly classifiable and
  redacted from errors, telemetry, logs, help defaults, debug output, and
  process summaries produced by the package.
- Documentation MUST warn that command-line secrets may be visible through
  operating-system process inspection and recommend safer input channels.
- Escape or strip untrusted terminal control sequences in framework-generated
  diagnostics and suggestions.
- Bound argv size, suggestion work, completion results, generated output, and
  command-tree depth.
- Never invoke a shell or evaluate command text.
- Avoid TOCTOU-prone convenience around files; commands own file policy.
- Core MUST not use `unsafe`, cgo, `go:linkname`, or hidden runtime hooks.
- Panics from handlers or middleware MAY be recovered only by explicit policy
  and MUST retain useful diagnostics without leaking secrets.

## Performance And Comparative Evidence

Benchmark:

- construction and validation of small and large command trees;
- root and deeply nested dispatch;
- typed conversion and validation;
- help, completion, and machine-manifest generation;
- human and JSON output;
- successful, usage-error, cancellation, and unknown-command paths; and
- allocation behavior for repeated in-process execution.

Compare fairly against current Cobra, `urfave/cli`, Kong, and standard `flag`
baselines where they provide equivalent behavior. Each comparison MUST use the
same command tree, argv, validation, output destination, setup boundary, and
error behavior. Separate startup/construction cost from steady-state dispatch.
Do not publish claims based on omitted features, `/dev/null` on only one side,
different parser policy, disabled validation, or precomputed output.

Performance should be competitive and predictable, but correctness, stable
semantics, and startup safety outrank benchmark marketing.

## Documentation

Documentation MUST include:

- installation and a minimal command;
- command construction and explicit dependency injection;
- typed arguments, options, validation, lifecycle, and middleware;
- human, JSON, quiet, no-color, and non-interactive operation;
- errors, exit codes, cancellation, signals, cleanup, and broken pipes;
- help, completion, generated references, and packaging;
- ECS one-off task, CI, migration, importer, backfill, and diagnostic recipes;
- composition with `config`, `validation`, `log`, `telemetry`,
  `correlation`, and `prompts`;
- migration guidance from Cobra, `urfave/cli`, Kong, Symfony Console, and
  Laravel Artisan where useful;
- API reference, architecture, compatibility, security, performance,
  troubleshooting, FAQ, and release policy; and
- explicit limitations and intentional differences from competing frameworks.

Examples MUST compile and exercise public APIs. Documentation MUST explain why
commands remain explicit and why package-global registration and hidden DI are
intentionally absent.

## CI, Quality, And Release Requirements

Set up GitHub Actions and equivalent local commands for:

- formatting and generated-file drift;
- build, `go vet`, strict linting, and static analysis;
- tests with meaningful 100% production statement coverage;
- race testing and repeated concurrency-sensitive tests;
- fuzz smoke tests and retained regression corpora;
- mutation testing with reviewed survivor classifications;
- benchmarks and regression budgets;
- API compatibility and dependency-boundary checks;
- documentation, examples, completion, and manifest generation checks;
- vulnerability, dependency, license, secret, and supply-chain scanning;
- minimum supported Go and current stable Go testing; and
- reproducible release artifacts, SBOM, provenance, and signed releases.

NilAway SHOULD run as advisory rather than a failing gate until its findings
are proven sufficiently precise. Required tools MUST have strict but mutually
consistent configurations and MUST be runnable locally through documented
commands.

Maintain `CHANGELOG.md` from the first implementation commit. Every user-
visible behavior, compatibility decision, security correction, deprecation,
and breaking change MUST be recorded under an unreleased section.

## Definition Of Done

`cli` is complete only when:

- its command, parsing, lifecycle, IO, cancellation, error, and exit contracts
  are implemented and documented;
- applications do not need Cobra types, global parser state, or `os.Exit` in
  testable command code;
- human and machine output remain deterministic and headless-safe;
- help, completion, and reference generation are complete and reproducible;
- `prompts` integration is optional and non-interactive behavior is proven;
- ECS, CI, import, migration, backfill, and diagnostic examples pass;
- meaningful 100% production statement coverage is achieved;
- race, fuzz, mutation, benchmark, security, compatibility, docs, and release
  gates pass from a clean clone with `GOWORK=off`; and
- no undocumented magic, global state, parsing ambiguity, security gap, or
  implementation-engine leakage remains.

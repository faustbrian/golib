# Goal Harden: `cli`

## Mission

Perform an evidence-driven API, parser, lifecycle, process-boundary, terminal,
security, compatibility, performance, and supply-chain audit of `cli`, then
implement every justified correction required for trustworthy local,
automated, containerized, and distributable command-line applications.

Hardening MUST prove behavior independently of the underlying parsing engine.
Passing Cobra tests, high coverage, attractive help, or successful happy-path
commands do not prove stable dispatch, safe cancellation, secret handling,
machine-output integrity, or cleanup correctness.

## Authoritative Inputs

- `.ai/GOAL.md`, public APIs, generated API baselines, source, tests, fuzzers,
  benchmarks, docs, workflows, dependencies, and changelog;
- Go contracts for `context`, `errors`, `io`, `os`, `os/signal`, `flag`,
  process exit, Unicode, and supported operating systems;
- the exact pinned Cobra revision and any optional integration revisions;
- documented parsing and completion behavior from Cobra, `urfave/cli`, Kong,
  standard `flag`, Symfony Console, and Laravel Artisan as comparison evidence;
- POSIX and platform conventions where the package makes compatibility claims;
  and
- representative consumers including local tools, ECS tasks, migrations,
  importers, backfills, and diagnostic commands.

Third-party behavior is interoperability evidence, not authority over the
package's documented contracts. Every divergence MUST be classified as a bug,
compatibility constraint, deliberate policy, or unsupported behavior.

## Audit Rules

- Establish a clean reproducible baseline before behavior changes.
- Inventory every exported API, default, parser rule, lifecycle phase, output
  path, exit code, global state interaction, dependency, and generated artifact.
- Add a failing regression before every behavior correction.
- Do not weaken tests, mutation thresholds, static analysis, or documented
  semantics to preserve an accidental implementation detail.
- Preserve source and semantic compatibility unless behavior is unsafe,
  incorrect, or explicitly scheduled for a breaking release.
- Record all user-visible corrections and compatibility decisions in the
  changelog.
- Separate framework behavior from the underlying parser engine and prove the
  owned behavior through black-box tests.

## Public API And Dependency Boundary Audit

- enumerate every exported type, function, option, interface, generic
  parameter, error, constant, and method;
- prove command implementations do not require Cobra or another engine type;
- ensure third-party errors and mutable command objects do not leak through
  public contracts accidentally;
- verify optional integrations remain in dependency-isolated packages or
  nested modules;
- reject hidden package initialization, mutable registries, environment reads,
  and process-global parser state;
- verify zero values, constructors, option ordering, duplicate options, nil
  callbacks, and typed nils;
- prove documentation examples compile against only supported public APIs; and
- compare the exported API against its compatibility baseline before release.

## Command Graph Audit

- empty, one-command, broad, deeply nested, and maximum-bound command trees;
- duplicate names, aliases, option names, shorthand names, and inherited
  options;
- cycles, reused mutable nodes, unreachable commands, ambiguous argument
  layouts, and invalid group constraints;
- hidden, deprecated, experimental, alias, and replacement metadata;
- deterministic registration, traversal, help, completion, and manifest order;
- immutable compiled graph behavior under concurrent reads; and
- bounded construction and validation for hostile graph sizes.

Use an independently implemented reference model for command selection and
registration invariants where practical.

## Parsing And Typed Input Audit

Exhaustively test:

- every supported long, short, combined, assigned, repeated, inherited, and
  positional syntax;
- `--`, empty strings, empty assigned values, whitespace-bearing tokens,
  negative numbers, exponent forms, and values beginning with hyphens;
- required, optional, repeated, remainder, mutually exclusive, and jointly
  required values;
- all typed converters at zero, minimum, maximum, overflow, underflow, invalid,
  and platform-dependent boundaries;
- omitted versus explicit empty, zero, false, and default values;
- unknown commands/options, suggestions, aliases, and typo-distance limits;
- Unicode, invalid UTF-8, combining characters, bidi controls, terminal
  controls, and confusable command text;
- Windows paths, drive letters, separators, and supported shell conventions;
- huge token count, huge token size, huge cumulative argv, and deep command
  paths; and
- differential behavior against the pinned engine and documented owned policy.

Fuzz argv token sequences and typed conversion independently. Never treat a
single command string as shell input, and verify that no parser path performs
shell expansion, globbing, interpolation, or command execution.

## Lifecycle And Middleware Audit

- prove the exact order of parse, conversion, validation, middleware, pre-run,
  run, post-run, cleanup, rendering, and exit selection;
- test every phase failing alone and in combination with cleanup/output failure;
- ensure cleanup executes exactly once where required and cannot erase the
  primary failure;
- verify `errors.Is`, `errors.As`, cancellation causes, joined errors, and
  renderer behavior across phase combinations;
- test middleware ordering, short-circuiting, reentrancy, panic, cancellation,
  and after-callback behavior;
- ensure no callback executes while framework locks are held;
- ensure repeated in-process execution starts from clean invocation state; and
- prove handler, middleware, and cleanup state does not leak across concurrent
  runners.

## Context, Signal, And Process Audit

- parent cancellation before parsing, validation, middleware, and execution;
- cancellation during handler, post-run, cleanup, and output;
- deadline expiration and explicit cancellation causes;
- signal setup, delivery simulation, repeated signal policy, restoration, and
  concurrent runners;
- no leaked signal subscriptions, goroutines, timers, contexts, or channels;
- no unintended `os.Exit`, `log.Fatal`, panic, stdin read, environment read, or
  working-directory mutation;
- portable exit-code bounds and deterministic status selection;
- subprocess integration tests for the narrow executable boundary; and
- ECS-style termination behavior without relying on production access.

The bulk of tests MUST execute in-process. Subprocess tests are reserved for
behavior that can only be proven at the process boundary.

## IO And Output Audit

- exact stdout/stderr separation for every success and failure class;
- human, JSON, quiet, no-color, non-interactive, piped, and TTY-aware behavior;
- JSON validity, schema/version stability, deterministic key and record
  ordering, and absence of ANSI or log contamination;
- short writes, zero writes, delayed writes, writer errors, broken pipes,
  closed pipes, and concurrent cancellation;
- terminal width unavailable, zero, tiny, huge, and changing;
- color capability, `NO_COLOR` policy, redirected output, and unsupported
  terminals;
- hostile control sequences, bidi content, invalid UTF-8, very long values,
  and multiline errors;
- no duplicate error rendering or success output after failure; and
- stable golden output across supported platforms where promised.

Structured output MUST be validated with independent JSON decoding and schema
fixtures rather than string snapshots alone.

## Help, Completion, And Generation Audit

- help for every command shape, argument cardinality, option group, inherited
  option, alias, deprecation, and hidden command;
- narrow and wide terminal formatting without semantic loss;
- Markdown and machine manifest determinism and generated-file drift;
- Bash, Zsh, Fish, and PowerShell syntax validation using available shell
  tooling or pinned parsers;
- hostile partial completion input, cancellation, provider failure, huge
  result sets, duplicate candidates, descriptions, and escaping;
- dynamic provider side-effect and IO boundaries;
- completion scripts never interpolate untrusted values unsafely;
- generation does not mutate the command graph; and
- all generated outputs reproduce byte-for-byte from a clean clone.

## Non-Interactive And Integration Audit

- every command's interaction capability is declared and enforced;
- `--no-interaction`, CI, JSON, redirected stdin, and non-TTY execution never
  block waiting for input;
- missing required values fail before side effects;
- interactive defaults cannot silently become headless production choices;
- `prompts` composition remains optional and dependency-safe;
- `config`, `validation`, `log`, `telemetry`, and `correlation`
  adapters preserve lifecycle, redaction, and output boundaries;
- ECS task, migration, importer, backfill, and diagnostic fixtures terminate
  with correct output and status; and
- concurrency between multiple independently configured runners is safe.

## Secret And Terminal Security Audit

- classify secret options and arguments throughout parse, validation, errors,
  middleware, logs, traces, metrics, help, completion, manifests, and tests;
- prove defaults and invalid values never expose secrets;
- document process-list exposure and safer stdin/file/secret-provider patterns;
- attack diagnostics and suggestions with ANSI escapes, OSC sequences, bidi
  controls, carriage returns, terminal hyperlinks, and huge Unicode strings;
- prove completion output cannot inject shell syntax;
- bound parser, edit-distance, formatting, completion, and generation work;
- verify no shell invocation, unsafe path expansion, or implicit file access;
  and
- run dependency, advisory, license, secret, provenance, and workflow audits.

## Concurrency And Resource Audit

- race-test graph reads, runner execution, middleware, output, completion,
  documentation generation, and cancellation;
- repeated sequential and parallel in-process runs with isolated IO;
- goroutine, timer, signal subscription, file descriptor, and memory leak tests;
- cancellation under blocked readers, writers, and completion providers;
- maximum graph, argv, completion, output, and diagnostic limits;
- no unbounded caches, retained invocation values, or goroutine-per-option
  behavior; and
- no global locks that serialize independent command applications.

Race-detector silence is necessary but not sufficient. Assert invocation
isolation and immutable publication directly.

## Fuzzing And Mutation

Required fuzz targets include:

- arbitrary argv token sequences;
- command graph registration and aliases;
- typed values and cross-field constraints;
- Unicode and terminal-control input;
- lifecycle failure and cancellation sequences;
- help, completion, and manifest generation;
- error rendering and JSON output; and
- engine-adapter translation.

Seed corpora MUST include every discovered regression. Mutation testing MUST
target dispatch, validation, lifecycle ordering, cleanup, redaction, output
routing, exit-code mapping, and limits. Every surviving production mutation
requires a useful new assertion or a documented equivalent/unkillable
classification reviewed by a maintainer.

## Performance And Benchmark Audit

- benchmark construction separately from execution;
- benchmark small, broad, deep, and maximum command graphs;
- benchmark successful dispatch, usage errors, suggestions, conversion,
  validation, help, completion, JSON output, and cancellation;
- measure allocations, startup latency, binary-size contribution, and retained
  memory;
- compare equivalent behavior against Cobra, `urfave/cli`, Kong, and `flag`;
- pin fixtures, environment, toolchain, CPU settings, output sinks, and setup
  boundaries;
- retain raw benchmark data and statistical comparison output; and
- investigate regressions rather than loosening budgets automatically.

Comparisons MUST not omit validation, output, or construction on only one side.
Published claims require reproducible commands and precise caveats.

## Meaningful Coverage And Static Analysis

Meaningful 100% production statement coverage is REQUIRED, but line execution
alone is insufficient. Review evidence by behavioral risk and require strong
assertions for:

- every parser and conversion branch;
- every lifecycle phase and failure combination;
- every output mode and writer failure;
- every exit classification;
- every redaction path;
- every limit and cleanup path; and
- every platform-specific branch supported by the package.

Run formatting, `go vet`, strict `golangci-lint`, `staticcheck`, vulnerability
analysis, race tests, fuzzing, mutation, API checks, and architecture checks.
NilAway remains advisory unless a separately reviewed decision promotes it.
Tool rules MUST be strict but non-contradictory and reproducible locally.

## Documentation And Adoption Audit

- execute every README, API, adoption, cookbook, ECS, CI, migration, import,
  backfill, diagnostic, completion, and prompt-composition example;
- verify all public APIs, defaults, exit codes, output schemas, limits,
  compatibility promises, and security guidance are documented;
- document tradeoffs against direct Cobra, `urfave/cli`, Kong, Symfony Console,
  and Laravel Artisan usage;
- distinguish intentional simplicity from missing functionality;
- verify cross-links to `prompts` and sibling integrations without creating
  dependency cycles; and
- ensure a new consumer can build a production command without reading source.

## Mandatory Hardening Evidence

- meaningful 100% production statement coverage report;
- command graph and parser conformance matrices;
- lifecycle failure and cleanup matrix;
- output-mode and exit-code compatibility matrix;
- fuzz corpus and mutation report;
- race, leak, cancellation, and process-boundary evidence;
- shell completion validation evidence;
- fair comparative benchmark report with raw data;
- public API and dependency-boundary report;
- threat model and secret/terminal safety report;
- documentation execution report; and
- clean-clone local and CI release-gate evidence with `GOWORK=off`.

## Definition Of Done

Hardening is complete only when all justified findings are fixed or explicitly
documented with owner, rationale, severity, and follow-up; all mandatory
evidence exists; the complete local and CI gate stack passes; generated output
is reproducible; changelog and compatibility records are current; and no known
parser ambiguity, global-state leak, lifecycle inconsistency, secret exposure,
terminal injection, output corruption, resource leak, or benchmark deception
remains.

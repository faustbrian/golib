# Goal: `prompts`

## Objective

Build `prompts` as a production-grade open source Go package for expressive,
accessible, secure, deterministic interactive terminal prompts with developer
experience comparable to Laravel Prompts, while retaining explicit control,
headless safety, testability, and composability expected from Go libraries.

The package MUST provide a cohesive API for text, secret, confirmation,
selection, search, validation, progress, spinner, note, and status interactions.
It MUST adapt safely to terminals, redirected streams, CI, containers, and
non-interactive execution without hidden reads, indefinite waits, or global
terminal state.

## Product Position

`prompts` owns:

- interactive prompt definitions and execution;
- terminal capability detection and rendering policy;
- keyboard input, editing, navigation, cancellation, and submission;
- validation, transformation, defaults, hints, and retry presentation;
- secret-safe input and redaction;
- selection, search, pagination, and bounded option handling;
- progress, spinner, note, warning, error, and status presentation;
- themes, styles, accessibility, and no-color behavior;
- deterministic virtual-terminal and IO test support; and
- explicit fallback or refusal behavior outside interactive terminals.

It MUST NOT own:

- command trees, argv parsing, command dispatch, or process exit;
- application dependency injection, business workflows, or service discovery;
- configuration, secrets management, logging, telemetry exporters, queues, or
  scheduling;
- a mandatory full-screen TUI application architecture;
- remote terminals, SSH sessions, browser UI, or web forms; or
- global mutable themes, package initialization side effects, hidden
  goroutines, or process-wide terminal mutation.

`cli` owns command execution and MAY compose `prompts` when a command
explicitly permits interaction. Neither package may require the other for its
core use cases.

## Dependency Strategy

Use Huh as the initial interactive form and prompt engine unless current
implementation-time evidence identifies a materially better maintained,
accessible, and testable choice. Own the public prompt model and result
contracts so consumers do not require Huh types.

Evaluate current stable versions of:

- `charmbracelet/huh`;
- `charmbracelet/bubbletea` and `charmbracelet/bubbles`;
- `AlecAivazis/survey`;
- `manifoldco/promptui`; and
- any current credible successor.

Record maintenance, accessibility, terminal compatibility, dependency weight,
testability, cancellation, rendering correctness, security, and performance
evidence. Huh-specific translation SHOULD remain internal or in an explicit
adapter.

Bubble Tea or equivalent full-screen machinery MUST NOT become part of the
core public contract. Advanced full-screen components, if justified, SHOULD
live in a dependency-isolated nested module.

The core module path MUST be:

`github.com/faustbrian/golib/pkg/prompts`

## Prompt Model

Provide explicit prompt definitions for:

- single-line text;
- multiline text;
- password, token, and other secret input;
- yes/no confirmation;
- single selection;
- multiple selection;
- searchable/autocomplete selection;
- integer, decimal, duration, date, and time input where semantics are clear;
- file or path input without implicit filesystem mutation;
- grouped forms and conditional follow-up fields;
- progress bars, spinners, tasks, and bounded status streams; and
- notes, informational messages, warnings, errors, success messages, tables,
  and key/value summaries.

Every prompt MUST define:

- stable identity for validation and testing;
- label, description, placeholder, hint, and help text;
- typed result contract;
- optional explicit default;
- validation and transformation order;
- retry and maximum-attempt policy;
- cancellation and end-of-input behavior;
- secret classification;
- accessibility metadata and textual fallback; and
- interactive, fallback, or forbidden headless behavior.

Definitions MUST be immutable or defensively copied during execution. Reusing
a prompt or form MUST not retain prior answers, errors, cursor position, or
terminal state unless an explicit session object owns that state.

## Execution Contract

Prompt execution MUST accept:

- `context.Context`;
- explicit input, output, and error streams;
- explicit terminal capability and interaction policy;
- optional clock for animation and timeout testing;
- optional renderer/theme; and
- caller-owned validation dependencies.

It MUST NOT read `os.Stdin`, write `os.Stdout`, inspect environment variables,
or mutate terminal state unless an explicitly constructed application adapter
provides those resources.

Execution MUST return typed values and errors. It MUST never call `os.Exit`,
`log.Fatal`, or panic for ordinary invalid input, cancellation, terminal loss,
or writer failure.

## Interaction Policy And Headless Safety

Model interaction policy explicitly:

- interactive required;
- interactive preferred with an explicit fallback;
- non-interactive only; or
- auto-detect under caller-selected rules.

Terminal detection alone MUST NOT authorize prompting. The caller must permit
interaction. In CI, JSON output, redirected input, ECS tasks, and
`--no-interaction` mode:

- the package MUST never wait unexpectedly;
- required unanswered prompts MUST return a stable non-interactive error;
- defaults MUST be used only when explicitly permitted;
- secret defaults MUST NOT be rendered;
- animation and cursor control MUST be disabled; and
- fallback output MUST remain deterministic and parse-safe.

EOF, closed input, terminal detachment, and context cancellation MUST terminate
prompt execution promptly and restore any acquired terminal state.

## Input And Editing

Define and test:

- insertion, deletion, cursor movement, home/end, word movement, and selection
  where supported;
- Enter, Escape, Ctrl-C, Ctrl-D, Tab, Shift-Tab, arrows, page navigation, and
  configurable key bindings;
- paste, bracketed paste, multiline paste, and huge paste limits;
- grapheme clusters, combining marks, emoji, East Asian width, bidi text,
  invalid UTF-8, and control characters;
- terminal resize, tiny dimensions, and capability changes;
- keyboard-only operation and visible focus;
- screen-reader-friendly linear fallback; and
- platform-specific terminal behavior, including supported Windows terminals.

Do not claim complete grapheme, width, accessibility, or platform support
without executable evidence. Unsupported behavior MUST degrade predictably and
be documented.

## Validation And Transformation

- Support synchronous typed validation with stable safe messages.
- Context-aware validation MAY support remote or expensive checks but MUST be
  explicit, cancellable, debounced where appropriate, and bounded.
- Validation order, transformation order, and revalidation after transformation
  MUST be documented.
- Cross-field form validation MUST identify relevant fields without leaking
  other secret values.
- Retry counts MUST be bounded when configured and unlimited retry MUST require
  explicit interactive policy.
- Validator panic MUST not leave terminal state corrupted.
- Validation errors MUST not echo secrets or unsafe control characters.
- Integrating `validation` SHOULD require a small adapter rather than a
  mandatory dependency in the prompt core.

## Select, Search, And Pagination

- Preserve stable option identity separately from display labels.
- Support disabled options, descriptions, initial selection, grouping, and
  deterministic ordering.
- Duplicate values or labels MUST have explicit behavior.
- Search MUST define case, Unicode, normalization, tokenization, ranking, and
  tie-breaking behavior.
- Search and rendering work MUST be bounded for large option sets.
- Dynamic option providers MUST be context-aware, cancellable, debounced,
  generation-safe, and protected from stale-result replacement.
- Pagination MUST preserve focus and selection predictably across resize and
  filtering.
- Multi-select MUST define min/max selections and deterministic result order.
- No option renderer may execute arbitrary terminal control supplied by an
  untrusted label.

## Secrets

Secret prompts require stronger guarantees:

- disable echo using an explicit terminal capability with guaranteed restore;
- redact answers from rendering, errors, validation, logs, traces, metrics,
  snapshots, panic output, test failure output, and debug formatting;
- avoid retaining secret bytes longer than necessary;
- document Go string immutability limitations and provide byte-oriented APIs
  where meaningful cleanup is possible;
- define paste and clipboard-related behavior without claiming clipboard
  control the package does not have;
- prevent secret defaults from appearing in help, placeholders, replay, or
  accessibility output; and
- test cancellation, panic, writer failure, and terminal loss during secret
  entry for restoration and non-disclosure.

Documentation MUST distinguish masked display from actual secret-memory
erasure and MUST not make unverifiable security claims.

## Rendering And Themes

Provide a semantic rendering model rather than hard-coded color fragments.

- Themes MUST style roles such as label, value, hint, help, error, success,
  warning, focus, disabled, and progress.
- Plain and no-color themes MUST preserve all meaning textually.
- Output MUST account for terminal width, grapheme display width, wrapping,
  indentation, and multiline content.
- Rendering MUST escape or neutralize untrusted control sequences.
- Color profiles and hyperlink support MUST be capability-driven.
- Themes MUST be immutable, composable, and safe for concurrent use.
- The package MUST provide a sober accessible default theme rather than making
  decorative animation necessary for comprehension.

Snapshot output MUST be deterministic under a fake clock and fixed terminal
capabilities. Do not couple correctness to exact ANSI output from an upstream
renderer without an owned semantic assertion layer.

## Progress, Spinners, And Tasks

- Progress MUST support determinate and indeterminate operations.
- Updates MUST be context-aware, thread-safe, bounded, and coalesced under
  excessive frequency.
- The renderer MUST not create an unbounded queue or goroutine per update.
- Completion, failure, cancellation, and panic MUST leave a final stable line
  or explicit caller-selected erase behavior.
- Redirected and non-interactive output MUST use deterministic line-oriented
  status or remain silent under policy.
- Nested or concurrent tasks require explicit ordering and ownership.
- Progress values, totals, rates, and estimated durations MUST handle zero,
  overflow, regression, and unknown total safely.
- Slow writers and terminal loss MUST not deadlock the underlying operation.

The package MUST not become a task runner. It presents caller-owned progress;
it does not own business task scheduling or retries.

## Errors

Provide stable typed errors for:

- interaction not permitted;
- terminal capability unavailable;
- cancellation and deadline expiration;
- end of input or terminal detachment;
- invalid definition or unsupported prompt configuration;
- validation exhaustion;
- renderer, reader, writer, and terminal-control failure; and
- internal adapter failure.

Errors MUST support `errors.Is` and `errors.As`, preserve safe causes, avoid
duplicate rendering, and never include secret input. Error strings MUST remain
safe when written to a terminal or log.

## Testing Foundation

Provide a deterministic test harness with:

- virtual input and terminal capabilities;
- fixed width, height, color profile, Unicode policy, and clock;
- key, paste, resize, cancellation, EOF, and terminal-loss events;
- semantic screen assertions and optional ANSI golden snapshots;
- prompt answer, validation, retry, focus, selection, and result assertions;
- secret non-disclosure assertions over every captured channel;
- goroutine, timer, terminal-state, and stream cleanup checks; and
- no use of real sleeps or mutation of the developer's terminal.

Helpers MUST be parallel-safe and restore every resource they acquire.
Meaningful 100% production statement coverage is REQUIRED. It MUST exercise
behavioral contracts and hostile paths, not merely cause rendering lines to
execute.

## Accessibility And Internationalization

- Keyboard operation MUST cover every interactive feature.
- Color MUST never be the only signal.
- Plain linear output MUST remain understandable without cursor movement.
- Focus, error, selected, disabled, and progress state MUST have textual forms.
- Respect reduced-motion or explicit no-animation policy.
- Support caller-provided localized labels and messages without embedding a
  translation system in core.
- Do not concatenate caller-facing phrases in ways that prevent correct
  localization.
- Handle RTL and complex Unicode honestly, documenting tested capability and
  safe fallback.

Perform manual accessibility review in representative terminals in addition
to automated assertions. Record terminals, versions, and observed limitations.

## Performance And Comparative Evidence

Benchmark:

- first render and key-to-render latency;
- text editing at representative and maximum lengths;
- select navigation, filtering, pagination, and large option sets;
- form validation and field transitions;
- progress update throughput and coalescing;
- plain, ANSI, no-color, and redirected output;
- allocations, retained memory, goroutines, timers, and binary-size impact; and
- cancellation and cleanup under slow IO.

Compare fairly with current Huh, Survey, PromptUI, and direct Bubble Tea/Bubbles
where equivalent scenarios exist. Use identical input event streams, terminal
dimensions, styling, validation, option counts, output capture, and setup
boundaries. Separate engine startup from steady-state interaction. Do not claim
speedups from rendering less information or disabling validation on one side.

## Documentation

Documentation MUST include:

- installation and minimal prompts;
- every prompt type and typed-result contract;
- forms, validation, transformation, search, pagination, and dynamic options;
- secret input and its real security limitations;
- themes, accessibility, Unicode, terminal compatibility, and no-color mode;
- progress, spinners, concurrent updates, cancellation, and cleanup;
- non-interactive, CI, pipe, container, and ECS behavior;
- composition with `cli` and optional `validation` integration;
- test harness and deterministic rendering examples;
- migration from Huh, Survey, PromptUI, Laravel Prompts, and Symfony Question
  Helper where useful;
- API, architecture, compatibility, security, performance, troubleshooting,
  FAQ, and release documentation; and
- explicit limitations and intentional differences from full-screen TUI
  frameworks.

Every example MUST compile. Adoption docs MUST show how the same operation
receives explicit input non-interactively rather than forcing a prompt.

## CI, Quality, And Release Requirements

Set up GitHub Actions and equivalent local commands for:

- formatting and generated-file drift;
- build, `go vet`, strict linting, and static analysis;
- tests with meaningful 100% production statement coverage;
- race testing and repeated concurrent progress/provider tests;
- fuzz smoke tests and retained regression corpora;
- mutation testing with reviewed survivor classifications;
- deterministic render, ANSI, docs, and example checks;
- benchmarks and regression budgets;
- supported terminal and operating-system matrix checks;
- API compatibility and dependency-boundary checks;
- vulnerability, dependency, license, secret, and supply-chain scanning;
- minimum supported Go and current stable Go testing; and
- reproducible releases, SBOM, provenance, and signing.

NilAway SHOULD run as advisory rather than a failing gate until demonstrated
precise. Strict tools MUST have mutually consistent configurations and local
documented entry points.

Maintain `CHANGELOG.md` from the first implementation commit. Record every
user-visible behavior, terminal compatibility decision, security correction,
accessibility improvement, deprecation, and breaking change.

## Definition Of Done

`prompts` is complete only when:

- all promised prompt, form, validation, selection, search, progress, theme,
  accessibility, and fallback contracts are implemented and documented;
- consumers do not require Huh or Bubble Tea types in application code;
- non-interactive execution can never wait unexpectedly;
- secret prompts are restored and redacted across every failure path;
- virtual-terminal tests deterministically prove input, rendering, resize,
  cancellation, and cleanup;
- `cli` composition remains optional, explicit, and cycle-free;
- meaningful 100% production statement coverage is achieved;
- race, fuzz, mutation, benchmark, security, compatibility, accessibility,
  docs, and release gates pass from a clean clone with `GOWORK=off`; and
- no undocumented global state, terminal leak, secret exposure, unbounded
  rendering work, inaccessible essential state, or upstream-engine leakage
  remains.

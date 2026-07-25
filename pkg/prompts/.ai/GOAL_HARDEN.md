# Goal Harden: `prompts`

## Mission

Perform an evidence-driven interaction, terminal, Unicode, accessibility,
secret-handling, concurrency, cancellation, API, performance, compatibility,
and supply-chain audit of `prompts`, then implement every justified
correction required for safe and predictable interactive use.

Hardening MUST go beyond attractive snapshots and happy-path terminal demos.
It must prove terminal restoration, headless refusal, deterministic input,
secret non-disclosure, bounded rendering, cancellation, Unicode behavior, and
cleanup under hostile readers, writers, validators, providers, and terminals.

## Authoritative Inputs

- `.ai/GOAL.md`, exported APIs, source, tests, fuzzers, benchmarks, docs,
  generated artifacts, workflows, dependencies, and changelog;
- exact pinned Huh, Bubble Tea/Bubbles, terminal, styling, Unicode, and width
  dependency revisions;
- Go contracts for `context`, `io`, `errors`, goroutines, timers, and supported
  operating systems;
- terminal mode, ANSI/ECMA-48, TTY, Unicode, grapheme, display-width, and
  accessibility references applicable to package claims;
- Huh, Survey, PromptUI, direct Bubble Tea/Bubbles, Laravel Prompts, and Symfony
  Question Helper behavior as comparative evidence; and
- representative `cli`, CI, pipe, local terminal, container, and ECS
  consumers.

Pin evidence versions and distinguish normative terminal behavior, dependency
behavior, package policy, accessibility guidance, and inference.

## Audit Rules

- Establish a reproducible baseline before behavior changes.
- Inventory every prompt, field, event, key binding, renderer, capability,
  theme role, validator, provider, goroutine, timer, terminal mutation, limit,
  error, and dependency.
- Add a failing regression before each correction.
- Do not update goldens blindly; explain the semantic reason for every output
  change.
- Do not weaken Unicode, accessibility, race, fuzz, mutation, or cleanup gates
  merely to accommodate an upstream engine.
- Preserve compatibility unless behavior is unsafe, incorrect, inaccessible,
  or explicitly scheduled for a breaking release.
- Record user-visible and security-relevant changes in the changelog.

## API And Engine-Isolation Audit

- enumerate every exported type, function, interface, option, error, constant,
  renderer contract, and test helper;
- prove consumers do not require Huh, Bubble Tea, or styling-engine types;
- prevent upstream mutable models and errors from leaking through public APIs;
- verify optional full-screen integrations remain dependency-isolated;
- test zero values, nil streams, typed nil callbacks, option ordering, duplicate
  options, and invalid definitions;
- prove prompt definitions are reusable without retained execution state;
- verify concurrent sessions do not share mutable themes, focus, answers,
  validation errors, terminal state, or timers; and
- compare the exported API to the compatibility baseline.

## Terminal Acquisition And Restoration Audit

- interactive permission separate from TTY detection;
- stdin and output referring to the same or different terminals;
- terminal unavailable, unsupported, detached, resized, suspended, resumed,
  closed, and replaced;
- raw/no-echo mode acquisition and restoration on success, validation retry,
  cancellation, deadline, EOF, panic, reader failure, writer failure, and
  renderer failure;
- nested sessions and competing terminal ownership;
- restoration idempotence and failure reporting without masking the primary
  error;
- subprocess tests for real terminal behavior using controlled pseudo-terminals;
- Windows and Unix-specific implementations under their supported CI matrix;
  and
- no mutation of a developer terminal during ordinary unit tests.

Terminal restoration is a release blocker. A defer statement without injected
failure tests is not sufficient evidence.

## Interaction Policy And Headless Audit

- required, preferred-with-fallback, non-interactive-only, and auto-detect
  policy combinations;
- caller permission, TTY capability, CI policy, redirected streams,
  `--no-interaction`, JSON mode, and no-color mode;
- no unexpected blocking when input is absent, closed, redirected, or
  forbidden;
- defaults accepted only under explicit policy and before side effects;
- stable errors for unanswered required prompts;
- deterministic plain fallback with no cursor control or animation;
- `cli` integration preserving stdout/stderr and exit behavior; and
- race-free independent policies across concurrent sessions.

Use timeout guards in integration tests to detect accidental waits, but do not
use timeouts as the primary correctness mechanism.

## Input Event And Editing Audit

- all documented keys, key chords, navigation, editing, deletion, submission,
  cancellation, and focus movement;
- partial, concatenated, delayed, reordered, malformed, and unknown escape
  sequences;
- bracketed paste, huge paste, multiline paste, control bytes, and paste during
  resize or cancellation;
- repeated keys and event storms without lost terminal restoration;
- cursor movement over ASCII, multibyte runes, grapheme clusters, combining
  marks, variation selectors, emoji sequences, and East Asian width;
- bidi controls, invalid UTF-8, NUL, carriage return, tabs, and terminal control
  content;
- tiny, zero, huge, and changing terminal dimensions;
- event reader failure, EOF, terminal detachment, and context cancellation; and
- platform-specific key encodings for every claimed terminal family.

Fuzz raw input bytes and normalized event sequences separately. Retain every
panic, hang, corruption, or restoration regression in the corpus.

## Prompt-Type Audit

For every prompt type, test:

- empty, omitted, explicit default, minimum, maximum, boundary, and invalid
  values;
- submit, cancel, retry, EOF, timeout, and renderer failure;
- label, description, hint, help, placeholder, and error rendering;
- typed result conversion and exact value preservation;
- reusable definition and independent repeated execution;
- non-interactive fallback and refusal;
- secret classification propagation; and
- accessibility and plain-text behavior.

Forms additionally require exhaustive focus order, conditional visibility,
cross-field validation, back navigation, retained answer, clearing, submission,
and cancellation tests.

## Validation And Provider Audit

- sync and context-aware validator success, failure, cancellation, timeout,
  panic, and unsafe error text;
- validation and transformation order across retries and forms;
- cross-field failures involving secret and non-secret fields;
- bounded retry and maximum-attempt behavior;
- dynamic option provider debounce, cancellation, overlapping generations,
  stale result suppression, error, panic, and huge result sets;
- no goroutine, timer, result, or secret retention after validation/provider
  completion;
- slow or blocked validators/providers cannot prevent session cancellation; and
- optional `validation` adapter preserves field identity and safe errors.

Use deterministic clocks and schedulers instead of real sleeps.

## Selection, Search, And Pagination Audit

- no options, one option, duplicate labels, duplicate identities, disabled
  options, grouped options, and maximum bounded options;
- initial focus and selection, min/max selection, select all/none, and stable
  result order;
- search case, Unicode normalization, combining marks, tokenization, ranking,
  tie-breaking, empty query, and no result;
- filtering while selection exists and while provider generations overlap;
- pagination boundaries, page-size changes, resize, focus retention, and
  selected off-screen items;
- huge labels, descriptions, control sequences, and invalid UTF-8;
- bounded CPU, allocations, rendered rows, and provider results; and
- reference-model comparison for focus, filter, page, and selection state.

## Secret Non-Disclosure Audit

Inject unique canary secrets and assert their absence from:

- visible and captured output;
- errors and wrapped causes;
- validation and transformation diagnostics;
- logs, traces, metrics, events, debug formatters, and panic recovery;
- snapshots, golden failures, fuzz artifacts, benchmark labels, and CI output;
- help, hints, placeholders, defaults, completion, and form summaries;
- renderer state retained after completion; and
- heap-retention probes where practical without making impossible erasure
  guarantees.

Test paste, backspace, retry, cancellation, timeout, panic, terminal loss,
writer failure, and restoration failure during secret entry. Verify no-echo is
restored independently of whether memory cleanup can be guaranteed.

## Rendering And ANSI Safety Audit

- semantic state mapped correctly to ANSI and plain renderers;
- width calculation, wrapping, truncation, indentation, multiline content,
  and cursor positioning for all tested Unicode classes;
- no-color retains focus, selected, disabled, success, warning, and error
  meaning textually;
- hostile ANSI, OSC, hyperlink, carriage-return, bidi, and erase sequences are
  escaped or neutralized in untrusted content;
- capability negotiation does not emit unsupported control sequences;
- frame diffing never leaves stale secret or error text visible;
- resize during partial write and slow writer behavior;
- deterministic frames under fixed clock and capabilities; and
- semantic assertions remain authoritative over fragile byte snapshots.

Differentially test width and grapheme dependencies against curated Unicode
fixtures and independent implementations. Document known terminal-specific
limitations rather than guessing.

## Progress And Concurrency Audit

- determinate, indeterminate, zero-total, unknown-total, complete, over-total,
  regressing, and overflow progress;
- concurrent updates, updates after completion, duplicate completion, failure,
  cancellation, and panic;
- bounded update coalescing and explicit dropped-intermediate-update policy;
- slow/failed writers and terminal detachment without blocking caller work
  indefinitely;
- nested and multiple concurrent progress displays with defined ownership;
- final-line, erase, quiet, plain, redirected, and non-interactive policies;
- exact goroutine and timer lifecycle; and
- race, deadlock, starvation, leak, and retained-value tests under stress.

No permanent goroutine, unbounded channel, or goroutine per update is allowed.

## Accessibility Audit

- complete keyboard operation without mouse assumptions;
- visible and textual focus, selection, disabled, validation, success, warning,
  error, and progress states;
- color-independent meaning under every default theme;
- logical linear reading order in plain fallback;
- reduced-motion/no-animation behavior;
- narrow-terminal and high-zoom equivalents;
- screen-reader-oriented manual review in representative supported terminals;
- localized and RTL content behavior; and
- documentation of known limitations and workarounds.

Automated checks support but do not replace manual accessibility review. Store
the review matrix, terminal versions, scenarios, and findings as release
evidence.

## Error, Cancellation, And Cleanup Audit

- stable `errors.Is`/`errors.As` behavior for every public error class;
- safe cause wrapping without duplicate user presentation;
- parent cancellation and deadline at every lifecycle point;
- primary versus validation, renderer, restoration, and cleanup error
  precedence;
- panic recovery only under explicit policy with terminal restoration;
- no retries after terminal loss or forbidden interaction;
- all readers, goroutines, timers, providers, progress loops, and terminal
  modes terminate; and
- repeated sessions after every failure begin from clean state.

## Fuzzing And Model Evidence

Required fuzz targets include:

- raw terminal bytes and escape sequences;
- normalized key, paste, resize, EOF, cancellation, and failure sequences;
- text editing and cursor/grapheme state;
- form focus, visibility, validation, and submission state;
- selection, search, pagination, and dynamic provider generations;
- ANSI/plain rendering with hostile content and dimensions;
- progress update and completion sequences; and
- secret-entry failure sequences.

Use small, clear reference models for editing, form focus, selection, and
progress lifecycle. Divergence from the optimized implementation is a release
blocker until proved intentional.

## Meaningful Coverage And Mutation

Meaningful 100% production statement coverage is REQUIRED. Review evidence for
assertion quality across:

- every prompt type and interaction policy;
- every terminal acquisition/restoration path;
- every cancellation and IO failure point;
- every secret handling and redaction path;
- every renderer capability and fallback;
- every validation/provider lifecycle;
- every progress terminal state; and
- every supported platform branch.

Mutation testing MUST target validation, defaults, focus, selection, search,
pagination, redaction, ANSI escaping, width, cancellation, restoration,
progress, and limits. Every survivor needs a useful assertion or reviewed
equivalent/unkillable classification.

## Performance And Comparative Audit

- measure first render, event latency, frame generation, writes, allocations,
  retained memory, goroutines, timers, and binary-size contribution;
- benchmark all prompt types, small/large forms, long text, Unicode, selection,
  search, pagination, dynamic options, and progress;
- benchmark slow writer and high-frequency update behavior;
- compare equivalent cases with Huh, Survey, PromptUI, and direct Bubble Tea;
- pin input streams, terminal dimensions, styling, validation, output capture,
  setup boundary, toolchain, and environment;
- retain raw data and statistical comparison reports; and
- investigate regressions without trading away correctness or accessibility.

Do not publish comparisons where implementations render different content or
where setup, validation, output, or terminal emulation differs materially.

## Supply Chain And CI Audit

- minimize and justify direct and transitive terminal/UI dependencies;
- audit maintenance, licenses, advisories, checksums, provenance, and release
  practices of Huh and its transitive graph;
- pin GitHub Actions by immutable revision with minimal permissions;
- isolate untrusted pull-request workflows from secrets and release rights;
- run formatting, vet, strict lint, static analysis, tests, race, fuzz,
  mutation, benchmarks, API, docs, vulnerability, and license checks locally
  and in CI;
- test minimum and current stable Go plus supported OS/terminal matrices;
- keep NilAway advisory unless separately promoted with evidence; and
- produce reproducible release artifacts, SBOM, provenance, and signatures.

## Documentation Audit

- execute every README, API, adoption, prompt, form, validation, selection,
  progress, secret, accessibility, non-interactive, `cli`, and testing
  example;
- verify exact defaults, key bindings, cancellation, errors, limits, terminal
  support, Unicode support, and fallback semantics;
- explain dependency-engine choices and migration paths;
- distinguish verified guarantees from best effort and known limitations;
- ensure every secret and terminal safety caveat is visible before adoption;
  and
- ensure consumers can provide equivalent explicit non-interactive input.

## Mandatory Hardening Evidence

- meaningful 100% production statement coverage report;
- prompt-type and interaction-policy matrix;
- terminal acquisition/restoration failure matrix;
- key/editing/Unicode/width fixture report;
- secret canary non-disclosure report;
- accessibility review matrix;
- renderer/ANSI security report;
- race, leak, cancellation, provider, and progress stress evidence;
- fuzz corpus, reference-model results, and mutation report;
- fair comparative benchmarks with raw data;
- API/dependency-boundary and supply-chain reports;
- documentation execution and compatibility reports; and
- clean-clone local and CI release-gate evidence with `GOWORK=off`.

## Definition Of Done

Hardening is complete only when every justified finding is fixed or explicitly
documented with owner, rationale, severity, and follow-up; terminal restoration
and secret non-disclosure are proven across hostile failure paths; headless
execution cannot block; accessibility and Unicode claims have recorded
evidence; all mandatory local and CI gates pass; generated evidence is
reproducible; changelog and compatibility records are current; and no known
global-state leak, terminal corruption, secret disclosure, unbounded work,
concurrency defect, inaccessible essential state, or misleading benchmark
claim remains.

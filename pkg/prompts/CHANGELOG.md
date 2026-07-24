# Changelog

All notable changes follow Keep a Changelog and Semantic Versioning.

## [Unreleased]

### Added

- Explicit interaction modes, caller-supplied terminal capabilities, and
  deterministic headless refusal without hidden input reads.
- Immutable typed text definitions and stable safe error classifications.
- Typed multiline, confirmation, integer, exact decimal, duration, calendar
  date, wall-clock time, and non-mutating path definitions with explicit
  parsing through caller-supplied input.
- Ordered typed pre-validation, transformation, and post-validation pipelines
  with caller-owned dependencies, defensive copies, cancellation checks, and
  panic containment.
- Immutable semantic frames, composable themes, deterministic plain and ANSI
  rendering, grapheme-safe wrapping, capability-driven color, and terminal and
  bidi control neutralization.
- Stable-identity single, multiple, and searchable selection definitions with
  disabled options, descriptions, grouping, bounded option counts, explicit
  defaults, selection bounds, declaration-order results, and deterministic
  Unicode-normalized ranking.
- Pinned implementation-time dependency evaluation for interactive engines.
- Classified string and byte-oriented secret values with default-redacted
  formatting, serialization, structured logging, validation errors, defensive
  copies, and explicit best-effort byte destruction.
- Explicit cancellable semantic input events, bounded grapheme-aware line
  editing, retry and cancel handling, resize events, terminal lifecycle and
  echo restoration, and a deterministic parallel-safe virtual terminal.
- Keyboard-driven single, multiple, and searchable selection with disabled
  option skipping, textual focus and selection state, grouped labels, bounded
  pagination, stable resize behavior, and declaration-order results.
- Immutable ordered forms with typed defensive result access, conditional
  follow-up fields, caller-owned cross-field dependencies, panic containment,
  secret-aware error redaction, and byte-secret result cleanup.
- Caller-driven determinate progress and spinners, bounded concurrent status
  streams, semantic messages and notes, Unicode-width tables, and ordered
  key/value summaries without hidden timers, goroutines, or output queues.
- Bounded ordered task groups with explicit parent ownership, concurrent
  caller-driven state handles, and stable nested textual rendering without
  task scheduling or retry ownership.
- A parallel-safe virtual clock with caller-driven timers, coalescing tickers,
  deterministic advancement, and no real sleeps or background goroutines.
- Retained fuzz corpora and iteration budgets for terminal sanitization,
  deterministic rendering, search ordering, and interactive secret
  non-disclosure, plus allocation-reporting interaction benchmarks.
- Pinned strict lint, static analysis, vulnerability, NilAway advisory, and
  secret-scanning entry points with mutually compatible local configuration.
- SHA-pinned CI jobs for quality, tests, race, coverage, fuzzing, security,
  benchmark smoke, advisory NilAway, Linux and macOS, the minimum Go toolchain,
  and current stable Go.
- Executable dependency and runtime boundaries that forbid ambient OS access,
  upstream engine leakage, package initialization, production panics, and
  unowned goroutines in core production files.
- Executable examples, adoption and migration guidance, security and
  accessibility limitations, compatibility and release documentation, and a
  documentation completeness gate.
- Caller-driven dynamic option sessions with cancellable providers,
  deterministic clock-based debounce, bounded results, panic containment,
  defensive snapshots, and generation-safe stale-result rejection.
- Exact exported-API compatibility evidence, reviewed mutation thresholds,
  dependency license and deterministic SBOM checks, and reproducible source
  archive verification with a composite release gate.
- SHA-pinned CodeQL and dependency-review workflows plus signed-tag release
  automation for deterministic source archives, SBOMs, checksums, keyless
  Sigstore bundles, GitHub attestations, and verified releases.
- Immutable execution-local key maps that rebind non-text editing, navigation,
  submission, cancellation, and end-of-input meanings while disabling prior
  shortcuts for the rebound action.
- Explicit-clock progress rates and bounded estimated remaining durations with
  safe omission for unknown totals, zero elapsed time, regression, and
  duration overflow.
- A bounded incremental terminal byte decoder for UTF-8, common navigation
  keys, control keys, partial sequences, and bracketed paste with safe reset on
  malformed, unsupported, truncated, or oversized input.
- An optional caller-constructed raw terminal adapter with explicit files,
  bounded deadline-based context polling, capability detection, echo control,
  restoration, Unix PTY tests, and Linux and macOS support.
- A dependency-isolated comparison module that drives current Huh, Survey,
  PromptUI, and direct Bubble Tea/Bubbles through equivalent pseudo-terminal
  text input and records minimum-import binary size.
- Opt-in byte-native bracketed paste and grapheme editing for `SecretBytes`,
  with redacting event payloads and cleanup of decoder, editor, and temporary
  buffers across completion and failure paths.
- Execution-local form Tab and Shift-Tab navigation with retained text,
  selection, search, and byte-secret drafts, inactive-field skipping, and
  conditional downstream result invalidation.
- Keyboard-native multiline editing with a distinct Ctrl-J newline event while
  Enter remains explicit submission.
- Complete runtime terminal capability-change events with deterministic
  ASCII-only rendering fallback and prompt termination on terminal loss.
- Safe semantic HTTP, HTTPS, and mailto hyperlinks with capability-gated OSC 8
  rendering and deterministic textual fallback.
- Benchmarks and allocation regression gates for maximum-bound editing,
  interactive search pagination, forms, render profiles, progress coalescing,
  and cancellation cleanup.
- Context-aware progress, spinner, and status mutation variants that reject
  canceled and expired operations without changing retained presentation state.
- Reference-model fuzzing for selection pagination, conditional form
  navigation, dynamic option generations, and progress lifecycle transitions.
- Independent editor and selection-state model fuzzing plus secret-entry
  non-disclosure checks across cancellation, EOF, detachment, and bad resize.
- A requirement-mapped hardening report with explicit release blockers and
  retained raw comparison and binary-size measurements.
- Dedicated CI mutation testing and repeated race-enabled progress, provider,
  status-stream, and task-group concurrency checks with matching local targets.
- A build-checked manual accessibility review application covering VoiceOver
  announcement, keyboard, secret, validation, selection, resize, cancellation,
  progress, and restoration scenarios.
- Recorded manual VoiceOver passes for iTerm2 and Apple Terminal with exact
  environment details and subjective claim boundaries.

### Fixed

- Raw-mode public prompts now keep kernel echo disabled while input is owned by
  the semantic renderer, preventing carriage returns from appearing as `^M`
  and avoiding duplicate terminal-managed input.
- Multi-select bound failures now reach caller post-validation before the
  built-in count check, allowing localized corrective messages. The manual
  accessibility review also permits repeated multi-select correction.
- Real Linux and macOS terminal input now uses bounded, context-aware polling
  when Go file deadlines are unavailable, and standalone Escape keys resolve
  without waiting for end-of-input.
- Raw terminal acquisition now preserves output line processing, preventing
  successive prompt lines from drifting across the screen. The accessibility
  review also permits repeated correction after an empty submission.
- Corrected the release evidence count to match the supported manual
  assistive-technology review environments.
- Made module-boundary checks accept both LF and CRLF Git checkouts so the
  architecture contract is independent of checkout line endings.
- Terminal acquisition now rejects nil contexts before terminal mutation and
  preserves cancellation as a cancellation error if the context ends during
  acquisition.
- Closed terminal input and detached event-source failures now retain the
  stable terminal-detachment error class instead of becoming generic reader or
  adapter failures.
- Rendered caller-localized accessibility labels, descriptions, and textual
  hints in both value and selection prompt frames instead of retaining them
  only in descriptors.
- Replaced virtual-terminal event queue overflow panic with a stable typed
  definition error and made closed-input queueing return end-of-input.
- Made the mutation gate preserve Gremlins failures, reject timeout results,
  and require complete coverage of the portable executable package surface.
- Removed the independently cataloged benchmark harness from the public API
  baseline so compatibility checks cover only packages shipped by this module.

### Changed

- Retained Huh as comparative evidence rather than the core executor after
  source inspection found ambient environment reads and package-global state.

### Removed

- Removed Windows terminal support, Windows-specific adapter code, Windows CI,
  and the Narrator review requirement. Supported platforms are Linux and
  macOS.

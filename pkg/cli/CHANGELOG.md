# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Run the local competitor benchmark harness through the repository workspace
  so it can measure the unreleased canonical CLI module before initial tags.

### Fixed

- Bounded fuzz worker parallelism to prevent resource saturation from turning
  successful structured-output cases into harness deadline failures.
- Terminated generated Markdown with exactly one newline so checked-in command
  references remain byte-for-byte reproducible without trailing blank lines.
- Rejected Unicode bidi controls in command identities and stripped them from
  owned terminal output and public error messages.
- Returned a classified error instead of panicking when manifest generation is
  invoked on a nil application.
- Made repeated limit and exit-code compile options preserve independent
  earlier overrides.
- Enforced cancellation between lifecycle phases, retained cancellation
  causes, and prevented middleware from continuing an invocation twice.
- Applied dynamic completion providers to shorthand values and assigned long
  option values.
- Made registered digit shorthands reachable without changing negative-number
  parsing on sibling commands.
- Enforced the output byte limit across informational and data payloads and
  retained output-error classification through lifecycle callbacks.
- Rejected command nodes that combine positional arguments with subcommands,
  eliminating ambiguous dispatch grammars before execution.
- Prevented retained middleware continuations from running handlers after
  cleanup or after `Run` returns.
- Added command aliases to static completion and skipped oversized provider
  candidates without discarding later bounded results.
- Retained dynamic completion and protocol writer failures together instead of
  allowing output failure to erase the primary provider error.
- Bounded aggregate command, argument, and option metadata before immutable
  graph publication and generation.
- Rejected option-group combinations whose required or defaulted values make
  the declared constraints impossible to satisfy.
- Validated enum declarations and defaults, published allowed values, and
  included the root command's full contract in machine manifests.
- Completed Markdown references with arguments, status, aliases, examples,
  type constraints, and control-safe metadata.
- Replaced parser-engine diagnostics and causes at the internal boundary with
  stable framework-owned classifications and messages.
- Added idempotent shutdown-controller cleanup so successful process lifetimes
  release their derived cancellation context without waiting for a signal.
- Expanded fuzzing across lifecycle failure and cancellation, help and
  reference generation, JSON and terminal rendering, and adapter translation.
- Split comparative construction from prepared dispatch, made validation and
  JSON output equivalent, and added broad, deep, maximum, failure, suggestion,
  conversion, output, and cancellation benchmarks.
- Completed enum values for separate, assigned, and attached option forms and
  arguments while redacting secret enum sets from every discovery surface.
- Added a built subprocess fixture proving JSON integrity, portable success and
  usage statuses, SIGTERM cancellation status, and signal cleanup at the only
  boundary where `os.Exit` is appropriate.
- Replaced eval-based upstream completion scripts with deterministic owned
  Bash, Zsh, Fish, and PowerShell templates that pass token arrays literally,
  preserve safe descriptions, and undergo native syntax and injection checks.
- Prevented direct handler writes from contaminating JSON stdout or stderr and
  quiet-mode stdout while retaining explicit human-mode streams.
- Validated custom value-type names and time layouts and published time formats
  through metadata, manifests, and Markdown references.

### Added

- Established the module, supported Go range, and internal Cobra engine
  boundary.
- Recorded the initial parser-engine architecture decision and comparison
  evidence.
- Added explicit command construction, ordered immutable compiled metadata,
  aliases, lifecycle-facing metadata, typed argument and option bindings, and
  persistent option declarations.
- Added construction-time rejection for invalid names, command cycles, reused
  nodes, duplicate identities and options, inherited option shadowing, and
  ambiguous positional layouts.
- Added invocation-local parsing through an internal Cobra adapter with fresh
  parser state for every run, explicit caller context and IO, aliases,
  inherited options, combined boolean shorthands, interspersed options, and
  double-dash handling.
- Added typed boolean, string, integer, unsigned integer, floating-point,
  duration, time, enum, string-slice, and key/value options with explicit
  omitted, defaulted, and supplied value states.
- Added deterministic validation, middleware, pre-run, handler, post-run, and
  reverse-order bounded cleanup phases with stable failure classification and
  primary-plus-cleanup error composition.
- Added explicit optional, required, and forbidden interaction capabilities;
  required interaction now fails before lifecycle side effects when a request
  is non-interactive.
- Added typed signed integer, unsigned integer, floating-point, duration, time,
  and enum positional arguments, including negative numeric positional tokens
  without weakening unknown-option handling.
- Added malformed-value classification, secret-safe conversion causes, exact
  cancellation-cause retention, invalid UTF-8 rejection, and bounded argv
  count and cumulative size.
- Added bounded human, quiet, and versioned deterministic JSON output policies,
  terminal-control stripping for diagnostics, stable success and error
  envelopes, and strict stdout/stderr separation.
- Added output-failure classification for encoding errors, writer errors, and
  short writes without erasing an earlier command failure.
- Added deterministic nested plain-text help, Markdown command references, and
  versioned JSON command manifests with argument, local option, inherited
  option, alias, lifecycle metadata, and provenance.
- Added side-effect-free concurrent-safe Bash, Zsh, Fish, and PowerShell
  completion script generation through the internal engine boundary.
- Added successful typed help and version request results, with generated help
  and version content honoring human, quiet, and JSON output policies.
- Added stable unknown-command, unknown-option, and missing-option-value error
  classifications while reserving framework-owned help and version flags.
- Added required options, mutually exclusive option groups, jointly required
  option groups, explicit-empty satisfaction, and default-backed required
  values, all validated before lifecycle side effects.
- Added construction rejection for unregistered group members and reused typed
  argument or option bindings.
- Added the parallel-safe `clitest` harness for isolated argv, context, stdin,
  stdout, stderr, output policy, interaction policy, selected-command, error,
  and exit-status assertions without process-global mutation.
- Added configurable positive construction and argv limits with auditable
  defaults for command depth, command count, option and argument definitions,
  token count, and cumulative token bytes.
- Added a concurrency-safe graceful-then-forced shutdown controller that
  preserves caller context and cancellation causes while leaving operating
  system signal registration and restoration under application ownership.
- Exported explicit argument and option definition contracts and added generic
  custom scalar parsers so application-owned value types remain typed without
  exposing or extending Cobra types.
- Added bounded, cancellation-aware static and explicit dynamic completion
  candidates with safe metadata-only provider requests, deterministic
  deduplication, hostile-description sanitization, and secret-provider
  rejection.
- Added the hidden Cobra-compatible completion protocol boundary used by
  generated shell scripts; it never executes command handlers and always emits
  a bounded directive even when a provider fails.
- Added deterministic owned unknown-command suggestions with bounded candidate,
  token-length, and edit-distance work; hidden commands never appear.
- Added compiled public examples and consumer documentation for construction,
  typed input, parsing, lifecycle, output, errors, shutdown, completion,
  operations, integrations, migrations, security, performance, compatibility,
  troubleshooting, limitations, and release policy.
- Added retained fuzz seeds and smoke targets for arbitrary argv, hostile
  command graphs, typed conversion boundaries, and partial completion input.
- Added construction, dispatch, generation, allocation, and equivalent pinned
  Cobra, `urfave/cli`, Kong, and standard `flag` comparison benchmarks in a
  dependency-isolated nested module with reproducible raw evidence.
- Added configurable portable exit-code policies, width-bounded help, and
  explicit experimental, deprecation, and replacement help metadata.
- Added generated-artifact, dependency-boundary, API-compatibility,
  architecture, coverage, fuzz, mutation, benchmark, SBOM, and reproducible
  build gates behind documented local targets.
- Added minimum/current Go CI, CodeQL, dependency review, scheduled mutation,
  deterministic source archives, CycloneDX SBOMs, Sigstore signatures, and
  GitHub build provenance for tagged releases.
- Preserved custom caller cancellation causes before parsing and corrected
  single-digit negative positional handling after boolean flags.
- Established a 100% mutation-efficacy and 98.5% mutator-coverage release gate
  with explicit reviewed classifications for unexecuted constant mutations.

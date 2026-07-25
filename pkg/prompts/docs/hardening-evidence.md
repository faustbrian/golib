# Hardening evidence

This report maps the mandatory hardening evidence to reproducible source,
test, and command evidence. It records automated guarantees separately from
manual or remote release evidence. Unless stated otherwise, the automated
evidence was refreshed on 2026-07-22 with Go 1.26.5 on macOS 27.0 arm64.

## Release status

| Evidence | Status | Reproduction |
| --- | --- | --- |
| Meaningful production statement coverage | Passed locally | `GOWORK=off make coverage`; every production function and total report 100.0% |
| Formatting, vet, tests, docs, race, static and security checks | Passed locally | `GOWORK=off make check` |
| Retained fuzz and reference-model budgets | Passed locally | `GOWORK=off make fuzz`; budgets are in `specification/fuzz-budgets.tsv` |
| Mutation campaign | Passed locally | `GOWORK=off make mutation`; details are in `docs/mutation.md` |
| Benchmarks and isolated comparisons | Passed locally | `GOWORK=off make benchmark comparison-benchmark comparison-binaries` |
| Clean-clone release gate | Passed locally | `GOWORK=off make release-check` in a separate clone |
| Remote CI for the release commit | Pending | Requires the release commit to be pushed and all protected workflows to pass |
| Manual assistive-technology review | Passed | VoiceOver review completed in iTerm2 3.6.11 and Apple Terminal 2.15; evidence and limitations are in `docs/accessibility-review.md` |

The local and manual passes are not substitutes for pending remote CI. A
release commit must be frozen and the complete local and remote evidence
regenerated before a stable tag.

## Prompt and interaction-policy matrix

| Contract | Interactive evidence | Headless and typed evidence |
| --- | --- | --- |
| Text and multiline editing | `interactive_test.go`, `state_model_fuzz_test.go` | `execution_test.go`, `typed_prompts_test.go`, `policy_test.go` |
| String and byte secrets | `interactive_test.go`, `interactive_secret_bytes_internal_test.go`, `fuzz_test.go` | `secret_test.go`, `validation_test.go` |
| Confirm, integer, decimal, duration, date, time, and path | `interactive_test.go`, `typed_prompts_test.go` | `typed_prompts_test.go`, `execution_test.go` |
| Select, multi-select, search, and pagination | `interactive_select_test.go`, `interaction_model_fuzz_test.go`, `state_model_fuzz_test.go` | `select_test.go`, `execution_test.go` |
| Forms and conditional follow-ups | `form_test.go`, `interaction_model_fuzz_test.go` | `form_test.go`, `execution_test.go` |
| Progress, spinner, tasks, and bounded status | `progress_test.go`, `task_test.go`, `interaction_model_fuzz_test.go` | `progress_test.go`, `presentation_test.go` |
| Notes, messages, tables, and summaries | `presentation_test.go` | `presentation_test.go`, `example_test.go` |

`policy_test.go` uses a reader that fails the test on any forbidden read. The
policy cases cover required, preferred with fallback, non-interactive-only,
and caller-authorized auto detection. Defaults and fallbacks are accepted only
under their explicit policy.

## Terminal acquisition and restoration matrix

| Exit or failure | Evidence | Required invariant |
| --- | --- | --- |
| Submit and validation retry | `interactive_test.go` | Echo restored and controller released |
| Escape, Ctrl-C, Ctrl-D, EOF, and detachment | `interactive_test.go`, `fuzz_test.go` | Prompt terminates with a stable typed error and restores state |
| Reader, writer, and renderer failure | `interactive_test.go` | Primary failure remains classifiable and restoration is attempted |
| Acquire or echo-control failure | `interactive_test.go` | No prompt read continues after control failure |
| Restoration failure with a primary failure | `interactive_test.go` | Both causes remain discoverable without secret content |
| Validator or callback panic | `validation_test.go`, `form_test.go` | Panic becomes a safe adapter error after restoration |
| Real Unix pseudo-terminal | `terminal/adapter_unix_test.go` | Kernel terminal attributes match their pre-run state |
| Linux and macOS implementations | Platform workflows and Unix PTY tests | Supported adapters compile, acquire, restore, and pass failure-path checks |

Unit tests use injected virtual controllers. Unix integration tests use a
controlled pseudo-terminal and never mutate the developer terminal.

## Key, editing, Unicode, and width fixtures

| Fixture | Evidence and claim boundary |
| --- | --- |
| Insert, delete, cursor, home/end, and word movement | Unit cases plus `FuzzLineEditorMatchesReferenceModel` |
| Tab, Shift-Tab, arrows, pages, Enter, Escape, Ctrl-C/D/J | `decoder_test.go`, `interactive_test.go`, `form_test.go`, and selection tests |
| Partial, malformed, unknown, and bracketed-paste bytes | `decoder_test.go` and `FuzzDecoderBoundsArbitraryBytes` |
| Combining marks, variation sequences, and emoji ZWJ clusters | Editing and rendering tests plus the editor model fuzzer |
| East Asian width, wrapping, tiny dimensions, and resize | `render_test.go`, `interactive_select_test.go`, and model fuzzers |
| Invalid UTF-8, NUL, controls, ANSI, OSC, and bidi controls | Decoder, renderer, secret, and sanitizer fuzz tests |

The fixtures prove the documented dependency behavior, not universal terminal
or Unicode conformance. RTL visual ordering, unsupported operating systems,
and emulator-specific escape sequences remain outside the current claim.

## Secret canary non-disclosure report

Unique canaries are checked across formatting, Go formatting, JSON, text
marshaling, structured logging, prompt output, validation and panic errors,
forms, event formatting, and terminal failures. `FuzzSecretEntryFailureNeverDiscloses`
adds submission, cancellation, EOF, detachment, and invalid-resize sequences
while asserting echo restoration and terminal release. Byte-secret tests also
exercise owned-buffer destruction.

These checks prove absence from captured package-owned channels. They do not
claim erasure of Go strings, caller copies, swap, crash dumps, terminal
history, or hardware. Those limitations are documented in `docs/secrets.md`.

## Renderer and ANSI security report

`render_test.go`, `presentation_test.go`, and `fuzz_test.go` verify semantic
roles in plain and ANSI profiles, color-independent markers, deterministic
wrapping, hostile control neutralization, capability-gated hyperlinks, and
fixed-capability determinism. Semantic assertions are authoritative; ANSI
snapshots are supplemental. Renderer and writer failures are classified and
do not bypass terminal cleanup.

## Race, leak, cancellation, provider, and progress stress

`GOWORK=off make race concurrency` runs the complete race suite and repeats
progress, dynamic-provider, status-stream, and task-group concurrency cases 20
times. Virtual clocks avoid real sleeps. Production progress values retain one
latest snapshot without worker goroutines or update queues; providers use
caller contexts and generation checks. Cancellation, stale generation,
overflow, regression, post-completion mutation, and slow or failed output are
covered by focused tests.

## Fuzz, model, and mutation evidence

`specification/fuzz-budgets.tsv` is the executable inventory for raw decoder,
normalized interaction, rendering, search, secret, form, provider, progress,
editor, and selection fuzzers. Source seeds and retained regression corpus
files live beside the fuzz targets and under `testdata/fuzz`. Independent
models cover editor cells and cursor, form navigation and conditional
visibility, selection focus/filter/page/selection, provider generations, and
progress lifecycle. Mutation results and exclusions are recorded in
`docs/mutation.md`.

## Comparative performance and raw data

The isolated comparison uses an identical `Ada` plus Enter byte stream, an 80
by 24 pseudo-terminal, the same answer assertion, and the same full setup and
cleanup boundary for prompts, Huh, Survey, PromptUI, and direct Bubble
Tea/Bubbles. Visual output differs, so the results are observations rather
than speedup claims. Raw three-sample data is retained in
`specification/benchmark-comparison-2026-07-22.tsv`; stripped minimum-import
binary sizes are in `specification/binary-size-comparison-2026-07-22.tsv`.
`docs/benchmarks.md` records the method, summaries, and limitations.

## API, dependency, and supply-chain evidence

`GOWORK=off make api` enforces `specification/api-v0.txt`. Architecture tests
reject ambient OS access, upstream prompt-engine types, initialization side
effects, production panic, and unowned core goroutines. `go.mod` keeps the core
independent of Huh and Bubble Tea; comparisons live in a nested module.
`make vulnerability license secret-scan sbom workflow-lint reproducible`
checks advisories, licenses, repository secrets, deterministic CycloneDX
generation, immutable workflow pins, and reproducible Git-object archives.
Release signing and provenance remain workflow-owned remote evidence.

## Documentation execution and compatibility

`GOWORK=off make docs` checks required documents and executes every Go example.
`README.md` links the adoption surface; `docs/compatibility.md` records OS,
terminal, and Go claim boundaries; `docs/dependency-evaluation.md` pins engine
evidence; migration and explicit non-interactive examples avoid mandatory
prompting. The exact exported API baseline detects unreviewed compatibility
changes.

## Open release findings

| Finding | Owner | Severity | Rationale and follow-up |
| --- | --- | --- | --- |
| Assistive-technology matrix is incomplete | Release maintainer | Release blocker | Automated semantic checks cannot establish announcement order or usability. Complete every pending row in `docs/accessibility-review.md`. |
| Current local commits have no protected remote CI result | Release operator | Release blocker | Local evidence cannot prove hosted OS, CodeQL, dependency review, signing, or provenance workflows. Push an approved branch or release commit and require every workflow to pass. |

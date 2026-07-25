# Progress and presentation

`Progress` stores one latest snapshot. `Update` and `Increment` are concurrent,
bounded state mutations that never write output, enqueue work, create a timer,
or start a goroutine. The caller invokes `Render` at its chosen cadence. This
coalesces excessive updates naturally and keeps a slow writer away from the
underlying operation unless the caller explicitly renders on that path.
`UpdateContext` and `IncrementContext` reject an ended caller context before
mutation and are the preferred variants inside cancellable operations.

Determinate progress rejects negative values, overflow, values above the
total, regression unless explicitly permitted, and updates after a terminal
state. A zero total means indeterminate progress and omits percentages.
`Complete`, `Fail`, and `Cancel` are stable idempotent terminal transitions.
Redirected-style and no-color output is a deterministic textual line; ANSI is
used only when the caller supplies a color capability.

An optional explicit `ProgressConfig.Clock` enables average rate and estimated
remaining duration snapshots. The first update establishes the measurement
point, so it never invents startup time. A forward update after positive
elapsed time exposes `RatePerSecond`; a known positive total also exposes a
bounded `EstimatedRemaining`. Unknown totals omit ETA. Zero elapsed time,
unchanged values, regression, negative duration, and duration overflow clear
or omit estimates instead of returning NaN, infinity, or a wrapped duration.
Regression starts a new measurement window. Rendering includes rate and ETA
textually when present.

`Spinner` follows the same model. `Advance` is caller-driven and merely selects
the next copied frame. Rendering omits decorative frames unless the caller
explicitly reports animation support, so reduced-motion and redirected output
remain understandable. It has no implicit ticker or cleanup goroutine.
`AdvanceContext` provides the corresponding cancellation-aware mutation.

`StatusStream` is a concurrent fixed-capacity oldest-drop ring. Its snapshot is
append ordered, reports the exact omitted count, and renders stable line output.
Unknown status kinds are rejected. `AppendContext` rejects an ended context
without retaining an entry.

`TaskGroup` owns presentation order and explicit parent identity for bounded
nested tasks. A parent must already exist before a child is added, preventing
cycles and making ordering deterministic. Each returned `Task` is a concurrent
progress handle. The group only snapshots and renders caller-owned state; it
does not start, schedule, cancel, retry, or recover application work.

`WriteMessage` presents informational notes, warnings, errors, and success
messages with textual semantic markers. Multiline bodies are rendered as
linear lines. `WriteTable` validates a rectangular bounded model and aligns
cells by Unicode display width before terminal-width wrapping. `WriteSummary`
preserves caller declaration order. All caller content passes through the same
terminal-control and bidi sanitization as prompt rendering.

Current limitations:

- Interactive in-place erase behavior is not implemented; every render leaves
  a stable line by design.

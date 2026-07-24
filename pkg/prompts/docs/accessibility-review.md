# Accessibility review evidence

## Automated evidence

The 2026-07-22 automated review ran on macOS 27.0 with iTerm2 3.6.11,
`TERM=xterm-256color`, and a true-color profile. Tests use semantic frames,
fixed virtual terminals, and controlled Unix pseudo-terminals rather than the
developer terminal. They verify:

- visible textual focus, selected, disabled, error, warning, success, hint,
  progress, and reduced-motion states;
- caller-localized accessible label and description overrides plus a complete
  textual hint;
- keyboard-only editing, selection, search, pagination, and cancellation;
- no-color linear output without cursor movement;
- combining-mark and emoji grapheme editing, East Asian display width, narrow
  dimensions, resize, and terminal-control neutralization; and
- raw-mode and echo restoration on byte-secret submit, cancellation, and
  writer failure.

This is automated behavioral evidence, not a screen-reader usability claim.

## Manual review protocol

The matrix below records the manual review required for the first stable tag.
Run the build-checked review application from the module root in each terminal:

```sh
GOWORK=off go run ./scripts/accessibility-review.go
```

It exercises text, secret, single-select, multi-select, search, validation
retry, progress, warning, narrow-width rendering, and cancellation flows using
only the keyboard. Verify announcement order, focus visibility, error recovery,
secret non-disclosure, reduced motion, no-color meaning, resize behavior, and
terminal restoration.

In the multi-select step, arrows move focus and Space toggles the textual `[x]`
state. Enter submits only after exactly two choices are selected. An invalid
submission announces an explicit corrective message and remains retryable.

| Terminal and assistive technology | Status | Evidence and limitations |
| --- | --- | --- |
| iTerm2 3.6.11 with macOS VoiceOver build 993 | Passed 2026-07-22 | Brian Faust completed the keyboard-only flow on macOS 27.0 build 26A5388g and reported that announcements seemed useful, with no observed issue |
| Apple Terminal 2.15 with macOS VoiceOver build 993 | Passed 2026-07-22 | Brian Faust completed the keyboard-only flow on macOS 27.0 build 26A5388g and reported that announcements were useful, with no observed issue |

Both flows covered validation recovery, public and secret text, focused,
selected, and disabled options, a localized multi-select error, narrow-width
search, cancellation, progress, and final status. Secret values were not
rendered. These were subjective manual passes without announcement recordings;
the iTerm2 wording “seemed useful” is retained as the reviewer's confidence
boundary.

Record the exact operating system, terminal, and assistive-technology versions,
date, reviewer, scenarios, failures, and accepted limitations. Do not replace
manual observations with ANSI snapshots or pseudo-terminal output.

## Current claim boundary

Linear output is designed and automatically verified to preserve semantic
meaning without color, animation, or cursor movement. Manual observations
support usable announcements only for the exact macOS, terminal, and VoiceOver
versions recorded above. They do not establish complete announcement timing,
bidi visual ordering, or behavior in other versions or environments.

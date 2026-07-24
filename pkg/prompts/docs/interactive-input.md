# Interactive input

Terminal detection does not authorize interaction. `Run` enters interactive
mode only when `InteractionPolicy` permits it and the caller-supplied
capabilities satisfy the selected mode. It then requires three explicit
resources:

- an `EventSource` that returns decoded semantic events and honors context
  cancellation;
- a `TerminalController` that acquires, configures echo, and releases terminal
  state; and
- an output writer.

The core does not inspect the environment, decode `os.Stdin`, choose process
streams, or start a goroutine around a blocking reader. Application adapters
must perform platform-specific byte decoding and terminal control. This keeps
headless execution unable to read accidentally and makes cancellation an
enforced event-source contract.

`Decoder` is the optional bounded byte-to-event layer for those adapters. It
incrementally decodes UTF-8 text, Enter, Ctrl-J newline, Tab, Backspace,
Ctrl-C, Ctrl-D, common CSI arrows, Home, End, Delete, Page Up, Page Down,
Shift-Tab, Ctrl-word
movement, and bracketed paste. Partial UTF-8 and escape sequences remain
bounded until the next chunk. `Flush` distinguishes a lone Escape from a
truncated sequence when a stream ends or reaches its configured inter-byte
timeout. Unsupported sequences, controls, bidi controls, invalid UTF-8,
truncated paste, and limit violations return a safe reader error and reset
decoder state. The decoder does not read a stream, choose a terminal, or claim
support for every terminal emulator's private sequences.

The editor supports grapheme-cluster insertion and deletion, left and
right movement, home and end, word movement, semantic paste, Enter, Escape,
Ctrl-C, Ctrl-D, resize, EOF, and terminal detachment. Tab, Shift-Tab, arrows,
and page keys are accepted as navigation no-ops until a form or selection owns
their meaning. Input and individual paste sizes are bounded. Invalid UTF-8,
unsupported controls, invalid events, and limit violations return a typed
reader failure without including input in the error.

For multiline prompts, Ctrl-J inserts a newline and Enter submits. The owned
decoder distinguishes the raw LF byte from the CR byte emitted by Enter in the
supported raw-terminal path. Bracketed paste may also insert multiple lines.
Single-line prompts ignore the newline key and still reject pasted line breaks.

Rendering is linear and semantic. Every frame retains textual label, hint,
help, validation, and secret-entry state, so color and cursor movement are not
required to understand the interaction. ANSI styling is capability-driven;
the plain renderer remains deterministic under redirected-style output.
Kernel echo remains disabled for both public and secret input while raw mode
is active. The owned semantic renderer displays public input, while secret
input is represented only by its redacted state. Terminal release restores
the state captured before acquisition.
An event source may emit `CapabilityEvent` with a complete replacement
snapshot after resize, attachment, or terminal-profile changes. Interactive
rendering immediately applies width, height, color, and Unicode fallback
changes. Loss of either input or output terminal capability terminates with a
stable terminal-detachment error and restores acquired state.

`VirtualTerminal` supplies events, terminal lifecycle operations, fixed
dimensions, failure injection, and output capture without real sleeps or
developer-terminal mutation. Its queue is bounded to 4096 events and returns a
typed definition error when a test attempts to exceed that harness limit.

`VirtualClock` supplies fixed time, owned timers, coalescing tickers, explicit
advancement, and deterministic stop behavior. Positive durations never use a
real sleep or goroutine. Non-positive timers fire immediately; non-positive
tickers are returned stopped because the `Ticker` interface has no error
return.

Selection prompts assign Up, Down, Home, End, Page Up, Page Down, Tab, and
Shift-Tab to focus navigation. Disabled options remain visible and are skipped.
Space toggles multi-selection, with textual bounds feedback. Search prompts use
the same bounded deterministic ranking as `Search`; editing the query cannot
replace selected identities or reorder multi-select results. Pagination is
derived from caller-supplied height and keeps the focused option visible after
resize.

`NewKeyMap` creates an immutable execution-local remap for non-text keys. Each
binding maps an input key to an existing semantic meaning. Rebinding removes
every prior key for that meaning, so changing submission from Enter to Tab does
not leave Enter as an undisclosed alternate. A zero `Execution.Keys` uses the
documented defaults. `KeyRune` is deliberately not remappable because rune
events carry caller text; Space remains the multi-select toggle only within a
multi-select interaction.

The optional caller-constructed raw file adapter is documented in
[Terminal adapter](terminal-adapter.md). Applications with an existing event
reactor may continue to supply the interfaces directly.

Current limitations:

- Editing uses Unicode grapheme boundaries from `uniseg`; executable tests
  cover combining marks and emoji, but do not claim complete bidi or terminal
  width behavior.
- Rendering emits deterministic linear frames rather than cursor-updating an
  existing frame.
- Byte-native paste requires a dedicated decoder configuration; ordinary
  string paste events retain Go's immutable-memory limitation.
- Dynamic provider sessions are caller-driven and are not mutated into a
  running immutable prompt.

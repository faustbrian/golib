# Accessibility

Every essential role has a textual marker. Focus uses `>`, selection uses
`[x]`, disabled options say `[disabled]`, and errors, warnings, successes,
hints, and progress have words in plain output. Color and animation are never
required to understand state.

The linear renderer does not require cursor movement and is the screen-reader
fallback. Keyboard events cover scalar editing and all selection operations.
Caller-localized metadata and messages are rendered as complete strings; the
core does not concatenate translated sentence fragments. A non-empty
`Accessibility.Label` or `Accessibility.Description` replaces its visual
counterpart in the authoritative linear frame, and `TextualHint` adds a
complete help line.

Automated tests cover textual state, control sanitization, combining marks,
emoji clusters, East Asian width calculations, tiny dimensions, no color,
reduced motion, and keyboard-only selection. Manual review in named terminal
versions is still required before a stable accessibility claim. Bidi layout,
screen-reader announcement timing, and terminal-specific behavior outside the
supported matrix are not yet claimed.

The automated evidence, named terminal matrix, and manual procedure are in
[Accessibility review evidence](accessibility-review.md).

# Semantic rendering

`Frame`, `SemanticLine`, and `Segment` are the authoritative screen model.
Tests should assert roles and content at this layer before optionally checking
rendered terminal bytes. Constructors and accessors deeply copy their slices,
so a captured frame cannot be changed through caller aliases.

Roles cover labels, values, hints, help, errors, success, warnings, focus,
selection, disabled state, and progress. `Theme` maps roles to value-only
styles and localized textual markers. `With` and `WithMarker` return new
themes; they do not mutate shared state. Plain and no-color output always keeps
the textual marker, so color is never the only state signal.

`PlainRenderer` never emits terminal controls. `ANSIRenderer` emits only owned
SGR sequences supported by the explicit color profile and resets styling at
every line boundary. True color is deterministically downsampled for 256-color
and 16-color profiles. A no-color profile uses the plain renderer path.

When `RenderOptions.ASCIIOnly` is set, every non-ASCII code point is rendered
as visible `\\u{HEX}` text. Core execution selects this deterministic fallback
when caller-supplied capabilities do not permit Unicode. Values remain
unchanged; only presentation falls back. Table measurement applies the same
policy before alignment.

`Hyperlink` creates semantic links only for absolute HTTP, HTTPS, and mailto
targets without credentials, controls, or bidi controls. `ANSIRenderer` emits
owned OSC 8 sequences only when `RenderOptions.Hyperlinks` is true. Plain or
unsupported output renders `label (target)`, so the destination remains
available without terminal controls. Link state is closed before wrapping and
at every line boundary.

All caller text and textual markers pass through `Sanitize`. C0, C1, DEL,
carriage return, ANSI/OSC introducers, and bidi control characters become
visible `\\u{HEX}` text. Invalid UTF-8 becomes the Unicode replacement
character. Line feed remains a semantic line boundary. The renderer never
executes control text from labels, descriptions, options, validation messages,
or themes.

Wrapping uses `rivo/uniseg v0.4.7` grapheme segmentation and display width.
Combining sequences, tested emoji ZWJ sequences, and East Asian wide characters
are not split. A single grapheme wider than the available width is emitted on
its own line rather than corrupted. This is deterministic library behavior,
not a claim that every terminal agrees on the width of every evolving Unicode
sequence. Terminal-specific width disagreements and bidi visual ordering remain
documented limitations; bidi controls are neutralized rather than interpreted.

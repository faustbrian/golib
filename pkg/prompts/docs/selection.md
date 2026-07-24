# Selection and search

`Option` keeps its stable identity separate from label, description, group,
value, and disabled state. Identities must be unique within a prompt. Duplicate
labels and duplicate values are allowed because identity remains unambiguous.
Caller option slices are copied when a definition is built. Arbitrary generic
values cannot be deeply copied by Go; callers must treat reference-like option
values as immutable while a definition is reusable.

Single-selection explicit input is an option identity, never a display label.
Disabled and unknown identities are rejected. Defaults, fallbacks, and initial
focus also name identities and cannot target disabled options.

Multi-selection explicit input is a comma-separated identity list. Duplicate,
disabled, unknown, below-minimum, and above-maximum selections are rejected.
Results always follow option declaration order, independent of selection event
order. Returned slices and stored default or fallback slices are copied so a
caller cannot mutate a reusable definition through a prior result.
Known, enabled identities are resolved before the validation pipeline. Caller
post-validators therefore receive the typed selection even when its count is
outside the configured bounds, allowing a localized corrective message. The
built-in minimum and maximum check runs afterward if no earlier validator
rejects the value. Defaults, fallbacks, and initial selections must satisfy the
bounds when the immutable definition is constructed.

Static `Search` is bounded by option count, result count, and query rune count.
Text is normalized with Unicode NFKC and Unicode case folding; accents are not
removed. Ranking is deterministic:

1. exact normalized label;
2. normalized label prefix;
3. every query token prefixes a label or description token;
4. every query token occurs within a label or description token; and
5. empty query in declaration order.

Ties preserve declaration order. Search is locale-independent and does not
perform language-specific stemming, transliteration, accent removal, or fuzzy
edit-distance matching.

Interactive single selection uses stable option identity internally while
rendering labels, descriptions, groups, disabled state, and textual focus.
Navigation skips disabled options and wraps deterministically. Pagination is
bounded by caller-supplied terminal height and recomputed on resize without
changing identity or selection.

Interactive multi-selection toggles with Space, enforces maximum bounds before
mutation, validates minimum bounds on submission, and returns declaration-order
values. Interactive search edits a bounded query and applies the same ranking
and tie-breaking rules above.

## Dynamic options

`DynamicOptions[T]` is an explicit, caller-driven provider session. Schedule a
complete query, wait until the configured debounce deadline using the same
caller-owned clock, then call `Resolve` with the returned generation. Resolve
returns `applied=false` without calling the provider before the deadline.

Provider callbacks receive a context and must honor cancellation. Returned
option count, identities, and labels are validated before replacement. A newer
scheduled generation supersedes an in-flight older generation; its late result
is discarded under the session lock. Snapshots and resolved slices are copied.
The session uses no timers or goroutines, so an event adapter or application
event loop owns when resolution work runs.

Dynamic sessions deliberately do not mutate an immutable prompt definition.
Applications may turn a current snapshot into a new `SearchSelectConfig`, or a
terminal adapter may compose the session into its own event loop. This keeps
provider latency and goroutine ownership outside the core prompt executor.

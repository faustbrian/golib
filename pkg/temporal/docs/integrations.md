# Calendar, clock, validation, config, and wire integration

`temporal` does not own a clock. Obtain current instants and timers from
`clock`, then pass concrete `time.Time` values into `instant`.

Civil arithmetic and DST resolution belong to `calendar`. Date-to-instant,
local-time application, daily-interval conversion, and civil snapping require
both `*time.Location` and a timezone resolution policy. This prevents
accidental dependence on the process-local timezone or the standard library's
implicit gap/fold choice.

`temporalvalidation` returns immutable `validation` reports with stable
codes: `temporal_empty`, `time_of_day_range`, and `fixed_duration_range`.

`temporalconfig` wrappers implement atomic text decoding recognized by
`config`. Keep the wrapper in configuration structs and call `Value()` after
successful loading.

`temporalwire.Document` and `CollectionDocument` are format-neutral and carry
JSON/YAML/TOML tags. They may be passed through the corresponding `wire`
encoder. Their JSON helpers are strict, bounded conveniences for consumers
that need no broader wire stack.

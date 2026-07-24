# Types and coercion

The engine never converts strings to numbers, integers to floats, timestamps
to strings, empty values to false, or null to missing. Integrations must
normalize data before constructing facts.

Integers are signed 64-bit values. Floats must be finite; NaN and infinities
are rejected recursively. Times compare as instants after monotonic clock data
is removed. Durations compare as nanoseconds. Strings compare by Go's stable
byte ordering over valid UTF-8. Lists use ordered structural equality.

Use explicit domain adapters for decimals and measurements rather than floats.
An adapter should encode the domain value canonically and register an operator
with exact signatures; it must not add implicit core coercion.

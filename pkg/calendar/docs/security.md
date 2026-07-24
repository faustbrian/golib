# Security

## Threat model

- Malicious or huge date, JSON, config, zone, holiday, and metadata input.
- Integer and supported-year overflow during negative or compound arithmetic.
- DST gap/fold misresolution and timezone database drift.
- Mutable caller maps/slices changing concurrent decisions.
- Infinite iteration through closed business calendars.
- PostgreSQL NULL/infinity reinterpretation.
- Unicode lookalikes in canonical values and unbounded telemetry labels.

## Controls

Canonical parsing is ASCII and length bounded. Arithmetic validates before
addition and uses Gregorian ordinals instead of duration subtraction. Timezone
names and conversion work are bounded. Business inputs are deep-copied and
validated; iterative work requires a caller limit. Infinity uses a distinct
type. Fuzz, race, mutation, vulnerability, and live persistence gates run
locally through `make check`.

Holiday names and metadata are display/application data. Do not use them as
unbounded metric dimensions; emit stable revision/provider identifiers instead.

# Contexts and rounding

Contexts are immutable arithmetic policies. They do not own or edit ISO
metadata.

- `DefaultContext` captures the authoritative minor-unit exponent and binds it
  to the currency used to derive it.
- `CustomContext` uses an application-selected scale from 0 through 18.
- `CashContext` adds a positive integer step at its scale; scale 2 and step 5
  means increments of 0.05.
- `AutomaticContext` preserves input scale. For rational results it succeeds
  only when the exact decimal terminates within 18 places.

Fixed construction rejects represented scale beyond the context, including
extra trailing zeroes. This prevents scale differences from disappearing
silently.

Explicit rational boundaries accept every `math` rounding mode: half-even,
half-up, half-down, toward zero, away from zero, ceiling, and floor. The returned
`RoundingResult` exposes `rounded` and `inexact` conditions. Cash rounding first
expresses the exact value in cash-step units, rounds that count, then converts
back to the configured scale.

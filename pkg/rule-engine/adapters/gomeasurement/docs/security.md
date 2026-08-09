# Security

The adapter treats every tagged value as untrusted. It checks the encoded size,
version, field count, strict decimal grammar, and canonical unit identity before
conversion. Unknown unit text and supplied amounts are not copied into adapter
diagnostics. Owned sentinel causes remain available through `errors.Is`.

Exact conversion inherits the measurement and math packages' allocation,
coefficient, exponent, expansion, and intermediate-work limits. There is no
float fallback, reflection, network or filesystem access, environment lookup,
global registry, background goroutine, or mutable package state.

Callers remain responsible for rule-engine limits, deadlines, storage access,
authorization, and deciding which facts may participate in a rule. A tagged
string proves only that its syntax and unit identity are valid; it is not an
authentication or integrity mechanism.

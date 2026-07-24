# Security

Trust boundaries include decimal text, unit aliases, JSON/XML/SQL payloads,
package counts, conversion scales, and caller-supplied formula constants.
Attackers may attempt huge coefficients, exponents, documents, diagnostic
strings, or division work.

The package delegates coefficient and exponent limits to `math`, bounds
text and direct JSON, bounds alias length and profile cardinality, validates
all units and dimensions before arithmetic, and rejects invalid conversion
contexts. The
`measurementwire` adapter adds streaming byte limits. No mutable global cache,
ambient locale, unsafe code, cgo, filesystem, network, or randomness is used.

Use tighter `math` limits for public APIs. Reject nonpositive dimensions,
truck widths, stacking factors, divisors, and indexes. Treat unit profiles and
carrier divisors as versioned configuration. Log error classes and field names,
not complete hostile payloads. Use `measurementwire` rather than raw XML
decoding at untrusted boundaries.

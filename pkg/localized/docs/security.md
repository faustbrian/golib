# Security model

## Threats and controls

| Threat | Control |
|---|---|
| Invalid UTF-8 or repair drift | reject before decoding or construction |
| Canonical duplicate smuggling | token-level/object or entry-level detection |
| Locale enumeration | no global registry API; observers omit tags |
| Private-use leakage | content-free errors/events; opt-in rejection policy |
| Fallback cycles/amplification | construction-time cycle, depth, and count limits |
| Oversized text/parser input | `Limits`, `MaxInputBytes`, HTTP and wire bounds |
| Mutable aliasing | copy all caller containers and database byte input |
| Content exposure | package errors never include localized strings |
| Observer failure | recover observer panics after resolution |
| Supply chain drift | pinned modules, checksums, Dependabot, vulnerability gate |

## Default budgets

| Resource | Default |
|---|---:|
| Locales per value | 128 |
| Canonical tag bytes | 255 |
| Bytes per text | 1 MiB |
| Total text bytes | 8 MiB |
| Canonical JSON input | 9 MiB |
| HTTP header bytes | 8 KiB |
| HTTP/match preferences | 64 |
| Fallback graph | caller-required depth and candidate limits |

Explicit limits are part of the operation, not mutable globals. Negative limits
are rejected. Zero selects documented defaults where an options type says so.

## Unicode

Text is valid UTF-8 but is not normalized by default. NFC, NFD, NFKC, and NFKD
are explicit persistent transforms. The package does not claim grapheme limits,
visual equivalence, confusable detection, HTML safety, language identity, or
semantic equivalence. Output contexts remain responsible for escaping.

Production-source gates reject unsafe, cgo, `go:linkname`, and package-level
variables. Race tests cover shared values, plans, codecs, and observers.

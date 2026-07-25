# Provider and interoperability matrix

| Scheme or provider | Status | Evidence |
| --- | --- | --- |
| Generic `v1` HMAC-SHA-256 | Supported | Go tests plus independent Python vectors |
| Generic `v1` HMAC-SHA-512 | Supported | Go tests plus independent Python vectors |
| Vendor presets | Not supported | No authoritative preset fixtures committed |

No vendor name should appear in a supported matrix until its authoritative
specification, timestamp and body rules, rotation behavior, positive vectors,
negative mutations, and source links are isolated in a provider package.
Generic compatibility does not imply provider compatibility.

Run `make interoperability`. The Python checker reconstructs canonical bytes
without importing this Go module, verifies the expected canonical base64url,
body digest, and signatures, and fails on drift.

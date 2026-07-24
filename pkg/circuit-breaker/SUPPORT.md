# Support and compatibility

Use GitHub issues for reproducible defects and focused feature proposals. Use
GitHub discussions, when enabled, for adoption questions. Security reports must
follow [SECURITY.md](SECURITY.md).

The module follows Semantic Versioning. Go 1.24 is the minimum supported
toolchain for v1. The latest two stable Go releases are tested when practical.
Exported APIs, defaults, transition behavior, timing, classification, snapshot
fields, and sentinel identity are compatibility surfaces. Deprecation precedes
removal except for urgent security/correctness issues.

Bug reports should include module/Go versions, configuration without secrets,
the exact state sequence, a minimal reproducer, and race-detector output where
concurrency is involved. This open source project provides no guaranteed
response time or production support SLA.

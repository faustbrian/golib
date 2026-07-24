# Codec dependencies

The standard library remains the implementation for JSON and XML. SOAP is
built on `encoding/xml`. The five additional formats use focused codecs rather
than incomplete local parsers.

| Format | Pinned module | Why it is used | Maintenance and security posture |
| --- | --- | --- | --- |
| YAML | `go.yaml.in/yaml/v4 v4.0.0-rc.6` | Official YAML organization implementation with duplicate-key handling, multi-document loading, and alias/depth limit plugins | v4 is the actively maintained line; v1-v3 are frozen except for security fixes. The release-candidate pin is a residual API risk tracked through Dependabot and the quality gate. `wire` adds explicit block-scalar indentation because rc.6 can emit tab-leading multiline scalars that its scanner rejects when indentation is implicit. |
| TOML | `github.com/BurntSushi/toml v1.6.0` | Mature TOML parser/encoder with metadata for unknown fields and native datetime/numeric conversion | Stable tagged module with a small dependency surface. It documents TOML 1.1 compatibility; wire's contract remains the tested subset described in the format matrix. Duplicate keys and malformed trailing text are rejected by its parser. |
| MessagePack | `github.com/vmihailenco/msgpack/v5 v5.4.1` | Mature v5 codec with timestamps, extension support, sorted maps, and stream APIs | Widely deployed stable major version, but older than the other binary codecs and without native structural limit options. `wire` adds bounded structural preflight and recursive numeric-fit validation because collection traversal and narrow integer methods otherwise rely directly on encoded structure and Go casts. The shamaton alternative was rejected because its v3 decoder has a published unfixed panic vulnerability. |
| CBOR | `github.com/fxamacker/cbor/v2 v2.9.2` | Fuzzed RFC 8949 implementation with immutable encode/decode modes and explicit duplicate, tag, deterministic, and resource options | Active tagged releases, extensive fuzzing, and published security assessment history. `github.com/x448/float16` is its sole runtime helper. |
| BSON | `go.mongodb.org/mongo-driver/v2 v2.8.0` | Official MongoDB BSON implementation and canonical ObjectID, datetime, raw document, and registry types | Actively maintained official stable driver. Only the BSON package is imported; network/client behavior is not part of `wire`. |

All versions are pinned in `go.mod` and checksummed in `go.sum`. Dependabot,
`govulncheck`, scheduled fuzzing, and the release gate monitor changes. A local
parser would materially increase grammar, allocation, recursion, extension,
and interoperability risk while duplicating mature conformance work.

Dependency behavior is not accepted blindly. Each package selects explicit
safe modes, wraps errors in `wire.Error`, bounds input before parsing, rejects
trailing objects, and documents residual limitations. Upgrading a codec can
change accepted input or emitted bytes and therefore requires regression,
fixture, fuzz, benchmark, documentation, and SemVer review.

The 2026-07-14 audit ran `go list -m -u -json all`: every direct dependency was
at its latest reported version. Older indirect versions shown in the module
graph are selected by dependencies and are not imported by production package
code; `govulncheck ./...` reported no reachable vulnerabilities. This is a
point-in-time result, not a permanent safety claim.

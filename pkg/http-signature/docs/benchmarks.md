# Benchmarks

Run `make benchmark` on an otherwise idle machine. The command reports the Go
toolchain, operation latency, throughput where byte size is known, and
allocations. Record CPU model, operating system, Go version, corpus, command,
sample count, and `benchstat` comparison when publishing results.

The maintained benchmark corpus covers canonical base construction,
HMAC-SHA-256 sign-and-verify, and SHA-256 over one MiB of content. Comparisons
with another implementation must use identical covered components, key type,
body bytes, and validation policy; parser-only and full-policy operations are
not equivalent.

`make benchmark` also runs the isolated
[`benchmarks/comparison`](../benchmarks/comparison/README.md) module. It compares
end-to-end request signing, field serialization and parsing, and verification
against pinned `yaronf/httpsign` using HMAC-SHA-256 and the same covered
components. The local operation retains its mandatory application-profile and
bounded resolver/provider checks, so the report documents that additional work
instead of presenting the candidates as identical internals.

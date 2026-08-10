# HTTP Message Signatures comparison benchmarks

This non-releasable module isolates the comparison dependency from the public
HTTP Message Signatures module. It compares an equivalent HMAC-SHA256 request
operation with the maintained `github.com/yaronf/httpsign` implementation at
commit `de382d35c1add89cc09b9355161d61471fb7f632`.

Both candidates cover `@method`, `@authority`, and `content-type`, include
`created`, `keyid`, and `alg`, serialize `Signature-Input` and `Signature`, and
verify those fields on the same request. The local case additionally executes
its explicit bounded key-provider/resolver and immutable application-profile
checks. These policy costs are part of its public operation and are not removed
to make the timing look more favorable.

Correctness is tested separately from timing. Run repeated samples and retain
the environment and raw results:

```sh
make test
make benchmark BENCH_TIME=1s BENCH_COUNT=10
```

Record the Go version, dependency versions, GOOS/GOARCH, CPU, `GOMAXPROCS`, GC
settings, concurrent load, and `benchstat` intervals. The benchmark has no
wall-clock threshold because shared CI and developer hardware do not provide a
stable regression budget.

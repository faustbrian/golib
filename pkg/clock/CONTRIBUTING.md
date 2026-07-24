# Contributing

Use Go 1.26.5 or newer within the Go 1.26 release line. Keep the module focused
on clock capabilities; calendar arithmetic, scheduling, and distributed-time
protocols are out of scope.

Behavior changes should follow a red-green-refactor cycle and include boundary,
ownership, and concurrency assertions. Run:

```sh
make install-tools
make check staticcheck lint nilaway vuln benchmark mutation
```

Do not add package-global clocks, hidden background goroutines, runtime
patching, cgo, `unsafe`, or callback payloads in observations. Public API
changes require an intentional `api/v1.txt` update and SemVer review.

Commits use Conventional Commits with a body explaining why the change is
needed. By contributing, you agree that your work is licensed under MIT.

# Third-party licenses

The production package uses only the Go standard library. Test and benchmark
dependencies are:

- `golang.org/x/sync` v0.22.0 - BSD-3-Clause License;
- `github.com/v8fg/kit4go` v0.9.0 - MIT License.

The module graph and checksums in `go.mod` and `go.sum` are authoritative for
exact versions and transitive module requirements. Release gates regenerate
the compiled dependency license inventory and software bill of materials.

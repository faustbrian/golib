# Third-party licenses

Production permit accounting uses `golang.org/x/sync` under the BSD-3-Clause
license. Test dependencies include Failsafe-Go under MIT and goleak under
Apache-2.0. The non-releasable comparison benchmark module additionally uses
Fortify under MIT.

Exact versions and transitive dependencies are recorded in `go.mod` and
`go.sum`. Distributors should reproduce the dependency license inventory for
the version they ship.

# Explicit discovery

`discover.Search` only examines paths supplied through `Explicit`,
`Directories`, `StartDir`, or an explicitly enabled user-config directory. It
does not traverse parents or home directories by default and never reads file
contents.

`SearchPlaces` is an ordered list of safe filenames, not paths. `SearchFirst`
stops at the first regular file; `SearchAll` preserves explicit directory and
place order. Candidate, result, and upward-depth limits are always enforced.

Upward search requires `Upward: true`, an explicit `StartDir`, and an explicit
ancestor `StopDir`. A `Root` lexically contains candidates and also contains
resolved symlink targets. Symlinks are rejected by default; `AllowWithinRoot`
permits only targets still inside the resolved root. `OwnerOnly` rejects files
with group or other permission bits.

Containment compares canonical path components exactly. This fails closed for
case-distinct directories on Windows volumes with per-directory case
sensitivity, but can also reject an alternate-case absolute spelling on an
ordinary case-insensitive volume. Use paths returned by the operating system
or relative paths beneath `Root` instead of changing path casing. Upward stop
directories and trusted-root termination use canonical or filesystem identity,
so alternate spellings of the same directory never permit traversal above the
configured boundary.

On Windows, the default rejection policy treats every reparse point as
link-like, including directory junctions and mount points that Go reports as
irregular files rather than symbolic links. `AllowWithinRoot` remains explicit
and still requires the resolved target to stay inside the exact canonical
root.

```go
results, err := discover.Search(ctx, discover.Options{
	Root:         repositoryRoot,
	StartDir:     workingDirectory,
	StopDir:      repositoryRoot,
	Upward:       true,
	SearchPlaces: []string{"app.yaml", "app.json", "app.toml"},
	Mode:         discover.SearchFirst,
})
```

Pass a result to `filesystem.FromDiscovered`. The file is opened through its
canonical resolved target while provenance records the lexical discovered path.
Opening and parsing remain separate from discovery, so malformed and unreadable
files are never mistaken for absence.

Errors expose only fixed policy categories. Rejected paths and arbitrary
platform error text are not formatted, while `errors.Is` still identifies the
policy sentinel or platform cause. Successful results intentionally contain
paths as provenance. Discovery does not protect against an attacker who can
replace files inside an otherwise trusted root; use deployment ownership and
read-only mounts.

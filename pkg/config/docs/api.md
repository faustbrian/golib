# API reference

The canonical symbol-level reference is generated from Go doc comments at
[pkg.go.dev](https://pkg.go.dev/github.com/faustbrian/golib/pkg/config). This guide
describes how the packages compose.

## Root package

`Source` exposes stable `SourceInfo` and a context-aware `Load` operation that
returns a format-independent `Document`. `NewPlan` orders sources by priority
and preserves caller order at equal priority. `NewDefaultPlan` applies the seven
documented precedence categories. `Plan.Sources` returns safe metadata for
inspection.

`LoadTree` merges documents into an immutable tree snapshot. `Load[T]` strictly
decodes the complete tree into `T`. `LoadWithValidators[T]` additionally runs
typed validators. All three return a snapshot only after the entire operation
succeeds. `Snapshot.Value` returns an independent deep copy and
`Snapshot.Origin` returns source, location, sensitivity, deprecation, presence,
and state but never a value.

Source documents are normalized at the load boundary. Only canonical scalar,
object, array, null, and `merge.Delete` values are accepted; unsupported values
return `TreeValueError` without formatting the value. Typed values whose
private mutable state cannot be cloned return `SnapshotValueError` before
validation or publication. This keeps the immutability guarantee explicit for
custom scalar types.

The load boundary also rejects cyclic source maps/slices and enforces aggregate
limits of 64 levels, 100,000 keys, and 100,000 array elements while checking
context cancellation.
`TreeCycleError` and `TreeLimitError` expose only safe path/category metadata.
Arbitrary source failures become `SourceError`; their identity remains
available through `errors.Is`, but their text and concrete type are hidden.

`Optional[T]` distinguishes absent, null, present, and defaulted states.
`Secret` redacts formatting and marshaling; `Reveal` is the explicit access
boundary. `ByteSize` accepts plain bytes and binary IEC units. `ErrNotFound` is
the only error an optional source may suppress. `ErrSourceChanged` identifies a
filesystem input whose metadata changed while it was being read.

`ContextFS`, `ContextFile`, and `ContextCloser` let custom filesystems make
open, read, stat, and close operations cancellation-aware. `GenerationFile`
supplies an opaque token that is compared around every read. Hostile or remote
filesystem implementations should implement all four contracts.

## Source packages

- `defaults.For[T]` reads typed `default` tags.
- `programmatic.Defaults`, `Map`, and `Overrides` clone caller maps.
- `json.Bytes`/`FromFS`, `yaml.Bytes`/`FromFS`, and
  `toml.Bytes`/`FromFS` provide strict bounded structured sources.
- `dotenv.BytesFor[T]`/`FromFSFor[T]` parses dotenv without mutating the
  process environment.
- `environment.EnvironFor[T]` maps an explicit environment snapshot;
  `ProcessFor[T]` reads `os.Environ` on each load.
- `filesystem.FromPath`, `FromFS`, `FromDiscovered`, and `Reader` dispatch
  structured formats while retaining path provenance.
- `discover.Search` returns ordered metadata without reading file contents.

## Supporting packages

`decode` implements strict atomic tree-to-type decoding and safe typed field
errors. `IntoContext` and `ValueContext` support cooperative
`ContextValueUnmarshaler` and `ContextTextUnmarshaler` hooks. `merge` implements
recursive object merge, replacement, null, delete, and conflict semantics.
`validation` provides `At`, deterministic `Errors`, and typed validators.
`configtest` supplies immutable sources, environment slices, `fstest.MapFS`
fixtures, plans, snapshots, origin assertions, and `DiffSecrets` for redacted
secret comparisons in test failures.

Typed conversion and parser errors expose safe metadata and preserve sentinel
identity through `errors.Is`. Arbitrary underlying error text and concrete
types are deliberately unavailable through `Unwrap` and `errors.As` because
extension and parser errors may contain configuration values.

All constructors validate names, policies, types, tags, and bounds before
returning a source. Zero limit fields select conservative defaults; negative
limits are invalid. Defaults and environment schema discovery reject recursive
types and schemas deeper than 64 nested structs with their package-specific
`SchemaError`.

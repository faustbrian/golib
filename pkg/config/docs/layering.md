# Layering, defaults, merging, interpolation, and validation

## Default precedence

`NewDefaultPlan` applies this complete low-to-high table:

| Priority | Category | Winner within category |
|---:|---|---|
| 10 | typed/programmatic defaults | later caller entry |
| 20 | discovered base files | later caller entry |
| 30 | discovered profile files | later caller entry |
| 40 | explicit files | later caller entry |
| 50 | dotenv | later caller entry |
| 60 | process environment | later caller entry |
| 70 | explicit programmatic overrides | later caller entry |

Order within a category is preserved. `NewPlan` supports a custom order by
sorting stable source priorities. Duplicate names, empty names, invalid
metadata, and nil sources fail before loading. Inspect `Plan.Sources()` before
load when startup diagnostics need to show the resolved order.

## Defaults and presence

`defaults.For[T]` reads `default` tags using the destination field type. Scalar
text hooks, durations, URLs, `ByteSize`, `Optional[T]`, JSON slices, and JSON
maps are supported. Invalid or trailing data fails without printing the default
text. Defaults are ordinary lowest-precedence sources.

Use `Optional[T]` when the application must distinguish:

- `Absent`: no winning source supplied the field;
- `Null`: a source explicitly supplied null;
- `Present`: a source supplied a value, including zero or empty;
- `Defaulted`: the winning value came from defaults.

Pointers still express nullable object ownership, while `Optional[T]` expresses
field presence. Do not infer presence from a Go zero value.

## Merge truth table

| Lower | Upper | Result |
|---|---|---|
| absent key | any non-delete value | cloned upper value |
| null | any non-delete value | cloned upper value |
| object | object | recursive key merge |
| bool/string/integer/unsigned/float | same exact kind | upper replacement |
| slice | slice | complete upper replacement |
| any | null | explicit null |
| any | `merge.Delete{}` | key removal |
| incompatible non-null kinds | incompatible non-null kind | `TypeConflictError` |

Slices never append or merge by index. Maps/objects merge recursively. A scalar
or slice replacing an object removes descendant provenance. A failure in any
layer discards the complete candidate snapshot.

## Strict decode and validation

`decode.Into` rejects unknown fields, missing required fields, ambiguous tags,
overflows, unsupported destinations, and conversion errors. Independent field
errors are sorted lexically and annotated with the nearest source/location
origin. Received descriptions identify a safe type category, never a value.

After complete decoding, `LoadWithValidators` runs `Validate() error` on the
candidate when implemented and then caller validators in registration order.
Use `validation.At("server.port", err)` for a safe path. Panics are recovered as
typed panic errors without retaining panic values. No failed candidate snapshot
is returned.

Interpolation belongs to the dotenv source and runs before environment mapping.
It does not inspect arbitrary process environment unless the caller explicitly
copies selected variables into `Interpolation.Variables`.

`decode.IntoContext` and `ValueContext` prefer `ContextValueUnmarshaler` and
`ContextTextUnmarshaler` when implemented. These cooperative hooks receive the
load context and already-bounded canonical input. Legacy standard-library text
hooks remain supported as trusted synchronous application code.

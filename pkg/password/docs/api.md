# API and error semantics

## Policy

`NewPolicy` validates a complete immutable `PolicyConfig`. A policy contains a
target algorithm, target parameters, parser and primitive resource ceilings,
password and encoding byte limits, and active/queued admission limits. Getters
return values, so callers cannot mutate a live policy.

All algorithm ceilings must be usable even when the target algorithm differs,
because verification and migration are cross-algorithm operations. Argon2id
policies are also rejected unless their generated PHC value fits the configured
encoded-hash byte limit.

`DefaultPolicy` targets Argon2id version 19 with time 2, 64 MiB, one lane,
16-byte salts, and 32-byte outputs. Limits permit verification of time up to 4,
128 MiB, four lanes, 64-byte salts/outputs, bcrypt cost 14, 1 KiB passwords,
512-byte encodings, four active operations, and sixteen queued callers.
Bcrypt operations reject passwords above the primitive's fixed 72-byte limit
before primitive work, even when the general password-input ceiling is higher.

## Operations

| Operation | Result | Expensive work |
| --- | --- | --- |
| `Hash` | New validated `EncodedHash` | Salt read and target primitive |
| `Verify` | `Result{Match, NeedsRehash}` | Primitive after strict parse |
| `NeedsRehash` | Boolean | None |
| `VerifyAndUpgrade` | Result plus existing/new hash | Verify, then hash only if needed |

On upgrade hashing failure, `VerifyAndUpgrade` returns the successful
verification result, an empty replacement, and the failure. The existing hash
is untouched because the package never owns persistence.

## Error classification

All root operation errors support `errors.Is`; `*password.Error` supports
`errors.As`. `Kind`, `Operation`, and `Cause` expose read-only inspection while
the error itself remains immutable. `Error.Error` never formats its cause.

| Sentinel | Meaning |
| --- | --- |
| `ErrMismatch` | Valid hash, password mismatch |
| `ErrMalformedHash` | Invalid/non-canonical syntax or decoded length |
| `ErrUnsupportedAlgorithm` | Algorithm has no supported adapter |
| `ErrUnsupportedVersion` | Algorithm version is unsupported |
| `ErrInvalidPolicy` | Constructor/configuration invariant failed |
| `ErrResourceRejected` | Input or encoded parameters exceed bounds |
| `ErrEntropy` | A complete salt could not be read |
| `ErrAdmission` | The bounded wait queue is full |
| `ErrCanceled` | Operation canceled before primitive execution |
| `ErrClosed` | Admission shutdown has begun |

Mismatch is an error and a zero `Result`; successful matches carry the typed
upgrade state. Malformed and unsupported hashes never reach a primitive.

## Cancellation

Context cancellation is checked before parsing/work and while waiting for
admission. The maintained Argon2id and bcrypt functions do not accept contexts;
once invoked they run to completion. Do not promise immediate cancellation.
Bound parameter costs and concurrency so this limitation is operationally safe.

## Encoded hashes

`EncodedHash.String()` intentionally returns the database encoding. Every
`fmt` verb is redacted by `Format`; `%v`, `%s`, `%q`, `%+v`, and `%#v` do not
emit the hash. Treat direct `String()` access as a persistence boundary.
Bcrypt salt and digest fields must use canonical bcrypt base64; alphabet-valid
aliases with non-zero trailing bits are malformed.

## Observations

`Observer` receives one synchronous event per public operation. Fields are
bounded operation/outcome enums, configured algorithm, rehash state, and
duration. Observer panics are isolated. Observers must bound their own latency
and must not derive high-cardinality labels from context values.

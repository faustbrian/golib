# Errors and recovery

Public sentinel errors support `errors.Is`. `ResourceError` supports
`errors.As` and exposes only resource kind, limit, and actual count.

| Error | Meaning | Recovery |
|---|---|---|
| `ErrUnsupportedProfile` | unknown or inconsistent profile identity | reject; select an implemented profile explicitly |
| `ErrUnsupportedAlgorithm` | algorithm is unavailable for the profile | reject; do not downgrade |
| `ErrUnsupportedEncodingVersion` | framed object uses another encoding version | dispatch to an explicitly supported decoder or reject |
| `ErrInvalidLimits` | a required limit is zero or impossible | fix trusted configuration |
| `ErrResourceExhausted` | untrusted work exceeds policy | reject or retry only under a deliberately larger trusted policy |
| `ErrInvalidContext` | context is nil | pass a non-nil context |
| `ErrInvalidSnapshot`, `ErrInvalidBuilder`, `ErrInvalidRootBuilder` | zero or corrupted local state | discard and reconstruct from trusted source |
| `ErrMalformedEncoding` | object is non-canonical, truncated, trailing, or structurally invalid | reject bytes; do not partially recover |
| `ErrMalformedProof` | operation-bound proof structure is impossible | reject proof |
| `ErrVerificationFailed` | well-formed proof does not authenticate | reject claim |
| `ErrInvalidTreeSize`, `ErrIndexOutOfRange`, `ErrInvalidLeafIndexes` | impossible operation metadata | reject request |
| `ErrIncompatibleRoot` | root identities cannot participate in one operation | reject; never convert implicitly |
| `ErrSnapshotAccountingMismatch` | persisted raw-byte count differs from trusted caller state | quarantine snapshot and rebuild |

Cancellation errors come directly from the supplied context and remain
detectable with `errors.Is`.

## Persistence recovery

After a parse, authenticate the complete root identity and publication record.
For resumption, compare cumulative byte accounting with separately trusted
state. If either check fails, do not repair individual nodes: rebuild a new
snapshot from authoritative ordered leaves, compare the resulting complete
identity, and publish atomically.

An interrupted write must leave the previously published snapshot readable.
The package intentionally does not prescribe filesystem rename, database
transaction, object-store generation, or compare-and-swap mechanics; the
adapter must provide that contract.

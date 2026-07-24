# Wall time and monotonic time

Wall time answers “what timestamp is it?” and can jump because of NTP, operator
changes, suspend/resume behavior, or test scenarios. Elapsed time answers “how
long did this take?” and must not silently depend on rollback-prone civil time.

`System.Now` delegates directly to `time.Now`, preserving location and the
standard library's process-local monotonic component. `System.Since` and
`System.Measure` use that component where available. Calling `UTC`, `Round`,
serialization, or persistence may remove it.

The manual clock stores wall offset and monotonic elapsed progress separately.
Construction intentionally strips the starting value's process-local monotonic
reading because a returned wall value must be able to jump independently.
Location is preserved. `Advance` changes elapsed progress; `Jump` changes only
wall time. `Mark`, `SinceMark`, and `Measure` remain correct across backward and
forward jumps. `Since(time.Time)` is wall subtraction and is documented as
such.

JSON, database, protobuf, and wire timestamps cannot retain Go's monotonic
reading. Reconstruct elapsed deadlines from durations or process-local marks,
not deserialized timestamps.

The maintained `TestTimeJSONRoundTripIntentionallyDropsMonotonicReading`
serializes a `time.Now` value, proves the wall instant survives, and proves the
decoded value no longer carries a monotonic reading. Manual tests separately
exercise rollback, forward jump, suspend-like wall movement with no elapsed
progress, frozen wall time with independent elapsed progress, and ordinary
monotonic advancement.

Clock agreement across processes is not a correctness primitive. Leases,
fencing tokens, versions, and idempotency keys belong in distributed protocol
packages such as `lease`.

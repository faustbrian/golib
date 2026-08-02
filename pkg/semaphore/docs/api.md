# API reference

The canonical API reference is generated from exported Go documentation:

```sh
go doc -all github.com/faustbrian/golib/pkg/semaphore
```

The primary entry points are:

- `New(Config)`: validated construction;
- `Acquire(context.Context, int64)`: FIFO context-aware admission;
- `TryAcquire(int64)`: immediate fair admission attempt;
- `Permit.Release`: concurrent exactly-once release;
- `Execute` and `Semaphore.Run`: release-preserving execution;
- `Close` and `Wait`: admission shutdown and acquired-weight drain;
- `Snapshot`: immutable local state;
- `Observer`: bounded transition delivery outside synchronization.

Every error category supports `errors.Is`; structured details support
`errors.As`. See package documentation for error precedence and cancellation
linearization.

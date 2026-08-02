# Bulkhead comparison benchmarks

This non-releasable benchmark module isolates comparison dependencies from the
public bulkhead module. It measures capacity-one admitted and saturated paths
for the local bulkhead, direct `x/sync/semaphore`, Failsafe-Go, and Fortify.

The candidates do not expose identical contracts. The local bulkhead includes
resource identity, snapshots, duplicate-release detection, bounded strict FIFO
waiting, observers, and drain. Direct semaphore and Failsafe-Go expose boolean
try-acquire rejection. Fortify rejection is measured through `Execute`, and
its queue and close semantics differ from the local package.

Run with:

```sh
go test -run '^$' -bench . -benchmem
```

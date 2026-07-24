# Examples

- [`basic`](basic/main.go) is a runnable single-process example using the
  memory lease backend and a short cooperative executor.
- [`queue`](queue/example.go) wires an application-provided durable `queue`
  backend and distributed lease store into a production-shaped runner.

Run the basic example with `go run ./examples/basic`. Multi-replica services
must replace the memory store with PostgreSQL or Valkey and should use the
queue dispatcher shape.

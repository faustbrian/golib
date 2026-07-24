# Performance

Compile schema graphs once and reuse `compile.Set` and `validate.Validator`
concurrently. Resolver work and schema compilation should not occur per
instance. Resource limits are correctness and security controls, not tuning
targets.

`make benchmark` runs the owned Go workloads and the JDK JAXP XML Schema
reference engine against the same schema and valid instance. Both engines must
accept the valid instance, and JAXP must reject the paired invalid instance,
before reference timing begins. Local and CI execution use the same
digest-pinned Eclipse Temurin 25 container with networking disabled and record
the exact runtime banner with the raw results.

The [local baseline](benchmark-baseline.md) is a comparison point, not a claim
that unlike implementations have identical feature coverage. Go allocation
counts apply only to the owned workloads. Throughput changes are release
evidence only when the correctness gate remains green.

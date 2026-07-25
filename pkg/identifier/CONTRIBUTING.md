# Contributing

Use Go 1.26.5 and run `make check` before proposing a change. Behavioral work
starts with a failing focused test. New formats require a stable specification,
official vectors, canonical parser rules, security and leakage analysis,
serialization contracts, fuzz targets, collision and failure tests, fair
benchmarks, and migration guidance.

Do not weaken validation or coverage gates to make a change pass. Keep
generators explicitly owned and avoid mutable package globals.

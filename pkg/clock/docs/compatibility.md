# Compatibility

Version 1 requires Go 1.26, the latest stable major release at implementation
time. CI exercises Linux, macOS, and Windows with Go 1.26.5. The package uses
only the standard library at runtime.

Public API is tracked in `api/v1.txt`. Additive changes follow SemVer; removing
or changing exported contracts requires a new major version. Standard-library
timer/ticker documentation is aligned to Go 1.26 synchronous timer channels.

The system implementation delegates wall time, elapsed time, timers, tickers,
and callbacks to `time`. Its intentional differences are error returns for
invalid ticker durations and nil callbacks instead of exposing panics across
factory interfaces, plus context-aware timer cleanup for sleep.

Manual channels are buffered by one value to make deterministic advancement
independent of receiver scheduling. This is an explicit fake-clock contract,
not a claim that it reproduces every internal implementation detail of `time`.

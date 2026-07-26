# Troubleshooting

`interaction_not_permitted` means policy did not authorize interaction or the
prompt had no permitted headless value. `terminal_unavailable` means required
input or output terminal capability or explicit terminal resources were absent.

`reader_failure`, `writer_failure`, and `renderer_failure` identify the failed
boundary without embedding unsafe input. `terminal_control_failure` means
acquisition, echo configuration, restoration, or release failed; inspect the
safe adapter cause with `errors.As` and verify cleanup.

Unexpected wrapping usually means the caller supplied a small width. Missing
color or animation follows capabilities by design. A prompt that appears to
wait after cancellation has an invalid `EventSource`: `Next` must honor its
context and the core will not hide the problem behind a goroutine.

Reproduce interaction bugs with `VirtualTerminal` and `VirtualClock`; do not
write tests against a developer terminal or real sleeps.

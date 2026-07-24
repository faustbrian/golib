# Terminal adapter

The `terminal` subpackage is the optional application boundary for real
terminal files. Constructing it requires explicit caller-owned `*os.File`
values. Importing the package performs no detection, reads, raw-mode changes,
environment inspection, or package initialization.

```go
adapter, err := terminal.New(os.Stdin, os.Stdout, terminal.Config{})
if err != nil {
    return err
}
capabilities := adapter.Capabilities()
value, err := prompts.Run(ctx, prompt, prompts.Execution{
    Events: adapter,
    Terminal: adapter,
    Output: os.Stdout,
    Error: os.Stderr,
    Capabilities: capabilities,
    Policy: prompts.InteractionPolicy{
        Mode: prompts.InteractivePreferred,
        PermitInteraction: allowInteraction,
    },
})
```

Capability detection does not grant interaction; the caller still sets policy.
`Acquire` places only the supplied input file in raw mode, `SetEcho` changes
only its echo flag, and `Release` restores the captured state. Raw acquisition
preserves terminal output post-processing so line feeds still return to the
first column. Core execution keeps kernel echo disabled while its semantic
renderer owns interactive input, then attempts restoration on every success
and failure path. Do not share one adapter between simultaneous prompts.

`Next` uses short file read deadlines to observe context cancellation without a
goroutine. Linux and macOS terminals, named pipes, and sockets that reject Go
read deadlines use bounded `poll(2)` readiness checks instead. Regular files
without deadline support are rejected before a blocking read. The polling
interval is caller-configurable from greater than zero through one second and
defaults to 50 ms. It also bounds how long a lone Escape waits for a possible
navigation-sequence suffix. Read buffers are bounded at 1 MiB. The decoder
independently bounds retained partial input and paste content.

For `SecretBytes` entry, construct a dedicated adapter with
`DecoderConfig{ByteInput: true}`. Bracketed paste then remains in mutable,
redacting byte wrappers through decoding and editing. Do not reuse that adapter
for ordinary text prompts because byte-mode paste deliberately leaves the
string payload empty.

The adapter is supported on Linux and macOS and tested with controlled pseudo
terminals. Windows is not supported or tested. Other operating systems are
outside the compatibility contract even if this package happens to compile on
them.

Applications that already own a terminal reactor may skip this package and
provide their own cancellable `EventSource` and `TerminalController`. The core
never requires the adapter.

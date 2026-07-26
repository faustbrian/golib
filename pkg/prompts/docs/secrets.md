# Secret handling

Secret prompts require an explicit `SecretClass`. `NewSecret` returns a
`SecretValue`; `NewSecretBytesPrompt` returns caller-owned `*SecretBytes`.
Both redact `fmt` output, JSON, text marshaling, and `slog` values. Prompt
metadata never includes defaults or fallbacks, and validation failures for a
secret prompt replace validator messages and causes with a stable safe issue.

Access is deliberately conspicuous:

```go
token, err := prompts.Parse(ctx, tokenPrompt, configuredToken, dependencies)
if err != nil {
    return err
}
useToken(token.Reveal())
```

`SecretValue` wraps a Go string. Go strings are immutable and cannot be
reliably erased from memory. Redaction prevents accidental representation; it
is not memory erasure and cannot prevent disclosure after `Reveal`.

`SecretBytes` copies input and returned bytes. `Destroy` overwrites and drops
the wrapper's owned slice, is safe to call repeatedly, and is synchronized
with `Reveal`. Callers must separately overwrite every copy returned by
`Reveal`. Compiler, allocator, runtime, swap, crash-dump, and hardware behavior
mean this is best-effort cleanup, not a guarantee that no copy remains.

Interactive string-backed secret prompts request echo disablement through the
explicit `TerminalController` and always attempt echo restoration followed by
release. Executable virtual-terminal tests cover success, cancellation,
callback panic, writer failure, EOF, detachment, and restoration failure. A
real application adapter is still responsible for correctly implementing that
contract for its supported operating systems and terminals.

`SecretBytes` interactive entry has an opt-in byte-native paste path. Configure
the real-terminal adapter with `DecoderConfig{ByteInput: true}` and use that
adapter only for a `SecretBytes` prompt. The decoder stores paste data in
mutable redacting wrappers, the byte editor segments graphemes without string
conversion, and execution overwrites event, editor, and temporary buffers on
success and failure paths.

```go
adapter, err := terminal.New(input, output, terminal.Config{
    Decoder: prompts.DecoderConfig{ByteInput: true},
})
```

`PasteBytesEvent` provides the same path to custom event sources and transfers
an owned copy that execution destroys. Ordinary `PasteEvent` remains accepted
for compatibility but necessarily starts from an immutable Go string. Typed
rune events are encoded directly into mutable editor bytes. Clipboard and
terminal driver buffers remain outside package control, and best-effort byte
cleanup is not a claim of complete process-memory erasure.

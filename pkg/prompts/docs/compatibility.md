# Compatibility

The initial minimum toolchain is Go 1.26.5. CI also runs the current stable Go
toolchain. The supported operating systems are Linux and macOS. CI tests the
semantic core, virtual terminal, and optional terminal adapter on both.
Windows is not supported or tested.

Plain output requires only an `io.Writer`. Interactive execution additionally
requires caller-authorized input and output terminal capabilities, a
context-aware semantic `EventSource`, and an explicit `TerminalController`.
Containers, CI, ECS tasks, redirected streams, and JSON-producing commands
should use `NonInteractiveOnly` or an interaction mode without permission.

The owned byte decoder recognizes portable UTF-8 and a documented subset of
ANSI/VT CSI keyboard sequences used by current supported Unix terminals. This
does not prove every terminal emulator or escape-sequence variant.

The optional `terminal` adapter is supported on Linux and macOS. Controlled
Unix PTY tests exercise raw acquisition, echo changes, failure paths, and
restoration. Builds on any other operating system are outside the compatibility
contract even when the semantic core happens to compile there.

Pre-v1 public compatibility is documented in the changelog and enforced by an
exact exported-API baseline. A released v1 will retain semantic import
compatibility and compare changes against the last published line.

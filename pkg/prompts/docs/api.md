# API

Prompt constructors validate and defensively copy definitions. `Prompt[T]`
retains the typed result and exposes only stable identity and descriptive
metadata. `Parse` handles explicit non-interactive input. `Run` applies caller
policy and either resolves a headless behavior or uses explicit events,
terminal control, output, capabilities, renderer, clock, and dependencies.

The public surface is grouped into prompt definitions, execution policy,
semantic rendering, forms, selection and search, secrets, progress and tasks,
presentation values, and deterministic test support. Executable usage is in
`example_test.go`; detailed contracts are linked from the README.

Dynamic option providers use the separate `DynamicOptions[T]` session. Its
caller-controlled schedule and resolve steps enforce deterministic debounce
and stale-generation rejection without hidden workers or timers.

`PasteBytesEvent` and `DecoderConfig.ByteInput` provide an opt-in mutable paste
payload for `SecretBytes` prompts. `InputEvent.Destroy` clears that payload;
core byte-secret execution invokes it after every consumed event.

No application must import Huh, Bubble Tea, Bubbles, Survey, or PromptUI types.
The core does not accept a command tree, parse argv, exit a process, or own a
business operation.

`specification/api-v0.txt` is the reviewed exported-module baseline. `make api`
requires the current public surface to match it exactly, so additions and
breaking changes both require an explicit baseline update before release.

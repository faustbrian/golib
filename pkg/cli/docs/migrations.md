# Migration guidance

## Cobra

Replace exported `*cobra.Command` construction with `NewCommand`, typed binding
descriptors, and `WithHandler`. Move `cmd.Flags().Get*` calls into captured
bindings and `Invocation.Input`. Replace `Execute` side effects with
`Application.Run`. Keep Cobra-specific custom completion or flag types behind
an application adapter until an owned typed parser is available.

## urfave/cli

Replace `*cli.Command` and context lookups with explicit definitions and typed
bindings. Map `Before`, `Action`, and `After` into validation, pre-run, handler,
post-run, and cleanup according to their actual failure semantics. Do not carry
package-global writers or process argv into the new command.

## Kong

Replace reflected tagged structs with explicit argument and option descriptors.
Keep application domain structs by constructing them from typed bindings in
validation or the handler. Reflection-driven command discovery is intentionally
not reproduced.

## Symfony Console

Map command configuration to explicit constructors, `InputArgument` and
`InputOption` values to typed bindings, events to ordered middleware, and return
codes to classified errors plus exit policy. Services are ordinary constructor
arguments; no container lookup occurs in `execute`.

## Laravel Artisan

Replace signatures and container-resolved command classes with explicit Go
command constructors. Captured dependencies replace method injection. Scheduler,
queue, migration, and model-binding behavior remains in the application rather
than entering `cli`.

Migration should pin old and new argv golden cases, output envelopes, help,
completion, exit codes, cancellation, and cleanup before switching the binary.

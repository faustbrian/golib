# Commands and typed input

Commands have stable names, aliases, summaries, descriptions, examples,
documentation links, hidden and experimental status, deprecation messages, and
replacement paths. Child order is registration order. Compiled metadata returns
copies and remains safe for concurrent help, manifest, and completion reads.
The aggregate byte size of names, aliases, descriptions, examples, links,
argument metadata, and option metadata is bounded by
`Limits.MaximumMetadataBytes` before the graph is published.

Arguments may be required, optional, repeated, or remainder. A required
argument cannot follow an optional argument, and repeated or remainder
arguments must be final. A command with positional arguments cannot also own
subcommands; split that grammar into explicit child commands instead. Built-in
argument types cover string, signed and unsigned 64-bit integers, float64,
duration, time, and enums. Repeated and remainder strings use
`StringsArgument`. Enums require a unique, control-safe allowed set; enum
defaults must belong to that set. Allowed values are available through option
metadata and generated manifests.

Options cover bool, string, signed and unsigned 64-bit integers, float64,
duration, time, enums, repeated strings, and key/value maps. They support long
names, ASCII shorthands, persistent inheritance, typed defaults, required
values, mutual exclusion, and jointly required groups. Compile rejects group
combinations that required or defaulted values make unsatisfiable. Descendants
cannot shadow inherited long or short names.

`Get` returns the typed value. `State` reports `ValueOmitted`, `ValueDefaulted`,
or `ValueExplicit`. An explicit empty string, false, or zero remains explicit.
Scalar options use the last repeated occurrence; string-slice options append in
argv order; key/value options use the last value for a repeated key.

Use `TypedArgument` and `TypedOption` for a domain type. Parsers receive one
exact token and return a value or error. Nil parsers fail compilation. Generic
bindings preserve typed handler access without reflection or string maps.
Custom value-type names must be non-empty, valid, control-free metadata. Time
layouts must also be non-empty and control-free and are published through
option metadata, manifests, and Markdown.

Every command declares `InteractionOptional`, `InteractionRequired`, or
`InteractionForbidden`; zero-value declarations are optional. A required
command fails before validation and side effects when `Request.NonInteractive`
is true. `prompts` can inspect `Invocation.Interactive` before prompting.

Secret bindings use `Secret()`. Their values and conversion causes do not enter
framework errors, metadata defaults, completion, telemetry-facing middleware,
or generated references. Dynamic completion on a secret binding is rejected.

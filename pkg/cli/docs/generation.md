# Help, completion, and references

`Help` renders root or nested plain text from immutable metadata. It includes
usage, arguments, local and inherited options, provenance, aliases, examples,
deprecations, replacements, related documentation, and deterministic ordering.
An alias path resolves to the canonical command path.
`HelpOptions.Width` wraps framework-rendered help for narrow terminals and
pipes; zero leaves content unwrapped. Experimental, deprecated, and replacement
metadata is rendered in a dedicated status section.

`ManifestJSON` returns `cli/manifest/v1` data. Its `root` object publishes
the root command contract, while `commands` retains the ordered descendant
tree for compatibility. Enum bindings publish allowed values but never default
or supplied values, and time bindings publish their parse layout. `Markdown`
returns a complete reference including
arguments, option constraints, status, aliases, examples, and inherited-option
provenance. Generation strips terminal controls, has no command side effects,
and does not mutate compiled metadata. CI should regenerate and byte-compare
committed artifacts.

`Completion` returns scripts for `ShellBash`, `ShellZsh`, `ShellFish`, and
`ShellPowerShell`; it never edits shell configuration. Applications should
write the selected script to stdout and document shell-specific installation.
The scripts pass shell-parsed token arrays directly to the executable and never
use `eval` or `Invoke-Expression`. Bash, Zsh, Fish, and PowerShell syntax is
checked with available native tools; Bash also passes ShellCheck and a live
command-substitution injection regression.

Generated scripts use the hidden `__complete` boundary. That boundary calls
`Complete`, never a command handler, writes tab-delimited candidates followed
by a bounded Cobra directive, and preserves a syntactically valid response when
a provider fails. Candidate controls are stripped before protocol rendering.

Static completion includes visible child command names, aliases, and option
names. Non-secret enums also complete declared values for positional,
separate, assigned, and attached-short forms. Dynamic completion only runs an
explicitly attached `CompletionProvider`. The provider receives caller
context, safe command metadata, and the hostile partial token; it does not
receive parsed values or secrets. Providers may access a network or database
only when the application deliberately implements that behavior. Results are
cancellation-aware,
deduplicated in provider order, and bounded by count and total bytes. An
oversized candidate is skipped without hiding later candidates that fit.

Example installation commands, after an application writes scripts to a
chosen directory:

```sh
source ./tool.bash
source ./_tool
mkdir -p ~/.config/fish/completions && cp tool.fish ~/.config/fish/completions/
# PowerShell: dot-source the generated tool.ps1 from the user's profile.
```

The framework does not append to `.bashrc`, `.zshrc`, Fish configuration, or a
PowerShell profile.

# Migrations

From Huh, keep Huh models out of application contracts. Define a `Prompt[T]`,
construct explicit execution resources, and move non-interactive values to
`Parse` or a declared fallback. Huh-specific translation belongs in an
optional adapter.

From Survey or PromptUI, replace label-based result matching with stable option
identity. Replace package-global stdio and templates with `Execution`, semantic
roles, and a local immutable theme.

From Laravel Prompts or Symfony Question Helper, preserve the expressive label,
hint, validation, selection, progress, and message concepts, but do not carry
over process-global IO or implicit interactivity. Go callers handle returned
errors and process exit themselves.

Migration is complete only when the same operation accepts explicit data in CI
or redirected execution without opening a terminal prompt.

# Integrations

`cli` may construct `Execution` after command parsing explicitly permits
interaction. It should pass already parsed flag or configuration values to
`Parse` in non-interactive mode and must not make a prompt the only way to
supply required data. `prompts` does not import `cli`.

Validation libraries, including `validation`, adapt through typed
`Validator`, `Transformer`, and `ValidationContext.Dependencies` callbacks.
The core has no mandatory validation-framework dependency.

Logging and telemetry should record prompt identity, kind, timing, and safe
outcome only. Never log input events, revealed values, validation dependency
objects, or rendered secret captures.

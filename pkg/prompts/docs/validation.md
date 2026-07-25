# Validation and transformation

Every typed prompt has three ordered callback phases:

1. pre-validation sees the parsed or explicitly supplied value;
2. transformations run in declaration order, each receiving the previous
   transformation result; and
3. post-validation sees the final transformed value.

The executor checks the context after every callback. A cancellation or
deadline stops the pipeline before another callback runs. A callback panic is
contained as a safe adapter failure and its value is not included in output or
the returned error string.

`ValidationContext.Dependencies` is the exact caller-owned value supplied in
`Execution.Dependencies`. The package does not create a dependency container.
Prompt constructors defensively copy callback slices, so later slice mutation
does not alter a reusable definition.

Validators should return `ValidationIssue` with a stable code, a localized
safe message, and relevant prompt identities. Other errors are converted to a
terminal-safe generic validation issue; their original error is not retained
because arbitrary error chains may contain submitted values. Secret prompts
apply stricter redaction and never reuse input-derived validator text.

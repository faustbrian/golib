# Migration from Shipit and Cline Ruler

Inventory every existing rule's identifier, input fields, missing/null
behavior, priority, conflict behavior, output, and failure policy. Do not begin
by translating syntax.

Map source fields to explicit paths and typed values. Replace implicit
truthiness and coercion with exact propositions. Preserve source priority and
choose a rule-set conflict strategy deliberately. Represent computed outputs
as bounded derived facts only when they are side-effect free.

For each source rule, keep fixtures for match, non-match, missing, null,
boundary, Unicode, and invalid input. Run the old and new evaluators against
the same normalized snapshot and compare decisions and selected rule IDs.
Record deliberate differences before activation.

Migrate in shadow mode by canonical hash. Observe mismatches without logging
sensitive facts. Cut over only after every production rule has differential
evidence and `Indeterminate` is wired to the owning service's safe state.

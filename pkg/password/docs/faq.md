# FAQ

## Why Argon2id by default?

It is the preferred maintained memory-hard primitive for new password hashes.
Bcrypt remains only for compatibility and controlled migration.

## Why does mismatch return an error?

It keeps mismatch distinct from malformed, unsupported, canceled, unavailable,
and resource-rejected outcomes through `errors.Is`. Successful results alone
carry `Match=true`.

## Can context cancellation stop Argon2id?

No. It stops before work or while waiting for admission. The maintained
primitive has no context API and runs to completion once invoked.

## Does the package erase passwords from memory?

It copies and does not retain caller bytes, then best-effort clears the copy.
It cannot guarantee Go runtime memory erasure.

## Should encoded hashes be logged?

No. They enable offline guessing and may be production-sensitive. Formatting is
redacted; direct `String()` is for persistence boundaries.

## What if two logins upgrade simultaneously?

Both may compute a replacement. Only a database CAS against the verified old
value may win. The losing login still verified successfully.

## Can I add pre-hashing for very long inputs?

Not transparently. It would be a separate versioned scheme incompatible with
ordinary Argon2id/bcrypt hashes. The package currently rejects over-limit input.

## Does `passwordauth` replace `authentication`?

No. It supplies lookup/verification/CAS data that an application adapter can
turn into a `authentication` principal.

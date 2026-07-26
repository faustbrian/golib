# Transactions

Transactions require a stable, unique `TransactionalID` for each live producer
identity. Reusing an identity fences the older producer.
Transactional IDs must be valid UTF-8 without whitespace padding or control
characters and are limited to 255 bytes.

`RunTransaction` requires exclusive producer ownership. It fails when another
transaction, maintenance operation, or admitted non-transactional production
is active; it does not hold a package lock across Kafka IO or the application
callback. It waits for every publish started through the callback capability
and closes that capability before commit. Callback failure or panic triggers a
bounded safe buffer abort and transaction abort.

Commit and abort completion use a bounded context derived without caller
cancellation because canceling `EndTransaction` can make the broker outcome
unknowable. Kafka lifecycle failures return a redacted `TransactionError`.
`Operation` identifies begin, commit, or abort; `Category` distinguishes
authorization, fencing, fatal producer state, retryable abort-required failure,
and ambiguous outcome. `Abortable` means Kafka rejected the commit and the
package attempted the required bounded abort. `OutcomeKnown` is false when the
broker result cannot be established. An unknown commit also preserves
`ErrTransactionOutcomeUnknown`; callers must stop the affected workflow and
reconcile before retrying. `errors.Is` and `errors.As` preserve causes for
deliberate programmatic inspection while `Error` never renders them.

The real-broker fixture proves a committed record is visible at both Kafka
isolation levels and an aborted record is filtered from a `read_committed`
consumer while remaining present for `read_uncommitted` inspection. This is
producer-transaction evidence against the pinned single-node Confluent Local
fixture, not consume-transform-produce or multi-broker fencing evidence.

These producer transactions do not replace a transactional database outbox.
Kafka-to-Kafka consume-transform-produce workflows additionally require
transactional offset handling, which this module does not currently expose.

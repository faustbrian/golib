# Transactions

Transactions require a stable, unique `TransactionalID` for each live producer
identity. Reusing an identity fences the older producer.

`RunTransaction` requires exclusive producer ownership. It fails when another
transaction, maintenance operation, or admitted non-transactional production
is active; it does not hold a package lock across Kafka IO or the application
callback. It waits for every publish started through the callback capability
and closes that capability before commit. Callback failure or panic triggers a
bounded safe buffer abort and transaction abort.

Commit and abort completion use a bounded context derived without caller
cancellation because canceling `EndTransaction` can make the broker outcome
unknowable. An unclassified commit failure includes
`ErrTransactionOutcomeUnknown`; callers must stop the affected workflow and
reconcile before retrying.

These producer transactions do not replace a transactional database outbox.
Kafka-to-Kafka consume-transform-produce workflows additionally require
transactional offset handling, which this module does not currently expose.

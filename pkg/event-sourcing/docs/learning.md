# Learning event sourcing

This library implements event-sourcing infrastructure; it does not make event
sourcing simple or appropriate for every domain. Read the tradeoffs before
adopting it:

- [Martin Fowler: Event Sourcing](https://martinfowler.com/eaaDev/EventSourcing.html)
  introduces deriving application state from an ordered event history and
  discusses reversal, replay, and external updates.
- [Martin Fowler: Focusing on Events](https://martinfowler.com/eaaDev/EventNarrative.html)
  places event sourcing among audit logs, temporal models, snapshots, and
  parallel models.
- [Microservices.io: Event Sourcing](https://microservices.io/patterns/data/event-sourcing.html)
  summarizes the pattern, benefits, query costs, snapshots, and relationship
  to event publication.
- [EventSauce documentation](https://eventsauce.io/docs/) is the conceptual
  reference for this library's pragmatic separation of aggregate repository,
  message repository, and dispatcher.

These sources explain patterns, not this package's guarantees. Where a source
assumes CQRS, a broker-like event store, framework handlers, or a particular
transaction model, this library keeps those choices optional and explicit.

## Questions to answer before adoption

1. Which business decisions require an immutable history rather than only
   current state?
2. Who owns stable event names and schema evolution for the lifetime of the
   history?
3. Can every historical event be replayed deterministically without external
   side effects?
4. How will projections be rebuilt, checkpointed, monitored, and recovered?
5. What are the retention, privacy, deletion, and legal requirements for
   immutable records?
6. How will optimistic conflicts, duplicate delivery, poison events, and
   ambiguous commits be handled?
7. What is the tested backup, restore, and history-repair procedure?

If those obligations do not buy material domain value, conventional state
persistence is usually the better design.

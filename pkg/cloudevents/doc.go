// Package cloudevents implements the transport-independent CloudEvents 1.0
// information model together with explicitly selected event formats, protocol
// bindings, and extensions.
//
// The package is an interoperability envelope, not an event bus, event store,
// queue, outbox, workflow engine, schema registry, or canonical domain-event
// model. Constructors and decoders own retained mutable input. The core does no
// network I/O, schema resolution, global registration, telemetry, or background
// work.
package cloudevents

# Architecture

The root owns immutable schedule semantics, not external transports or business
policy. `Date` aliases the immutable `calendar` civil value, while
`LocalTime` remains distinct from both dates and instants. Schedule construction
uses `calendar/timezone` for bounded IANA identity loading; conversion to
instants always uses that explicit schedule timezone. `openinghourscalendar`
adapts bounded business-calendar holiday expansion, while
`openinghourstemporal` accepts only `temporal/timeofday` mappings whose state,
bounds, and precision remain representable.

Weekly recurrence and exact-date operations remain separate. Queries resolve
them into bounded internal segments. Composition stores immutable expression
trees instead of flattening rules and losing provenance. Persistence always
passes through strict canonical encoding.

Adapter packages depend inward on root and the single capability they adapt.
Root aliases the narrow `clock` capability because current time belongs to
that module. There is no dependency on localization, HTTP, application
repositories, provider payloads, or holiday datasets.

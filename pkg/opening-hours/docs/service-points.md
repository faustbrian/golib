# Service-point adoption

Store one schedule per service point with the point's physical IANA timezone.
Model ordinary collection windows in weekly rules and public holidays or
temporary closures as dated exceptions. Keep carrier payload interpretation in
the Location/Postal adapter.

Location's structured `opening_hours` maps weekday names to arrays of
`{from,to}` slots. Use `encoding.ImportLocation`; present empty/null days become
closed and absent days remain inherited. Track and Postal currently consume the
shared Location boundary and should migrate through that same representation,
not through a new provider parser.

Opening hours answer only physical availability. Order eligibility, supported
services, parcel dimensions, cutoff times, and inventory remain application
policy.

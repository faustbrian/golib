# Storefront adoption

Use weekly rules for public trading hours, named exception sets for holiday
calendars, and high-priority exact-date rules for emergency closures. Use
metadata for a bounded source/revision, not customer-facing translations.

When combining tenant and building availability, use `Intersection`. To extend
hours, use `Union`. To publish an authoritative temporary schedule, use
`Overlay`. Always verify the resulting schedule at boundaries and around the
store timezone's DST transitions.

Do not treat `IsOpen` as proof that orders can be accepted; checkout, inventory,
authorization, and fulfillment remain separate decisions.

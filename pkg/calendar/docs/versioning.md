# Versioning and persisted meaning

Calendar APIs follow semantic versioning after v1. Business calendars carry an
application-supplied revision independently from the Go module version.

Persist civil values in canonical form. Persist business revision and dataset
provenance with derived decisions. Persist IANA zone identity, resolution
policy, and tzdata/application version when local-to-instant replay matters.
Never reinterpret an old decision silently under a new calendar revision.

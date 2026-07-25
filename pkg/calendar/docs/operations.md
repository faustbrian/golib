# Operations

Pin Go 1.26.5 and all analysis tools. Run `make check` before release. Run
`make timezone` after OS, container, Go, or tzdata updates. Review transition
corpus drift rather than weakening assertions.

Monitor classified error counts (`invalid_date`, `nonexistent`, `ambiguous`,
`search_limit`) using bounded labels. Record calendar revision and dataset
checksum in decision logs. Do not record holiday names or hostile input as
telemetry labels.

For database upgrades, run `make integration` against the target PostgreSQL
version. For business-policy changes, version the calendar, compare decisions,
and retain the prior revision for replay.

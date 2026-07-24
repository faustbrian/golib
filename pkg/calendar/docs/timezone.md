# Timezones and DST

Civil dates carry no location. An instant is produced only from a
`timezone.LocalDateTime`, an explicit `*time.Location`, and a resolution policy.

- `Reject` accepts exactly one occurrence and rejects both gaps and folds.
- `Earlier` and `Later` select chronological fold occurrences; gaps still fail.
- `MatchOffset(seconds)` selects only an occurrence with that UTC offset.

Resolution enumerates possible offsets in a bounded 145-hour lookup window and
verifies each occurrence by round trip. Standard-library tzdata remains
authoritative. Applications may import `time/tzdata` when they need an embedded
snapshot; otherwise operating-system updates apply. Persisted local values can
therefore resolve differently after tzdata changes. Persist the zone identity,
policy, intended local value, and tzdata/application version when replay matters.

The compatibility corpus covers New York gap/fold, `US/Eastern` alias,
Lord Howe's 30-minute fold, Kathmandu's +05:45 offset, Helsinki's 23-hour day,
and Apia's 2011 date-line skip. Run `make timezone` after any toolchain or
tzdata update and review intentional drift before deployment.

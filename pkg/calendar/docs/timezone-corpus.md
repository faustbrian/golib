# Timezone compatibility corpus

The public `calendartest.TransitionVectors` corpus is deliberately small enough
to review and broad enough to detect transition-policy or tzdata drift.

| Vector | Zone | Local value | Policy | Expected result |
| --- | --- | --- | --- | --- |
| New York spring gap | America/New_York | 2024-03-10 02:30 | Reject | nonexistent |
| New York fold earlier | America/New_York | 2024-11-03 01:30 | Earlier | UTC-04:00 |
| New York alias later | US/Eastern | 2024-11-03 01:30 | Later | UTC-05:00 |
| Lord Howe half-hour fold | Australia/Lord_Howe | 2024-04-07 01:45 | Later | UTC+10:30 |
| Kathmandu unusual offset | Asia/Kathmandu | 2024-01-01 12:00 | Reject | UTC+05:45 |
| Dublin second-offset gap | Europe/Dublin | 1916-05-21 02:30 | Reject | nonexistent |
| Dublin second-offset fold | Europe/Dublin | 1916-10-01 02:30 | Earlier | UTC+00:34:39 |
| Monrovia second-offset gap | Africa/Monrovia | 1972-01-07 00:20 | Reject | nonexistent |
| Apia date-line skip | Pacific/Apia | 2011-12-30 12:00 | Reject | nonexistent |
| Kwajalein date-line skip | Pacific/Kwajalein | 1993-08-21 12:00 | Reject | nonexistent |
| Helsinki short day | Europe/Helsinki | 2024-03-31 | day range | 23 hours |

The standard library is authoritative for transition calculation. In addition
to these fixed drift vectors,
`TestTimezoneConversionsDifferentialAgainstStandardLibrary` round-trips every
month from 1900 through 2030 across nine representative zone and alias
identities. On an intentional tzdata update, record the old/new Go version, OS
or embedded tzdata version, changed zone/vector, authoritative upstream
rationale, and application impact before updating an expected value.

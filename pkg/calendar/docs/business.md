# Business calendars

A calendar requires a non-empty revision and owns copies of weekend and holiday
inputs. A country code is never treated as a complete calendar identity.

Holiday names, metadata, provenance, and total entries are bounded. Overlapping
holidays remain separate. Observance never overwrites a source date: generated
entries carry `SourceDate` and `IsObserved`.

`NextBusinessDay`, `PreviousBusinessDay`, `AddBusinessDays`, and
`CountBusinessDays` require explicit positive work limits. A seven-day weekend
or long closure therefore returns `ErrSearchLimit` instead of looping. The zero
calendar fails closed.

Cutoffs, opening hours, settlement rules, and carrier schedules are intentionally
outside this package.

## Resource budget

| Input | Limit |
| --- | ---: |
| Holidays per calendar | 10,000 |
| Holiday name | 256 bytes |
| Metadata entries per holiday | 32 |
| Metadata key/value | 128/1,024 bytes |
| Provenance field | 1,024 bytes |

Search and date-sequence limits are caller supplied so domain owners choose an
appropriate operational budget.

## Provenance, revision, and compatibility report

No holiday dataset is bundled, so there is no repository-owned checksum or
generated dataset whose provenance can drift. Applications must persist the
calendar revision and supplied provenance beside derived decisions. A checksum
identifies exact source bytes; the revision identifies the application's
interpretation, including weekends and observance policy.

Any future separately shipped dataset must be generated deterministically from
an authoritative source and verified by checksum. Its update report must
classify every compatibility diff as an added holiday, removed/corrected
holiday, observance-date change, metadata-only change, or provenance-only
change. Unclassified diffs and nondeterministic output block release. See the
[holiday dataset policy](holiday-datasets.md).

# Troubleshooting

## `ErrInvalidDate`

Check the 1–9999 range, Gregorian month length, and zero values. Parsing accepts
only `YYYY-MM-DD` ASCII text.

## `ErrNonexistent` or `ErrAmbiguous`

The local wall value crosses a timezone transition. Choose a domain policy;
do not retry with a random location or fabricate an offset.

## `ErrSearchLimit`

The calendar is closed longer than the supplied budget or the budget is zero.
Inspect weekend/holiday configuration before increasing it.

## Timezone results changed

Compare Go, OS/container tzdata, zone identity, and stored policy versions. Run
`make timezone` and review the corpus.

## Integration cannot start

Supply `POSTGRES_URL` or start Docker. `make integration` never accesses a
production database unless a caller explicitly supplies such a URL; use only a
disposable test database.

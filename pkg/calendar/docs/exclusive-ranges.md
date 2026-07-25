# Exclusive day ranges

Never invent `23:59:59.999999999` as an end-of-day value. Civil days vary in
elapsed length and local wall times can be skipped or repeated.

`timezone.DayRange` resolves midnight at the requested date and at the next
date, returning `[start,end)`. `calendartemporal.InclusiveDates` converts
inclusive civil endpoints to the same exclusive instant form suitable for
`temporal/instant.Range`.

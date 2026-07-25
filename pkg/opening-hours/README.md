# opening-hours

[![quality](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/ci.yml/badge.svg?branch=main&job=quality)](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/ci.yml)
[![lint](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/ci.yml/badge.svg?branch=main&job=lint)](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/ci.yml)
[![vulnerability](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/ci.yml/badge.svg?branch=main&job=vulnerability)](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/ci.yml)
[![NilAway advisory](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/ci.yml/badge.svg?branch=main&job=nilaway-advisory)](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/ci.yml)
[![PostgreSQL](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/integration.yml/badge.svg?branch=main&job=postgres)](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/integration.yml)
[![release](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/release.yml/badge.svg?job=verify)](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/release.yml)
[![publish](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/release.yml/badge.svg?job=publish)](https://github.com/faustbrian/golib/pkg/opening-hours/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/faustbrian/golib/pkg/opening-hours.svg)](https://pkg.go.dev/github.com/faustbrian/golib/pkg/opening-hours)

Immutable, deterministic, timezone-safe recurring opening hours and dated
exceptions for Go 1.26.5 and later.

The package models generic availability for service points, storefronts,
offices, pickup locations, and support desks. It does not parse carrier prose,
book appointments, plan workforces, or decide whether an order is eligible.

## Five-minute start

```go
package main

import (
	"fmt"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func main() {
	start, _ := openinghours.NewLocalTime(9, 0, 0, 0)
	end, _ := openinghours.NewLocalTime(17, 0, 0, 0)
	dayRange, _ := openinghours.NewRange(start, end)
	monday, _ := openinghours.OpenRanges(
		[]openinghours.Range{dayRange},
		openinghours.RejectOverlapAndAdjacent,
	)

	schedule, _ := openinghours.NewSchedule(openinghours.Config{
		Timezone: "Europe/Helsinki",
		Weekly: map[time.Weekday]openinghours.DayRule{
			time.Monday: monday,
		},
	})

	result, _ := schedule.IsOpen(
		time.Date(2026, time.January, 5, 10, 0, 0, 0, time.UTC),
	)
	fmt.Println(result.Open, result.Explanation.Timezone)
}
```

All boundaries are start-inclusive and end-exclusive. A range such as
`22:00-02:00` belongs to its start date. The zero `Schedule` is closed and has
no timezone; it never means always open.

## What is explicit

- IANA timezone identity and DST gap/fold policy
- inherited, ranged, all-day, and closed day states
- overlap, adjacency, and normalization policy
- exact-date replace, add, subtract, and close operations
- exception priority, source, revision, and optional named set
- inclusive effective dates and outside-range behavior
- bounded transition horizons, output counts, parsing, and composition depth
- canonical JSON, stable comparison/hash, separate human display summaries
- SQL/JSONB persistence and native pgx behavior
- injected clocks and privacy-safe observation callbacks

## Packages

| Package | Purpose |
| --- | --- |
| root | Values, rules, exceptions, algebra, queries, encoding, SQL |
| `compile` | Immutable prepared query handle |
| `encoding` | Canonical Location/Spatie imports; Track/Postal fixtures |
| `postgres` | Nullable JSONB wrapper and pgx compatibility |
| `openinghourswire` | Byte-codec adapter |
| `openinghoursvalidation` | Canonical validation adapter |
| `openinghoursconfig` | Strict configuration adapter |
| `openinghourscalendar` | `calendar` dates and holiday closures |
| `openinghourstemporal` | Lossless `temporal/timeofday` conversion |
| `openinghourstest` | Panic-on-error test builders |

## Documentation

Start at the [documentation index](docs/README.md). The five-minute guides cover
[weekly schedules](docs/weekly-schedules.md), [exceptions](docs/exceptions.md),
[overnight ranges](docs/overnight.md), [timezones](docs/timezones.md), and
[queries](docs/queries.md). The formal contracts are in
[precedence](docs/precedence.md) and [normalization](docs/normalization.md).
Owned-module integration is covered in [integrations](docs/integrations.md).

## Local verification

```sh
make check
make lint
make nilaway
make integration
```

`make check` is the core release gate; lint, advisory NilAway, and PostgreSQL
integration have separate reproducible targets shown above. PostgreSQL skips
when `POSTGRES_URL` is absent and runs against the supplied disposable database
when present.

## Support and policy

See [security policy](SECURITY.md), [contribution guide](CONTRIBUTING.md),
[compatibility policy](docs/compatibility.md), and [changelog](CHANGELOG.md).
The project is available under the [MIT License](LICENSE).

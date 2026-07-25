# Migrating from Carbon

| Carbon-style behavior | calendar replacement |
| --- | --- |
| Mutable fluent date | Immutable returned value plus error |
| Implicit current time | `clock` plus `calendarclock.Today` |
| Implicit/global timezone | Explicit location at conversion |
| Silent month rollover | `Clamp`, `Reject`, or `Overflow` |
| End of day at 23:59:59.999… | Next-day exclusive boundary |
| Natural-language parsing | Strict canonical parser |
| `diffForHumans` / translations | Presentation layer |
| Holiday lookup by country | Application-supplied revisioned calendar |

There is no compatibility facade. Migrate domain intent, add explicit policy at
every ambiguous seam, and retain interval algebra in temporal.

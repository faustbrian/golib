# Runnable examples

Each directory is an independent `main` package:

| Scenario | Command |
| --- | --- |
| Coordinate order, ellipsoidal distance, spherical bounds | `go run ./examples/quickstart` |
| Bearings, destination, line length, nearest candidates | `go run ./examples/measurements` |
| Polygon holes and boundary semantics | `go run ./examples/polygon` |
| GeoJSON, EWKT, and EWKB | `go run ./examples/codecs` |
| GeoJSON Feature IDs and owned properties | `go run ./examples/feature` |
| Geohash cells and neighbors | `go run ./examples/geohash` |
| geom adapter isolation | `go run ./examples/adapter` |
| Safe PostGIS SQL fragments | `go run ./examples/postgis` |
| Live pgx codec registration and scan | `POSTGIS_DSN=... go run ./examples/pgx` |

The pgx example exits successfully with setup guidance when `POSTGIS_DSN` is
unset. All other programs run without external services.

# Codecs, pgx, and PostGIS

## GeoJSON

`geojson.Marshal` and `Unmarshal` support every package geometry.
`MarshalFeature` and `UnmarshalFeature` support a geometry, optional ID, and an
owned JSON property object. GeoJSON has no standard SRID field here: callers
must supply the CRS when decoding. Coordinate order remains longitude,
latitude.

## WKT, EWKT, WKB, and EWKB

Plain WKT/WKB omit CRS, so decoding requires a caller-supplied CRS. EWKT/EWKB
carry a positive SRID. Encoders emit canonical 2D representations and reject
nil or invalid geometry. WKB callers choose byte order. All decoders apply
`geo.Limits`, including `MaxEncodedBytes`, before unbounded work.

The live PostGIS corpus exercises every geometry family, supported empty
aggregate, GeoJSON/WKT/EWKT/WKB/EWKB representation, NDR and XDR byte order,
SRID metadata, and 2D rejection on PostGIS 16 / 3.5 and 18 / 3.6. See
[`hardening.md`](hardening.md#codec-and-postgis-interoperability-corpus).

## geom adapter

`adapter/gogeom` converts through canonical EWKB. Only two-dimensional layouts
with positive SRIDs are accepted. The dependency remains outside the root
model, so callers do not inherit its mutability or public API.

## database/sql

`postgis.Value` owns a geometry, implements `driver.Valuer` and `sql.Scanner`,
and represents SQL NULL when invalid. It accepts binary EWKB and canonical
hexadecimal EWKB. Initialize it with limits before scanning untrusted rows:

```go
value, err := postgis.NewValue(nil, limits)
err = value.Scan(source)
geometry, valid := value.Geometry()
```

## pgx registration

PostGIS OIDs are installation-specific. Query and register both spatial types
on every new connection that will use both helpers:

```go
var geometryOID, geographyOID uint32
err := conn.QueryRow(ctx,
    "SELECT 'geometry'::regtype::oid, 'geography'::regtype::oid",
).Scan(&geometryOID, &geographyOID)
postgis.Register(conn.TypeMap(), geometryOID, limits)
postgis.Register(conn.TypeMap(), geographyOID, limits)
```

Registration is connection/type-map local. Perform it in pool connection setup.

## Safe query fragments

`postgis.NewColumn` accepts one to three ordinary SQL identifier segments and
quotes each segment. `GeographyDWithin` and `Intersects` return fixed SQL plus
separate arguments. They never interpolate values. The caller owns the full
query and must pass the returned arguments in placeholder order.

Use `GeographyDWithin` for metre distances on WGS84 geography. Use `Intersects`
for the column's geometry coordinate system. These helpers are intentionally not
a query builder.

## Process or database?

Calculate in process when the candidate set is already small and loaded, when
the result is part of validation, or when deterministic serialization is the
goal. Query PostGIS when an index can reduce candidates, spatial data remains in
the database, or a join/aggregate would otherwise move large geometry sets into
the service.

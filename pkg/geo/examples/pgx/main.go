package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/postgis"
)

func main() {
	dsn := os.Getenv("POSTGIS_DSN")
	if dsn == "" {
		fmt.Println("set POSTGIS_DSN to run the live pgx example")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		panic(err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if closeErr := connection.Close(closeCtx); closeErr != nil {
			panic(fmt.Errorf("close PostGIS connection: %w", closeErr))
		}
	}()

	var geometryOID, geographyOID uint32
	err = connection.QueryRow(ctx,
		"SELECT 'geometry'::regtype::oid, 'geography'::regtype::oid",
	).Scan(&geometryOID, &geographyOID)
	if err != nil {
		panic(err)
	}
	postgis.Register(connection.TypeMap(), geometryOID, geo.DefaultLimits())
	postgis.Register(connection.TypeMap(), geographyOID, geo.DefaultLimits())

	var value postgis.Value
	err = connection.QueryRow(ctx, `
		SELECT ST_SetSRID(ST_MakePoint(24.9384, 60.1699), 4326)::geometry
	`).Scan(&value)
	if err != nil {
		panic(err)
	}
	geometry, valid := value.Geometry()
	if !valid {
		panic("PostGIS returned NULL geometry")
	}
	fmt.Printf("valid: %t, type: %s, SRID: %d\n",
		valid, geometry.Type(), geometry.CRS().SRID())
}

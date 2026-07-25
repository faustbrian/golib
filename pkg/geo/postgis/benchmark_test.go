package postgis_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/postgis"
)

func BenchmarkPgxBinaryCodec(b *testing.B) {
	point := benchmarkPoint(b, 24.9384, 60.1699)
	value, err := postgis.NewValue(point, geo.DefaultLimits())
	if err != nil {
		b.Fatal(err)
	}
	typeMap := pgtype.NewMap()
	postgis.Register(typeMap, geometryOID, geo.DefaultLimits())
	encoded, err := typeMap.Encode(
		geometryOID,
		pgtype.BinaryFormatCode,
		value,
		nil,
	)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("encode", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(encoded)))
		for b.Loop() {
			if _, encodeErr := typeMap.Encode(
				geometryOID,
				pgtype.BinaryFormatCode,
				value,
				nil,
			); encodeErr != nil {
				b.Fatal(encodeErr)
			}
		}
	})
	b.Run("scan", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(encoded)))
		for b.Loop() {
			var target postgis.Value
			if scanErr := typeMap.Scan(
				geometryOID,
				pgtype.BinaryFormatCode,
				encoded,
				&target,
			); scanErr != nil {
				b.Fatal(scanErr)
			}
		}
	})
}

func TestPgxBinaryCodecAllocationBudgets(t *testing.T) {
	point := benchmarkPoint(t, 24.9384, 60.1699)
	value, err := postgis.NewValue(point, geo.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	typeMap := pgtype.NewMap()
	postgis.Register(typeMap, geometryOID, geo.DefaultLimits())
	encoded, err := typeMap.Encode(
		geometryOID,
		pgtype.BinaryFormatCode,
		value,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	encodeAllocations := testing.AllocsPerRun(20, func() {
		if _, encodeErr := typeMap.Encode(
			geometryOID,
			pgtype.BinaryFormatCode,
			value,
			nil,
		); encodeErr != nil {
			t.Fatal(encodeErr)
		}
	})
	if encodeAllocations > 8 {
		t.Fatalf(
			"pgx binary encode allocations = %.1f, budget is 8",
			encodeAllocations,
		)
	}
	scanAllocations := testing.AllocsPerRun(20, func() {
		var target postgis.Value
		if scanErr := typeMap.Scan(
			geometryOID,
			pgtype.BinaryFormatCode,
			encoded,
			&target,
		); scanErr != nil {
			t.Fatal(scanErr)
		}
	})
	if scanAllocations > 8 {
		t.Fatalf(
			"pgx binary scan allocations = %.1f, budget is 8",
			scanAllocations,
		)
	}
}

func benchmarkPoint(test testing.TB, longitude, latitude float64) geo.Point {
	test.Helper()
	lon, err := geo.NewLongitude(longitude)
	if err != nil {
		test.Fatal(err)
	}
	lat, err := geo.NewLatitude(latitude)
	if err != nil {
		test.Fatal(err)
	}
	coordinate, err := geo.NewCoordinate(lon, lat, geo.WGS84())
	if err != nil {
		test.Fatal(err)
	}
	point, err := geo.NewPoint(coordinate)
	if err != nil {
		test.Fatal(err)
	}
	return point
}

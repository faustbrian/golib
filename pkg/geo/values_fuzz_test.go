package geo_test

import (
	"encoding/json"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func FuzzValueDecoding(f *testing.F) {
	f.Add([]byte("24.9384"))
	f.Add([]byte(`{"longitude":24.9384,"latitude":60.1699,"crs":{"srid":4326,"name":"EPSG:4326"}}`))
	f.Add([]byte("NaN"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 4_096 {
			data = data[:4_096]
		}

		textTargets := []interface{ UnmarshalText([]byte) error }{
			new(geo.Longitude),
			new(geo.Latitude),
			new(geo.Altitude),
			new(geo.Bearing),
			new(geo.Distance),
			new(geo.CRS),
			new(geo.Coordinate),
		}
		for _, target := range textTargets {
			_ = target.UnmarshalText(data)
			_ = json.Unmarshal(data, target)
		}
	})
}

package geojson

import (
	"errors"
	"strings"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func TestDepthCountdownRejectsExhaustedAndNegativeLimits(t *testing.T) {
	t.Parallel()

	data := []byte(`{"type":"Point","coordinates":[0,0]}`)
	limits := geo.DefaultLimits()
	for _, remainingDepth := range []int{0, -1} {
		if _, err := unmarshalGeometry(data, geo.WGS84(), limits, remainingDepth); !errors.Is(err, geo.ErrTopology) {
			t.Fatalf("unmarshalGeometry(depth %d) error = %v", remainingDepth, err)
		}
	}
	limits.MaxCollectionDepth = -1
	if _, err := Unmarshal(data, geo.WGS84(), limits); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("Unmarshal(negative depth limit) error = %v", err)
	}
	deep := []byte(`{"type":"GeometryCollection","geometries":[{"type":"GeometryCollection","geometries":[]}]}`)
	if _, err := unmarshalGeometry(deep, geo.WGS84(), geo.DefaultLimits(), 1); err == nil ||
		!strings.Contains(err.Error(), "collection depth limit exceeded") {
		t.Fatalf("unmarshalGeometry(exhausted child depth) error = %v", err)
	}
}

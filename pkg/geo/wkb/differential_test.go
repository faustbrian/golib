package wkb_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	geom "github.com/twpayne/go-geom"
	upstreamewkb "github.com/twpayne/go-geom/encoding/ewkb"
	upstreamwkb "github.com/twpayne/go-geom/encoding/wkb"

	"github.com/faustbrian/golib/pkg/geo/wkb"
)

func TestPointEncodingMatchesGoGeom(t *testing.T) {
	t.Parallel()

	point := mustPoint(t, 24.9384, 60.1699)
	upstream := geom.NewPointFlat(geom.XY, []float64{24.9384, 60.1699}).
		SetSRID(4326)

	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		gotWKB, err := wkb.Marshal(point, order)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		wantWKB, err := upstreamwkb.Marshal(upstream, order)
		if err != nil {
			t.Fatalf("upstream wkb.Marshal() error = %v", err)
		}
		if !bytes.Equal(gotWKB, wantWKB) {
			t.Fatalf("WKB differs from geom:\ngot  %x\nwant %x", gotWKB, wantWKB)
		}

		gotEWKB, err := wkb.MarshalEWKB(point, order)
		if err != nil {
			t.Fatalf("MarshalEWKB() error = %v", err)
		}
		wantEWKB, err := upstreamewkb.Marshal(upstream, order)
		if err != nil {
			t.Fatalf("upstream ewkb.Marshal() error = %v", err)
		}
		if !bytes.Equal(gotEWKB, wantEWKB) {
			t.Fatalf("EWKB differs from geom:\ngot  %x\nwant %x", gotEWKB, wantEWKB)
		}
	}
}

package geohash_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/geo/geohash"
)

func FuzzDecode(f *testing.F) {
	f.Add("ezs42")
	f.Add("u60g0b3")
	f.Add("")

	f.Fuzz(func(_ *testing.T, value string) {
		hash := geohash.Hash(value)
		_, _ = geohash.Decode(hash)
		_, _ = geohash.Neighbors(hash)
	})
}

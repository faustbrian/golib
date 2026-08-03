package country

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	international "github.com/faustbrian/golib/pkg/international"
)

// DatasetRecords returns a sorted, independent compatibility projection for
// deterministic dataset review. AliasOf is empty because this dataset retains
// historic identifiers instead of silently aliasing them.
func DatasetRecords() []international.Record {
	records := make([]international.Record, 0, len(countryRecords))
	for id, metadata := range countryRecords {
		records = append(records, international.Record{
			ID:          id,
			Status:      metadata.status,
			Fingerprint: fingerprint(fmt.Sprintf("%s\n%03d", metadata.alpha3, metadata.numeric)),
		})
	}
	slices.SortFunc(records, func(left, right international.Record) int {
		return cmp.Compare(left.ID, right.ID)
	})
	return records
}

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

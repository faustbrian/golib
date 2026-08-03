package subdivision

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"slices"

	international "github.com/faustbrian/golib/pkg/international"
)

// DatasetRecords returns a sorted, independent compatibility projection for
// deterministic dataset review. AliasOf is empty because deleted identifiers
// remain distinct instead of being silently mapped to replacements.
func DatasetRecords() []international.Record {
	records := make([]international.Record, 0, len(subdivisionRecords))
	for id, metadata := range subdivisionRecords {
		sum := sha256.Sum256([]byte(metadata.name))
		records = append(records, international.Record{
			ID:          id,
			Status:      metadata.status,
			Fingerprint: hex.EncodeToString(sum[:]),
		})
	}
	slices.SortFunc(records, func(left, right international.Record) int {
		return cmp.Compare(left.ID, right.ID)
	})
	return records
}

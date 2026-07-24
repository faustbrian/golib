package currency

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	international "github.com/faustbrian/golib/pkg/international"
)

// DatasetRecords returns a sorted, independent compatibility projection for
// deterministic dataset review. AliasOf is empty because withdrawn identifiers
// remain distinct instead of being silently mapped to replacements.
func DatasetRecords() []international.Record {
	records := make([]international.Record, 0, len(currencyRecords))
	for id, metadata := range currencyRecords {
		canonical := fmt.Sprintf(
			"%s\n%d\n%t\n%s\n%s",
			metadata.numeric,
			metadata.minorUnits,
			metadata.hasMinorUnits,
			metadata.name,
			metadata.history,
		)
		sum := sha256.Sum256([]byte(canonical))
		records = append(records, international.Record{
			ID:          id,
			Status:      metadata.status,
			Fingerprint: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(records, func(left, right int) bool { return records[left].ID < records[right].ID })
	return records
}

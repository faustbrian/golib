package currency

import (
	"time"

	international "github.com/faustbrian/golib/pkg/international"
)

// DatasetProvenance returns the pinned SIX List One and List Three provenance.
func DatasetProvenance() international.Provenance {
	return international.Provenance{
		Dataset:         "iso-4217-current-and-historic",
		Source:          "SIX ISO 4217 List One and List Three XML",
		RetrievedAt:     time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC),
		UpstreamVersion: "ISO 4217 2026-01-01",
		License:         "SIX ISO 4217 Terms of Use",
		SHA256:          "66964b4a4c13cfbc940f351117705e5391c1fe1194b02c3db61b4c3c0c63c4e5",
		Generator:       "international-generate/v1",
		Transformations: []string{
			"deduplicate country-specific currency rows",
			"preserve official names and withdrawal text",
			"sort by alphabetic code",
		},
	}
}

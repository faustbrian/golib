package subdivision

import (
	"time"

	international "github.com/faustbrian/golib/pkg/international"
)

// DatasetProvenance returns the pinned CLDR subdivision provenance.
func DatasetProvenance() international.Provenance {
	return international.Provenance{
		Dataset:         "iso-3166-2-derived-subdivisions",
		Source:          "CLDR release-48-2 subdivision.xml and subdivisions/en.xml",
		RetrievedAt:     time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC),
		UpstreamVersion: "CLDR 48.2",
		License:         "Unicode-3.0",
		SHA256:          "5783b06a3f753acecc72668f44c00f5cc654349475238d4873cedfaef7a9262b",
		Generator:       "international-generate/v1",
		Transformations: []string{
			"expand CLDR compact validity ranges",
			"retain two-letter region and one-to-three character suffixes",
			"join non-alternate English display names",
			"sort by identifier",
		},
	}
}

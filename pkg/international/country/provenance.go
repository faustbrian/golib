package country

import (
	"time"

	international "github.com/faustbrian/golib/pkg/international"
)

// DatasetProvenance returns the pinned CLDR mapping and validity provenance.
func DatasetProvenance() international.Provenance {
	return international.Provenance{
		Dataset:         "iso-3166-country-mappings-and-status",
		Source:          "CLDR release-48-2 region.xml and supplementalData.xml",
		RetrievedAt:     time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC),
		UpstreamVersion: "CLDR 48.2",
		License:         "Unicode-3.0",
		SHA256:          "b153dccbe41ba22675a52902fa5193a2b5f8202ffe3b5f3e782edb05084381b2",
		Generator:       "international-generate/v1",
		Transformations: []string{
			"expand CLDR validity ranges",
			"join alpha-2, alpha-3, and numeric mappings",
			"exclude entries without all ISO 3166-1 representations",
			"sort by alpha-2 code",
		},
	}
}

package phone

import (
	"time"

	international "github.com/faustbrian/golib/pkg/international"
)

// DatasetProvenance returns the pinned libphonenumber-compatible provenance.
func DatasetProvenance() international.Provenance {
	return international.Provenance{
		Dataset:         "libphonenumber-metadata",
		Source:          "github.com/nyaruka/phonenumbers v1.8.1 module archive",
		RetrievedAt:     time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC),
		UpstreamVersion: "phonenumbers v1.8.1; libphonenumber v9.0.32",
		License:         "Apache-2.0",
		SHA256:          "79ff27d5ee74c223c5851d9c562751bc21863358c1b7070d3ec2ab9b0cd6a070",
		Generator:       "nyaruka/phonenumbers buildmetadata",
		Transformations: []string{
			"generate Go protobuf metadata from upstream libphonenumber XML",
			"snapshot canonical and display results into immutable values",
		},
	}
}

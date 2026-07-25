package language

import (
	"time"

	international "github.com/faustbrian/golib/pkg/international"
)

// DatasetProvenance returns the pinned IANA registry parser provenance.
func DatasetProvenance() international.Provenance {
	return international.Provenance{
		Dataset:         "iana-language-subtag-registry",
		Source:          "https://www.iana.org/assignments/language-subtag-registry",
		RetrievedAt:     time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC),
		UpstreamVersion: "IANA registry 2026-06-14; x/text v0.40.0",
		License:         "IANA Terms of Service; BSD-3-Clause",
		SHA256:          "be1fad86a99e3a932d07b80c9b3c271ec2381a5909ce22420144e5077ab0a43a",
		Generator:       "golang.org/x/text/language v0.40.0",
		Transformations: []string{
			"parse maintained BCP 47 registry tables",
			"require canonical ISO 639 representation",
		},
	}
}

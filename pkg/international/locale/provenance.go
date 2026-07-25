package locale

import (
	international "github.com/faustbrian/golib/pkg/international"
	intlLanguage "github.com/faustbrian/golib/pkg/international/language"
)

// DatasetProvenance returns the IANA and x/text parsing provenance.
func DatasetProvenance() international.Provenance {
	return intlLanguage.DatasetProvenance()
}

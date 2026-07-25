package encoding_test

import (
	"testing"

	openinghoursencoding "github.com/faustbrian/golib/pkg/opening-hours/encoding"
)

func FuzzStructuredImports(f *testing.F) {
	f.Add("monday", "09:00", "17:00", false)
	f.Add("sunday", "22:00", "02:00", true)
	f.Fuzz(func(t *testing.T, weekday, from, to string, spatie bool) {
		limits := openinghoursencoding.DefaultImportLimits()
		var encoded []byte
		var err error
		if spatie {
			schedule, importErr := openinghoursencoding.ImportSpatie(
				"UTC", map[string][]string{weekday: {from + "-" + to}}, limits,
			)
			if importErr != nil {
				return
			}
			encoded, err = schedule.CanonicalJSON()
		} else {
			schedule, importErr := openinghoursencoding.ImportLocation(
				"UTC", map[string][]openinghoursencoding.Slot{
					weekday: {{From: from, To: to}},
				}, limits,
			)
			if importErr != nil {
				return
			}
			encoded, err = schedule.CanonicalJSON()
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := openinghoursencoding.Unmarshal(encoded); err != nil {
			t.Fatalf("accepted import did not round trip: %v", err)
		}
	})
}

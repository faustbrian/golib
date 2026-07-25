package postgres_test

import (
	"testing"

	calendarpg "github.com/faustbrian/golib/pkg/calendar/postgres"
)

func FuzzDateScan(f *testing.F) {
	for _, seed := range []string{"2024-02-29", "infinity", "-infinity", "bad", string([]byte{0xff})} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		var value calendarpg.Date
		if err := value.Scan(input); err == nil && value.CalendarDate().String() != input {
			t.Fatalf("finite scan was not canonical: %q", input)
		}
		var infinity calendarpg.InfinityDate
		if err := infinity.Scan(input); err == nil {
			encoded, err := infinity.Value()
			if err != nil || encoded != input {
				t.Fatalf("infinity-aware round trip = %#v, %v", encoded, err)
			}
		}
	})
}

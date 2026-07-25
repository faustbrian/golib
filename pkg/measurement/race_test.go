package measurement_test

import (
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
)

func TestQuantitiesContextsAndProfilesAreSafeForConcurrentReuse(t *testing.T) {
	t.Parallel()

	quantity := measurement.MustNew(decimal.MustParse("123.456"), measurement.Metre)
	conversion := measurement.RoundedConversion(9, decimal.HalfEven)
	profile, err := measurement.NewProfile(map[string]measurement.Unit{"metres": measurement.Metre})
	if err != nil {
		t.Fatalf("NewProfile() error = %v", err)
	}

	var wait sync.WaitGroup
	for range 64 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			converted, err := quantity.Convert(measurement.Foot, conversion)
			if err != nil || converted.Unit() != measurement.Foot {
				t.Errorf("Convert() = %s, %v", converted, err)
			}
			parsed, err := measurement.Parse("123.456 metres", profile)
			if err != nil || parsed.String() != quantity.String() {
				t.Errorf("Parse() = %s, %v", parsed, err)
			}
		}()
	}
	wait.Wait()
}

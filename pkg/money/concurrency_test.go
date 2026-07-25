package money_test

import (
	"context"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/international/locale"
	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/money"
	moneyformat "github.com/faustbrian/golib/pkg/money/format"
)

func TestSharedValuesAndFormattersAreRaceSafe(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := money.DefaultContext(euro)
	value, _ := money.Parse("1234.56", euro, monetaryContext)
	rate, _ := money.ParseRate("1/3")
	finnish, _ := locale.Parse("fi-FI")

	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				exact, err := value.Mul(context.Background(), rate)
				if err != nil {
					t.Error(err)
					return
				}
				if _, _, err := exact.Round(monetaryContext, gomath.RoundHalfEven); err != nil {
					t.Error(err)
					return
				}
				if _, err := moneyformat.Locale(value, finnish, moneyformat.Options{Symbol: true}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wait.Wait()
}

package money

import (
	"context"
	"testing"

	rhymond "github.com/Rhymond/go-money"
	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/math/integer"
	govalues "github.com/govalues/money"
)

func TestFixedScaleArithmeticMatchesMatureImplementations(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	for leftUnits := int64(-200); leftUnits <= 200; leftUnits += 17 {
		for rightUnits := int64(-200); rightUnits <= 200; rightUnits += 19 {
			left, _ := FromMinorUnits(integer.New(leftUnits), euro, monetaryContext)
			right, _ := FromMinorUnits(integer.New(rightUnits), euro, monetaryContext)
			govaluesLeft, _ := govalues.ParseAmount("EUR", decimalTextFromMinor(integer.New(leftUnits).String(), 2))
			govaluesRight, _ := govalues.ParseAmount("EUR", decimalTextFromMinor(integer.New(rightUnits).String(), 2))
			rhymondLeft := rhymond.New(leftUnits, rhymond.EUR)
			rhymondRight := rhymond.New(rightUnits, rhymond.EUR)

			localSum, localErr := left.Add(right)
			govaluesSum, govaluesErr := govaluesLeft.Add(govaluesRight)
			rhymondSum, rhymondErr := rhymondLeft.Add(rhymondRight)
			assertDifferentialUnits(
				t,
				"add",
				leftUnits+rightUnits,
				localSum,
				localErr,
				govaluesSum,
				govaluesErr,
				rhymondSum,
				rhymondErr,
			)

			localDifference, localErr := left.Sub(right)
			govaluesDifference, govaluesErr := govaluesLeft.Sub(govaluesRight)
			rhymondDifference, rhymondErr := rhymondLeft.Subtract(rhymondRight)
			assertDifferentialUnits(
				t,
				"subtract",
				leftUnits-rightUnits,
				localDifference,
				localErr,
				govaluesDifference,
				govaluesErr,
				rhymondDifference,
				rhymondErr,
			)
		}
	}
}

func TestEqualSplitMatchesMatureImplementationsAcrossSigns(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	for totalUnits := int64(-40); totalUnits <= 40; totalUnits++ {
		for count := 1; count <= 9; count++ {
			local, _ := FromMinorUnits(integer.New(totalUnits), euro, monetaryContext)
			localParts, localErr := local.EqualSplit(context.Background(), count)
			govaluesTotal, _ := govalues.ParseAmount(
				"EUR",
				decimalTextFromMinor(integer.New(totalUnits).String(), 2),
			)
			govaluesParts, govaluesErr := govaluesTotal.Split(count)
			rhymondParts, rhymondErr := rhymond.New(totalUnits, rhymond.EUR).Split(count)
			if localErr != nil || govaluesErr != nil || rhymondErr != nil {
				t.Fatalf("split(%d, %d) errors = %v, %v, %v", totalUnits, count, localErr, govaluesErr, rhymondErr)
			}
			parts := localParts.Parts()
			for index := range parts {
				localUnits, _ := parts[index].MinorUnits()
				govaluesUnits, ok := govaluesParts[index].MinorUnits()
				if !ok || localUnits.String() != integer.New(govaluesUnits).String() || localUnits.String() != integer.New(rhymondParts[index].Amount()).String() {
					t.Fatalf(
						"split(%d, %d)[%d] = %s, %d, %d",
						totalUnits,
						count,
						index,
						localUnits,
						govaluesUnits,
						rhymondParts[index].Amount(),
					)
				}
			}
		}
	}
}

func TestCurrencyMismatchMatchesMatureImplementations(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	euroContext, _ := DefaultContext(euro)
	dollarContext, _ := DefaultContext(dollar)
	euros, _ := Parse("1.00", euro, euroContext)
	dollars, _ := Parse("1.00", dollar, dollarContext)
	if _, err := euros.Add(dollars); err == nil {
		t.Fatal("money accepted cross-currency addition")
	}
	govaluesEuro, _ := govalues.ParseAmount("EUR", "1.00")
	govaluesDollar, _ := govalues.ParseAmount("USD", "1.00")
	if _, err := govaluesEuro.Add(govaluesDollar); err == nil {
		t.Fatal("govalues/money accepted cross-currency addition")
	}
	if _, err := rhymond.New(100, rhymond.EUR).Add(rhymond.New(100, rhymond.USD)); err == nil {
		t.Fatal("Rhymond/money accepted cross-currency addition")
	}
}

func assertDifferentialUnits(
	t *testing.T,
	operation string,
	want int64,
	local Money,
	localErr error,
	govaluesAmount govalues.Amount,
	govaluesErr error,
	rhymondAmount *rhymond.Money,
	rhymondErr error,
) {
	t.Helper()
	if localErr != nil || govaluesErr != nil || rhymondErr != nil {
		t.Fatalf("%s errors = %v, %v, %v", operation, localErr, govaluesErr, rhymondErr)
	}
	localUnits, err := local.MinorUnits()
	if err != nil {
		t.Fatal(err)
	}
	govaluesUnits, ok := govaluesAmount.MinorUnits()
	if !ok || localUnits.String() != integer.New(want).String() || govaluesUnits != want || rhymondAmount.Amount() != want {
		t.Fatalf(
			"%s units = %s, %d, %d; want %d",
			operation,
			localUnits,
			govaluesUnits,
			rhymondAmount.Amount(),
			want,
		)
	}
}

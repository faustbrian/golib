package international_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/international/locale"
	"github.com/faustbrian/golib/pkg/international/phone"
)

func BenchmarkCountryLookup(benchmark *testing.B) {
	for benchmark.Loop() {
		_, _ = country.Parse("FI")
	}
}

func BenchmarkLocaleCanonicalization(benchmark *testing.B) {
	for benchmark.Loop() {
		tag, _ := locale.Parse("zh-hant-tw-u-ca-gregory")
		_, _ = tag.Canonical()
	}
}

func BenchmarkCurrencyLookup(benchmark *testing.B) {
	for benchmark.Loop() {
		_, _ = currency.Parse("EUR")
	}
}

func BenchmarkPhoneParse(benchmark *testing.B) {
	for benchmark.Loop() {
		_, _ = phone.ParseE164("+16502530000")
	}
}

func BenchmarkBulkCountryConversion(benchmark *testing.B) {
	codes := country.All()
	benchmark.ResetTimer()
	for benchmark.Loop() {
		for _, code := range codes {
			_, _ = code.Alpha3()
		}
	}
}

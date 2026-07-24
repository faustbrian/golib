package correlation_test

import (
	"strconv"
	"strings"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
)

type benchmarkGenerator uint64

func (generator *benchmarkGenerator) New() (string, error) {
	*generator++
	return "id_" + strconv.FormatUint(uint64(*generator), 10), nil
}

func BenchmarkCarrierRejectsOversizedValue(b *testing.B) {
	codec, _ := correlation.NewCodec(correlation.CodecOptions{})
	carrier := memoryCarrier{
		correlation.DefaultCorrelationField: {strings.Repeat("a", 4096)},
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := codec.Extract(carrier); err == nil {
			b.Fatal("oversized carrier was accepted")
		}
	}
}

func BenchmarkFactoryNext(b *testing.B) {
	generator := new(benchmarkGenerator)
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{Generator: generator})
	parent := correlation.Values{CorrelationID: "flow", RequestID: "parent"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := factory.Next(parent); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCarrierRoundTrip(b *testing.B) {
	codec, _ := correlation.NewCodec(correlation.CodecOptions{})
	values := correlation.Values{CorrelationID: "flow", RequestID: "request", CausationID: "parent"}
	b.ReportAllocs()
	for b.Loop() {
		carrier := memoryCarrier{}
		if err := codec.Inject(carrier, values); err != nil {
			b.Fatal(err)
		}
		if _, err := codec.Extract(carrier); err != nil {
			b.Fatal(err)
		}
	}
}

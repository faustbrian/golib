package authotel_test

import (
	"context"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authotel"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func BenchmarkStartAndFinish(b *testing.B) {
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: tracenoop.NewTracerProvider(),
		MeterProvider:  metricnoop.NewMeterProvider(),
	})
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	event := authentication.Event{
		Outcome:  authentication.OutcomeAuthenticated,
		Duration: time.Millisecond,
	}

	b.ReportAllocs()
	for b.Loop() {
		_, finish := instrumenter.Start(ctx, authentication.CredentialBearer)
		finish(event)
	}
}

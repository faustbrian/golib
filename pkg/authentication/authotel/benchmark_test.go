package authotel_test

import (
	"context"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authotel"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

var (
	benchmarkAuthenticationResult authentication.Result
	benchmarkAuthenticationError  error
)

func BenchmarkAuthenticationInstrumentation(b *testing.B) {
	base := authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
		return authentication.AnonymousResult(), nil
	})
	cases := []struct {
		name  string
		build func(*testing.B) authentication.Authenticator
	}{
		{name: "direct", build: func(b *testing.B) authentication.Authenticator {
			return instrumentedBenchmarkAuthenticator(b, base, directInstrumenter{})
		}},
		{name: "opentelemetry_noop", build: func(b *testing.B) authentication.Authenticator {
			return instrumentedBenchmarkAuthenticator(b, base, newBenchmarkInstrumenter(
				b,
				tracenoop.NewTracerProvider(),
				metricnoop.NewMeterProvider(),
			))
		}},
		{name: "opentelemetry_sampled_out", build: func(b *testing.B) authentication.Authenticator {
			tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
			b.Cleanup(func() {
				_ = tracerProvider.Shutdown(context.Background())
				_ = meterProvider.Shutdown(context.Background())
			})
			return instrumentedBenchmarkAuthenticator(
				b,
				base,
				newBenchmarkInstrumenter(b, tracerProvider, meterProvider),
			)
		}},
		{name: "opentelemetry_enabled", build: func(b *testing.B) authentication.Authenticator {
			tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
			b.Cleanup(func() {
				_ = tracerProvider.Shutdown(context.Background())
				_ = meterProvider.Shutdown(context.Background())
			})
			return instrumentedBenchmarkAuthenticator(
				b,
				base,
				newBenchmarkInstrumenter(b, tracerProvider, meterProvider),
			)
		}},
	}

	credential := authentication.NewBearerCredential("benchmark-token")
	ctx := context.Background()
	for _, test := range cases {
		b.Run(test.name, func(b *testing.B) {
			authenticator := test.build(b)
			b.ReportAllocs()
			_, _ = authenticator.Authenticate(ctx, credential)
			b.ResetTimer()
			for b.Loop() {
				benchmarkAuthenticationResult, benchmarkAuthenticationError = authenticator.Authenticate(ctx, credential)
			}
		})
	}
}

type directInstrumenter struct{}

func (directInstrumenter) Start(ctx context.Context, _ authentication.CredentialKind) (
	context.Context,
	func(authentication.Event),
) {
	return ctx, noopAuthenticationFinish
}

func noopAuthenticationFinish(authentication.Event) {}

func newBenchmarkInstrumenter(
	b *testing.B,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
) authentication.Instrumenter {
	b.Helper()
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
	})
	if err != nil {
		b.Fatalf("authotel.New() error = %v", err)
	}
	return instrumenter
}

func instrumentedBenchmarkAuthenticator(
	b *testing.B,
	base authentication.Authenticator,
	instrumenter authentication.Instrumenter,
) authentication.Authenticator {
	b.Helper()
	authenticator, err := authentication.NewInstrumented(base, instrumenter, fixedClock{})
	if err != nil {
		b.Fatalf("authentication.NewInstrumented() error = %v", err)
	}
	return authenticator
}

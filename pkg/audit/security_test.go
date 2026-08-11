package audit_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
)

type rejectingSink struct{}

func (rejectingSink) Append(context.Context, audit.Record) (audit.AppendResult, error) {
	return audit.AppendResult{}, nil
}

func (rejectingSink) AppendBatch(context.Context, []audit.Record) (audit.BatchResult, error) {
	return audit.BatchResult{}, nil
}

func TestBuilderRejectsPartialIntegrityMetadata(t *testing.T) {
	t.Parallel()

	tests := map[string]audit.IntegrityInput{
		"algorithm only": {Algorithm: audit.IntegritySHA256},
		"partition only": {Partition: "tenant-1"},
		"key only":       {KeyID: "key-1"},
		"sequence only":  {Sequence: 1},
	}
	for name, integrity := range tests {
		integrity := integrity
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := securityBuilder(t).Build(securityInput(integrity, nil))
			if !errors.Is(err, audit.ErrInvalidArgument) {
				t.Fatalf("Build() error = %v", err)
			}
		})
	}
}

func TestBuilderRejectsUnrestrictedBodyFields(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"request_body", "response.body", "raw-body", "http.request.body",
		"password_hint", "pass_word", "api-key", "cred.ential",
	} {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			_, err := securityBuilder(t).Build(securityInput(audit.IntegrityInput{}, map[string]string{key: "private payload"}))
			if !errors.Is(err, audit.ErrSensitiveData) {
				t.Fatalf("Build() error = %v", err)
			}
		})
	}
}

func TestBuilderRejectsCredentialsInAuthenticationMethod(t *testing.T) {
	t.Parallel()

	credentialShaped := strings.Join([]string{
		"eyJhbGciOiJIUzI1NiJ9", "eyJzdWIiOiJ1c2VyIn0", "signature",
	}, ".")
	for _, method := range []string{
		"Authorization: Bearer secret",
		"Basic dXNlcjpwYXNzd29yZA==",
		"password=hunter2",
		credentialShaped,
		strings.Repeat("0123456789abcdef", 2),
		"a", "z", "A", "Z", "0", "9", ".", "_", "-",
		strings.Repeat("a", 128),
	} {
		method := method
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			input := securityInput(audit.IntegrityInput{}, nil)
			input.Actor.AuthenticationMethod = method
			_, err := securityBuilder(t).Build(input)
			if !errors.Is(err, audit.ErrSensitiveData) {
				t.Fatalf("Build() error = %v", err)
			}
		})
	}
	input := securityInput(audit.IntegrityInput{}, nil)
	input.Actor.AuthenticationMethod = strings.Repeat("a", 129)
	if _, err := securityBuilder(t).Build(input); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("oversized authentication method error = %v", err)
	}

	for _, method := range []string{
		"", audit.AuthenticationMethodAPIKey,
		audit.AuthenticationMethodCertificate, audit.AuthenticationMethodEmailOTP,
		audit.AuthenticationMethodHOTP, audit.AuthenticationMethodMutualTLS,
		audit.AuthenticationMethodMagicLink, audit.AuthenticationMethodOAuth2,
		audit.AuthenticationMethodOIDC, audit.AuthenticationMethodPasskey,
		audit.AuthenticationMethodPassword, audit.AuthenticationMethodRecoveryCode,
		audit.AuthenticationMethodSAML, audit.AuthenticationMethodSession,
		audit.AuthenticationMethodSMSOTP, audit.AuthenticationMethodTOTP,
		audit.AuthenticationMethodWebAuthn,
		audit.AuthenticationMethodWorkloadIdentity,
	} {
		input := securityInput(audit.IntegrityInput{}, nil)
		input.Actor.AuthenticationMethod = method
		if _, err := securityBuilder(t).Build(input); err != nil {
			t.Fatalf("safe method %q error = %v", method, err)
		}
	}
	for _, method := range []string{"`", "{", "@", "[", "/", ":"} {
		input := securityInput(audit.IntegrityInput{}, nil)
		input.Actor.AuthenticationMethod = method
		if _, err := securityBuilder(t).Build(input); !errors.Is(err, audit.ErrSensitiveData) {
			t.Fatalf("unsafe method %q error = %v", method, err)
		}
	}
}

func TestRecorderDoesNotExposeRedactionDiagnostics(t *testing.T) {
	t.Parallel()

	cause := errors.New("request body contained password=hunter2")
	recorder, err := audit.NewRecorder(audit.RecorderConfig{
		Sink: rejectingSink{},
		Redactor: audit.RedactorFunc(func(context.Context, audit.Record) (audit.Record, error) {
			return audit.Record{}, cause
		}),
		Mode: audit.DeliveryFailClosed,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = recorder.Submit(context.Background(), mustSecurityRecord(t))
	if errors.Is(err, cause) {
		t.Fatalf("Submit() retained secret-bearing cause: %v", err)
	}
	if strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "password") {
		t.Fatalf("Submit() exposed redaction diagnostic: %v", err)
	}
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		t.Fatalf("Submit() exposed unwrap diagnostic: %v", unwrapped)
	}
}

func TestRecorderDoesNotExposeSinkAlertOrBufferDiagnostics(t *testing.T) {
	t.Parallel()

	record := mustSecurityRecord(t)
	primarySecret := errors.New("primary password=primary-secret")
	alertSecret := errors.New("pager token=alert-secret")
	alertRecorder, err := audit.NewRecorder(audit.RecorderConfig{
		Sink: failingSink(primarySecret), Redactor: passthroughRedactor(), Mode: audit.DeliveryFailOpenWithAlert,
		Alerter: audit.AlertFunc(func(context.Context, audit.DeliveryAlert) error { return alertSecret }), RecoveryTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := alertRecorder.Submit(context.Background(), record); err == nil || strings.Contains(err.Error(), "secret") || errors.Unwrap(err) != nil {
		t.Fatalf("alert failure exposed diagnostic = %v", err)
	}

	bufferSecret := errors.New("buffer credential=buffer-secret")
	bufferRecorder, err := audit.NewRecorder(audit.RecorderConfig{
		Sink: failingSink(primarySecret), Redactor: passthroughRedactor(), Mode: audit.DeliveryDurableBuffer,
		Buffer: testBuffer(failingSink(bufferSecret)), RecoveryTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bufferRecorder.Submit(context.Background(), record); err == nil || strings.Contains(err.Error(), "secret") || errors.Unwrap(err) != nil {
		t.Fatalf("buffer failure exposed diagnostic = %v", err)
	}
}

func TestRecorderContainsSecretBearingDependencyPanics(t *testing.T) {
	t.Parallel()

	record := mustSecurityRecord(t)
	panicValue := "password=panic-secret"
	panicSink := sinkFunc{
		append: func(context.Context, audit.Record) (audit.AppendResult, error) {
			panic(panicValue)
		},
		appendBatch: func(context.Context, []audit.Record) (audit.BatchResult, error) {
			panic(panicValue)
		},
	}
	primaryFailure := failingSink(audit.NewAppendError(audit.AppendRejected, errors.New("primary failed")))
	cases := []struct {
		name string
		call func() error
	}{
		{"redactor", func() error {
			recorder, _ := audit.NewRecorder(audit.RecorderConfig{
				Sink: acceptingSink(audit.AppendAccepted), Mode: audit.DeliveryFailClosed,
				Redactor: audit.RedactorFunc(func(context.Context, audit.Record) (audit.Record, error) { panic(panicValue) }),
			})
			_, err := recorder.Submit(context.Background(), record)
			return err
		}},
		{"sink", func() error {
			recorder, _ := audit.NewRecorder(audit.RecorderConfig{Sink: panicSink, Redactor: passthroughRedactor(), Mode: audit.DeliveryFailClosed})
			_, err := recorder.SubmitBatch(context.Background(), []audit.Record{record})
			return err
		}},
		{"sink-single", func() error {
			recorder, _ := audit.NewRecorder(audit.RecorderConfig{Sink: panicSink, Redactor: passthroughRedactor(), Mode: audit.DeliveryFailClosed})
			_, err := recorder.Submit(context.Background(), record)
			return err
		}},
		{"alerter", func() error {
			recorder, _ := audit.NewRecorder(audit.RecorderConfig{
				Sink: primaryFailure, Redactor: passthroughRedactor(), Mode: audit.DeliveryFailOpenWithAlert,
				Alerter: audit.AlertFunc(func(context.Context, audit.DeliveryAlert) error { panic(panicValue) }), RecoveryTimeout: time.Second,
			})
			_, err := recorder.Submit(context.Background(), record)
			return err
		}},
		{"buffer", func() error {
			recorder, _ := audit.NewRecorder(audit.RecorderConfig{
				Sink: primaryFailure, Redactor: passthroughRedactor(), Mode: audit.DeliveryDurableBuffer,
				Buffer: testBuffer(panicSink), RecoveryTimeout: time.Second,
			})
			_, err := recorder.SubmitBatch(context.Background(), []audit.Record{record})
			return err
		}},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("dependency panic escaped: %v", recovered)
				}
			}()
			err := test.call()
			if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "password") {
				t.Fatalf("contained panic error = %v", err)
			}
		})
	}
}

func TestIntegrityContainsSecretBearingKeyProviderPanic(t *testing.T) {
	t.Parallel()

	chain, err := audit.NewChain(audit.ChainConfig{
		Algorithm: audit.IntegrityHMACSHA256,
		Keys: audit.KeyProviderFunc(func(context.Context, audit.KeyRequest) (audit.IntegrityKey, error) {
			panic("private-key=panic-secret")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("key-provider panic escaped: %v", recovered)
		}
	}()
	_, err = chain.Seal(context.Background(), mustSecurityRecord(t), audit.ChainLink{Partition: "tenant-1", Sequence: 1})
	if !errors.Is(err, audit.ErrKeyUnavailable) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "key=") {
		t.Fatalf("contained key-provider panic error = %v", err)
	}
}

func TestBuilderContainsSecretBearingConstructionDependencies(t *testing.T) {
	t.Parallel()

	input := securityInput(audit.IntegrityInput{}, nil)
	secret := errors.New("entropy token=builder-secret")
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock:       time.Now,
		IDGenerator: func() (string, error) { return "", secret },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Build(input); !errors.Is(err, audit.ErrRecordIDUnavailable) || strings.Contains(err.Error(), "secret") || errors.Unwrap(err) != nil {
		t.Fatalf("ID generator failure exposed diagnostic = %v", err)
	}

	for name, test := range map[string]struct {
		config audit.BuilderConfig
		want   error
	}{
		"clock": {config: audit.BuilderConfig{
			Clock:       func() time.Time { panic("clock password=builder-secret") },
			IDGenerator: func() (string, error) { return "clock-panic", nil },
		}, want: audit.ErrClockUnavailable},
		"ID generator": {config: audit.BuilderConfig{
			Clock:       time.Now,
			IDGenerator: func() (string, error) { panic("generator password=builder-secret") },
		}, want: audit.ErrRecordIDUnavailable},
	} {
		builder, err := audit.NewBuilder(test.config)
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("%s panic escaped: %v", name, recovered)
				}
			}()
			if _, err := builder.Build(input); !errors.Is(err, test.want) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("%s panic error = %v", name, err)
			}
		}()
	}
}

type panicLimitsBuffer struct{ sinkFunc }

func (panicLimitsBuffer) BufferLimits() audit.BufferLimits {
	panic("buffer password=configuration-secret")
}

func TestRecorderContainsSecretBearingConfigurationDependencies(t *testing.T) {
	t.Parallel()

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("recorder configuration panic escaped: %v", recovered)
		}
	}()
	if _, err := audit.NewRecorder(audit.RecorderConfig{
		Sink: acceptingSink(audit.AppendAccepted), Redactor: passthroughRedactor(),
		Mode: audit.DeliveryDurableBuffer, Buffer: panicLimitsBuffer{}, RecoveryTimeout: time.Second,
	}); !errors.Is(err, audit.ErrInvalidArgument) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("panicking BufferLimits error = %v", err)
	}

	recorder, err := audit.NewRecorder(audit.RecorderConfig{
		Sink: acceptingSink(audit.AppendAccepted), Redactor: passthroughRedactor(),
		Mode: audit.DeliveryFailClosed, DelayThreshold: time.Second,
		Clock: func() time.Time { panic("clock token=observation-secret") },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := recorder.Submit(context.Background(), mustSecurityRecord(t))
	if err != nil || result.Disposition != audit.DeliveryPersisted {
		t.Fatalf("panicking observation clock Submit() = %#v, %v", result, err)
	}
}

type panicClassificationError struct{}

func (panicClassificationError) Error() string { return "token=classification-secret" }
func (panicClassificationError) Is(error) bool { panic("classification Is password=secret") }
func (panicClassificationError) As(any) bool   { panic("classification As password=secret") }

func TestDependencyErrorClassificationPanicsAreContained(t *testing.T) {
	t.Parallel()

	dependencyErr := panicClassificationError{}
	record := mustSecurityRecord(t)
	query, _ := audit.NewQuery(audit.QueryInput{Tenant: audit.AllTenants(), Limit: 1})
	cases := []struct {
		name string
		call func() error
	}{
		{name: "redactor", call: func() error {
			recorder, _ := audit.NewRecorder(audit.RecorderConfig{
				Sink: acceptingSink(audit.AppendAccepted), Mode: audit.DeliveryFailClosed,
				Redactor: audit.RedactorFunc(func(context.Context, audit.Record) (audit.Record, error) {
					return audit.Record{}, dependencyErr
				}),
			})
			_, err := recorder.Submit(context.Background(), record)
			return err
		}},
		{name: "sink", call: func() error {
			recorder, _ := audit.NewRecorder(audit.RecorderConfig{
				Sink: failingSink(dependencyErr), Redactor: passthroughRedactor(), Mode: audit.DeliveryFailClosed,
			})
			_, err := recorder.Submit(context.Background(), record)
			return err
		}},
		{name: "exporter", call: func() error {
			exporter, _ := audit.NewObservedExporter(exporterFunc(func(context.Context, audit.Query, func(audit.Record) error) error {
				return dependencyErr
			}), audit.ObserverFunc(func(context.Context, audit.Observation) {}))
			return exporter.Export(context.Background(), query, func(audit.Record) error { return nil })
		}},
		{name: "key provider", call: func() error {
			chain, _ := audit.NewChain(audit.ChainConfig{
				Algorithm: audit.IntegrityHMACSHA256,
				Keys: audit.KeyProviderFunc(func(context.Context, audit.KeyRequest) (audit.IntegrityKey, error) {
					return audit.IntegrityKey{}, dependencyErr
				}),
			})
			_, err := chain.Seal(context.Background(), record, audit.ChainLink{Partition: "tenant-1", Sequence: 1})
			return err
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("dependency error classification panic escaped: %v", recovered)
				}
			}()
			err := test.call()
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("contained dependency classification error = %v", err)
			}
			_ = errors.Is(err, audit.ErrBackpressure)
		})
	}
	if outcome := audit.AppendOutcomeOf(dependencyErr); outcome != audit.AppendUnknown {
		t.Fatalf("AppendOutcomeOf(panicking error) = %v", outcome)
	}
}

func securityBuilder(t *testing.T) *audit.Builder {
	t.Helper()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	builder, err := audit.NewBuilder(audit.BuilderConfig{
		Clock:       func() time.Time { return now },
		IDGenerator: func() (string, error) { return "security-record", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return builder
}

func securityInput(integrity audit.IntegrityInput, attributes map[string]string) audit.RecordInput {
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	return audit.RecordInput{
		OccurredAt: now,
		Action:     "account.viewed",
		Outcome:    audit.OutcomeSucceeded,
		Actor:      audit.ActorInput{Kind: audit.ActorSystem, ID: "billing"},
		Subject:    audit.SubjectInput{Type: "account", ID: "account-1"},
		Changes:    audit.ChangeSetInput{NoChange: true},
		Attributes: attributes,
		Integrity:  integrity,
	}
}

func mustSecurityRecord(t *testing.T) audit.Record {
	t.Helper()
	record, err := securityBuilder(t).Build(securityInput(audit.IntegrityInput{}, nil))
	if err != nil {
		t.Fatal(err)
	}
	return record
}

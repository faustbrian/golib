package contracts_test

import (
	"context"
	"log/slog"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/authlog"
	"github.com/faustbrian/golib/pkg/authorization/authn"
	"github.com/faustbrian/golib/pkg/authorization/authotel"
	log "github.com/faustbrian/golib/pkg/log"
	"github.com/faustbrian/golib/pkg/log/handler/capture"
	"github.com/faustbrian/golib/pkg/telemetry/testtelemetry"
)

func TestOwnedModuleInteroperability(t *testing.T) {
	t.Parallel()

	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{
		Subject: "alice",
		Method:  "oidc",
		Claims: map[string]any{
			"department": "finance",
			"groups":     []string{"reviewers"},
			"labels":     []string{"audited", "trusted"},
		},
	})
	if err != nil {
		t.Fatalf("authentication.NewPrincipal() error = %v", err)
	}
	subject, err := authn.Subject(principal, authn.Config{
		Kind:        authorization.SubjectUser,
		GroupsClaim: "groups",
		AttributeClaims: map[authorization.AttributeName]string{
			"department": "department",
			"labels":     "labels",
		},
	})
	if err != nil {
		t.Fatalf("authn.Subject() error = %v", err)
	}
	if subject.ID != "alice" || len(subject.Groups) != 1 ||
		subject.Groups[0] != "reviewers" {
		t.Fatalf("mapped subject = %+v", subject)
	}
	labels, ok := subject.Attributes["labels"].StringSet()
	if !ok || len(labels) != 2 || labels[0] != "audited" {
		t.Fatalf("mapped labels = %v, %v", labels, ok)
	}

	handler := capture.New()
	logger, err := log.New(handler)
	if err != nil {
		t.Fatalf("log.New() error = %v", err)
	}
	logInstrumenter, err := authlog.New(logger, slog.LevelInfo)
	if err != nil {
		t.Fatalf("authlog.New() error = %v", err)
	}
	_, finishLog := logInstrumenter.Start(context.Background())
	finishLog(authorization.Event{Outcome: authorization.Allow, Revision: 1})
	if handler.Len() != 1 {
		t.Fatalf("captured audit events = %d, want 1", handler.Len())
	}

	telemetry := testtelemetry.New()
	t.Cleanup(func() {
		if err := telemetry.Shutdown(context.Background()); err != nil {
			t.Errorf("telemetry.Shutdown() error = %v", err)
		}
	})
	telemetryInstrumenter, err := authotel.New(authotel.Config{
		TracerProvider: telemetry.TracerProvider(),
		MeterProvider:  telemetry.MeterProvider(),
	})
	if err != nil {
		t.Fatalf("authotel.New() error = %v", err)
	}
	_, finishTelemetry := telemetryInstrumenter.Start(context.Background())
	finishTelemetry(authorization.Event{Outcome: authorization.Allow, Revision: 1})
	if len(telemetry.Spans()) != 1 {
		t.Fatalf("recorded authorization spans = %d, want 1", len(telemetry.Spans()))
	}
	if _, err := telemetry.Metrics(context.Background()); err != nil {
		t.Fatalf("telemetry.Metrics() error = %v", err)
	}
}

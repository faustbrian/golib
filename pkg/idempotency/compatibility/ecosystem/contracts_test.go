package ecosystem_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	idempotency "github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencylog"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencyoutbox"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencyqueue"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytelemetry"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
	idempotencypostgres "github.com/faustbrian/golib/pkg/idempotency/postgres"
	log "github.com/faustbrian/golib/pkg/log"
	migrations "github.com/faustbrian/golib/pkg/migrations"
	"github.com/faustbrian/golib/pkg/outbox"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	queuecore "github.com/faustbrian/golib/pkg/queue/core"
	telemetry "github.com/faustbrian/golib/pkg/telemetry"
	webhook "github.com/faustbrian/golib/pkg/webhook"
	webhookidempotency "github.com/faustbrian/golib/pkg/webhook/adapters/goidempotency"
)

func TestPublishedEcosystemContractsCompile(t *testing.T) {
	var _ idempotencyoutbox.Writer[outbox.Envelope] = (*outboxpostgres.Writer)(nil)
	var _ idempotencyoutbox.Completer = (*idempotencypostgres.Store)(nil)
	var _ idempotencyqueue.Message = (queuecore.TaskMessage)(nil)
	var _ webhook.ReplayStore = (*webhookidempotency.Store)(nil)
	var _ = idempotencyqueue.Wrap[queuecore.TaskMessage]

	logger, err := log.New(slog.NewTextHandler(io.Discard, nil))
	if err != nil {
		t.Fatalf("log.New() error = %v", err)
	}
	if _, err := idempotencylog.New(logger); err != nil {
		t.Fatalf("idempotencylog.New() error = %v", err)
	}

	_ = bindTelemetry
	_ = bindMigration
}

func TestWebhookReplayStoreUsesDurableIdempotency(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	clock := idempotencytest.NewClock(now)
	tokens := idempotencytest.NewTokenSource("webhook")
	backend, err := memory.New(memory.Options{
		Clock:       clock,
		OwnerTokens: tokens.Next,
	})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	service, err := idempotency.NewService(backend)
	if err != nil {
		t.Fatalf("idempotency.NewService() error = %v", err)
	}
	store, err := webhookidempotency.New(webhookidempotency.Config{
		Service:   service,
		Namespace: "webhooks",
		Tenant:    "tenant-1",
		Operation: "receive",
		Caller:    "provider",
		Clock:     clock.Now,
	})
	if err != nil {
		t.Fatalf("goidempotency.New() error = %v", err)
	}

	expiresAt := now.Add(5 * time.Minute)
	recorded, err := store.CheckAndRecord(context.Background(), "delivery-1", expiresAt)
	if err != nil || !recorded {
		t.Fatalf("first CheckAndRecord() = (%v, %v), want (true, nil)", recorded, err)
	}
	recorded, err = store.CheckAndRecord(context.Background(), "delivery-1", expiresAt)
	if err != nil || recorded {
		t.Fatalf("replay CheckAndRecord() = (%v, %v), want (false, nil)", recorded, err)
	}
}

func bindMigration() (migrations.Migration, error) {
	return idempotencypostgres.GoMigration()
}

func bindTelemetry(runtime *telemetry.Runtime) (*idempotencytelemetry.Observer, error) {
	return idempotencytelemetry.New(runtime.MeterProvider())
}

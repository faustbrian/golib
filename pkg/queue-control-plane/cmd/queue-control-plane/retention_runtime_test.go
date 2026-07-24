package main

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	gopostgres "github.com/faustbrian/golib/pkg/postgres"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
	"github.com/jackc/pgx/v5"
)

func TestExecuteRetentionLoadsAppliesAndClosesResources(t *testing.T) {
	t.Parallel()

	document := `{"tenants":[` + retentionPolicyJSON("tenant-1") + `]}`
	pool := lazyProcessPool(t)
	audit := &retentionAuditStub{}
	err := executeRetention(
		context.Background(),
		"postgres://database/control",
		"/etc/control/retention.json",
		2048,
		func(path string) (io.ReadCloser, error) {
			if path != "/etc/control/retention.json" {
				t.Fatalf("retention path = %q", path)
			}

			return io.NopCloser(strings.NewReader(document)), nil
		},
		func(_ context.Context, config gopostgres.Config) (*gopostgres.Pool, error) {
			if config.DSN != "postgres://database/control" {
				t.Fatalf("retention DSN = %q", config.DSN)
			}

			return pool, nil
		},
		func(got *gopostgres.Pool) (retentionAudit, error) {
			if got != pool {
				t.Fatalf("retention pool = %p, want %p", got, pool)
			}

			return audit, nil
		},
		func() time.Time { return time.Unix(100000, 0) },
	)
	if err != nil || !reflect.DeepEqual(audit.verified, []string{"tenant-1", "tenant-1"}) {
		t.Fatalf("executeRetention() = %v, verified = %v", err, audit.verified)
	}
	if !errors.Is(pool.Liveness().Err, gopostgres.ErrPoolClosed) {
		t.Fatalf("pool liveness = %+v, want closed", pool.Liveness())
	}
}

func TestExecuteRetentionPropagatesEveryFailure(t *testing.T) {
	t.Parallel()

	stageErr := errors.New("stage failed")
	validDocument := `{"tenants":[` + retentionPolicyJSON("tenant-1") + `]}`
	validOpen := func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(validDocument)), nil
	}
	validPool := func(context.Context, gopostgres.Config) (*gopostgres.Pool, error) {
		return lazyProcessPool(t), nil
	}
	validAudit := func(*gopostgres.Pool) (retentionAudit, error) {
		return &retentionAuditStub{}, nil
	}
	for name, test := range map[string]struct {
		open  retentionFileOpener
		pool  func(context.Context, gopostgres.Config) (*gopostgres.Pool, error)
		audit func(*gopostgres.Pool) (retentionAudit, error)
		want  error
	}{
		"open": {
			open: func(string) (io.ReadCloser, error) { return nil, stageErr },
			want: stageErr,
		},
		"document": {
			open: func(string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(`{}`)), nil
			},
			want: ErrInvalidRetentionDocument,
		},
		"pool": {
			open: validOpen,
			pool: func(context.Context, gopostgres.Config) (*gopostgres.Pool, error) {
				return nil, stageErr
			},
			want: stageErr,
		},
		"audit": {
			open: validOpen,
			pool: validPool,
			audit: func(*gopostgres.Pool) (retentionAudit, error) {
				return nil, stageErr
			},
			want: stageErr,
		},
		"apply": {
			open: validOpen,
			pool: validPool,
			audit: func(*gopostgres.Pool) (retentionAudit, error) {
				return &retentionAuditStub{verifyErr: stageErr}, nil
			},
			want: stageErr,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			open := test.open
			if open == nil {
				open = validOpen
			}
			pool := test.pool
			if pool == nil {
				pool = validPool
			}
			audit := test.audit
			if audit == nil {
				audit = validAudit
			}
			err := executeRetention(
				context.Background(),
				"postgres://database/control",
				"retention.json",
				2048,
				open,
				pool,
				audit,
				time.Now,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("executeRetention() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestExecuteProductionRetentionFailsWithoutPolicyFile(t *testing.T) {
	t.Parallel()

	if err := executeProductionRetention(
		context.Background(),
		"postgres://database/control",
		"/missing/retention.json",
		1024,
	); err == nil {
		t.Fatal("executeProductionRetention() returned nil")
	}
}

func TestBuildProductionRetentionAuditUsesRuntime(t *testing.T) {
	t.Parallel()

	if audit, err := buildProductionRetentionAudit(nil); audit != nil || !errors.Is(err, controlpostgres.ErrInvalidRuntimePool) {
		t.Fatalf("buildProductionRetentionAudit(nil) = (%v, %v)", audit, err)
	}
	pool := lazyProcessPool(t)
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	if audit, err := buildProductionRetentionAudit(pool); audit == nil || err != nil {
		t.Fatalf("buildProductionRetentionAudit() = (%v, %v)", audit, err)
	}
}

func TestProductionRetentionStoreDelegatesEveryRepositoryOperation(t *testing.T) {
	t.Parallel()

	beginner := retentionBeginnerStub{}
	audit, err := controlpostgres.NewAuditStore(beginner)
	if err != nil {
		t.Fatalf("NewAuditStore() error = %v", err)
	}
	commands, err := controlpostgres.NewCommandStore(beginner)
	if err != nil {
		t.Fatalf("NewCommandStore() error = %v", err)
	}
	store := productionRetentionStore{audit: audit, commands: commands}
	if _, err := store.VerifyTenant(context.Background(), "", 1); !errors.Is(err, controlpostgres.ErrInvalidAuditRequest) {
		t.Fatalf("VerifyTenant() error = %v", err)
	}
	if _, err := store.RetainBefore(context.Background(), "", time.Now(), 1); !errors.Is(err, controlpostgres.ErrInvalidAuditRequest) {
		t.Fatalf("RetainBefore() error = %v", err)
	}
	if _, err := store.RetainCommandsBefore(context.Background(), "", time.Now(), 1); !errors.Is(err, controlpostgres.ErrInvalidCommandRetentionRequest) {
		t.Fatalf("RetainCommandsBefore() error = %v", err)
	}
}

type retentionBeginnerStub struct{}

func (retentionBeginnerStub) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, errors.New("unexpected transaction")
}

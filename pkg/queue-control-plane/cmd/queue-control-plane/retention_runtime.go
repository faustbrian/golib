package main

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	gopostgres "github.com/faustbrian/golib/pkg/postgres"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
)

type retentionFileOpener func(string) (io.ReadCloser, error)

func executeRetention(
	ctx context.Context,
	dsn string,
	path string,
	maxBytes int64,
	openFile retentionFileOpener,
	openPool func(context.Context, gopostgres.Config) (*gopostgres.Pool, error),
	buildAudit func(*gopostgres.Pool) (retentionAudit, error),
	now func() time.Time,
) (resultErr error) {
	file, err := openFile(path)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	policies, err := loadRetentionPolicies(file, maxBytes)
	if err != nil {
		return err
	}

	pool, err := openPool(ctx, gopostgres.Config{DSN: dsn})
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, pool.Close(context.Background()))
	}()
	audit, err := buildAudit(pool)
	if err != nil {
		return err
	}

	return applyRetention(ctx, audit, policies, now)
}

func executeProductionRetention(
	ctx context.Context,
	dsn string,
	path string,
	maxBytes int64,
) error {
	return executeRetention(
		ctx,
		dsn,
		path,
		maxBytes,
		func(path string) (io.ReadCloser, error) {
			return os.Open(path) //nolint:gosec // The operator explicitly configures this policy path.
		},
		gopostgres.New,
		buildProductionRetentionAudit,
		time.Now,
	)
}

func buildProductionRetentionAudit(pool *gopostgres.Pool) (retentionAudit, error) {
	runtime, err := controlpostgres.NewRuntime(pool)
	if err != nil {
		return nil, err
	}

	return productionRetentionStore{audit: runtime.Audit, commands: runtime.Commands}, nil
}

type productionRetentionStore struct {
	audit    *controlpostgres.AuditStore
	commands *controlpostgres.CommandStore
}

func (store productionRetentionStore) VerifyTenant(
	ctx context.Context,
	tenant string,
	batch uint32,
) (controlpostgres.VerificationReport, error) {
	return store.audit.VerifyTenant(ctx, tenant, batch)
}

func (store productionRetentionStore) RetainBefore(
	ctx context.Context,
	tenant string,
	cutoff time.Time,
	batch uint32,
) (controlpostgres.RetentionResult, error) {
	return store.audit.RetainBefore(ctx, tenant, cutoff, batch)
}

func (store productionRetentionStore) RetainCommandsBefore(
	ctx context.Context,
	tenant string,
	cutoff time.Time,
	batch uint32,
) (controlpostgres.CommandRetentionResult, error) {
	return store.commands.RetainCommandsBefore(ctx, tenant, cutoff, batch)
}

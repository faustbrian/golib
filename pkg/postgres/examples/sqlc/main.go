package main

import (
	"context"
	"fmt"
	"os"
	"time"

	postgres "github.com/faustbrian/golib/pkg/postgres"
	"github.com/jackc/pgx/v5"
)

type dbtx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type queries struct {
	db dbtx
}

func (q *queries) WithTx(tx pgx.Tx) *queries {
	return &queries{db: tx}
}

func (q *queries) CurrentTime(ctx context.Context) (time.Time, error) {
	var value time.Time
	err := q.db.QueryRow(ctx, "SELECT now()").Scan(&value)

	return value, err
}

func main() {
	ctx := context.Background()
	pool, err := postgres.New(ctx, postgres.Config{DSN: os.Getenv("DATABASE_URL")})
	if err != nil {
		panic(err)
	}
	defer func() { _ = pool.Close(context.Background()) }()

	generated := &queries{db: pool.Raw()}
	err = postgres.RunTransaction(ctx, pool.Raw(), postgres.TransactionOptions{}, func(ctx context.Context, tx pgx.Tx) error {
		value, err := generated.WithTx(tx).CurrentTime(ctx)
		if err == nil {
			fmt.Println(value.UTC())
		}
		return err
	})
	if err != nil {
		panic(err)
	}
}

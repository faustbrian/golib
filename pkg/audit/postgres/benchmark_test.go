package postgres

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/audit"
)

func BenchmarkBoundedQueryConstruction(b *testing.B) {
	query, err := audit.NewQuery(audit.QueryInput{
		Tenant: audit.AllTenants(), ActorID: "actor", SubjectType: "invoice",
		SubjectID: "invoice-1", Action: "invoice.viewed", CorrelationID: "correlation",
		Outcome: audit.OutcomeSucceeded, Limit: audit.MaxQueryRecords,
	})
	if err != nil {
		b.Fatal(err)
	}
	database := &fakeDatabase{}
	store := &Store{pool: database, limits: audit.DefaultLimits(), maxBatchRecords: audit.MaxAppendBatchRecords}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		database.rows = &fakeRows{}
		if _, err := store.Query(context.Background(), query); err != nil {
			b.Fatal(err)
		}
	}
}

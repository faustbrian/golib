package transactionrollback_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/analysis/analyzers/transactionrollback"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	t.Parallel()

	analyzer, err := transactionrollback.New(transactionrollback.Options{
		Transactions: []transactionrollback.Transaction{
			{
				Package: "txapi", Symbol: "Begin", Result: 0,
				RollbackMethod: "Rollback",
			},
			{
				Package: "txapi", Symbol: "BeginFor", Result: 0,
				RollbackMethod: "Rollback",
			},
			{
				Package: "txapi", Symbol: "BeginPair", Result: 0,
				RollbackMethod: "Rollback",
			},
			{
				Package: "txapi", Symbol: "DB.Begin", Result: 0,
				RollbackMethod: "Rollback",
			},
			{
				Package: "txapi", Symbol: "BadResult", Result: 2,
				RollbackMethod: "Rollback",
			},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysistest.Run(t, analysistest.TestData(), analyzer, "txconsumer")
}

func TestUnconfiguredAnalyzerIsInactive(t *testing.T) {
	t.Parallel()

	analysistest.Run(
		t,
		analysistest.TestData(),
		transactionrollback.Analyzer,
		"txunconfigured",
	)
}

func TestNewRejectsMalformedPolicy(t *testing.T) {
	t.Parallel()

	valid := transactionrollback.Transaction{
		Package: "txapi", Symbol: "Begin", Result: 0,
		RollbackMethod: "Rollback",
	}
	tests := []transactionrollback.Options{
		{Transactions: []transactionrollback.Transaction{{
			Package: "txapi/*", Symbol: "Begin", RollbackMethod: "Rollback",
		}}},
		{Transactions: []transactionrollback.Transaction{{
			Package: "txapi", Symbol: "bad-name", RollbackMethod: "Rollback",
		}}},
		{Transactions: []transactionrollback.Transaction{{
			Package: "txapi", Symbol: "A.B.C", RollbackMethod: "Rollback",
		}}},
		{Transactions: []transactionrollback.Transaction{{
			Package: "txapi", Symbol: "Begin", Result: -1,
			RollbackMethod: "Rollback",
		}}},
		{Transactions: []transactionrollback.Transaction{{
			Package: "txapi", Symbol: "Begin", RollbackMethod: "bad-name",
		}}},
		{Transactions: []transactionrollback.Transaction{valid, valid}},
	}
	for _, options := range tests {
		if _, err := transactionrollback.New(options); err == nil {
			t.Fatalf("New(%#v) error = nil", options)
		}
	}
}

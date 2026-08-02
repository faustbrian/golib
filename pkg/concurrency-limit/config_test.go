package concurrencylimit_test

import (
	"testing"

	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
)

func TestNewRejectsInvalidLimitBounds(t *testing.T) {
	t.Parallel()

	_, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit:     2,
		MaxLimit:     1,
		InitialLimit: 1,
		Algorithm:    concurrencylimit.NewFixedAlgorithm(),
	})
	if err == nil {
		t.Fatal("New() error = nil, want invalid limit bounds")
	}
}

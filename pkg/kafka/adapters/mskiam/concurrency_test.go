package mskiam_test

import (
	"context"
	"sync"
	"testing"
	"time"

	mskiam "github.com/faustbrian/golib/pkg/kafka/adapters/mskiam"
)

func TestProviderIsSafeForConcurrentTokenGeneration(t *testing.T) {
	t.Parallel()

	provider, err := mskiam.New(context.Background(), mskiam.Config{
		Region:              "eu-north-1",
		CredentialsProvider: staticCredentialsProvider{},
		TokenTimeout:        time.Second,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	const workers = 32
	failures := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			token, tokenErr := provider.Token(context.Background())
			if tokenErr != nil {
				failures <- tokenErr

				return
			}
			if len(token.Token) == 0 {
				failures <- mskiam.ErrInvalidToken
			}
		}()
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		t.Fatalf("concurrent token generation: %v", failure)
	}
}

package password_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
	"github.com/faustbrian/golib/pkg/password/passwordtest"
)

func TestSharedPolicyAndAdmissionAreRaceSafe(t *testing.T) {
	limits := testLimits()
	limits.Concurrent = 4
	limits.Queue = 64
	policy, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Argon2id, Argon2id: password.Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 8, Parallelism: 1, SaltLength: 8, OutputLength: 16}, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := passwordtest.NewService(policy, []byte("deterministic synthetic entropy"))
	if err != nil {
		t.Fatal(err)
	}
	const workers = 32
	errorsChannel := make(chan error, workers)
	var group sync.WaitGroup
	for worker := range workers {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			secret := fmt.Appendf(nil, "synthetic-%d", worker)
			hash, err := svc.Hash(context.Background(), secret)
			if err != nil {
				errorsChannel <- err
				return
			}
			result, err := svc.Verify(context.Background(), secret, hash.String())
			if err != nil {
				errorsChannel <- err
				return
			}
			if !result.Match() {
				errorsChannel <- fmt.Errorf("worker %d did not match", worker)
			}
		}(worker)
	}
	group.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Error(err)
	}
}

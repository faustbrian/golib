//go:build resource

package password_test

import (
	"context"
	"sync"
	"testing"
	"time"

	password "github.com/faustbrian/golib/pkg/password"
)

func TestDefaultPolicyResourceAdmissionStress(t *testing.T) {
	policy := password.DefaultPolicy()
	admission, err := password.NewAdmission(2, 8)
	if err != nil {
		t.Fatal(err)
	}
	service, err := password.New(policy, password.WithAdmission(admission))
	if err != nil {
		t.Fatal(err)
	}

	const callers = 8
	start := make(chan struct{})
	errorsChannel := make(chan error, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := service.Hash(context.Background(), []byte("synthetic resource stress password"))
			errorsChannel <- err
		}()
	}
	close(start)
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()

	maximumActive := 0
	ticker := time.NewTicker(100 * time.Microsecond)
	defer ticker.Stop()
observe:
	for {
		select {
		case <-done:
			break observe
		case <-ticker.C:
			active := admission.Active()
			if active > maximumActive {
				maximumActive = active
			}
			if active > 2 {
				t.Fatalf("active operations exceeded memory slots: %d", active)
			}
		}
	}
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	if maximumActive != 2 {
		t.Fatalf("stress did not exercise both memory slots: %d", maximumActive)
	}
}

//go:build interoperability

package kafka_test

import (
	"testing"
	"time"
)

func TestBrokerIntegrationGateExcludesHostAccessFromSharedFixtures(t *testing.T) {
	t.Parallel()

	gate := newBrokerIntegrationGate(2)
	releaseShared := gate.acquireShared()
	exclusiveAcquired := make(chan func(), 1)
	go func() {
		exclusiveAcquired <- gate.acquireExclusive()
	}()

	select {
	case releaseExclusive := <-exclusiveAcquired:
		releaseExclusive()
		t.Fatal("exclusive host-access fixture overlapped a shared broker fixture")
	case <-time.After(25 * time.Millisecond):
	}

	releaseShared()
	select {
	case releaseExclusive := <-exclusiveAcquired:
		releaseExclusive()
	case <-time.After(time.Second):
		t.Fatal("exclusive host-access fixture did not start after shared fixture release")
	}
}

func TestBrokerIntegrationGatePreservesSharedConcurrency(t *testing.T) {
	t.Parallel()

	gate := newBrokerIntegrationGate(2)
	releaseFirst := gate.acquireShared()
	releaseSecond := gate.acquireShared()
	releaseSecond()
	releaseFirst()
}

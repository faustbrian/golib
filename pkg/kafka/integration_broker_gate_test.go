//go:build interoperability

package kafka_test

import (
	"bufio"
	"io"
	"net"
	"testing"
	"time"
)

func TestBrokerIntegrationGateExcludesExclusiveFromSharedFixtures(t *testing.T) {
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
		t.Fatal("exclusive fixture overlapped a shared broker fixture")
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

func TestSecureKafkaEndpointProxySwitchesNewConnections(t *testing.T) {
	t.Parallel()

	first := startPrefixedTCPServer(t, "first:")
	second := startPrefixedTCPServer(t, "second:")
	proxy := startSecureKafkaEndpointProxy(t)

	proxy.setTarget(first)
	if got := exchangeProxyLine(t, proxy.endpoint(), "one\n"); got != "first:one\n" {
		t.Fatalf("first proxied response = %q", got)
	}
	proxy.setTarget(second)
	if got := exchangeProxyLine(t, proxy.endpoint(), "two\n"); got != "second:two\n" {
		t.Fatalf("second proxied response = %q", got)
	}
}

func startPrefixedTCPServer(t *testing.T, prefix string) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for endpoint proxy test: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		line, readErr := bufio.NewReader(connection).ReadString('\n')
		if readErr == nil {
			_, _ = io.WriteString(connection, prefix+line)
		}
	}()

	return listener.Addr().String()
}

func exchangeProxyLine(t *testing.T, endpoint string, line string) string {
	t.Helper()

	connection, err := net.DialTimeout("tcp", endpoint, time.Second)
	if err != nil {
		t.Fatalf("dial endpoint proxy: %v", err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set endpoint proxy deadline: %v", err)
	}
	if _, err := io.WriteString(connection, line); err != nil {
		t.Fatalf("write endpoint proxy line: %v", err)
	}
	response, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		t.Fatalf("read endpoint proxy line: %v", err)
	}

	return response
}

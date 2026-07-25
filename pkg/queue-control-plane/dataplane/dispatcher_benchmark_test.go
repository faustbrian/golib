package dataplane

import (
	"errors"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func BenchmarkControllerDispatchBackendOutage(b *testing.B) {
	command := validControlCommand()
	dispatcher, err := NewControllerDispatcher(
		&controllerResolverStub{err: errors.New("backend unavailable")},
		queue.ProtocolVersion{Major: 1},
		time.Minute,
		func() time.Time { return command.RequestedAt.Add(time.Second) },
	)
	if err != nil {
		b.Fatalf("NewControllerDispatcher() error = %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := dispatcher.DispatchResult(b.Context(), command); err == nil {
			b.Fatal("DispatchResult() error = nil")
		}
	}
}

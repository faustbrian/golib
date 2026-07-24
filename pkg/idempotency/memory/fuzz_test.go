package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
)

func FuzzTakeoverPermanentlyFencesOldOwner(f *testing.F) {
	f.Add(uint16(0), false, []byte("ok"), "content-type", "text/plain")
	f.Add(uint16(65535), true, []byte{0x00, 0xff}, "", "")

	f.Fuzz(func(
		t *testing.T,
		extraMillis uint16,
		terminalFailure bool,
		result []byte,
		metadataKey string,
		metadataValue string,
	) {
		if len(result) > 4096 || len(metadataKey) > idempotency.MaxMetadataKeyBytes ||
			len(metadataValue) > idempotency.MaxMetadataValueBytes {
			return
		}
		clock := idempotencytest.NewClock(time.Unix(1_700_000_000, 0).UTC())
		store, err := memory.New(memory.Options{
			Clock:       clock,
			OwnerTokens: idempotencytest.NewTokenSource("property-owner").Next,
		})
		if err != nil {
			t.Fatalf("memory.New() error = %v", err)
		}
		key, err := idempotency.NewKey("property", "tenant", "operation", "caller", "key")
		if err != nil {
			t.Fatalf("NewKey() error = %v", err)
		}
		fingerprint, err := idempotency.NewFingerprint("v1", []byte("request"))
		if err != nil {
			t.Fatalf("NewFingerprint() error = %v", err)
		}
		request := idempotency.AcquireRequest{
			Key: key, Fingerprint: fingerprint, Lease: time.Millisecond,
		}
		first, err := store.Acquire(context.Background(), request)
		if err != nil {
			t.Fatalf("first Acquire() error = %v", err)
		}
		clock.Advance(time.Millisecond + time.Duration(extraMillis)*time.Millisecond)
		second, err := store.Acquire(context.Background(), request)
		if err != nil || second.Outcome != idempotency.OutcomeStaleOwnerTakeover {
			t.Fatalf("takeover Acquire() = %#v, %v", second, err)
		}
		if second.Record.FencingToken <= first.Record.FencingToken ||
			second.Record.Attempt <= first.Record.Attempt {
			t.Fatalf("takeover did not advance fence and attempt: %#v", second.Record)
		}

		old := first.Record.Ownership()
		staleCalls := []func() error{
			func() error {
				_, err := store.Heartbeat(context.Background(), idempotency.HeartbeatRequest{
					Ownership: old, Lease: time.Second,
				})
				return err
			},
			func() error {
				_, err := store.Complete(context.Background(), idempotency.CompleteRequest{
					Ownership: old, Result: []byte("stale"),
				})
				return err
			},
			func() error {
				_, err := store.Fail(context.Background(), idempotency.FailRequest{
					Ownership: old, Result: []byte("stale"),
				})
				return err
			},
			func() error {
				_, err := store.Release(context.Background(), old)
				return err
			},
		}
		for _, call := range staleCalls {
			var semantic *idempotency.Error
			if err := call(); !errors.As(err, &semantic) ||
				semantic.Reason != idempotency.ReasonStaleOwner {
				t.Fatalf("old ownership mutation error = %v", err)
			}
		}

		metadata := map[string]string(nil)
		if metadataKey != "" {
			metadata = map[string]string{metadataKey: metadataValue}
		}
		if terminalFailure {
			_, err = store.Fail(context.Background(), idempotency.FailRequest{
				Ownership: second.Record.Ownership(), Result: result, Metadata: metadata,
			})
		} else {
			_, err = store.Complete(context.Background(), idempotency.CompleteRequest{
				Ownership: second.Record.Ownership(), Result: result, Metadata: metadata,
			})
		}
		if err != nil {
			t.Fatalf("terminal mutation error = %v", err)
		}
		terminal, err := store.Acquire(context.Background(), request)
		want := idempotency.OutcomeReplayed
		if terminalFailure {
			want = idempotency.OutcomeTerminalFailure
		}
		if err != nil || terminal.Outcome != want ||
			terminal.Record.FencingToken != second.Record.FencingToken {
			t.Fatalf("terminal Acquire() = %#v, %v, want %s", terminal, err, want)
		}
	})
}

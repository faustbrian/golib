package keyphrase

import (
	"context"
	"testing"
	"time"
)

func TestFillRejectsZeroProgressWithoutRetryingForever(t *testing.T) {
	selector, err := NewSelector(&internalSource{})
	if err != nil {
		t.Fatal(err)
	}
	destination := []byte{9}
	result := make(chan error, 1)
	go func() {
		result <- selector.Fill(context.Background(), destination)
	}()

	select {
	case err := <-result:
		if errorCode(err) != CodeShortRead {
			t.Fatalf("Fill(zero progress) code = %q, want %q", errorCode(err), CodeShortRead)
		}
		if destination[0] != 0 {
			t.Fatalf("Fill(zero progress) destination = %v, want cleared output", destination)
		}
	case <-time.After(time.Second):
		t.Fatal("Fill(zero progress) did not terminate")
	}
}

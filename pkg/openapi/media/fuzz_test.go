package media_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/media"
)

func FuzzServerSentEventParsingDeterminism(f *testing.F) {
	for _, seed := range []string{
		"data: value\n\n",
		"\ufeff: comment\r\ndata: first\rdata: second\n\n",
		"id: one\ndata:\xff\nretry: 0010\n\n",
		"data: incomplete",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		limits := media.ServerSentEventLimits{
			MaxBytes: 32 << 10, MaxLineBytes: 8 << 10,
			MaxDataBytes: 16 << 10, MaxEvents: 256,
		}
		first, firstErr := media.ParseServerSentEvents(
			context.Background(), bytes.NewReader(raw), limits,
		)
		second, secondErr := media.ParseServerSentEvents(
			context.Background(), bytes.NewReader(raw), limits,
		)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("parser errors differ: %v and %v", firstErr, secondErr)
		}
		if firstErr == nil && !equalEventValues(t, first, second) {
			t.Fatal("server-sent event parsing is nondeterministic")
		}
	})
}

func equalEventValues(t *testing.T, left, right []jsonvalue.Value) bool {
	t.Helper()
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		leftJSON, leftErr := left[index].MarshalJSON()
		rightJSON, rightErr := right[index].MarshalJSON()
		if leftErr != nil || rightErr != nil || !bytes.Equal(leftJSON, rightJSON) {
			return false
		}
	}
	return true
}

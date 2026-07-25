package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func FuzzAdapterTranslation(f *testing.F) {
	for _, seed := range [][]byte{
		{},
		[]byte("child\x00--name=value\x00-1"),
		[]byte("--\x00--literal"),
		[]byte("--unknown"),
		{0xff, 0x00, '-', 'x'},
	} {
		f.Add(seed)
	}
	root := testCommand()
	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 1<<15 {
			t.Skip()
		}
		result, err := Parse(context.Background(), root, strings.Split(string(encoded), "\x00"))
		if err != nil {
			var parseError *ParseError
			if !errors.As(err, &parseError) || errors.Unwrap(parseError) != nil {
				t.Fatalf("adapter leaked a non-owned failure: %T %v", err, err)
			}
			return
		}
		if result.CommandID != 1 && result.CommandID != 2 {
			t.Fatalf("adapter selected unknown command %d", result.CommandID)
		}
		for key := range result.Options {
			if key < 1 || key > 3 {
				t.Fatalf("adapter returned unknown option key %d", key)
			}
		}
	})
}

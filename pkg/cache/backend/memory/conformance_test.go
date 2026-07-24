package memory_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/cache/cachetest"
)

func TestBackendConformance(t *testing.T) {
	t.Parallel()

	backend, err := memoryBackendForConformance()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	cachetest.RunBackendConformance(t, cachetest.BackendHarness{
		Backend: backend,
		MakeUnavailable: func(t *testing.T) {
			t.Helper()
			if err := backend.Close(); err != nil {
				t.Fatal(err)
			}
		},
	})
}

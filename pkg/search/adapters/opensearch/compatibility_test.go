package opensearch

import (
	"slices"
	"testing"
)

func TestSupportedOpenSearchVersionsIncludesCurrentRelease(t *testing.T) {
	t.Parallel()

	want := []string{"2.19.6", "3.8.0"}
	if got := SupportedOpenSearchVersions(); !slices.Equal(got, want) {
		t.Fatalf("SupportedOpenSearchVersions() = %v, want %v", got, want)
	}
}

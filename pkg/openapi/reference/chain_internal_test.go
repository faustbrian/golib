package reference

import (
	"strings"
	"testing"
)

func TestTargetIdentityUsesCanonicalRetrievalAndRequestedFallbacks(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		target   Target
		resource string
	}{
		{
			name: "canonical",
			target: Target{RequestedURI: "requested", Resource: Resource{
				CanonicalURI: "canonical", RetrievalURI: "retrieval",
			}},
			resource: "canonical",
		},
		{
			name: "retrieval",
			target: Target{RequestedURI: "requested", Resource: Resource{
				RetrievalURI: "retrieval",
			}},
			resource: "retrieval",
		},
		{
			name:     "requested",
			target:   Target{RequestedURI: "requested"},
			resource: "requested",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := targetIdentity(test.target); !strings.HasPrefix(
				got, test.resource+"\x00",
			) {
				t.Fatalf("target identity = %q", got)
			}
		})
	}
}

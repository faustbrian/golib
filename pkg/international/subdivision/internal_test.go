package subdivision

import "testing"

func TestValidCodeShapeBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		value string
		want  bool
	}{
		{"US-", false},
		{"US-A", true},
		{"US-ABC", true},
		{"US-ABCD", false},
		{"US_A", false},
		{"@S-A", false},
		{"AS-A", true},
		{"ZS-A", true},
		{"[S-A", false},
		{"U@-A", false},
		{"UA-A", true},
		{"UZ-A", true},
		{"U[-A", false},
		{"US-@", false},
		{"US-Z", true},
		{"US-[", false},
		{"US-/", false},
		{"US-0", true},
		{"US-9", true},
		{"US-:", false},
		{"US-\xff", false},
	} {
		if got := validCode(test.value); got != test.want {
			t.Errorf("validCode(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}

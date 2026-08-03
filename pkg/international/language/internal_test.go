package language

import "testing"

func TestInternalLowerAlphaBoundaries(t *testing.T) {
	for _, test := range []struct {
		value  string
		length int
	}{
		{value: "aa", length: 2},
		{value: "az", length: 2},
		{value: "za", length: 2},
		{value: "zz", length: 2},
		{value: "aaa", length: 3},
		{value: "zzz", length: 3},
	} {
		if !validLowerAlpha(test.value, test.length) {
			t.Errorf("validLowerAlpha(%q, %d) = false", test.value, test.length)
		}
	}
	for _, value := range []string{"@a", "{a", "a@", "a{", "AA", "a", "aaa"} {
		if validLowerAlpha(value, 2) {
			t.Errorf("validLowerAlpha(%q, 2) = true", value)
		}
	}
}

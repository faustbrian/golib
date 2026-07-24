package semver

import "testing"

func TestValidateTag(t *testing.T) {
	t.Parallel()

	valid := []string{
		"v0.0.0",
		"v1.2.3",
		"v1.0.0-rc.1",
		"v1.2.3-alpha-beta",
		"v1.2.3+build.5",
		"v1.2.3-rc.1+build.5",
	}
	for _, tag := range valid {
		if err := ValidateTag(tag); err != nil {
			t.Errorf("ValidateTag(%q) error = %v", tag, err)
		}
	}

	invalid := []string{
		"1.2.3",
		"v01.2.3",
		"v1.02.3",
		"v1.2.03",
		"v1.2",
		"v1.2.3-",
		"v1.2.3-..",
		"v1.2.3-01",
		"v1.2.3+",
		"v1.2.3+bad..value",
	}
	for _, tag := range invalid {
		if err := ValidateTag(tag); err == nil {
			t.Errorf("ValidateTag(%q) unexpectedly succeeded", tag)
		}
	}
}

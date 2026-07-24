package semver

import "testing"

func TestValidateTag(t *testing.T) {
	t.Parallel()

	valid := []string{
		"v0.0.0", "v1.2.3", "v1.0.0-rc.1", "v1.2.3-alpha-beta",
		"v1.2.3+build.5", "v1.2.3-rc.1+build.5",
	}
	for _, tag := range valid {
		if err := ValidateTag(tag); err != nil {
			t.Errorf("ValidateTag(%q) error = %v", tag, err)
		}
	}

	invalid := []string{
		"1.2.3", "v01.2.3", "v1.02.3", "v1.2.03", "v1.2",
		"v1.2.3-", "v1.2.3-..", "v1.2.3-01", "v1.2.3+",
		"v1.2.3+bad..value",
	}
	for _, tag := range invalid {
		if err := ValidateTag(tag); err == nil {
			t.Errorf("ValidateTag(%q) unexpectedly succeeded", tag)
		}
	}
}

func TestNextStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, tag, part, want string
	}{
		{name: "patch", tag: "v1.2.3", part: "patch", want: "v1.2.4"},
		{name: "minor", tag: "v1.2.3", part: "minor", want: "v1.3.0"},
		{name: "major", tag: "v1.2.3", part: "major", want: "v2.0.0"},
	}
	for _, test := range tests {
		got, err := NextStable(test.tag, test.part)
		if err != nil {
			t.Fatalf("NextStable(%q, %q) error = %v", test.tag, test.part, err)
		}
		if got != test.want {
			t.Errorf("NextStable(%q, %q) = %q, want %q", test.tag, test.part, got, test.want)
		}
	}

	if _, err := NextStable("v1.2.3-rc.1", "patch"); err == nil {
		t.Error("NextStable accepted a prerelease current tag")
	}
	if _, err := NextStable("v1.2.3", "build"); err == nil {
		t.Error("NextStable accepted an unknown release part")
	}
}

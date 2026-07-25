package analysis

import "testing"

func TestPolicyExceptionIdentityValidation(t *testing.T) {
	t.Parallel()

	if !validExceptionPackage("example.com/service/internal/bridge") {
		t.Fatal("validExceptionPackage() rejected an exact package")
	}
	for _, value := range []string{
		"",
		" ",
		".",
		"/absolute",
		"example.com/../service",
		"example.com/service/...",
		"example.com/service/*",
		`example.com\service`,
	} {
		if validExceptionPackage(value) {
			t.Fatalf("validExceptionPackage(%q) = true", value)
		}
	}

	if !validExceptionPath("internal/bridge/abi.go") {
		t.Fatal("validExceptionPath() rejected an exact path")
	}
	for _, value := range []string{
		"",
		".",
		"/absolute.go",
		"internal/../abi.go",
		"..",
		"../abi.go",
		`internal\abi.go`,
	} {
		if validExceptionPath(value) {
			t.Fatalf("validExceptionPath(%q) = true", value)
		}
	}
}

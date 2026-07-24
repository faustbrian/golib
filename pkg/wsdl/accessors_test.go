package wsdl_test

import (
	"testing"

	wsdl "github.com/faustbrian/golib/pkg/wsdl"
)

func TestNilDocumentAndDiagnosticAccessors(t *testing.T) {
	t.Parallel()

	var document *wsdl.Document
	if document.Version() != "" {
		t.Fatalf("Version() = %q", document.Version())
	}
	if _, ok := document.Definitions11(); ok {
		t.Fatal("nil Definitions11() succeeded")
	}
	if _, ok := document.Description20(); ok {
		t.Fatal("nil Description20() succeeded")
	}
	if (wsdl.Diagnostics{{Code: "warning", Severity: wsdl.SeverityWarning}}).HasErrors() {
		t.Fatal("warning diagnostics reported errors")
	}
	if got := (wsdl.Diagnostics{}).Error(); got != "" {
		t.Fatalf("empty Diagnostics.Error() = %q", got)
	}
}

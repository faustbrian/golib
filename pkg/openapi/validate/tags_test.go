package validate_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentValidatesTagIdentityAndHierarchy(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0",
		"info":{"title":"API","version":"1"},
		"paths":{},
		"tags":[
			{"name":"A","parent":"B"},
			{"name":"B","parent":"A"},
			{"name":"A"},
			{"name":"C","parent":"Missing"}
		]
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"openapi.tag.name.duplicate": false,
		"openapi.tag.parent.unknown": false,
		"openapi.tag.parent.cycle":   false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if _, exists := want[diagnostic.Code]; exists {
			want[diagnostic.Code] = true
		}
	}
	for code, seen := range want {
		if !seen {
			t.Errorf("missing tag diagnostic %q: %#v", code, report.Diagnostics())
		}
	}
}

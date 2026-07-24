package compile

import (
	"errors"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/resolve"
)

func TestDocumentAccessorsReturnIsolatedCopies(t *testing.T) {
	t.Parallel()

	set := &Set{documents: []Document{{
		URI:          "https://example.test/schema.xsd",
		Namespace:    "urn:test",
		Dependencies: []string{"https://example.test/types.xsd"},
	}}}
	documents := set.Documents()
	documents[0].Dependencies[0] = "mutated"
	if set.documents[0].Dependencies[0] != "https://example.test/types.xsd" {
		t.Fatal("Documents() retained a dependency slice alias")
	}
	document, ok := set.Document("https://example.test/schema.xsd")
	if !ok || document.Namespace != "urn:test" {
		t.Fatalf("Document() = %#v, %t", document, ok)
	}
	document.Dependencies[0] = "mutated"
	if set.documents[0].Dependencies[0] != "https://example.test/types.xsd" {
		t.Fatal("Document() retained a dependency slice alias")
	}
	if _, ok := set.Document("https://example.test/missing.xsd"); ok {
		t.Fatal("Document() found a missing resource")
	}
}

func TestCollectModelIdentityConstraintsTraversesNestedContent(t *testing.T) {
	t.Parallel()

	leaf := xsd.Element{Name: "leaf"}
	inline := xsd.Element{
		Name: "inline",
		InlineComplexType: &xsd.ComplexType{Content: &xsd.ModelGroup{
			Compositor: xsd.Sequence,
			Particles:  []xsd.Particle{{Element: &leaf}},
		}},
	}
	nested := xsd.Element{Name: "nested"}
	group := &xsd.ModelGroup{
		Compositor: xsd.Sequence,
		Particles: []xsd.Particle{
			{Element: &inline},
			{Group: &xsd.ModelGroup{Compositor: xsd.Choice, Particles: []xsd.Particle{{Element: &nested}}}},
		},
	}
	var names []string
	err := collectModelIdentityConstraints(group, "urn:test", func(
		namespace string,
		element xsd.Element,
	) error {
		if namespace != "urn:test" {
			t.Fatalf("namespace = %q", namespace)
		}
		names = append(names, element.Name)
		return nil
	})
	if err != nil || len(names) != 3 {
		t.Fatalf("collectModelIdentityConstraints() = %v, %v", names, err)
	}
	if err := collectModelIdentityConstraints(nil, "urn:test", func(
		string,
		xsd.Element,
	) error {
		t.Fatal("collector called for nil group")
		return nil
	}); err != nil {
		t.Fatalf("nil group error = %v", err)
	}

	want := errors.New("stop")
	for _, stopAt := range []string{"inline", "leaf", "nested"} {
		stopAt := stopAt
		err := collectModelIdentityConstraints(group, "urn:test", func(
			_ string,
			element xsd.Element,
		) error {
			if element.Name == stopAt {
				return want
			}
			return nil
		})
		if !errors.Is(err, want) {
			t.Fatalf("stop at %q error = %v", stopAt, err)
		}
	}
}

func TestResolutionPrimitiveDecisionTables(t *testing.T) {
	t.Parallel()

	for kind, want := range map[xsd.ReferenceKind]resolve.Kind{
		xsd.ReferenceInclude:  resolve.KindInclude,
		xsd.ReferenceImport:   resolve.KindImport,
		xsd.ReferenceRedefine: resolve.KindRedefine,
		"unknown":             "",
	} {
		if got := resolveKind(kind); got != want {
			t.Fatalf("resolveKind(%q) = %q, want %q", kind, got, want)
		}
	}
	for _, identity := range []string{
		"https://example.test/schema.xsd",
		"urn:test:schema",
	} {
		if err := validateIdentity(identity); err != nil {
			t.Fatalf("validateIdentity(%q) error = %v", identity, err)
		}
	}
	for _, identity := range []string{
		"",
		"schema.xsd",
		"https://example.test/schema.xsd#fragment",
		"%",
	} {
		if err := validateIdentity(identity); err == nil {
			t.Fatalf("validateIdentity(%q) succeeded", identity)
		}
	}
}

func TestIdentityXPathGrammarDecisionTables(t *testing.T) {
	t.Parallel()

	namespaces := map[string]string{"t": "urn:test"}
	for _, expression := range []string{
		".",
		"./child",
		"child/./nested",
		"child::t:child",
		".//t:child/*",
		"child | t:other",
	} {
		if !validIdentitySelector(expression, namespaces) {
			t.Fatalf("validIdentitySelector(%q) = false", expression)
		}
	}
	for _, expression := range []string{"", " | child", "child//nested", "missing:child"} {
		if validIdentitySelector(expression, namespaces) {
			t.Fatalf("validIdentitySelector(%q) = true", expression)
		}
	}

	for _, expression := range []string{".", "child/./nested", "child::t:child/attribute::id", "child/@id", "t:child/*", "name | @code"} {
		if !validIdentityField(expression, namespaces) {
			t.Fatalf("validIdentityField(%q) = false", expression)
		}
	}
	for _, expression := range []string{"@id/child", "missing:child", "child/"} {
		if validIdentityField(expression, namespaces) {
			t.Fatalf("validIdentityField(%q) = true", expression)
		}
	}

	for _, test := range []struct {
		expression    string
		allowWildcard bool
		want          bool
	}{
		{expression: "*", allowWildcard: true, want: true},
		{expression: "*"},
		{expression: "local", want: true},
		{expression: "not valid"},
		{expression: "t:local", want: true},
		{expression: "missing:local"},
		{expression: "t:not valid"},
		{expression: "a:b:c"},
	} {
		if got := validIdentityName(
			test.expression,
			namespaces,
			test.allowWildcard,
		); got != test.want {
			t.Fatalf("validIdentityName(%q) = %t, want %t", test.expression, got, test.want)
		}
	}
}

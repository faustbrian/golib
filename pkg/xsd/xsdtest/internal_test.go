package xsdtest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/faustbrian/golib/pkg/xsd/compile"
	"github.com/faustbrian/golib/pkg/xsd/resolve"
)

func TestRunPropagatesAbsolutePathFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("absolute path failed")
	_, err := run(context.Background(), ".", "suite.xml", func(string) (string, error) {
		return "", want
	})
	if !errors.Is(err, want) {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRunReportsMetadataAndRequiredCaseFailures(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeInternalFixture(t, root, "valid.xsd", `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:element name="value" type="xs:boolean"/>
</xs:schema>`)
	writeInternalFixture(t, root, "invalid.xsd", `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:element name="value" type="missing"/>
</xs:schema>`)
	writeInternalFixture(t, root, "bad.xml", `<value>not-boolean</value>`)
	writeInternalFixture(t, root, "odd-hint.xml", `<value xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
 xsi:schemaLocation="urn:missing"/>`)
	writeInternalFixture(t, root, "bad-hint.xml", `<value xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
 xsi:noNamespaceSchemaLocation="invalid.xsd"/>`)
	writeInternalFixture(t, root, "tests.testSet", `<testSet xmlns="http://www.w3.org/XML/2004/xml-schema-test-suite/"
 xmlns:xlink="http://www.w3.org/1999/xlink">
 <testGroup name="cases">
  <schemaTest name="missing-document"><expected validity="valid"/><current status="accepted"/></schemaTest>
  <schemaTest name="wrong-schema"><schemaDocument xlink:href="invalid.xsd"/><expected validity="valid"/><current status="accepted"/></schemaTest>
  <schemaTest name="schema"><schemaDocument xlink:href="valid.xsd"/><expected validity="valid"/><current status="accepted"/></schemaTest>
  <instanceTest name="missing-document"><expected validity="valid"/><current status="accepted"/></instanceTest>
  <instanceTest name="escape"><instanceDocument xlink:href="../outside.xml"/><expected validity="valid"/><current status="accepted"/></instanceTest>
  <instanceTest name="missing"><instanceDocument xlink:href="missing.xml"/><expected validity="valid"/><current status="accepted"/></instanceTest>
  <instanceTest name="odd-hint"><instanceDocument xlink:href="odd-hint.xml"/><expected validity="valid"/><current status="accepted"/></instanceTest>
  <instanceTest name="bad-hint"><instanceDocument xlink:href="bad-hint.xml"/><expected validity="valid"/><current status="accepted"/></instanceTest>
  <instanceTest name="invalid"><instanceDocument xlink:href="bad.xml"/><expected validity="valid"/><current status="accepted"/></instanceTest>
 </testGroup>
</testSet>`)

	report, err := Run(context.Background(), root, "tests.testSet")
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed != 1 || report.Failed != 6 || report.Skipped != 2 {
		t.Fatalf("Report = %#v", report)
	}
}

func TestRunRejectsUnreadableAndMalformedMetadata(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := Run(context.Background(), root, "missing.testSet"); err == nil {
		t.Fatal("Run() accepted missing metadata")
	}
	writeInternalFixture(t, root, "bad.testSet", `<testSet>`)
	if _, err := Run(context.Background(), root, "bad.testSet"); err == nil {
		t.Fatal("Run() accepted malformed metadata")
	}
	if _, err := Run(context.Background(), root, "../outside.testSet"); err == nil {
		t.Fatal("Run() accepted metadata outside the suite root")
	}
}

func TestRunHonorsCancellationBetweenGroups(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeInternalFixture(t, root, "tests.testSet", `<testSet xmlns="http://www.w3.org/XML/2004/xml-schema-test-suite/">
 <testGroup name="group"/>
</testSet>`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, root, "tests.testSet"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestCompileDocumentsRejectsMissingAndMalformedInputs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	metadata := filepath.Join(root, "tests.testSet")
	compiler, err := compile.New(compile.Options{Resolver: &suiteResolver{root: root}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileDocuments(context.Background(), compiler, root, metadata,
		[]documentXML{{Href: "missing.xsd"}}, "missing"); err == nil {
		t.Fatal("compileDocuments() accepted a missing schema")
	}
	writeInternalFixture(t, root, "one.xsd", `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`)
	writeInternalFixture(t, root, "bad.xsd", `<xs:schema`)
	if _, err := compileDocuments(context.Background(), compiler, root, metadata,
		[]documentXML{{Href: "one.xsd"}, {Href: "bad.xsd"}}, "bad"); err == nil {
		t.Fatal("compileDocuments() accepted malformed wrapper input")
	}
	if _, err := compileDocuments(context.Background(), compiler, root, metadata,
		[]documentXML{{Href: "one.xsd"}, {Href: "one.xsd"}}, "duplicate"); err != nil {
		t.Fatalf("compileDocuments(duplicate) error = %v", err)
	}
}

func TestSuiteResolverRejectsUnsafeRequests(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resolver := &suiteResolver{root: root}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.Resolve(ctx, resolve.Request{URI: "file:///schema.xsd"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve(canceled) error = %v", err)
	}
	for _, uri := range []string{"https://example.test/schema.xsd", "file://host/schema.xsd", "file:///%zz", "file:///%25zz"} {
		if _, err := resolver.Resolve(context.Background(), resolve.Request{URI: uri}); err == nil {
			t.Fatalf("Resolve(%q) succeeded", uri)
		}
	}
	outside := fileURI(filepath.Join(root, "..", "outside.xsd"))
	if _, err := resolver.Resolve(context.Background(), resolve.Request{URI: outside}); err == nil {
		t.Fatal("Resolve() escaped the suite root")
	}
	missing := fileURI(filepath.Join(root, "missing.xsd"))
	if _, err := resolver.Resolve(context.Background(), resolve.Request{URI: missing}); err == nil {
		t.Fatal("Resolve() accepted a missing resource")
	}
}

func TestReferenceHelpersRejectUnsafeLexicalForms(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	metadata := filepath.Join(root, "tests.testSet")
	for _, href := range []string{"https://example.test/schema.xsd", "schema.xsd?query", "schema.xsd#part", "%zz", "%25zz"} {
		if _, err := resolveMetadataReference(root, metadata, href); err == nil {
			t.Fatalf("resolveMetadataReference(%q) succeeded", href)
		}
	}
	if _, err := instanceSchemaDocuments(root, metadata, filepath.Join(root, "value.xml"),
		[]byte(`<value xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:noNamespaceSchemaLocation="../outside.xsd"/>`)); err == nil {
		t.Fatal("instanceSchemaDocuments() escaped the suite root")
	}
}

func writeInternalFixture(t *testing.T, root string, name string, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

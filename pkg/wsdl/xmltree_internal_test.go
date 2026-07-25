package wsdl

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"testing"
)

func TestXMLTreePropagatesBaseAndLexicalNameFailures(t *testing.T) {
	t.Parallel()

	child := &xmlNode{attributes: []xml.Attr{{
		Name:  xml.Name{Space: "http://www.w3.org/XML/1998/namespace", Local: "base"},
		Value: "relative",
	}}}
	if err := assignBaseURIs(&xmlNode{children: []*xmlNode{child}}, "%"); err == nil {
		t.Fatal("assignBaseURIs() error = nil")
	}
	if _, err := resolveURI("", "%"); err == nil {
		t.Fatal("resolveURI(reference) error = nil")
	}
	if _, err := lexicalXMLName(xml.Name{Space: "urn:missing", Local: "Name"}, nil, false); err == nil {
		t.Fatal("lexicalXMLName() error = nil")
	}
	if _, err := marshalXMLNode(&xmlNode{name: xml.Name{Space: "urn:missing", Local: "Name"}}); err == nil {
		t.Fatal("marshalXMLNode(unbound root) error = nil")
	}
	root := &xmlNode{
		name:       xml.Name{Local: "root"},
		namespaces: map[string]string{},
		content: []xmlContent{{child: &xmlNode{
			name: xml.Name{Space: "urn:missing", Local: "child"},
		}}},
	}
	if err := writeXMLNode(&bytes.Buffer{}, root, true); err == nil {
		t.Fatal("writeXMLNode(unbound child) error = nil")
	}
	attribute := &xmlNode{
		name:       xml.Name{Local: "root"},
		attributes: []xml.Attr{{Name: xml.Name{Space: "urn:missing", Local: "value"}}},
	}
	if err := writeXMLNode(&bytes.Buffer{}, attribute, true); err == nil {
		t.Fatal("writeXMLNode(unbound attribute) error = nil")
	}
}

func TestXMLTreePropagatesInjectedSerializationFailures(t *testing.T) {
	injected := errors.New("injected XML tree write failure")
	originalWriteNode := writeNode
	writeNode = func(*bytes.Buffer, *xmlNode, bool) error { return injected }
	if _, err := marshalXMLNode(&xmlNode{}); !errors.Is(err, injected) {
		t.Fatalf("marshalXMLNode() error = %v", err)
	}
	writeNode = originalWriteNode
	t.Cleanup(func() { writeNode = originalWriteNode })

	originalEscapeXMLText := escapeXMLText
	escapeXMLText = func(io.Writer, []byte) error { return injected }
	t.Cleanup(func() { escapeXMLText = originalEscapeXMLText })
	tests := map[string]*xmlNode{
		"namespace": {
			name: xml.Name{Local: "root"}, namespaces: map[string]string{"p": "urn:test"},
		},
		"attribute": {
			name: xml.Name{Local: "root"}, attributes: []xml.Attr{{Name: xml.Name{Local: "value"}}},
		},
		"text": {
			name: xml.Name{Local: "root"}, content: []xmlContent{{text: []byte("text")}},
		},
	}
	for name, node := range tests {
		t.Run(name, func(t *testing.T) {
			if err := writeXMLNode(&bytes.Buffer{}, node, true); !errors.Is(err, injected) {
				t.Fatalf("writeXMLNode() error = %v", err)
			}
		})
	}
}

func TestReadXMLNodeEnforcesEveryInternalBoundary(t *testing.T) {
	t.Parallel()

	start := xml.StartElement{Name: xml.Name{Local: "root"}, Attr: []xml.Attr{{Name: xml.Name{Local: "a"}}}}
	tests := map[string]struct {
		source  string
		options ParseOptions
		depth   int
		want    error
	}{
		"depth":      {source: `</root>`, options: ParseOptions{MaxDepth: 1, MaxElements: 1, MaxAttributes: 1, MaxTextBytes: 1}, depth: 2, want: ErrLimitExceeded},
		"elements":   {source: `</root>`, options: ParseOptions{MaxDepth: 1, MaxElements: 0, MaxAttributes: 1, MaxTextBytes: 1}, depth: 1, want: ErrLimitExceeded},
		"attributes": {source: `</root>`, options: ParseOptions{MaxDepth: 1, MaxElements: 1, MaxAttributes: 0, MaxTextBytes: 1}, depth: 1, want: ErrLimitExceeded},
		"decoder":    {source: ``, options: ParseOptions{MaxDepth: 1, MaxElements: 1, MaxAttributes: 1, MaxTextBytes: 1}, depth: 1},
		"directive":  {source: `<!directive></root>`, options: ParseOptions{MaxDepth: 1, MaxElements: 1, MaxAttributes: 1, MaxTextBytes: 1}, depth: 1, want: ErrDTDForbidden},
		"child":      {source: `<child></child></root>`, options: ParseOptions{MaxDepth: 1, MaxElements: 2, MaxAttributes: 1, MaxTextBytes: 1}, depth: 1, want: ErrLimitExceeded},
		"text":       {source: `too much</root>`, options: ParseOptions{MaxDepth: 1, MaxElements: 1, MaxAttributes: 1, MaxTextBytes: 1}, depth: 1, want: ErrLimitExceeded},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			decoder := xml.NewDecoder(bytes.NewBufferString(test.source))
			_, err := readXMLNode(decoder, start, &parseState{options: test.options}, test.depth)
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("readXMLNode() error = %v, want %v", err, test.want)
			}
			if test.want == nil && err == nil {
				t.Fatal("readXMLNode() error = nil")
			}
		})
	}
}

func TestXMLTreeAcceptsExactTextAndNamespaceBoundaries(t *testing.T) {
	t.Parallel()

	decoder := xml.NewDecoder(bytes.NewBufferString(
		`<root xmlns:p="urn:prefixed" xmlns="urn:default" ordinary="value">x</root>`,
	))
	token, err := decoder.Token()
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	start := token.(xml.StartElement)
	node, err := readXMLNode(decoder, start, &parseState{options: ParseOptions{
		MaxDepth: 1, MaxElements: 1, MaxAttributes: 3, MaxTextBytes: 1,
	}}, 1)
	if err != nil {
		t.Fatalf("readXMLNode() error = %v", err)
	}
	if node.text.String() != "x" || node.namespaces["p"] != "urn:prefixed" ||
		node.namespaces[""] != "urn:default" || len(node.namespaces) != 2 {
		t.Fatalf("node = %#v", node)
	}

	constructedDecoder := xml.NewDecoder(bytes.NewBufferString(`<root></root>`))
	constructedToken, err := constructedDecoder.Token()
	if err != nil {
		t.Fatalf("constructed Token() error = %v", err)
	}
	constructedStart := constructedToken.(xml.StartElement)
	constructedStart.Attr = []xml.Attr{
		{Name: xml.Name{Space: "xmlns", Local: "p"}, Value: "urn:prefixed"},
		{Name: xml.Name{Local: "xmlns"}, Value: "urn:default"},
		{Name: xml.Name{Local: "ordinary"}, Value: "value"},
	}
	constructed, err := readXMLNode(
		constructedDecoder,
		constructedStart,
		&parseState{options: ParseOptions{
			MaxDepth: 1, MaxElements: 1, MaxAttributes: 3, MaxTextBytes: 1,
		}},
		1,
	)
	if err != nil {
		t.Fatalf("readXMLNode(constructed namespaces) error = %v", err)
	}
	if constructed.namespaces["p"] != "urn:prefixed" ||
		constructed.namespaces[""] != "urn:default" || len(constructed.namespaces) != 2 {
		t.Fatalf("constructed namespaces = %#v", constructed.namespaces)
	}
}

func TestComponentCountingDistinguishesCoreSchemaAndExtensions(t *testing.T) {
	t.Parallel()

	core := NamespaceWSDL20
	root := &xmlNode{
		name: xml.Name{Space: core, Local: "description"},
		attributes: []xml.Attr{
			{Name: xml.Name{Space: "urn:extension", Local: "enabled"}},
			{Name: xml.Name{Space: "xmlns", Local: "ext"}},
			{Name: xml.Name{Space: core, Local: "core"}},
			{Name: xml.Name{Space: "http://www.w3.org/XML/1998/namespace", Local: "base"}},
			{Name: xml.Name{Local: "ordinary"}},
		},
		children: []*xmlNode{
			{name: xml.Name{Space: core, Local: "import"}},
			{name: xml.Name{Space: core, Local: "operation"}},
			{name: xml.Name{Space: core, Local: "binding"}},
			{name: xml.Name{Space: core, Local: "endpoint"}},
			{name: xml.Name{Space: NamespaceXMLSchema, Local: "schema"}},
			{name: xml.Name{Space: "urn:extension", Local: "policy"}, children: []*xmlNode{{
				name: xml.Name{Space: "urn:extension", Local: "nested"},
			}}},
		},
	}
	counts := componentCounts{}
	countComponents(root, core, false, &counts)
	if counts.imports != 1 || counts.operations != 1 || counts.bindings != 1 ||
		counts.endpoints != 1 || counts.extensions != 2 {
		t.Fatalf("counts = %#v", counts)
	}
	if err := enforceComponentLimits(root, core, ParseOptions{
		MaxImports: 1, MaxOperations: 1, MaxBindings: 1,
		MaxEndpoints: 1, MaxExtensions: 2,
	}); err != nil {
		t.Fatalf("enforceComponentLimits(exact) error = %v", err)
	}

	attributeCases := map[string]struct {
		attribute xml.Attr
		want      int
	}{
		"extension": {attribute: xml.Attr{Name: xml.Name{Space: "urn:extension", Local: "enabled"}}, want: 1},
		"ordinary":  {attribute: xml.Attr{Name: xml.Name{Local: "ordinary"}}},
		"namespace": {attribute: xml.Attr{Name: xml.Name{Space: "xmlns", Local: "ext"}}},
		"core":      {attribute: xml.Attr{Name: xml.Name{Space: core, Local: "core"}}},
		"xml":       {attribute: xml.Attr{Name: xml.Name{Space: "http://www.w3.org/XML/1998/namespace", Local: "base"}}},
	}
	for name, test := range attributeCases {
		counts = componentCounts{}
		countComponents(&xmlNode{
			name: xml.Name{Space: core, Local: "description"}, attributes: []xml.Attr{test.attribute},
		}, core, false, &counts)
		if counts.extensions != test.want {
			t.Errorf("countComponents(%s) extensions = %d, want %d", name, counts.extensions, test.want)
		}
	}
}

func TestXMLTreeWritesOnlyChangedNamespacesAndNonDeclarationAttributes(t *testing.T) {
	t.Parallel()

	node := &xmlNode{
		name: xml.Name{Local: "root"},
		namespaces: map[string]string{
			"same": "urn:same", "changed": "urn:new",
		},
		attributes: []xml.Attr{
			{Name: xml.Name{Space: "xmlns", Local: "same"}, Value: "urn:same"},
			{Name: xml.Name{Local: "xmlns"}, Value: "urn:default"},
			{Name: xml.Name{Local: "value"}, Value: "kept"},
		},
	}
	var output bytes.Buffer
	if err := writeXMLNodeScoped(&output, node, map[string]string{
		"same": "urn:same", "changed": "urn:old",
	}, true); err != nil {
		t.Fatalf("writeXMLNodeScoped() error = %v", err)
	}
	serialized := output.String()
	if bytes.Count(output.Bytes(), []byte("xmlns:changed")) != 1 ||
		bytes.Contains(output.Bytes(), []byte("xmlns:same")) ||
		bytes.Contains(output.Bytes(), []byte(`xmlns="urn:default"`)) ||
		!bytes.Contains(output.Bytes(), []byte(`value="kept"`)) {
		t.Fatalf("serialized XML = %s", serialized)
	}
}

func TestQNameHelpersRejectInvalidPrefixesAndRewriteOnlyTargetAttribute(t *testing.T) {
	t.Parallel()

	node := &xmlNode{
		namespaces: map[string]string{"p": "urn:p", "q": "urn:q"},
		attributes: []xml.Attr{
			{Name: xml.Name{Space: "urn:qualified", Local: "items"}, Value: "q:Wrong"},
			{Name: xml.Name{Local: "other"}, Value: "p:Wrong"},
			{Name: xml.Name{Local: "items"}, Value: "p:One q:Two"},
		},
	}
	values, err := node.qnamesAttribute("items")
	if err != nil {
		t.Fatalf("qnamesAttribute() error = %v", err)
	}
	if len(values) != 2 || values[0] != (QName{Namespace: "urn:p", Local: "One"}) ||
		values[1] != (QName{Namespace: "urn:q", Local: "Two"}) {
		t.Fatalf("qnamesAttribute() = %#v", values)
	}
	if _, err := (&xmlNode{namespaces: map[string]string{"1bad": "urn:bad"}}).parseQName("1bad:Name"); err == nil {
		t.Fatal("parseQName(invalid prefix) error = nil")
	}
}

func TestContextReaderPropagatesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := contextReader{ctx: ctx, reader: bytes.NewBufferString("payload")}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read() error = %v", err)
	}
}

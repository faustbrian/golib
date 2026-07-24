package wsdl

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

type xmlNode struct {
	name       xml.Name
	attributes []xml.Attr
	children   []*xmlNode
	text       strings.Builder
	content    []xmlContent
	namespaces map[string]string
	location   Location
	baseURI    string
}

var marshalNode = marshalXMLNode

var (
	writeNode     = writeXMLNode
	escapeXMLText = xml.EscapeText
)

func assignBaseURIs(node *xmlNode, inherited string) error {
	base := inherited
	for _, attribute := range node.attributes {
		if attribute.Name != (xml.Name{
			Space: "http://www.w3.org/XML/1998/namespace",
			Local: "base",
		}) {
			continue
		}
		resolved, err := resolveURI(inherited, attribute.Value)
		if err != nil {
			return fmt.Errorf("wsdl: resolve xml:base %q: %w", attribute.Value, err)
		}
		base = resolved
		break
	}
	node.baseURI = base
	for _, child := range node.children {
		if err := assignBaseURIs(child, base); err != nil {
			return err
		}
	}
	return nil
}

func resolveURI(base, reference string) (string, error) {
	referenceURL, err := url.Parse(reference)
	if err != nil {
		return "", err
	}
	if base == "" {
		return referenceURL.String(), nil
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return baseURL.ResolveReference(referenceURL).String(), nil
}

type xmlContent struct {
	text  []byte
	child *xmlNode
}

type parseState struct {
	ctx        context.Context
	options    ParseOptions
	elements   int
	attributes int
	textBytes  int64
}

type componentCounts struct {
	imports    int
	operations int
	bindings   int
	endpoints  int
	extensions int
}

func validateCoreNCNames(node *xmlNode, coreNamespace string) error {
	if node.name.Space == coreNamespace {
		for _, local := range []string{"name", "messageLabel"} {
			value, exists := node.namespacedAttribute("", local)
			if !exists {
				continue
			}
			if datatype.ValidateBuiltInLexical("NCName", value) != nil {
				return fmt.Errorf(
					"wsdl: {%s}%s attribute %s value %q is not an NCName",
					node.name.Space,
					node.name.Local,
					local,
					value,
				)
			}
		}
	}
	for _, child := range node.children {
		if err := validateCoreNCNames(child, coreNamespace); err != nil {
			return err
		}
	}
	return nil
}

func enforceComponentLimits(
	root *xmlNode,
	coreNamespace string,
	options ParseOptions,
) error {
	counts := componentCounts{}
	countComponents(root, coreNamespace, false, &counts)
	limits := []struct {
		name  string
		count int
		max   int
	}{
		{name: "imports", count: counts.imports, max: options.MaxImports},
		{name: "operations", count: counts.operations, max: options.MaxOperations},
		{name: "bindings", count: counts.bindings, max: options.MaxBindings},
		{name: "endpoints", count: counts.endpoints, max: options.MaxEndpoints},
		{name: "extensions", count: counts.extensions, max: options.MaxExtensions},
	}
	for _, limit := range limits {
		if limit.count > limit.max {
			return fmt.Errorf(
				"%w: %s exceed %d",
				ErrLimitExceeded,
				limit.name,
				limit.max,
			)
		}
	}
	return nil
}

func countComponents(
	node *xmlNode,
	coreNamespace string,
	parentCore bool,
	counts *componentCounts,
) {
	core := node.name.Space == coreNamespace
	if core {
		switch node.name.Local {
		case "import", "include":
			counts.imports++
		case "operation":
			counts.operations++
		case "binding":
			counts.bindings++
		case "port", "endpoint":
			counts.endpoints++
		}
		for _, attribute := range node.attributes {
			if attribute.Name.Space != "" && attribute.Name.Space != "xmlns" &&
				attribute.Name.Space != coreNamespace &&
				attribute.Name.Space != "http://www.w3.org/XML/1998/namespace" {
				counts.extensions++
			}
		}
	} else if parentCore &&
		(node.name.Space != NamespaceXMLSchema || node.name.Local != "schema") {
		counts.extensions++
	}
	for _, child := range node.children {
		countComponents(child, coreNamespace, core, counts)
	}
}

func marshalXMLNode(node *xmlNode) ([]byte, error) {
	var output bytes.Buffer
	if err := writeNode(&output, node, true); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeXMLNode(output *bytes.Buffer, node *xmlNode, root bool) error {
	return writeXMLNodeScoped(output, node, nil, root)
}

func writeXMLNodeScoped(
	output *bytes.Buffer,
	node *xmlNode,
	inherited map[string]string,
	emitNamespaces bool,
) error {
	name, err := lexicalXMLName(node.name, node.namespaces, false)
	if err != nil {
		return err
	}
	output.WriteByte('<')
	output.WriteString(name)
	if emitNamespaces {
		prefixes := make([]string, 0, len(node.namespaces))
		for prefix, namespace := range node.namespaces {
			if inheritedNamespace, ok := inherited[prefix]; ok && inheritedNamespace == namespace {
				continue
			}
			prefixes = append(prefixes, prefix)
		}
		sort.Strings(prefixes)
		for _, prefix := range prefixes {
			output.WriteString(" xmlns")
			if prefix != "" {
				output.WriteByte(':')
				output.WriteString(prefix)
			}
			output.WriteString(`="`)
			if err := escapeXMLText(output, []byte(node.namespaces[prefix])); err != nil {
				return err
			}
			output.WriteByte('"')
		}
	}
	for _, attribute := range node.attributes {
		if attribute.Name.Space == "xmlns" ||
			(attribute.Name.Space == "" && attribute.Name.Local == "xmlns") {
			continue
		}
		attributeName, nameErr := lexicalXMLName(attribute.Name, node.namespaces, true)
		if nameErr != nil {
			return nameErr
		}
		output.WriteByte(' ')
		output.WriteString(attributeName)
		output.WriteString(`="`)
		if err := escapeXMLText(output, []byte(attribute.Value)); err != nil {
			return err
		}
		output.WriteByte('"')
	}
	output.WriteByte('>')
	for _, content := range node.content {
		if content.child != nil {
			if err := writeXMLNodeScoped(
				output,
				content.child,
				node.namespaces,
				true,
			); err != nil {
				return err
			}
			continue
		}
		if err := escapeXMLText(output, content.text); err != nil {
			return err
		}
	}
	output.WriteString("</")
	output.WriteString(name)
	output.WriteByte('>')
	return nil
}

func lexicalXMLName(name xml.Name, namespaces map[string]string, attribute bool) (string, error) {
	if name.Space == "" {
		return name.Local, nil
	}
	if name.Space == "http://www.w3.org/XML/1998/namespace" {
		return "xml:" + name.Local, nil
	}
	prefixes := make([]string, 0)
	for prefix, namespace := range namespaces {
		if namespace == name.Space && (!attribute || prefix != "") {
			prefixes = append(prefixes, prefix)
		}
	}
	if len(prefixes) == 0 {
		return "", fmt.Errorf("wsdl: namespace %q has no in-scope prefix", name.Space)
	}
	sort.Strings(prefixes)
	if prefixes[0] == "" {
		return name.Local, nil
	}
	return prefixes[0] + ":" + name.Local, nil
}

func readXMLNode(
	decoder *xml.Decoder,
	start xml.StartElement,
	state *parseState,
	depth int,
) (*xmlNode, error) {
	state.elements++
	state.attributes += len(start.Attr)
	if depth > state.options.MaxDepth {
		return nil, fmt.Errorf("%w: element depth exceeds %d", ErrLimitExceeded, state.options.MaxDepth)
	}
	if state.elements > state.options.MaxElements {
		return nil, fmt.Errorf("%w: element count exceeds %d", ErrLimitExceeded, state.options.MaxElements)
	}
	if state.attributes > state.options.MaxAttributes {
		return nil, fmt.Errorf("%w: attribute count exceeds %d", ErrLimitExceeded, state.options.MaxAttributes)
	}
	line, column := decoder.InputPos()
	node := &xmlNode{
		name:       start.Name,
		attributes: append([]xml.Attr(nil), start.Attr...),
		namespaces: make(map[string]string),
		location: Location{
			SystemID: state.options.SystemID,
			Line:     line,
			Column:   column,
			Offset:   decoder.InputOffset(),
		},
	}
	for _, attribute := range start.Attr {
		if attribute.Name.Space == "xmlns" {
			node.namespaces[attribute.Name.Local] = attribute.Value
			continue
		}
		if attribute.Name.Space == "" && attribute.Name.Local == "xmlns" {
			node.namespaces[""] = attribute.Value
		}
	}

	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("wsdl: parse {%s}%s: %w", start.Name.Space, start.Name.Local, err)
		}
		switch value := token.(type) {
		case xml.Directive:
			return nil, ErrDTDForbidden
		case xml.StartElement:
			child, err := readXMLNode(decoder, value, state, depth+1)
			if err != nil {
				return nil, err
			}
			inheritNamespaces(child, node.namespaces)
			node.children = append(node.children, child)
			node.content = append(node.content, xmlContent{child: child})
		case xml.CharData:
			state.textBytes += int64(len(value))
			if state.textBytes > state.options.MaxTextBytes {
				return nil, fmt.Errorf("%w: text bytes exceed %d", ErrLimitExceeded, state.options.MaxTextBytes)
			}
			text := append([]byte(nil), value...)
			node.text.Write(text)
			node.content = append(node.content, xmlContent{text: text})
		case xml.EndElement:
			if value.Name == start.Name {
				return node, nil
			}
		}
	}
}

func inheritNamespaces(node *xmlNode, inherited map[string]string) {
	for prefix, namespace := range inherited {
		if _, exists := node.namespaces[prefix]; !exists {
			node.namespaces[prefix] = namespace
		}
	}
	for _, child := range node.children {
		inheritNamespaces(child, node.namespaces)
	}
}

func (n *xmlNode) attribute(local string) string {
	for _, attribute := range n.attributes {
		if attribute.Name.Space == "" && attribute.Name.Local == local {
			return attribute.Value
		}
	}
	return ""
}

func (n *xmlNode) hasAttribute(local string) bool {
	for _, attribute := range n.attributes {
		if attribute.Name.Space == "" && attribute.Name.Local == local {
			return true
		}
	}
	return false
}

func (n *xmlNode) namespacedAttribute(namespace, local string) (string, bool) {
	for _, attribute := range n.attributes {
		if attribute.Name == (xml.Name{Space: namespace, Local: local}) {
			return attribute.Value, true
		}
	}
	return "", false
}

func (n *xmlNode) qnameAttribute(local string) (QName, error) {
	if !n.hasAttribute(local) {
		return QName{}, nil
	}
	return n.parseQName(n.attribute(local))
}

func (n *xmlNode) parseQName(value string) (QName, error) {
	lexical := value
	if lexical == "" || strings.TrimSpace(lexical) != lexical {
		return QName{}, fmt.Errorf("wsdl: invalid QName %q", lexical)
	}
	parts := strings.Split(lexical, ":")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] == "") {
		return QName{}, fmt.Errorf("wsdl: invalid QName %q", lexical)
	}
	prefix := ""
	name := parts[0]
	if len(parts) == 2 {
		prefix, name = parts[0], parts[1]
	}
	if datatype.ValidateBuiltInLexical("NCName", name) != nil ||
		(prefix != "" && datatype.ValidateBuiltInLexical("NCName", prefix) != nil) {
		return QName{}, fmt.Errorf("wsdl: invalid QName %q", lexical)
	}
	namespace, exists := n.namespaces[prefix]
	if prefix != "" && !exists {
		return QName{}, fmt.Errorf("wsdl: QName %q uses undeclared prefix %q", lexical, prefix)
	}
	return QName{Namespace: namespace, Local: name}, nil
}

func (n *xmlNode) qnamesAttribute(local string) ([]QName, error) {
	values := splitSpaceSeparated(n.attribute(local))
	result := make([]QName, 0, len(values))
	for _, value := range values {
		copyNode := *n
		copyNode.attributes = append([]xml.Attr(nil), n.attributes...)
		for index := range copyNode.attributes {
			attribute := &copyNode.attributes[index]
			if attribute.Name.Space == "" && attribute.Name.Local == local {
				attribute.Value = value
			}
		}
		name, err := copyNode.qnameAttribute(local)
		if err != nil {
			return nil, err
		}
		result = append(result, name)
	}
	return result, nil
}

func (n *xmlNode) documentation() *Documentation {
	if n.name.Local != "documentation" ||
		(n.name.Space != NamespaceWSDL11 && n.name.Space != NamespaceWSDL20) {
		return nil
	}
	language := ""
	for _, attribute := range n.attributes {
		if attribute.Name == (xml.Name{Space: "http://www.w3.org/XML/1998/namespace", Local: "lang"}) {
			language = attribute.Value
		}
	}
	return &Documentation{
		Language: language,
		Content:  strings.TrimSpace(n.text.String()),
		Location: n.location,
	}
}

func splitSpaceSeparated(value string) []string {
	return strings.Fields(value)
}

func soapVersion(namespace string) Version {
	if namespace == NamespaceSOAP12Binding {
		return Version12
	}
	return Version11
}

// Version12 identifies SOAP 1.2 where a model carries a SOAP version.
const Version12 Version = "1.2"

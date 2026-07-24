package xsd_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/compile"
	"github.com/faustbrian/golib/pkg/xsd/validate"
)

func TestReferenceDifferentialCorpus(t *testing.T) {
	t.Parallel()

	root := filepath.Join("testdata", "differential")
	schema, err := os.ReadFile(filepath.Join(root, "schema.xsd"))
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "urn:xsd:differential", Content: schema,
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := os.Open(filepath.Join(root, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := manifest.Close(); closeErr != nil {
			t.Errorf("close differential manifest: %v", closeErr)
		}
	}()

	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 3 || fields[0] == "id" {
			continue
		}
		name, expected, file := fields[0], fields[1] == "valid", fields[2]
		t.Run(name, func(t *testing.T) {
			instance, readErr := os.ReadFile(filepath.Join(root, file))
			if readErr != nil {
				t.Fatal(readErr)
			}
			fromBytes, validateErr := validator.Validate(context.Background(), instance)
			if validateErr != nil {
				t.Fatal(validateErr)
			}
			if fromBytes.Valid != expected {
				t.Fatalf("Validate() valid = %t, want %t: %#v", fromBytes.Valid, expected, fromBytes.Diagnostics)
			}

			fromReader, readerErr := validator.ValidateReader(context.Background(), bytes.NewReader(instance))
			if readerErr != nil || !reflect.DeepEqual(fromReader, fromBytes) {
				t.Fatalf("ValidateReader() = %#v, %v; want %#v", fromReader, readerErr, fromBytes)
			}

			tree, treeErr := differentialTree(instance)
			if treeErr != nil {
				t.Fatal(treeErr)
			}
			fromTree, treeErr := validator.ValidateTree(context.Background(), tree)
			if treeErr != nil || !equivalentValidation(fromTree, fromBytes) {
				t.Fatalf("ValidateTree() = %#v, %v; want equivalent to %#v", fromTree, treeErr, fromBytes)
			}
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

func differentialTree(source []byte) (validate.Node, error) {
	decoder := xml.NewDecoder(bytes.NewReader(source))
	var stack []*validate.Node
	var root validate.Node
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				return root, nil
			}
			return validate.Node{}, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			node := validate.Node{
				Name:       xsd.QName{Namespace: value.Name.Space, Local: value.Name.Local},
				Attributes: make(map[xsd.QName]string),
				Namespaces: make(map[string]string),
			}
			if len(stack) > 0 {
				for prefix, namespace := range stack[len(stack)-1].Namespaces {
					node.Namespaces[prefix] = namespace
				}
			}
			for _, attribute := range value.Attr {
				switch {
				case attribute.Name.Space == "" && attribute.Name.Local == "xmlns":
					node.Namespaces[""] = attribute.Value
				case attribute.Name.Space == "xmlns":
					node.Namespaces[attribute.Name.Local] = attribute.Value
				default:
					node.Attributes[xsd.QName{Namespace: attribute.Name.Space, Local: attribute.Name.Local}] = attribute.Value
				}
			}
			if len(stack) == 0 {
				root = node
				stack = []*validate.Node{&root}
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
				stack = append(stack, &parent.Children[len(parent.Children)-1])
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(value)
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
}

func equivalentValidation(left validate.Result, right validate.Result) bool {
	if left.Valid != right.Valid || len(left.Diagnostics) != len(right.Diagnostics) {
		return false
	}
	for index := range left.Diagnostics {
		if left.Diagnostics[index].Code != right.Diagnostics[index].Code ||
			left.Diagnostics[index].Path != right.Diagnostics[index].Path ||
			left.Diagnostics[index].Message != right.Diagnostics[index].Message {
			return false
		}
	}
	return true
}

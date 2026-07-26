// Package xsdtest runs XML Schema test-set metadata against xsd.
package xsdtest

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/compile"
	"github.com/faustbrian/golib/pkg/xsd/resolve"
	"github.com/faustbrian/golib/pkg/xsd/validate"
)

// Report summarizes one official test-set run.
type Report struct {
	Passed   int
	Failed   int
	Skipped  int
	Excluded int
	Cases    []Case
}

// Case records one schema or instance expectation.
type Case struct {
	Group    string
	Name     string
	Kind     string
	Expected string
	Actual   string
	Err      error
}

// Run executes accepted valid and invalid expectations in one XSTS testSet.
func Run(ctx context.Context, suiteRoot string, metadataPath string) (Report, error) {
	return run(ctx, suiteRoot, metadataPath, filepath.Abs)
}

func run(
	ctx context.Context,
	suiteRoot string,
	metadataPath string,
	absolute func(string) (string, error),
) (Report, error) {
	root, err := absolute(suiteRoot)
	if err != nil {
		return Report{}, err
	}
	metadata, err := confinedPath(root, metadataPath)
	if err != nil {
		return Report{}, err
	}
	content, err := os.ReadFile(metadata)
	if err != nil {
		return Report{}, err
	}
	var testSet testSetXML
	if err := xml.Unmarshal(content, &testSet); err != nil {
		return Report{}, fmt.Errorf("xsdtest: parse %s: %w", metadata, err)
	}
	resolver := &suiteResolver{root: root}
	compiler, _ := compile.New(compile.Options{Resolver: resolver})
	report := Report{}
	for _, group := range testSet.Groups {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		var set *compile.Set
		var schemaDocuments []documentXML
		for _, schemaTest := range group.SchemaTests {
			if !schemaTest.runnable() {
				report.exclude(group.Name, schemaTest.Name, "schema", schemaTest.Expected.Validity)
				continue
			}
			if len(schemaTest.Documents) == 0 {
				report.skip(group.Name, schemaTest.Name, "schema", schemaTest.Expected.Validity)
				continue
			}
			compiled, compileErr := compileDocuments(
				ctx,
				compiler,
				root,
				metadata,
				schemaTest.Documents,
				group.Name,
			)
			actual := "valid"
			if compileErr != nil {
				actual = "invalid"
			}
			if actual == schemaTest.Expected.Validity {
				report.pass(group.Name, schemaTest.Name, "schema", actual)
				if compileErr == nil {
					set = compiled
					schemaDocuments = schemaTest.Documents
				}
			} else {
				report.fail(
					group.Name,
					schemaTest.Name,
					"schema",
					schemaTest.Expected.Validity,
					actual,
					compileErr,
				)
			}
		}
		for _, instanceTest := range group.InstanceTests {
			if !instanceTest.runnable() {
				report.exclude(group.Name, instanceTest.Name, "instance", instanceTest.Expected.Validity)
				continue
			}
			if len(instanceTest.Documents) == 0 || set == nil {
				report.skip(group.Name, instanceTest.Name, "instance", instanceTest.Expected.Validity)
				continue
			}
			instancePath, pathErr := resolveMetadataReference(
				root,
				metadata,
				instanceTest.Documents[0].Href,
			)
			if pathErr != nil {
				report.fail(group.Name, instanceTest.Name, "instance", instanceTest.Expected.Validity, "error", pathErr)
				continue
			}
			instance, readErr := os.ReadFile(instancePath)
			if readErr != nil {
				report.fail(group.Name, instanceTest.Name, "instance", instanceTest.Expected.Validity, "error", readErr)
				continue
			}
			instanceSet := set
			hints, hintErr := instanceSchemaDocuments(root, metadata, instancePath, instance)
			if hintErr != nil {
				report.fail(group.Name, instanceTest.Name, "instance", instanceTest.Expected.Validity, "error", hintErr)
				continue
			}
			if len(hints) > 0 {
				instanceSet, hintErr = compileDocuments(
					ctx,
					compiler,
					root,
					metadata,
					append(append([]documentXML(nil), schemaDocuments...), hints...),
					group.Name,
				)
				if hintErr != nil {
					report.fail(group.Name, instanceTest.Name, "instance", instanceTest.Expected.Validity, "invalid", hintErr)
					continue
				}
			}
			validator, _ := validate.New(instanceSet, validate.Options{SystemID: fileURI(instancePath)})
			result, validateErr := validator.Validate(ctx, instance)
			actual := "valid"
			if validateErr != nil || !result.Valid {
				actual = "invalid"
			}
			assessmentErr := validateErr
			if assessmentErr == nil && !result.Valid && len(result.Diagnostics) > 0 {
				assessmentErr = fmt.Errorf(
					"%s: %s",
					result.Diagnostics[0].Code,
					result.Diagnostics[0].Message,
				)
			}
			if actual == instanceTest.Expected.Validity {
				report.pass(group.Name, instanceTest.Name, "instance", actual)
			} else {
				report.fail(
					group.Name,
					instanceTest.Name,
					"instance",
					instanceTest.Expected.Validity,
					actual,
					assessmentErr,
				)
			}
		}
	}
	return report, nil
}

func compileDocuments(
	ctx context.Context,
	compiler *compile.Compiler,
	root string,
	metadata string,
	documents []documentXML,
	group string,
) (*compile.Set, error) {
	paths := make([]string, 0, len(documents))
	contents := make([][]byte, 0, len(documents))
	seen := make(map[string]struct{}, len(documents))
	for _, document := range documents {
		path, err := resolveMetadataReference(root, metadata, document.Href)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
		contents = append(contents, content)
	}
	if len(paths) == 1 {
		return compiler.Compile(ctx, compile.Source{URI: fileURI(paths[0]), Content: contents[0]})
	}

	var wrapper strings.Builder
	wrapper.WriteString(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">`)
	for index, content := range contents {
		document, err := xsd.Parse(ctx, content, xsd.ParseOptions{SystemID: fileURI(paths[index])})
		if err != nil {
			return nil, err
		}
		if document.TargetNamespace == "" {
			wrapper.WriteString(`<xs:include schemaLocation="`)
		} else {
			wrapper.WriteString(`<xs:import namespace="`)
			_ = xml.EscapeText(&wrapper, []byte(document.TargetNamespace))
			wrapper.WriteString(`" schemaLocation="`)
		}
		_ = xml.EscapeText(&wrapper, []byte(fileURI(paths[index])))
		wrapper.WriteString(`"/>`)
	}
	wrapper.WriteString(`</xs:schema>`)
	uri := fileURI(filepath.Join(filepath.Dir(metadata), ".xsdtest-"+url.PathEscape(group)+".xsd"))
	return compiler.Compile(ctx, compile.Source{URI: uri, Content: []byte(wrapper.String())})
}

func instanceSchemaDocuments(
	root string,
	metadata string,
	instancePath string,
	instance []byte,
) ([]documentXML, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(instance)))
	for {
		token, err := decoder.Token()
		if err != nil {
			// Validation reports malformed instance documents. Schema hints are
			// only available after a document element has been parsed.
			return nil, nil
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		var references []string
		for _, attribute := range start.Attr {
			if attribute.Name.Space != "http://www.w3.org/2001/XMLSchema-instance" {
				continue
			}
			switch attribute.Name.Local {
			case "noNamespaceSchemaLocation":
				references = append(references, strings.Fields(attribute.Value)...)
			case "schemaLocation":
				fields := strings.Fields(attribute.Value)
				if len(fields)%2 != 0 {
					return nil, errors.New("xsdtest: xsi:schemaLocation must contain namespace-location pairs")
				}
				for index := 1; index < len(fields); index += 2 {
					references = append(references, fields[index])
				}
			}
		}
		documents := make([]documentXML, 0, len(references))
		for _, reference := range references {
			path, err := resolveMetadataReference(root, instancePath, reference)
			if err != nil {
				return nil, err
			}
			relative, _ := filepath.Rel(filepath.Dir(metadata), path)
			documents = append(documents, documentXML{Href: filepath.ToSlash(relative)})
		}
		return documents, nil
	}
}

func (r *Report) pass(group string, name string, kind string, actual string) {
	r.Passed++
	r.Cases = append(r.Cases, Case{
		Group: group, Name: name, Kind: kind, Expected: actual, Actual: actual,
	})
}

func (r *Report) fail(
	group string,
	name string,
	kind string,
	expected string,
	actual string,
	err error,
) {
	r.Failed++
	r.Cases = append(r.Cases, Case{
		Group: group, Name: name, Kind: kind, Expected: expected, Actual: actual, Err: err,
	})
}

func (r *Report) skip(group string, name string, kind string, expected string) {
	r.Skipped++
	r.Cases = append(r.Cases, Case{
		Group: group, Name: name, Kind: kind, Expected: expected, Actual: "skipped",
	})
}

func (r *Report) exclude(group string, name string, kind string, expected string) {
	r.Excluded++
	r.Cases = append(r.Cases, Case{
		Group: group, Name: name, Kind: kind, Expected: expected, Actual: "excluded",
	})
}

type testSetXML struct {
	Groups []testGroupXML `xml:"testGroup"`
}

type testGroupXML struct {
	Name          string            `xml:"name,attr"`
	SchemaTests   []schemaTestXML   `xml:"schemaTest"`
	InstanceTests []instanceTestXML `xml:"instanceTest"`
}

type schemaTestXML struct {
	Name      string        `xml:"name,attr"`
	Documents []documentXML `xml:"schemaDocument"`
	Expected  expectedXML   `xml:"expected"`
	Current   currentXML    `xml:"current"`
}

func (t schemaTestXML) runnable() bool {
	return runnable(t.Expected.Validity, t.Current.Status)
}

type instanceTestXML struct {
	Name      string        `xml:"name,attr"`
	Documents []documentXML `xml:"instanceDocument"`
	Expected  expectedXML   `xml:"expected"`
	Current   currentXML    `xml:"current"`
}

func (t instanceTestXML) runnable() bool {
	return runnable(t.Expected.Validity, t.Current.Status)
}

func runnable(validity string, status string) bool {
	return (validity == "valid" || validity == "invalid") &&
		(status == "" || status == "accepted")
}

type documentXML struct {
	Href string `xml:"http://www.w3.org/1999/xlink href,attr"`
}

type expectedXML struct {
	Validity string `xml:"validity,attr"`
}

type currentXML struct {
	Status string `xml:"status,attr"`
}

type suiteResolver struct {
	root string
}

func (r *suiteResolver) Resolve(
	ctx context.Context,
	request resolve.Request,
) (resolve.Resource, error) {
	if err := ctx.Err(); err != nil {
		return resolve.Resource{}, err
	}
	parsed, err := url.Parse(request.URI)
	if err != nil || parsed.Scheme != "file" || parsed.Host != "" {
		return resolve.Resource{}, fmt.Errorf("xsdtest: unsupported suite URI %q", request.URI)
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return resolve.Resource{}, err
	}
	path, err = confinedPath(r.root, path)
	if err != nil {
		return resolve.Resource{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return resolve.Resource{}, err
	}
	return resolve.Resource{URI: request.URI, Content: content}, nil
}

func resolveMetadataReference(root string, metadata string, href string) (string, error) {
	parsed, err := url.Parse(href)
	if err != nil || parsed.IsAbs() || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("xsdtest: invalid relative test reference %q", href)
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return "", err
	}
	return confinedPath(root, filepath.Join(filepath.Dir(metadata), filepath.FromSlash(path)))
}

func confinedPath(root string, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	clean := filepath.Clean(path)
	relative, err := filepath.Rel(root, clean)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("xsdtest: path escapes suite root")
	}
	return clean, nil
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

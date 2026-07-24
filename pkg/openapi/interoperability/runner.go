//go:build interop

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"

	canonical "github.com/faustbrian/golib/pkg/json-schema"
	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/serialize"
	"github.com/faustbrian/golib/pkg/openapi/validate"
	"github.com/getkin/kin-openapi/openapi3"
	goopenapiloads "github.com/go-openapi/loads"
	"github.com/pb33f/libopenapi"
)

type result struct {
	parse     string
	model     string
	validate  string
	roundtrip string
}

func main() {
	paths := append([]string(nil), os.Args[1:]...)
	slices.Sort(paths)
	fmt.Println("fixture\ttool\tversion\tparse\tmodel\tvalidate\troundtrip")
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			panic("read interoperability fixture")
		}
		name := filepath.Base(path)
		write(name, "golib-openapi", "workspace", runOurs(path, raw))
		write(
			name, "getkin/kin-openapi",
			moduleVersion("github.com/getkin/kin-openapi"), runKin(name, raw),
		)
		write(
			name, "pb33f/libopenapi",
			moduleVersion("github.com/pb33f/libopenapi"), runLibopenapi(name, raw),
		)
		write(
			name, "go-openapi/loads",
			moduleVersion("github.com/go-openapi/loads"), runLoads(name, raw),
		)
	}
}

func moduleVersion(path string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		panic("read interoperability build information")
	}
	for _, dependency := range info.Deps {
		if dependency.Path == path {
			return dependency.Version
		}
	}
	panic("missing interoperability module version")
}

func write(fixture, tool, version string, outcome result) {
	fmt.Printf(
		"%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		fixture, tool, version,
		outcome.parse, outcome.model, outcome.validate, outcome.roundtrip,
	)
}

func runOurs(path string, raw []byte) result {
	limits := parse.DefaultLimits()
	var document openapi.Document
	var err error
	if strings.HasSuffix(path, ".json") {
		document, err = openapi.ParseJSON(
			context.Background(), bytes.NewReader(raw), limits,
		)
	} else {
		document, err = openapi.ParseYAML(
			context.Background(), bytes.NewReader(raw), limits,
		)
	}
	if err != nil {
		return result{parse: "reject", model: "na", validate: "na", roundtrip: "na"}
	}
	documentLimits := canonical.DefaultLimits()
	documentLimits.MaxEvaluationOps = 20_000_000
	validator, err := validate.NewValidatorWithDocumentSchemaLimits(documentLimits)
	if err != nil {
		return result{parse: "accept", model: "accept", validate: "reject", roundtrip: "na"}
	}
	validationOptions := validate.DefaultOptions()
	validationOptions.MaxReferences = 1_000_000
	validationOptions.ReferenceLimits.MaxTraversalNodes = 1_000_000
	report, err := validator.DocumentWithOptions(
		context.Background(), document, validationOptions,
	)
	validation := "accept"
	if err != nil || !report.Valid() {
		validation = "reject"
	}
	var rendered bytes.Buffer
	roundtrip := "accept"
	if err := serialize.JSON(
		context.Background(), &rendered, document, serialize.DefaultOptions(),
	); err != nil {
		roundtrip = "reject"
	} else if _, err := openapi.ParseJSON(
		context.Background(), bytes.NewReader(rendered.Bytes()), limits,
	); err != nil {
		roundtrip = "reject"
	}
	return result{
		parse: "accept", model: "accept", validate: validation,
		roundtrip: roundtrip,
	}
}

func runKin(name string, raw []byte) result {
	if strings.HasPrefix(name, "swagger20") {
		return result{parse: "na", model: "na", validate: "na", roundtrip: "na"}
	}
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	document, err := loader.LoadFromData(raw)
	if err != nil {
		return result{parse: "reject", model: "reject", validate: "na", roundtrip: "na"}
	}
	validation := "accept"
	if err := document.Validate(context.Background()); err != nil {
		validation = "reject"
	}
	roundtrip := "accept"
	if rendered, marshalErr := json.Marshal(document); marshalErr != nil {
		roundtrip = "reject"
	} else if reloaded, reloadErr := loader.LoadFromData(rendered); reloadErr != nil {
		roundtrip = "reject"
	} else if validationErr := reloaded.Validate(context.Background()); validationErr != nil {
		roundtrip = "reject"
	}
	return result{
		parse: "accept", model: "accept", validate: validation,
		roundtrip: roundtrip,
	}
}

func runLibopenapi(name string, raw []byte) result {
	document, err := libopenapi.NewDocument(raw)
	if err != nil {
		return result{parse: "reject", model: "na", validate: "na", roundtrip: "na"}
	}
	if strings.HasPrefix(name, "swagger20") {
		_, err = document.BuildV2Model()
	} else {
		_, err = document.BuildV3Model()
	}
	model := "accept"
	if err != nil {
		model = "reject"
	}
	roundtrip := "accept"
	rendered, renderErr := document.Render()
	if renderErr != nil {
		roundtrip = "reject"
	} else if reloaded, reloadErr := libopenapi.NewDocument(rendered); reloadErr != nil {
		roundtrip = "reject"
	} else if strings.HasPrefix(name, "swagger20") {
		if _, reloadErr = reloaded.BuildV2Model(); reloadErr != nil {
			roundtrip = "reject"
		}
	} else if _, reloadErr = reloaded.BuildV3Model(); reloadErr != nil {
		roundtrip = "reject"
	}
	return result{
		parse: "accept", model: model, validate: "na", roundtrip: roundtrip,
	}
}

func runLoads(name string, raw []byte) result {
	if !strings.HasPrefix(name, "swagger20") {
		return result{parse: "na", model: "na", validate: "na", roundtrip: "na"}
	}
	document, err := goopenapiloads.Analyzed(json.RawMessage(raw), "2.0")
	if err != nil {
		return result{parse: "reject", model: "reject", validate: "na", roundtrip: "na"}
	}
	roundtrip := "accept"
	if rendered, marshalErr := json.Marshal(document.Spec()); marshalErr != nil {
		roundtrip = "reject"
	} else if _, reloadErr := goopenapiloads.Analyzed(
		json.RawMessage(rendered), "2.0",
	); reloadErr != nil {
		roundtrip = "reject"
	}
	return result{
		parse: "accept", model: "accept", validate: "na", roundtrip: roundtrip,
	}
}

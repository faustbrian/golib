package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/faustbrian/golib/pkg/openapi/internal/modelgen"
	"github.com/faustbrian/golib/pkg/openapi/internal/specification"
)

const (
	maximumFieldInventoryBytes     = 8_388_608
	maximumFieldInventoryReadBytes = 8_388_609
)

var rootFlag = flag.String("root", ".", "openapi repository root")
var exitProcess = os.Exit

func main() {
	flag.Parse()
	exitProcess(execute(*rootFlag, os.Stderr))
}

func execute(root string, stderr io.Writer) int {
	if err := run(root); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func run(root string) error {
	return runWith(root, modelgen.GenerateTests)
}

type generateTestsFunc func(
	modelgen.Config,
	[]specification.ObjectField,
) ([]byte, error)

func runWith(root string, generateTests generateTestsFunc) error {
	fields, err := readFields(filepath.Join(root, "specification", "conformance", "object-fields.tsv"))
	if err != nil {
		return err
	}
	configs := []modelgen.Config{
		{Package: "oas32", Version: "3.2.0", RootObject: "OpenAPI Object", VersionField: "openapi", Dialect: "DialectOAS32", BooleanSchema: true},
		{Package: "oas31", Version: "3.1.2", RootObject: "OpenAPI Object", VersionField: "openapi", Dialect: "DialectOAS31", BooleanSchema: true},
		{Package: "oas30", Version: "3.0.4", RootObject: "OpenAPI Object", VersionField: "openapi", Dialect: "DialectOAS30"},
		{Package: "swagger20", Version: "2.0", RootObject: "Swagger Object", VersionField: "swagger", Dialect: "DialectSwagger20"},
	}
	for _, config := range configs {
		generated, err := modelgen.Generate(config, fields[config.Version])
		if err != nil {
			return fmt.Errorf("modelgen: generate %s: %w", config.Package, err)
		}
		directory := filepath.Join(root, config.Package)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("modelgen: create %s: %w", config.Package, err)
		}
		if err := writeAtomic(filepath.Join(directory, "model_generated.go"), generated); err != nil {
			return err
		}
		generatedTests, err := generateTests(config, fields[config.Version])
		if err != nil {
			return fmt.Errorf("modelgen: generate %s tests: %w", config.Package, err)
		}
		if err := writeAtomic(
			filepath.Join(directory, "model_generated_test.go"),
			generatedTests,
		); err != nil {
			return err
		}
	}
	return nil
}

func readFields(path string) (map[string][]specification.ObjectField, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("modelgen: open field inventory: %w", err)
	}
	defer func() { _ = file.Close() }()
	return decodeFields(file)
}

func decodeFields(input io.Reader) (map[string][]specification.ObjectField, error) {
	body, err := io.ReadAll(io.LimitReader(input, maximumFieldInventoryReadBytes))
	if err != nil || len(body) > maximumFieldInventoryBytes {
		return nil, errors.New("modelgen: field inventory exceeds byte limit")
	}
	reader := csv.NewReader(bytes.NewReader(body))
	reader.Comma = '\t'
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("modelgen: read field header: %w", err)
	}
	if len(header) != 11 || header[0] != "id" || header[10] != "description" {
		return nil, errors.New("modelgen: unexpected field inventory header")
	}

	result := make(map[string][]specification.ObjectField)
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("modelgen: read field row: %w", err)
		}
		line, err := strconv.Atoi(record[3])
		if err != nil {
			return nil, fmt.Errorf("modelgen: parse source line: %w", err)
		}
		pattern, err := strconv.ParseBool(record[8])
		if err != nil {
			return nil, fmt.Errorf("modelgen: parse pattern marker: %w", err)
		}
		required, err := strconv.ParseBool(record[9])
		if err != nil {
			return nil, fmt.Errorf("modelgen: parse required marker: %w", err)
		}
		field := specification.ObjectField{
			ID:          record[0],
			Version:     record[1],
			Source:      record[2],
			Line:        line,
			Object:      record[4],
			Variant:     record[5],
			Name:        record[6],
			Type:        record[7],
			Pattern:     pattern,
			Required:    required,
			Description: record[10],
		}
		result[field.Version] = append(result[field.Version], field)
	}
	return result, nil
}

func writeAtomic(path string, contents []byte) error {
	return writeAtomicWith(path, contents, func(
		directory string,
		pattern string,
	) (temporaryFile, error) {
		return os.CreateTemp(directory, pattern)
	})
}

type createTemporaryFile func(string, string) (temporaryFile, error)

func writeAtomicWith(
	path string,
	contents []byte,
	createTemporary createTemporaryFile,
) error {
	temporary, err := createTemporary(filepath.Dir(path), ".modelgen-*")
	if err != nil {
		return fmt.Errorf("modelgen: create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := writeAndCloseTemporary(temporary, contents); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("modelgen: replace generated file: %w", err)
	}
	return nil
}

type temporaryFile interface {
	Write([]byte) (int, error)
	Close() error
	Name() string
}

func writeAndCloseTemporary(temporary temporaryFile, contents []byte) error {
	if _, err := temporary.Write(contents); err != nil {
		closeErr := temporary.Close()
		return errors.Join(
			fmt.Errorf("modelgen: write temporary file: %w", err),
			closeErr,
		)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("modelgen: close temporary file: %w", err)
	}
	return nil
}

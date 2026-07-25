package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/internal/specification"
)

const (
	maximumSpecmatrixManifestBytes     = 1_048_576
	maximumSpecmatrixManifestReadBytes = 1_048_577
)

type manifest struct {
	GeneratedAt    string          `json:"generatedAt"`
	AcceptedErrata json.RawMessage `json:"acceptedErrata"`
	OpenAPI        struct {
		Repository    string            `json:"repository"`
		License       string            `json:"license"`
		LicenseSource string            `json:"licenseSource"`
		Revisions     map[string]string `json:"revisions"`
		Files         []struct {
			Version string `json:"version"`
			Source  string `json:"source"`
			Path    string `json:"path"`
			SHA256  string `json:"sha256"`
		} `json:"files"`
	} `json:"openapiSpecification"`
	JSONSchemaArtifacts json.RawMessage `json:"jsonSchemaArtifacts"`
	IANAArtifacts       json.RawMessage `json:"ianaArtifacts"`
	Independent         json.RawMessage `json:"independentDescriptions"`
	PublishedArtifacts  json.RawMessage `json:"publishedArtifacts"`
}

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
	return runWith(root, openInput, writeAtomic)
}

type inputFile interface {
	io.Reader
	Close() error
}

type openInputFile func(string) (inputFile, error)
type writeOutputFile func(string, func(io.Writer) error) error

func openInput(path string) (inputFile, error) { return os.Open(path) }

func runWith(
	root string,
	openFile openInputFile,
	writeOutput writeOutputFile,
) error {
	specificationRoot := filepath.Join(root, "specification")
	manifestFile, err := openFile(filepath.Join(specificationRoot, "manifest.json"))
	if err != nil {
		return fmt.Errorf("specmatrix: open manifest: %w", err)
	}
	defer func() { _ = manifestFile.Close() }()

	inputs, err := decodeManifest(manifestFile)
	if err != nil {
		return err
	}

	var occurrences []specification.Occurrence
	var objectFields []specification.ObjectField
	for _, input := range inputs.OpenAPI.Files {
		if input.Version == "all" || !strings.HasSuffix(input.Path, ".md") {
			continue
		}

		specificationFile, err := openFile(filepath.Join(specificationRoot, filepath.FromSlash(input.Path)))
		if err != nil {
			return fmt.Errorf("specmatrix: open %s: %w", input.Path, err)
		}
		extracted, extractErr := specification.ExtractNormative(input.Version, input.Path, specificationFile)
		closeErr := specificationFile.Close()
		if extractErr != nil {
			return fmt.Errorf("specmatrix: extract %s: %w", input.Path, extractErr)
		}
		if closeErr != nil {
			return fmt.Errorf("specmatrix: close %s: %w", input.Path, closeErr)
		}
		occurrences = append(occurrences, extracted...)

		objectFile, err := openFile(filepath.Join(specificationRoot, filepath.FromSlash(input.Path)))
		if err != nil {
			return fmt.Errorf("specmatrix: reopen %s: %w", input.Path, err)
		}
		extractedFields, extractFieldsErr := specification.ExtractObjectFields(
			input.Version,
			input.Path,
			objectFile,
		)
		closeFieldsErr := objectFile.Close()
		if extractFieldsErr != nil {
			return fmt.Errorf("specmatrix: extract object fields from %s: %w", input.Path, extractFieldsErr)
		}
		if closeFieldsErr != nil {
			return fmt.Errorf("specmatrix: close object field source %s: %w", input.Path, closeFieldsErr)
		}
		objectFields = append(objectFields, extractedFields...)
	}

	conformanceRoot := filepath.Join(specificationRoot, "conformance")
	if err := os.MkdirAll(conformanceRoot, 0o755); err != nil {
		return fmt.Errorf("specmatrix: create conformance directory: %w", err)
	}
	outputs := []struct {
		path  string
		write func(io.Writer) error
	}{
		{path: filepath.Join(conformanceRoot, "normative.tsv"), write: func(writer io.Writer) error {
			return specification.WriteNormativeTSV(writer, occurrences)
		}},
		{path: filepath.Join(conformanceRoot, "object-fields.tsv"), write: func(writer io.Writer) error {
			return specification.WriteObjectFieldsTSV(writer, objectFields)
		}},
	}

	evidencePath := filepath.Join(conformanceRoot, "evidence.tsv")
	if _, err := os.Stat(evidencePath); errors.Is(err, os.ErrNotExist) {
		outputs = append(outputs, struct {
			path  string
			write func(io.Writer) error
		}{path: evidencePath, write: func(writer io.Writer) error {
			return specification.WriteInitialEvidenceTSV(writer, occurrences)
		}})
	} else if err != nil {
		return fmt.Errorf("specmatrix: inspect evidence: %w", err)
	}
	for _, output := range outputs {
		if err := writeOutput(output.path, output.write); err != nil {
			return err
		}
	}

	return nil
}

func decodeManifest(input io.Reader) (manifest, error) {
	body, err := io.ReadAll(io.LimitReader(
		input, maximumSpecmatrixManifestReadBytes,
	))
	if err != nil || len(body) > maximumSpecmatrixManifestBytes {
		return manifest{}, errors.New("specmatrix: manifest exceeds byte limit")
	}
	var inputs manifest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&inputs); err != nil {
		return manifest{}, fmt.Errorf("specmatrix: decode manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return manifest{}, errors.New("specmatrix: manifest contains trailing data")
	}
	return inputs, nil
}

func writeAtomic(destination string, write func(io.Writer) error) error {
	return writeAtomicWith(destination, write, func(
		directory string,
		pattern string,
	) (temporaryOutput, error) {
		return os.CreateTemp(directory, pattern)
	})
}

type temporaryOutput interface {
	io.Writer
	Close() error
	Name() string
}

type createTemporaryOutput func(string, string) (temporaryOutput, error)

func writeAtomicWith(
	destination string,
	write func(io.Writer) error,
	createTemporary createTemporaryOutput,
) error {
	temporary, err := createTemporary(filepath.Dir(destination), ".specmatrix-*")
	if err != nil {
		return fmt.Errorf("specmatrix: create temporary output: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()

	if err := write(temporary); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("specmatrix: close temporary output: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("specmatrix: replace output: %w", err)
	}

	return nil
}

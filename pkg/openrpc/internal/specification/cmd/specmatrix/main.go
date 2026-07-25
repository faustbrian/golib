// Command specmatrix regenerates the reviewed conformance inventories from the
// pinned OpenRPC specification inputs.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/faustbrian/golib/pkg/openrpc/internal/specification"
)

const (
	specificationPath     = "specification/openrpc-1.4.1/spec-template.md"
	schemaPath            = "specification/openrpc-1.4.1/schema.json"
	normativePath         = "specification/conformance/normative.tsv"
	fieldsPath            = "specification/conformance/object-fields.tsv"
	normativeEvidencePath = "specification/conformance/evidence.tsv"
	fieldEvidencePath     = "specification/conformance/object-field-evidence.tsv"
)

func main() {
	runMain(os.Stderr, os.Exit)
}

func runMain(stderr io.Writer, exit func(int)) {
	switch err := run(); err {
	case nil:
		return
	default:
		_, _ = fmt.Fprintln(stderr, err)
		exit(1)
	}
}

func run() error {
	specificationInput, err := os.ReadFile(specificationPath)
	switch err {
	case nil:
	default:
		return fmt.Errorf("read specification: %w", err)
	}
	schemaInput, err := os.ReadFile(schemaPath)
	switch err {
	case nil:
	default:
		return fmt.Errorf("read schema: %w", err)
	}

	normative, fields, err := specification.GenerateMatrices(
		string(specificationInput),
		schemaInput,
	)
	switch err {
	case nil:
	default:
		return err
	}
	normativeEvidence, err := os.ReadFile(normativeEvidencePath)
	switch err {
	case nil:
	default:
		return fmt.Errorf("read normative evidence: %w", err)
	}
	normative, err = specification.ApplyNormativeEvidence(normative, normativeEvidence)
	switch err {
	case nil:
	default:
		return fmt.Errorf("apply normative evidence: %w", err)
	}
	fieldEvidence, err := os.ReadFile(fieldEvidencePath)
	switch err {
	case nil:
	default:
		return fmt.Errorf("read field evidence: %w", err)
	}
	fields, err = specification.ApplyFieldEvidence(fields, fieldEvidence)
	switch err {
	case nil:
	default:
		return fmt.Errorf("apply field evidence: %w", err)
	}
	//nolint:gosec // Output paths are fixed package constants, not user input.
	switch err := os.WriteFile(normativePath, normative, 0o600); err {
	case nil:
	default:
		return fmt.Errorf("write normative matrix: %w", err)
	}
	//nolint:gosec // Output paths are fixed package constants, not user input.
	switch err := os.WriteFile(fieldsPath, fields, 0o600); err {
	case nil:
	default:
		return fmt.Errorf("write object-field matrix: %w", err)
	}
	return nil
}

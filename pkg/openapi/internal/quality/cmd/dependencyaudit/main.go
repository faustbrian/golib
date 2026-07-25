package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	maximumEvidenceBytes                     = 1_048_576
	maximumEvidenceReadBytes                 = 1_048_577
	maximumModuleListBytes                   = 4_194_304
	maximumModuleListReadBytes               = 4_194_305
	maximumStderrBytes                       = 65_536
	moduleListTimeout          time.Duration = 30_000_000_000
)

var evidenceHeader = []string{
	"module", "version", "class", "license", "license-source", "source",
	"owner", "maintenance", "release", "necessity", "replacement",
}

type module struct {
	Path     string `json:"Path"`
	Version  string `json:"Version"`
	Main     bool   `json:"Main"`
	Indirect bool   `json:"Indirect"`
}

type evidenceFile interface {
	io.Reader
	Close() error
}

var exitProcess = os.Exit
var loadModules = listModules
var openEvidence = openEvidenceFile

func openEvidenceFile(path string) (evidenceFile, error) { return os.Open(path) }

func main() {
	exitProcess(execute(os.Args[1:], os.Stderr))
}

func execute(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("dependencyaudit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "openapi repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	modules, err := loadModules(*root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	path := filepath.Join(*root, "docs", "dependencies.tsv")
	file, err := openEvidence(path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "dependency audit: open evidence")
		return 1
	}
	defer func() { _ = file.Close() }()
	if err := verifyEvidence(file, modules); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func listModules(root string) ([]module, error) {
	ctx, cancel := context.WithTimeout(context.Background(), moduleListTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "list", "-m", "-json", "all")
	command.Dir = root
	var stdout limitedBuffer
	stdout.remaining = maximumModuleListReadBytes
	command.Stdout = &stdout
	command.Stderr = &limitedBuffer{remaining: maximumStderrBytes}
	if err := command.Run(); err != nil {
		return nil, errors.New("dependency audit: list module build graph")
	}
	return decodeModules(bytes.NewReader(stdout.Bytes()))
}

type limitedBuffer struct {
	bytes.Buffer
	remaining int
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	if len(value) > buffer.remaining {
		return 0, errors.New("output limit exceeded")
	}
	buffer.remaining -= len(value)
	return buffer.Buffer.Write(value)
}

func decodeModules(input io.Reader) ([]module, error) {
	body, err := io.ReadAll(io.LimitReader(input, maximumModuleListReadBytes))
	if err != nil || len(body) > maximumModuleListBytes {
		return nil, errors.New("dependency audit: module list exceeds limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var modules []module
	for {
		var item module
		if err := decoder.Decode(&item); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, errors.New("dependency audit: decode module list")
		}
		if item.Path == "" {
			return nil, errors.New("dependency audit: module path is required")
		}
		if !item.Main {
			modules = append(modules, item)
		}
	}
	return modules, nil
}

func verifyEvidence(input io.Reader, modules []module) error {
	body, err := io.ReadAll(io.LimitReader(input, maximumEvidenceReadBytes))
	if err != nil || len(body) > maximumEvidenceBytes {
		return errors.New("dependency audit: evidence exceeds limit")
	}
	reader := csv.NewReader(bytes.NewReader(body))
	reader.Comma = '\t'
	reader.FieldsPerRecord = len(evidenceHeader)
	records, err := reader.ReadAll()
	if err != nil {
		return errors.New("dependency audit: decode evidence")
	}
	if len(records) == 0 || !equalRecord(records[0], evidenceHeader) {
		return errors.New("dependency audit: invalid evidence header")
	}

	want := make(map[string]string, len(modules))
	for _, item := range modules {
		if item.Version == "" {
			return fmt.Errorf("dependency audit: module %q has no version", item.Path)
		}
		want[item.Path] = item.Version
	}
	seen := make(map[string]struct{})
	for _, row := range records[1:] {
		for _, field := range row {
			if strings.TrimSpace(field) == "" {
				return errors.New("dependency audit: evidence fields must not be empty")
			}
		}
		if row[2] != "runtime" && row[2] != "graph-only" {
			return fmt.Errorf("dependency audit: invalid class for %q", row[0])
		}
		if !isHTTPS(row[4]) || !isHTTPS(row[5]) {
			return fmt.Errorf("dependency audit: insecure source for %q", row[0])
		}
		version, exists := want[row[0]]
		if !exists {
			return fmt.Errorf("dependency audit: unexpected module %q", row[0])
		}
		if _, exists := seen[row[0]]; exists {
			return fmt.Errorf("dependency audit: duplicate module %q", row[0])
		}
		if row[1] != version {
			return fmt.Errorf("dependency audit: version drift for %q", row[0])
		}
		seen[row[0]] = struct{}{}
	}
	for path := range want {
		if _, exists := seen[path]; !exists {
			return fmt.Errorf("dependency audit: missing module %q", path)
		}
	}
	return nil
}

func isHTTPS(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" &&
		parsed.User == nil
}

func equalRecord(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

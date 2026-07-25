package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maximumManifestBytes     = 1_048_576
	maximumManifestReadBytes = 1_048_577
)

type artifact struct {
	Path   string
	SHA256 string
}

func main() {
	exitProcess(execute(os.Args[1:], os.Stderr))
}

var exitProcess = os.Exit

func execute(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("provenance", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "openapi repository root")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if err := verify(*root); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func verify(root string) error {
	specificationRoot := filepath.Join(root, "specification")
	manifestFile, err := os.Open(filepath.Join(specificationRoot, "manifest.json"))
	if err != nil {
		return fmt.Errorf("provenance: open manifest: %w", err)
	}
	defer func() { _ = manifestFile.Close() }()

	manifest, err := decodeManifest(manifestFile)
	if err != nil {
		return err
	}
	if err := validateArtifactMetadata(manifest); err != nil {
		return err
	}

	artifacts, err := collectArtifacts(manifest)
	if err != nil {
		return err
	}
	if len(artifacts) == 0 {
		return errors.New("provenance: manifest contains no artifacts")
	}
	seen := make(map[string]struct{}, len(artifacts))
	for _, item := range artifacts {
		if _, exists := seen[item.Path]; exists {
			return fmt.Errorf("provenance: duplicate artifact path %q", item.Path)
		}
		seen[item.Path] = struct{}{}

		path, err := secureArtifactPath(specificationRoot, item.Path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("provenance: read %q: %w", item.Path, err)
		}
		want, err := hex.DecodeString(item.SHA256)
		if err != nil || len(want) != sha256.Size {
			return fmt.Errorf("provenance: invalid SHA-256 for %q", item.Path)
		}
		got := sha256.Sum256(body)
		if !equalDigest(got[:], want) {
			return fmt.Errorf("provenance: checksum mismatch for %q", item.Path)
		}
	}

	return nil
}

func decodeManifest(input io.Reader) (any, error) {
	body, err := io.ReadAll(io.LimitReader(input, maximumManifestReadBytes))
	if err != nil || len(body) > maximumManifestBytes {
		return nil, errors.New("provenance: manifest exceeds byte limit")
	}
	var manifest any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("provenance: decode manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return nil, fmt.Errorf("provenance: trailing manifest data: %w", err)
	}
	return manifest, nil
}

func validateArtifactMetadata(value any) error {
	var walk func(any) error
	walk = func(current any) error {
		switch typed := current.(type) {
		case map[string]any:
			if files, exists := typed["files"]; exists {
				if _, ok := files.([]any); !ok {
					return errors.New("provenance: artifact files must be an array")
				}
				if err := validateArtifactGroup(typed); err != nil {
					return err
				}
			}
			for name, child := range typed {
				if name == "files" {
					continue
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range typed {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(value)
}

func validateArtifactGroup(group map[string]any) error {
	if textField(group, "license") == "" {
		return errors.New("provenance: artifact group license is required")
	}
	licenseSource := textField(group, "licenseSource")
	parsedSource, err := url.Parse(licenseSource)
	if err != nil || parsedSource.Scheme != "https" || parsedSource.Host == "" {
		return errors.New("provenance: artifact group license source must be HTTPS")
	}
	if !hasArtifactRevision(group) {
		return errors.New("provenance: artifact group revision or retrieval date is required")
	}
	files, _ := group["files"].([]any)
	for _, raw := range files {
		file, ok := raw.(map[string]any)
		if !ok || textField(file, "source") == "" {
			return errors.New("provenance: artifact source is required")
		}
	}
	return nil
}

func textField(value map[string]any, name string) string {
	text, _ := value[name].(string)
	return strings.TrimSpace(text)
}

func hasArtifactRevision(group map[string]any) bool {
	if textField(group, "revision") != "" {
		return true
	}
	if revisions, ok := group["revisions"].(map[string]any); ok && len(revisions) > 0 {
		for _, revision := range revisions {
			text, ok := revision.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return false
			}
		}
		return true
	}
	retrieved := textField(group, "retrievedAt")
	if retrieved == "" {
		return false
	}
	_, err := time.Parse(time.DateOnly, retrieved)
	return err == nil
}

func collectArtifacts(value any) ([]artifact, error) {
	var artifacts []artifact
	var walk func(any) error
	walk = func(current any) error {
		switch typed := current.(type) {
		case map[string]any:
			path, hasPath := typed["path"].(string)
			checksum, hasChecksum := typed["sha256"].(string)
			if hasPath != hasChecksum {
				return errors.New("provenance: artifact must contain path and sha256")
			}
			if hasPath {
				artifacts = append(artifacts, artifact{Path: path, SHA256: checksum})
			}
			for _, child := range typed {
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range typed {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(value); err != nil {
		return nil, err
	}
	return artifacts, nil
}

func secureArtifactPath(root, slashPath string) (string, error) {
	if slashPath == "" || strings.Contains(slashPath, "\\") || !filepath.IsLocal(slashPath) {
		return "", fmt.Errorf("provenance: unsafe artifact path %q", slashPath)
	}
	path := root
	var info os.FileInfo
	for _, element := range strings.Split(slashPath, "/") {
		path = filepath.Join(path, element)
		var err error
		info, err = os.Lstat(path)
		if err != nil {
			return "", fmt.Errorf("provenance: inspect %q: %w", slashPath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("provenance: artifact path %q contains a symlink", slashPath)
		}
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("provenance: artifact %q is not a regular file", slashPath)
	}
	return path, nil
}

func equalDigest(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}

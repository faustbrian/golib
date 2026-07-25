package specification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"strings"
)

var (
	// ErrManifestRead reports an unavailable manifest or pinned input.
	ErrManifestRead = errors.New("specification: pinned input unavailable")
	// ErrManifestInvalid reports malformed or unsafe manifest metadata.
	ErrManifestInvalid = errors.New("specification: invalid manifest")
	// ErrManifestMismatch reports a pinned input whose digest changed.
	ErrManifestMismatch = errors.New("specification: pinned input checksum mismatch")
)

type inputManifest struct {
	OpenRPC struct {
		Files []manifestFile `json:"files"`
	} `json:"openrpc"`
	Examples struct {
		Files []manifestFile `json:"files"`
	} `json:"examples"`
	JSONSchema struct {
		OpenRPCMetaDialect manifestFile `json:"openrpcMetaDialect"`
	} `json:"jsonSchema"`
}

type manifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// VerifyPinnedInputs checks every local artifact listed in the provenance
// manifest without network access.
func VerifyPinnedInputs(filesystem fs.FS) error {
	if filesystem == nil {
		return ErrManifestRead
	}
	data, err := fs.ReadFile(filesystem, "specification/manifest.json")
	if err != nil {
		return ErrManifestRead
	}
	var manifest inputManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ErrManifestInvalid
	}

	inputs := make([]manifestFile, 0,
		len(manifest.OpenRPC.Files)+len(manifest.Examples.Files)+1)
	for _, file := range manifest.OpenRPC.Files {
		file.Path = path.Join("openrpc-1.4.1", file.Path)
		inputs = append(inputs, file)
	}
	for _, file := range manifest.Examples.Files {
		file.Path = path.Join("examples", file.Path)
		inputs = append(inputs, file)
	}
	if manifest.JSONSchema.OpenRPCMetaDialect != (manifestFile{}) {
		inputs = append(inputs, manifest.JSONSchema.OpenRPCMetaDialect)
	}
	if len(inputs) == 0 {
		return ErrManifestInvalid
	}

	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if !validManifestFile(input) {
			return ErrManifestInvalid
		}
		fullPath := path.Join("specification", input.Path)
		if _, duplicate := seen[fullPath]; duplicate {
			return ErrManifestInvalid
		}
		seen[fullPath] = struct{}{}
		contents, err := fs.ReadFile(filesystem, fullPath)
		if err != nil {
			return ErrManifestRead
		}
		digest := sha256.Sum256(contents)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), input.SHA256) {
			return ErrManifestMismatch
		}
	}
	return nil
}

func validManifestFile(input manifestFile) bool {
	if !fs.ValidPath(input.Path) || strings.HasPrefix(input.Path, "../") ||
		len(input.SHA256) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(input.SHA256)
	return err == nil
}

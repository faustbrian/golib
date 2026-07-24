package reference

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"path"
	"strings"
)

var (
	// ErrStorePolicy reports invalid store configuration or byte bounds.
	ErrStorePolicy = errors.New("reference: invalid store policy")
	// ErrStoreURI reports a malformed, unknown, or out-of-scope document URI.
	ErrStoreURI = errors.New("reference: document URI outside store scope")
	// ErrStoreLimit reports a document larger than the supplied read bound.
	ErrStoreLimit = errors.New("reference: store byte limit exceeded")
	// ErrStoreRead reports a filesystem read failure without exposing paths or
	// underlying system details.
	ErrStoreRead = errors.New("reference: store read failed")
)

// MemoryStore is an immutable caller-supplied document store.
type MemoryStore struct {
	documents map[string][]byte
}

// NewMemoryStore validates absolute document URIs and owns every byte slice.
func NewMemoryStore(documents map[string][]byte) (*MemoryStore, error) {
	owned := make(map[string][]byte, len(documents))
	for documentURI, document := range documents {
		canonical, err := storeDocumentURI(documentURI)
		if err != nil {
			return nil, ErrStoreURI
		}
		if _, duplicate := owned[canonical]; duplicate {
			return nil, ErrStoreURI
		}
		owned[canonical] = append([]byte(nil), document...)
	}
	return &MemoryStore{documents: owned}, nil
}

// Load implements Store without I/O.
func (store *MemoryStore) Load(ctx context.Context, documentURI string, maxBytes int) ([]byte, error) {
	if store == nil || maxBytes <= 0 || ctx == nil {
		return nil, ErrStorePolicy
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	canonical, err := storeDocumentURI(documentURI)
	if err != nil {
		return nil, ErrStoreURI
	}
	document, exists := store.documents[canonical]
	if !exists {
		return nil, ErrStoreURI
	}
	if len(document) > maxBytes {
		return nil, ErrStoreLimit
	}
	return append([]byte(nil), document...), nil
}

// FSStore maps document URIs below one explicit absolute base into a supplied
// fs.FS. Supplying os.DirFS enables caller-authorized files; embed.FS and
// fstest.MapFS work without granting ambient filesystem access.
type FSStore struct {
	filesystem fs.FS
	base       *url.URL
}

// NewFSStore constructs a traversal-safe filesystem store.
func NewFSStore(filesystem fs.FS, baseURI string) (*FSStore, error) {
	if filesystem == nil {
		return nil, ErrStorePolicy
	}
	base, err := url.Parse(baseURI)
	if err != nil || !base.IsAbs() || base.Fragment != "" || base.RawQuery != "" ||
		base.User != nil || base.RawPath != "" || !strings.HasSuffix(base.Path, "/") {
		return nil, ErrStorePolicy
	}
	return &FSStore{filesystem: filesystem, base: base}, nil
}

// Load implements Store with a hard read limit and URI-scope enforcement.
func (store *FSStore) Load(ctx context.Context, documentURI string, maxBytes int) ([]byte, error) {
	if store == nil || store.filesystem == nil || store.base == nil || maxBytes <= 0 || ctx == nil {
		return nil, ErrStorePolicy
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := url.Parse(documentURI)
	if err != nil || target.Fragment != "" || target.RawQuery != "" ||
		target.User != nil || target.RawPath != "" ||
		!strings.EqualFold(target.Scheme, store.base.Scheme) ||
		!strings.EqualFold(target.Host, store.base.Host) ||
		!strings.HasPrefix(target.Path, store.base.Path) {
		return nil, ErrStoreURI
	}
	relative := strings.TrimPrefix(target.Path, store.base.Path)
	if relative == "" || path.Clean(relative) != relative || !fs.ValidPath(relative) {
		return nil, ErrStoreURI
	}
	file, err := store.filesystem.Open(relative)
	if err != nil {
		return nil, ErrStoreRead
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return nil, ErrStoreRead
	}
	if len(data) > maxBytes {
		return nil, ErrStoreLimit
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func storeDocumentURI(input string) (string, error) {
	parsed, err := url.Parse(input)
	if err != nil || !parsed.IsAbs() || parsed.Fragment != "" || !utf8ValidURI(input) {
		return "", ErrStoreURI
	}
	return parsed.String(), nil
}

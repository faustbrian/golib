package resolve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
)

const defaultFileMaxBytes int64 = 16 << 20

// ErrLimitExceeded reports a resolver resource larger than its explicit cap.
var ErrLimitExceeded = errors.New("xsd resolve: resource limit exceeded")

// FileOptions configures an opt-in, root-confined file resolver.
type FileOptions struct {
	Root     string
	MaxBytes int64
}

// File resolves file URIs beneath one opened filesystem root. It never
// resolves network URIs and os.Root prevents symlink traversal outside Root.
type File struct {
	path     string
	root     *os.Root
	maxBytes int64
}

// NewFile opens a traversal-resistant filesystem root. The caller must close
// the returned resolver when it is no longer needed.
func NewFile(options FileOptions) (*File, error) {
	if options.Root == "" {
		return nil, errors.New("xsd resolve: file root is required")
	}
	if !filepath.IsAbs(options.Root) {
		return nil, errors.New("xsd resolve: file root must be absolute")
	}
	if options.MaxBytes < 0 || options.MaxBytes == int64(^uint64(0)>>1) {
		return nil, errors.New("xsd resolve: file byte limit is outside the supported range")
	}
	maximum := options.MaxBytes
	if maximum == 0 {
		maximum = defaultFileMaxBytes
	}
	rootPath := filepath.Clean(options.Root)
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	return &File{path: rootPath, root: root, maxBytes: maximum}, nil
}

// Close releases the opened filesystem root.
func (r *File) Close() error {
	if r == nil || r.root == nil {
		return nil
	}
	return r.root.Close()
}

// Resolve reads a fresh owned copy of a confined file resource.
func (r *File) Resolve(ctx context.Context, request Request) (Resource, error) {
	if err := ctx.Err(); err != nil {
		return Resource{}, err
	}
	if r == nil || r.root == nil {
		return Resource{}, fmt.Errorf("%w: file resolver is not open", ErrAccessDenied)
	}
	parsed, err := url.Parse(request.URI)
	if err != nil || parsed.Scheme != "file" || parsed.Host != "" ||
		parsed.Opaque != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Resource{}, fmt.Errorf("%w: %s", ErrAccessDenied, request.URI)
	}
	requested := filePathFromURIPath(parsed.Path)
	if !filepath.IsAbs(requested) {
		return Resource{}, fmt.Errorf("%w: %s", ErrAccessDenied, request.URI)
	}
	relative, err := filepath.Rel(r.path, requested)
	relative, ok := confinedRelative(relative, err)
	if !ok {
		return Resource{}, fmt.Errorf("%w: %s", ErrAccessDenied, request.URI)
	}
	file, err := r.root.Open(relative)
	if err != nil {
		if os.IsNotExist(err) {
			return Resource{}, fmt.Errorf("%w: %s", ErrNotFound, request.URI)
		}
		return Resource{}, fmt.Errorf("%w: %s: %v", ErrAccessDenied, request.URI, err)
	}
	content, err := io.ReadAll(io.LimitReader(file, r.maxBytes+1))
	if operationErr := errors.Join(err, file.Close()); operationErr != nil {
		return Resource{}, operationErr
	}
	if int64(len(content)) > r.maxBytes {
		return Resource{}, fmt.Errorf("%w: %s exceeds %d bytes", ErrLimitExceeded, request.URI, r.maxBytes)
	}
	if err := ctx.Err(); err != nil {
		return Resource{}, err
	}
	return Resource{URI: request.URI, Content: content}, nil
}

func filePathFromURIPath(path string) string {
	requested := filepath.FromSlash(path)
	if len(requested) < 3 {
		return requested
	}
	if requested[0] != filepath.Separator {
		return requested
	}
	if requested[2] != ':' {
		return requested
	}

	return requested[1:]
}

func confinedRelative(relative string, err error) (string, bool) {
	if err != nil {
		return "", false
	}
	if !filepath.IsLocal(relative) {
		return "", false
	}

	return relative, true
}

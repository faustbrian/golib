package reference

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parse"
)

var (
	// ErrResourceDenied reports a resource identifier outside resolver policy.
	ErrResourceDenied = errors.New("reference resource denied")
	// ErrResourceLimitExceeded reports exhaustion of a resolver-wide budget.
	ErrResourceLimitExceeded = errors.New("reference resource limit exceeded")
	// ErrUnsupportedResourceFormat reports a resource whose syntax cannot be
	// selected without guessing.
	ErrUnsupportedResourceFormat = errors.New("unsupported reference resource format")
	// ErrResourceAccess reports an operational resource access failure whose
	// underlying message is withheld because it can contain sensitive paths.
	ErrResourceAccess = errors.New("reference resource access failed")
)

// FileResolverOptions defines the complete authority and resource budget of a
// local-file resolver. AllowedRoots must contain existing directories. Files
// are authorized after symlink evaluation, and only .json, .yaml, and .yml
// resources are accepted.
type FileResolverOptions struct {
	AllowedRoots []string
	MaxBytes     int64
	MaxDocuments int
	ParseLimits  parse.Limits
}

// DefaultFileResolverOptions returns bounded parsing defaults but no allowed
// roots. Callers must explicitly grant every filesystem root.
func DefaultFileResolverOptions() FileResolverOptions {
	return FileResolverOptions{
		MaxBytes:     16_777_216,
		MaxDocuments: 128,
		ParseLimits:  parse.DefaultLimits(),
	}
}

// NewFileResolver constructs a concurrency-safe file resolver with no ambient
// filesystem authority beyond the configured roots. Its document budget is
// cumulative, so callers should own one resolver per bounded operation.
func NewFileResolver(options FileResolverOptions) (*FileResolver, error) {
	return newFileResolver(options, canonicalDirectory, openFileRoot)
}

func newFileResolver(
	options FileResolverOptions,
	canonicalize func(string) (string, error),
	openRoot func(string) (fileRootHandle, error),
) (*FileResolver, error) {
	if len(options.AllowedRoots) == 0 {
		return nil, fmt.Errorf("file resolver: %w: no allowed roots", ErrResourceDenied)
	}
	if options.MaxBytes < 1 || options.MaxBytes == math.MaxInt64 {
		return nil, fmt.Errorf("file resolver: %w: invalid byte limit", ErrResourceLimitExceeded)
	}
	if options.MaxDocuments < 1 {
		return nil, fmt.Errorf("file resolver: %w: invalid document limit", ErrResourceLimitExceeded)
	}
	if err := validParseLimits(options.ParseLimits); err != nil {
		return nil, err
	}

	roots := make([]fileRoot, 0, len(options.AllowedRoots))
	seen := make(map[string]struct{}, len(options.AllowedRoots))
	for _, configured := range options.AllowedRoots {
		if configured == "" {
			return nil, errors.Join(
				errors.New("file resolver: allowed root is empty"),
				closeFileRoots(roots),
			)
		}
		rootPath, err := canonicalize(configured)
		if err != nil {
			return nil, errors.Join(fileAccessError("canonicalize allowed root"), closeFileRoots(roots))
		}
		if _, exists := seen[rootPath]; exists {
			continue
		}
		handle, err := openRoot(rootPath)
		if err != nil {
			return nil, errors.Join(
				fileAccessError("open allowed root"),
				closeFileRoots(roots),
			)
		}
		seen[rootPath] = struct{}{}
		roots = append(roots, fileRoot{path: rootPath, handle: handle})
	}
	limits := options.ParseLimits
	limits.MaxBytes = min(options.MaxBytes, limits.MaxBytes)
	return &FileResolver{
		roots:        roots,
		maxDocuments: options.MaxDocuments,
		parseLimits:  limits,
	}, nil
}

type fileRoot struct {
	path   string
	handle fileRootHandle
}

type fileRootHandle interface {
	Open(string) (*os.File, error)
	Close() error
}

func openFileRoot(path string) (fileRootHandle, error) {
	return os.OpenRoot(path)
}

// FileResolver resolves files beneath explicitly opened roots. It is safe for
// concurrent use. Call Close when the bounded operation is finished.
type FileResolver struct {
	roots        []fileRoot
	maxDocuments int
	parseLimits  parse.Limits

	mu        sync.Mutex
	documents int
	closed    bool
}

// Resolve opens and parses one authorized JSON or YAML resource.
func (resolver *FileResolver) Resolve(
	ctx context.Context,
	identifier string,
) (Resource, error) {
	if ctx == nil {
		return Resource{}, errors.New("file resolver: nil context")
	}
	if err := ctx.Err(); err != nil {
		return Resource{}, err
	}
	parsed, err := url.Parse(identifier)
	if err != nil || parsed.Scheme != "file" || parsed.Opaque != "" ||
		parsed.Host != "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || !filepath.IsAbs(parsed.Path) {
		return Resource{}, fmt.Errorf("file resolver: %w", ErrResourceDenied)
	}

	path, err := filepath.EvalSymlinks(filepath.Clean(parsed.Path))
	if err != nil {
		return Resource{}, fileAccessError("open resource")
	}
	authorizedRoot, relative, allowed := resolver.authorized(path)
	if !allowed {
		return Resource{}, fmt.Errorf("file resolver: %w", ErrResourceDenied)
	}
	format := strings.ToLower(filepath.Ext(path))
	if format != ".json" && format != ".yaml" && format != ".yml" {
		return Resource{}, fmt.Errorf("file resolver: %w", ErrUnsupportedResourceFormat)
	}
	if err := resolver.reserveDocument(); err != nil {
		return Resource{}, err
	}
	file, err := authorizedRoot.handle.Open(relative)
	if err != nil {
		return Resource{}, fileAccessError("open resource")
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return Resource{}, fileAccessError("inspect resource")
	}
	if !info.Mode().IsRegular() {
		return Resource{}, fmt.Errorf("file resolver: %w: not a regular file", ErrResourceDenied)
	}
	if info.Size() > resolver.parseLimits.MaxBytes {
		return Resource{}, fmt.Errorf("file resolver: %w", parse.ErrLimitExceeded)
	}
	var rootErr error
	var root jsonvalue.Value
	if format == ".json" {
		root, rootErr = parse.JSON(ctx, file, resolver.parseLimits)
	} else {
		root, rootErr = parse.YAML(ctx, file, resolver.parseLimits)
	}
	if rootErr != nil {
		return Resource{}, fmt.Errorf("file resolver: parse resource: %w", rootErr)
	}
	canonical := (&url.URL{Scheme: "file", Path: path}).String()
	return Resource{
		RetrievalURI: identifier,
		CanonicalURI: canonical,
		Root:         root,
	}, nil
}

// Close releases every traversal-resistant root handle. Active Resolve calls
// may finish or fail with their current filesystem operation.
func (resolver *FileResolver) Close() error {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	if resolver.closed {
		return nil
	}
	resolver.closed = true
	return closeFileRoots(resolver.roots)
}

func (resolver *FileResolver) authorized(path string) (*fileRoot, string, bool) {
	for index := range resolver.roots {
		root := &resolver.roots[index]
		relative, err := filepath.Rel(root.path, path)
		if err == nil && relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return root, relative, true
		}
	}
	return nil, "", false
}

func (resolver *FileResolver) reserveDocument() error {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	if resolver.closed {
		return fmt.Errorf("file resolver: %w: resolver is closed", ErrResourceDenied)
	}
	if resolver.documents >= resolver.maxDocuments {
		return fmt.Errorf("file resolver: %w: document count", ErrResourceLimitExceeded)
	}
	resolver.documents++
	return nil
}

func closeFileRoots(roots []fileRoot) error {
	errorsByRoot := make([]error, 0, len(roots))
	for index := range roots {
		if err := roots[index].handle.Close(); err != nil {
			errorsByRoot = append(errorsByRoot, fileAccessError("close allowed root"))
		}
	}
	return errors.Join(errorsByRoot...)
}

func canonicalDirectory(path string) (string, error) {
	return canonicalDirectoryWith(path, filepath.Abs, filepath.EvalSymlinks, os.Stat)
}

func canonicalDirectoryWith(
	path string,
	absolutePath func(string) (string, error),
	evaluateSymlinks func(string) (string, error),
	stat func(string) (os.FileInfo, error),
) (string, error) {
	absolute, err := absolutePath(path)
	if err != nil {
		return "", fileAccessError("canonicalize allowed root")
	}
	canonical, err := evaluateSymlinks(absolute)
	if err != nil {
		return "", fileAccessError("canonicalize allowed root")
	}
	info, err := stat(canonical)
	if err != nil {
		return "", fileAccessError("inspect allowed root")
	}
	if !info.IsDir() {
		return "", errors.New("file resolver: allowed root is not a directory")
	}
	return filepath.Clean(canonical), nil
}

func fileAccessError(operation string) error {
	return fmt.Errorf("file resolver: %s: %w", operation, ErrResourceAccess)
}

func validParseLimits(limits parse.Limits) error {
	if limits.MaxBytes < 1 || limits.MaxBytes == math.MaxInt64 ||
		limits.MaxTokens < 1 || limits.MaxDepth < 1 ||
		limits.MaxObjectMembers < 1 || limits.MaxArrayItems < 1 ||
		limits.MaxScalarBytes < 1 || limits.MaxTotalValues < 1 {
		return fmt.Errorf("file resolver: %w: invalid parse limits", ErrResourceLimitExceeded)
	}
	return nil
}

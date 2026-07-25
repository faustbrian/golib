// Package sourceio provides immutable, bounded byte and fs.FS inputs for
// format source packages.
package sourceio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"time"

	config "github.com/faustbrian/golib/pkg/config"
)

// Input is a repeatable immutable byte or filesystem input.
type Input struct {
	data     []byte
	open     func(context.Context) (io.ReadCloser, error)
	stableFS bool
}

const closeTimeout = time.Second

// Bytes returns an immutable copy-backed input.
func Bytes(data []byte) Input { return Input{data: bytes.Clone(data)} }

// ContextReader returns a reader over data that checks ctx before every read.
func ContextReader(ctx context.Context, data []byte) io.Reader {
	return &contextReader{ctx: ctx, reader: bytes.NewReader(data)}
}

type contextReader struct {
	ctx    context.Context
	reader *bytes.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

// FromFS returns a repeatable input for path.
func FromFS(filesystem fs.FS, path string) (Input, error) {
	if filesystem == nil {
		return Input{}, errors.New("source filesystem must not be nil")
	}
	if !fs.ValidPath(path) {
		return Input{}, fmt.Errorf("source path %q is invalid", path)
	}
	return Input{open: func(ctx context.Context) (io.ReadCloser, error) {
		var (
			file fs.File
			err  error
		)
		file, err = openFile(ctx, filesystem, path)
		if err != nil {
			if file != nil {
				_ = closeInput(ctx, file)
			}
			if classifyNotExist(err) {
				return nil, config.ErrNotFound
			}
			return nil, err
		}
		return file, nil
	}, stableFS: true}, nil
}

// Read returns at most maxBytes and checks ctx between reader operations.
func (i Input) Read(ctx context.Context, maxBytes int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if i.open == nil {
		if int64(len(i.data)) > maxBytes {
			return nil, fmt.Errorf("source input exceeds %d byte limit", maxBytes)
		}
		return bytes.Clone(i.data), nil
	}

	reader, err := i.open(ctx)
	if err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, errors.New("filesystem source returned nil file")
	}
	var before fileVersion
	if i.stableFS {
		file, ok := reader.(fs.File)
		if !ok {
			_ = closeInput(ctx, reader)
			return nil, errors.New("filesystem source did not return a file")
		}
		before, err = inspectFile(ctx, file)
		if err != nil {
			_ = closeInput(ctx, reader)
			return nil, err
		}
	}
	data, readErr := Read(ctx, reader, maxBytes)
	var after fileVersion
	if readErr == nil && i.stableFS {
		after, err = inspectFile(ctx, reader.(fs.File))
	}
	closeErr := closeInput(ctx, reader)
	if readErr != nil {
		return nil, readErr
	}
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if i.stableFS && fileChanged(before, after) {
		return nil, config.ErrSourceChanged
	}
	return data, nil
}

type fileVersion struct {
	size       int64
	mode       fs.FileMode
	modified   time.Time
	generation string
	generated  bool
	change     string
	tracked    bool
}

func inspectFile(ctx context.Context, file fs.File) (fileVersion, error) {
	info, err := statContext(ctx, file)
	if err != nil {
		return fileVersion{}, err
	}
	if info == nil {
		return fileVersion{}, errors.New("filesystem source returned nil file metadata")
	}
	version, err := snapshotFileInfo(info)
	if err != nil {
		return fileVersion{}, err
	}
	if generated, ok := file.(config.GenerationFile); ok {
		version.generation, err = generationContext(ctx, generated)
		if err != nil {
			return fileVersion{}, err
		}
		version.generated = true
	}
	return version, nil
}

func fileChanged(before, after fileVersion) bool {
	if before.generated != after.generated ||
		before.generated && before.generation != after.generation {
		return true
	}
	return before.size != after.size ||
		before.mode != after.mode ||
		!before.modified.Equal(after.modified) ||
		before.tracked != after.tracked ||
		before.tracked && before.change != after.change
}

// Read reads a bounded stream and checks ctx between reader operations.
func Read(ctx context.Context, reader io.Reader, maxBytes int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("source reader must not be nil")
	}
	limit := maxBytes + 1
	if maxBytes == math.MaxInt64 {
		limit = maxBytes
	}
	var buffer bytes.Buffer
	chunk := make([]byte, 32*1024)
	emptyReads := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := limit - int64(buffer.Len())
		if remaining == 0 {
			break
		}
		readBuffer := chunk
		if remaining < int64(len(readBuffer)) {
			readBuffer = readBuffer[:remaining]
		}
		count, err := readContext(ctx, reader, readBuffer)
		if count < 0 || count > len(readBuffer) {
			return nil, errors.New("source reader returned invalid byte count")
		}
		if count > 0 {
			_, _ = buffer.Write(chunk[:count])
			emptyReads = 0
		} else if err == nil {
			emptyReads++
			if emptyReads >= 100 {
				return nil, io.ErrNoProgress
			}
		}
		// Reader implementations must return io.EOF directly. Avoid invoking
		// extension-owned Is methods while controlling the file lifecycle.
		//nolint:errorlint // Deliberately do not traverse an untrusted error.
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if int64(buffer.Len()) > maxBytes {
		return nil, fmt.Errorf("source input exceeds %d byte limit", maxBytes)
	}
	return buffer.Bytes(), nil
}

func readContext(ctx context.Context, reader io.Reader, buffer []byte) (int, error) {
	return readContextSafely(ctx, reader, buffer)
}

func openFile(ctx context.Context, filesystem fs.FS, path string) (file fs.File, err error) {
	defer func() {
		if recover() != nil {
			file = nil
			err = errors.New("filesystem open panicked")
		}
	}()
	if contextual, ok := filesystem.(config.ContextFS); ok {
		return contextual.OpenContext(ctx, path)
	}
	return filesystem.Open(path)
}

//nolint:errorlint // Only traverse trusted wrappers; never call extension code.
func classifyNotExist(sourceErr error) bool {
	for range 16 {
		if sourceErr == fs.ErrNotExist {
			return true
		}
		pathError, ok := sourceErr.(*fs.PathError)
		if !ok || pathError == nil {
			return false
		}
		sourceErr = pathError.Err
	}
	return false
}

func readContextSafely(
	ctx context.Context,
	reader io.Reader,
	buffer []byte,
) (count int, err error) {
	defer func() {
		if recover() != nil {
			count = 0
			err = errors.New("filesystem read panicked")
		}
	}()
	if contextual, ok := reader.(interface {
		ReadContext(context.Context, []byte) (int, error)
	}); ok {
		return contextual.ReadContext(ctx, buffer)
	}
	return reader.Read(buffer)
}

func closeInput(ctx context.Context, reader io.Closer) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("filesystem close panicked")
		}
	}()
	if contextual, ok := reader.(config.ContextCloser); ok {
		cleanupCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			closeTimeout,
		)
		defer cancel()
		return contextual.CloseContext(cleanupCtx)
	}
	return reader.Close()
}

func statContext(
	ctx context.Context,
	file fs.File,
) (info fs.FileInfo, err error) {
	defer func() {
		if recover() != nil {
			info = nil
			err = errors.New("filesystem stat panicked")
		}
	}()
	if contextual, ok := file.(config.ContextFile); ok {
		return contextual.StatContext(ctx)
	}
	return file.Stat()
}

func snapshotFileInfo(info fs.FileInfo) (version fileVersion, err error) {
	defer func() {
		if recover() != nil {
			version = fileVersion{}
			err = errors.New("filesystem metadata inspection panicked")
		}
	}()
	version.size = info.Size()
	version.mode = info.Mode()
	version.modified = info.ModTime()
	version.change, version.tracked = systemChangeToken(info)
	return version, nil
}

func generationContext(
	ctx context.Context,
	file config.GenerationFile,
) (generation string, err error) {
	defer func() {
		if recover() != nil {
			generation = ""
			err = errors.New("filesystem generation lookup panicked")
		}
	}()
	return file.GenerationContext(ctx)
}

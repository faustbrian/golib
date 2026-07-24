package tabular

import (
	"archive/zip"
	"errors"
	"io"
	"path"
	"strings"
)

const (
	defaultMaxZIPEntries    = 1000
	defaultMaxZIPEntryBytes = 1024 * 1024 * 1024
	defaultMaxZIPTotalBytes = 4 * 1024 * 1024 * 1024
)

// ZIPConfig defines archive-bomb safeguards. Zero values select documented
// defaults rather than disabling limits.
type ZIPConfig struct {
	MaxEntries    int
	MaxEntryBytes uint64
	MaxTotalBytes uint64
}

// ZIPEntry is immutable metadata for one archive member.
type ZIPEntry struct {
	Name             string
	UncompressedSize uint64
	Directory        bool
}

// ZIPArchive is a validated, indexed ZIP source.
type ZIPArchive struct {
	entries []ZIPEntry
	files   map[string]*zip.File
}

type zipEntryReader struct {
	io.ReadCloser
}

func (reader *zipEntryReader) Read(destination []byte) (int, error) {
	count, err := reader.ReadCloser.Read(destination)
	if err != nil && !errors.Is(err, io.EOF) {
		return count, &Error{Kind: ErrorArchive, Op: "zip.entry.read", Format: "zip", Err: err}
	}
	return count, err
}

// OpenZIP validates and indexes an archive from a random-access source.
func OpenZIP(source io.ReaderAt, size int64, config ZIPConfig) (*ZIPArchive, error) {
	if source == nil || size < 0 || config.MaxEntries < 0 {
		return nil, &Error{Kind: ErrorInvalidConfig, Op: "zip.open", Format: "zip"}
	}
	maxEntries := config.MaxEntries
	if maxEntries == 0 {
		maxEntries = defaultMaxZIPEntries
	}
	maxEntryBytes := config.MaxEntryBytes
	if maxEntryBytes == 0 {
		maxEntryBytes = defaultMaxZIPEntryBytes
	}
	maxTotalBytes := config.MaxTotalBytes
	if maxTotalBytes == 0 {
		maxTotalBytes = defaultMaxZIPTotalBytes
	}

	reader, err := zip.NewReader(source, size)
	if err != nil {
		return nil, &Error{Kind: ErrorArchive, Op: "zip.open", Format: "zip", Err: err}
	}
	if len(reader.File) > maxEntries {
		return nil, &Error{Kind: ErrorLimitExceeded, Op: "zip.open", Format: "zip", Err: errors.New("archive contains too many entries")}
	}

	archive := &ZIPArchive{
		entries: make([]ZIPEntry, 0, len(reader.File)),
		files:   make(map[string]*zip.File, len(reader.File)),
	}
	var total uint64
	for _, file := range reader.File {
		if !safeZIPName(file.Name) {
			return nil, &Error{Kind: ErrorArchive, Op: "zip.open", Format: "zip", Err: errors.New("archive contains an unsafe entry name")}
		}
		if _, exists := archive.files[file.Name]; exists {
			return nil, &Error{Kind: ErrorArchive, Op: "zip.open", Format: "zip", Err: errors.New("archive contains a duplicate entry name")}
		}
		if file.UncompressedSize64 > maxEntryBytes {
			return nil, &Error{Kind: ErrorLimitExceeded, Op: "zip.open", Format: "zip", Err: errors.New("archive entry is too large")}
		}
		if file.UncompressedSize64 > maxTotalBytes-total {
			return nil, &Error{Kind: ErrorLimitExceeded, Op: "zip.open", Format: "zip", Err: errors.New("archive is too large")}
		}
		total += file.UncompressedSize64
		archive.files[file.Name] = file
		archive.entries = append(archive.entries, ZIPEntry{
			Name:             file.Name,
			UncompressedSize: file.UncompressedSize64,
			Directory:        file.FileInfo().IsDir(),
		})
	}
	return archive, nil
}

// Entries returns archive metadata in the original central-directory order.
func (archive *ZIPArchive) Entries() []ZIPEntry {
	return append([]ZIPEntry(nil), archive.entries...)
}

// Open returns a streaming reader for an exact, non-directory entry name.
func (archive *ZIPArchive) Open(name string) (io.ReadCloser, error) {
	file, ok := archive.files[name]
	if !ok {
		return nil, &Error{Kind: ErrorEntryNotFound, Op: "zip.entry.open", Format: "zip", Err: errors.New(name)}
	}
	if file.FileInfo().IsDir() {
		return nil, &Error{Kind: ErrorArchive, Op: "zip.entry.open", Format: "zip", Err: errors.New("entry is a directory")}
	}
	reader, err := file.Open()
	if err != nil {
		return nil, &Error{Kind: ErrorArchive, Op: "zip.entry.open", Format: "zip", Err: err}
	}
	return &zipEntryReader{ReadCloser: reader}, nil
}

// Extract streams one exact entry to destination and verifies its ZIP checksum.
func (archive *ZIPArchive) Extract(name string, destination io.Writer) error {
	if destination == nil {
		return &Error{Kind: ErrorInvalidConfig, Op: "zip.extract", Format: "zip"}
	}
	reader, err := archive.Open(name)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, reader)
	_ = reader.Close()
	if copyErr != nil {
		return &Error{Kind: ErrorArchive, Op: "zip.extract", Format: "zip", Err: copyErr}
	}
	return nil
}

func safeZIPName(name string) bool {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return false
	}
	cleanedName := strings.TrimSuffix(name, "/")
	return cleanedName != "" && path.Clean(cleanedName) == cleanedName &&
		cleanedName != ".." && !strings.HasPrefix(cleanedName, "../")
}

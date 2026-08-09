// Package externalsort provides bounded external sorting for fixed-size
// records encrypted at rest in owner-only temporary storage.
package externalsort

import (
	"bytes"
	"container/heap"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
)

const (
	// AES256KeyBytes is the exact key size accepted by Factory.Open.
	AES256KeyBytes = 32
	// MaximumMergeFiles is the hard upper bound for one merge fan-in.
	MaximumMergeFiles = 64
	// MaximumRecordBytes bounds one fixed-size record.
	MaximumRecordBytes = 1 << 20
	// MaximumChunkRecords bounds the number of records held in memory.
	MaximumChunkRecords = 1_000_000
	// MaximumChunkBytes bounds the contiguous in-memory sort buffer.
	MaximumChunkBytes = 256 << 20

	chunkPrefix  = "chunk-"
	storePrefix  = ".external-sort-"
	aadVersion   = "extsort1"
	redacted     = "[REDACTED]"
	redactedJSON = `"` + redacted + `"`
)

var (
	ErrInvalidConfiguration = errors.New(
		"external sort configuration is invalid",
	)
	ErrUnsafeParent = errors.New(
		"external sort parent is not an owner-only directory",
	)
	ErrInvalidKey    = errors.New("external sort key is invalid")
	ErrInvalidRecord = errors.New("external sort record is invalid")
	ErrRecordLimit   = errors.New("external sort record limit reached")
	ErrClosed        = errors.New("external sort store is closed")
	ErrFinalized     = errors.New("external sort store is finalized")
	ErrEntropy       = errors.New("external sort entropy failed")
	ErrStorage       = errors.New("external sort storage failed")
	ErrCorrupt       = errors.New("external sort chunk is corrupt")
)

// Config declares every storage and memory bound before temporary storage is
// opened. MaximumRecords divided into ChunkRecords MUST fit one merge of at
// most MaximumMergeFiles.
type Config struct {
	ParentDirectory string
	RecordBytes     int
	ChunkRecords    int
	MaximumRecords  int
}

// Factory validates a reusable external-sort storage policy. Factory is safe
// for concurrent use. Stores returned by Open are not safe for concurrent use.
type Factory struct {
	config     Config
	entropy    io.Reader
	mkdirTemp  func(string, string) (string, error)
	createTemp func(string, string) (chunkFile, error)
	openFile   func(string) (chunkFile, error)
	chmod      func(string, os.FileMode) error
	remove     func(string) error
	removeAll  func(string) error
}

// String prevents configuration and temporary paths from entering text logs.
func (Factory) String() string {
	return redacted
}

// GoString prevents configuration and temporary paths from entering debug
// representations.
func (Factory) GoString() string {
	return redacted
}

// LogValue prevents configuration and temporary paths from entering slog.
func (Factory) LogValue() slog.Value {
	return slog.StringValue(redacted)
}

// MarshalJSON prevents configuration and temporary paths from entering JSON.
func (Factory) MarshalJSON() ([]byte, error) {
	return []byte(redactedJSON), nil
}

// NewFactory validates bounds and requires an existing non-symlink parent
// whose group and other permission bits are clear.
func NewFactory(config Config) (*Factory, error) {
	if !validConfig(config) {
		return nil, ErrInvalidConfiguration
	}
	config.ParentDirectory = filepath.Clean(config.ParentDirectory)
	if err := validateParent(config.ParentDirectory); err != nil {
		return nil, err
	}

	return &Factory{
		config:     config,
		entropy:    rand.Reader,
		mkdirTemp:  os.MkdirTemp,
		createTemp: createTemporaryChunk,
		openFile:   openChunk,
		chmod:      os.Chmod,
		remove:     os.Remove,
		removeAll:  os.RemoveAll,
	}, nil
}

// Open creates one owner-only temporary work directory. The caller provides
// an AES-256 key for immediate cipher construction and MUST call
// Close on the returned store on every path.
func (factory *Factory) Open(
	ctx context.Context,
	key []byte,
) (*Store, error) {
	if factory == nil || !validConfig(factory.config) ||
		factory.entropy == nil || factory.mkdirTemp == nil ||
		factory.createTemp == nil || factory.openFile == nil ||
		factory.chmod == nil || factory.remove == nil ||
		factory.removeAll == nil {
		return nil, ErrInvalidConfiguration
	}
	if ctx == nil {
		return nil, ErrInvalidConfiguration
	}
	if len(key) != AES256KeyBytes {
		return nil, ErrInvalidKey
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateParent(factory.config.ParentDirectory); err != nil {
		return nil, err
	}

	block, _ := aes.NewCipher(key)
	authenticatedCipher, _ := cipher.NewGCM(block)
	directory, err := factory.mkdirTemp(
		factory.config.ParentDirectory,
		storePrefix,
	)
	if err != nil {
		return nil, ErrStorage
	}
	if err := factory.chmod(directory, 0o700); err != nil {
		_ = factory.removeAll(directory)

		return nil, ErrStorage
	}

	return &Store{
		config:     factory.config,
		entropy:    factory.entropy,
		createTemp: factory.createTemp,
		openFile:   factory.openFile,
		remove:     factory.remove,
		removeAll:  factory.removeAll,
		directory:  directory,
		cipher:     authenticatedCipher,
		buffer: recordBuffer{
			data: make(
				[]byte,
				0,
				factory.config.RecordBytes*factory.config.ChunkRecords,
			),
			recordBytes: factory.config.RecordBytes,
		},
	}, nil
}

// Store owns encrypted temporary chunks for one sort. It is deliberately
// single-owner and MUST NOT be used concurrently. A record passed to the
// ForEachSorted callback is valid only until that callback returns.
type Store struct {
	config     Config
	entropy    io.Reader
	createTemp func(string, string) (chunkFile, error)
	openFile   func(string) (chunkFile, error)
	remove     func(string) error
	removeAll  func(string) error
	directory  string
	cipher     cipher.AEAD
	buffer     recordBuffer
	chunks     []string
	total      int
	finalized  bool
	closing    bool
	closed     bool
}

// String prevents records, cipher state, and temporary paths from entering
// text logs.
func (Store) String() string {
	return redacted
}

// GoString prevents records, cipher state, and temporary paths from entering
// debug representations.
func (Store) GoString() string {
	return redacted
}

// LogValue prevents records, cipher state, and temporary paths from entering
// slog.
func (Store) LogValue() slog.Value {
	return slog.StringValue(redacted)
}

// MarshalJSON prevents records, cipher state, and temporary paths from entering
// JSON.
func (Store) MarshalJSON() ([]byte, error) {
	return []byte(redactedJSON), nil
}

// Add copies one record into the bounded in-memory chunk. A failed Add does
// not retain the supplied record and may be retried unless temporary cleanup
// fails, in which case only Close is accepted.
func (store *Store) Add(ctx context.Context, record []byte) error {
	if store == nil || ctx == nil {
		return ErrInvalidConfiguration
	}
	if store.closed || store.closing {
		return ErrClosed
	}
	if store.finalized {
		return ErrFinalized
	}
	if !store.valid() {
		return ErrInvalidConfiguration
	}
	if len(record) != store.config.RecordBytes {
		return ErrInvalidRecord
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if store.total >= store.config.MaximumRecords {
		return ErrRecordLimit
	}

	store.buffer.append(record)
	store.total++
	if store.buffer.Len() < store.config.ChunkRecords {
		return nil
	}
	if err := store.spill(ctx); err != nil {
		store.buffer.removeOne(record)
		store.total--

		return err
	}

	return nil
}

// ForEachSorted finalizes input and yields every record in ascending byte
// order. Duplicate records are preserved. Once pending input is spilled, the
// store remains finalized even when merge iteration, authentication,
// cancellation, or the callback fails.
func (store *Store) ForEachSorted(
	ctx context.Context,
	yield func([]byte) error,
) (result error) {
	if store == nil || ctx == nil || yield == nil {
		return ErrInvalidConfiguration
	}
	if store.closed || store.closing {
		return ErrClosed
	}
	if store.finalized {
		return ErrFinalized
	}
	if !store.valid() {
		return ErrInvalidConfiguration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if store.buffer.Len() > 0 {
		if err := store.spill(ctx); err != nil {
			return err
		}
	}
	store.finalized = true
	if len(store.chunks) == 0 {
		return nil
	}

	readers, err := store.openReaders()
	if err != nil {
		return err
	}
	defer func() {
		closeErr := closeReaders(readers)
		if result == nil {
			result = closeErr
		}
	}()

	return merge(ctx, readers, yield)
}

// Close clears in-memory records and removes the complete work directory.
// It is idempotent. A storage failure leaves the store closable so cleanup can
// be retried, but no other operation is accepted after closing begins.
func (store *Store) Close() error {
	if store == nil || store.closed {
		return nil
	}
	if !store.valid() {
		return ErrInvalidConfiguration
	}
	store.closing = true
	store.buffer.clear()
	if err := store.removeAll(store.directory); err != nil {
		return ErrStorage
	}
	store.directory = ""
	store.chunks = nil
	store.cipher = nil
	store.entropy = nil
	store.closing = false
	store.closed = true

	return nil
}

func (store *Store) valid() bool {
	return validConfig(store.config) &&
		store.entropy != nil &&
		store.createTemp != nil &&
		store.openFile != nil &&
		store.remove != nil &&
		store.removeAll != nil &&
		store.directory != "" &&
		store.cipher != nil &&
		store.buffer.recordBytes == store.config.RecordBytes
}

func validConfig(config Config) bool {
	if config.ParentDirectory == "" ||
		!filepath.IsAbs(config.ParentDirectory) ||
		config.RecordBytes <= 0 ||
		config.RecordBytes > MaximumRecordBytes ||
		config.ChunkRecords <= 0 ||
		config.ChunkRecords > MaximumChunkRecords ||
		config.MaximumRecords < config.ChunkRecords ||
		config.RecordBytes > MaximumChunkBytes/config.ChunkRecords {
		return false
	}
	chunks := config.MaximumRecords / config.ChunkRecords
	if config.MaximumRecords%config.ChunkRecords != 0 {
		chunks++
	}

	return chunks <= MaximumMergeFiles
}

func validateParent(parent string) error {
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() ||
		info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o077 != 0 {
		return ErrUnsafeParent
	}

	return nil
}

func (store *Store) spill(ctx context.Context) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	sort.Sort(&store.buffer)
	file, err := store.createTemp(store.directory, chunkPrefix)
	if err != nil {
		return ErrStorage
	}
	path := file.Name()
	complete := false
	defer func() {
		if !complete {
			closeErr := file.Close()
			removeErr := store.remove(path)
			if closeErr != nil || removeErr != nil {
				store.closing = true
				result = ErrStorage
			}
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return ErrStorage
	}

	chunkIndex := uint64(len(store.chunks))
	for recordIndex := 0; recordIndex < store.buffer.Len(); recordIndex++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		nonce := make([]byte, store.cipher.NonceSize())
		if _, err := io.ReadFull(store.entropy, nonce); err != nil {
			clear(nonce)

			return ErrEntropy
		}
		plaintext := store.buffer.record(recordIndex)
		aad := additionalData(
			chunkIndex,
			uint64(recordIndex),
			uint64(store.config.RecordBytes),
		)
		encrypted := store.cipher.Seal(nil, nonce, plaintext, aad)
		record := append(nonce, encrypted...)
		if err := writeFull(file, record); err != nil {
			clear(nonce)
			clear(encrypted)
			clear(record)

			return ErrStorage
		}
		clear(nonce)
		clear(encrypted)
		clear(record)
	}
	if err := file.Sync(); err != nil {
		return ErrStorage
	}
	if err := file.Close(); err != nil {
		return ErrStorage
	}
	complete = true
	store.chunks = append(store.chunks, path)
	store.buffer.clear()

	return nil
}

func (store *Store) openReaders() ([]*chunkReader, error) {
	readers := make([]*chunkReader, 0, len(store.chunks))
	for index, path := range store.chunks {
		file, err := store.openFile(filepath.Clean(path))
		if err != nil {
			_ = closeReaders(readers)

			return nil, ErrStorage
		}
		readers = append(readers, &chunkReader{
			file:            file,
			cipher:          store.cipher,
			recordBytes:     store.config.RecordBytes,
			chunkIndex:      uint64(index),
			expectedRecords: uint64(store.expectedChunkRecords(index)),
		})
	}

	return readers, nil
}

func (store *Store) expectedChunkRecords(index int) int {
	return min(
		store.config.ChunkRecords,
		store.total-index*store.config.ChunkRecords,
	)
}

func merge(
	ctx context.Context,
	readers []*chunkReader,
	yield func([]byte) error,
) error {
	queue := make(recordHeap, 0, len(readers))
	defer queue.clear()
	for index, reader := range readers {
		record, err := reader.next()
		if errors.Is(err, io.EOF) {
			continue
		}
		if err != nil {
			return err
		}
		heap.Push(&queue, heapRecord{record: record, reader: index})
	}

	for queue.Len() > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		current := heap.Pop(&queue).(heapRecord)
		yieldErr := func() error {
			defer clear(current.record)

			return yield(current.record)
		}()
		if yieldErr != nil {
			return yieldErr
		}
		next, err := readers[current.reader].next()
		switch {
		case errors.Is(err, io.EOF):
		case err != nil:
			return err
		default:
			heap.Push(
				&queue,
				heapRecord{record: next, reader: current.reader},
			)
		}
	}

	return nil
}

type chunkReader struct {
	file            chunkFile
	cipher          cipher.AEAD
	recordBytes     int
	chunkIndex      uint64
	recordIndex     uint64
	expectedRecords uint64
}

type chunkFile interface {
	io.Reader
	io.Writer
	Name() string
	Chmod(os.FileMode) error
	Sync() error
	Close() error
}

func createTemporaryChunk(directory string, pattern string) (chunkFile, error) {
	return os.CreateTemp(directory, pattern)
}

func openChunk(path string) (chunkFile, error) {
	return os.Open(path)
}

func (reader *chunkReader) next() ([]byte, error) {
	if reader.recordIndex == reader.expectedRecords {
		var trailing [1]byte
		count, err := reader.file.Read(trailing[:])
		if count == 0 && errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		if count == 0 && err != nil {
			return nil, ErrStorage
		}

		return nil, ErrCorrupt
	}
	encryptedRecordBytes := reader.cipher.NonceSize() +
		reader.recordBytes +
		reader.cipher.Overhead()
	record := make([]byte, encryptedRecordBytes)
	_, err := io.ReadFull(reader.file, record)
	if err != nil {
		clear(record)
		if !errors.Is(err, io.EOF) &&
			!errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, ErrStorage
		}

		return nil, ErrCorrupt
	}
	nonce := record[:reader.cipher.NonceSize()]
	aad := additionalData(
		reader.chunkIndex,
		reader.recordIndex,
		uint64(reader.recordBytes),
	)
	plaintext, err := reader.cipher.Open(
		nil,
		nonce,
		record[reader.cipher.NonceSize():],
		aad,
	)
	clear(record)
	if err != nil || len(plaintext) != reader.recordBytes {
		clear(plaintext)

		return nil, ErrCorrupt
	}
	reader.recordIndex++

	return plaintext, nil
}

func closeReaders(readers []*chunkReader) error {
	var closeFailed bool
	for _, reader := range readers {
		if err := reader.file.Close(); err != nil {
			closeFailed = true
		}
	}
	if closeFailed {
		return ErrStorage
	}

	return nil
}

func additionalData(
	chunkIndex uint64,
	recordIndex uint64,
	recordBytes uint64,
) []byte {
	result := make([]byte, len(aadVersion)+24)
	copy(result, aadVersion)
	offset := len(aadVersion)
	binary.BigEndian.PutUint64(result[offset:offset+8], chunkIndex)
	binary.BigEndian.PutUint64(result[offset+8:offset+16], recordIndex)
	binary.BigEndian.PutUint64(result[offset+16:], recordBytes)

	return result
}

func writeFull(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}

	return nil
}

type recordBuffer struct {
	data        []byte
	recordBytes int
}

func (buffer *recordBuffer) Len() int {
	return len(buffer.data) / buffer.recordBytes
}

func (buffer *recordBuffer) Less(left int, right int) bool {
	return bytes.Compare(buffer.record(left), buffer.record(right)) < 0
}

func (buffer *recordBuffer) Swap(left int, right int) {
	leftRecord := buffer.record(left)
	rightRecord := buffer.record(right)
	for index := range leftRecord {
		leftRecord[index], rightRecord[index] =
			rightRecord[index], leftRecord[index]
	}
}

func (buffer *recordBuffer) append(record []byte) {
	buffer.data = append(buffer.data, record...)
}

func (buffer *recordBuffer) removeOne(record []byte) {
	for index := 0; index < buffer.Len(); index++ {
		if !bytes.Equal(buffer.record(index), record) {
			continue
		}
		start := index * buffer.recordBytes
		next := start + buffer.recordBytes
		copy(buffer.data[start:], buffer.data[next:])
		newLength := len(buffer.data) - buffer.recordBytes
		clear(buffer.data[newLength:])
		buffer.data = buffer.data[:newLength]

		return
	}
}

func (buffer *recordBuffer) record(index int) []byte {
	start := index * buffer.recordBytes

	return buffer.data[start : start+buffer.recordBytes]
}

func (buffer *recordBuffer) clear() {
	clear(buffer.data)
	buffer.data = buffer.data[:0]
}

type heapRecord struct {
	record []byte
	reader int
}

type recordHeap []heapRecord

func (records recordHeap) Len() int {
	return len(records)
}

func (records recordHeap) Less(left int, right int) bool {
	comparison := bytes.Compare(
		records[left].record,
		records[right].record,
	)
	if comparison == 0 {
		return records[left].reader < records[right].reader
	}

	return comparison == -1
}

func (records recordHeap) Swap(left int, right int) {
	records[left], records[right] = records[right], records[left]
}

func (records *recordHeap) Push(value any) {
	*records = append(*records, value.(heapRecord))
}

func (records *recordHeap) Pop() any {
	old := *records
	last := len(old) - 1
	value := old[last]
	old[last] = heapRecord{}
	*records = old[:last]

	return value
}

func (records *recordHeap) clear() {
	for index := range *records {
		clear((*records)[index].record)
		(*records)[index] = heapRecord{}
	}
	*records = nil
}

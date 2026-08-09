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
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
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

	storeIdentityBytes = 32
	temporaryNameBytes = 16
	temporaryAttempts  = 128
	nonceCounterOffset = 8
	chunkPrefix        = "chunk-"
	storePrefix        = ".external-sort-"
	aadVersion         = "extsort1"
	redacted           = "[REDACTED]"
	redactedJSON       = `"` + redacted + `"`
)

var (
	ErrInvalidConfiguration = errors.New(
		"external sort configuration is invalid",
	)
	ErrUnsafeParent = errors.New(
		"external sort parent is not an owner-only directory",
	)
	ErrInvalidKey       = errors.New("external sort key is invalid")
	ErrInvalidRecord    = errors.New("external sort record is invalid")
	ErrRecordLimit      = errors.New("external sort record limit reached")
	ErrConcurrentUse    = errors.New("external sort store is already in use")
	ErrClosed           = errors.New("external sort store is closed")
	ErrFinalized        = errors.New("external sort store is finalized")
	ErrEntropy          = errors.New("external sort entropy failed")
	ErrStorage          = errors.New("external sort storage failed")
	ErrCorrupt          = errors.New("external sort chunk is corrupt")
	processNonceDomains = nonceDomainAllocator{entropy: rand.Reader}
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
// for concurrent use. Each Store permits one active lifecycle operation.
type Factory struct {
	config          Config
	parentInfo      os.FileInfo
	entropy         io.Reader
	nameEntropy     io.Reader
	openRoot        func(string) (rootDirectory, error)
	mkdir           func(rootDirectory, string, os.FileMode) error
	chmod           func(rootDirectory, string, os.FileMode) error
	removeAll       func(rootDirectory, string) error
	nextNonceDomain func() (uint64, bool)
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
	configuredParent := filepath.Clean(config.ParentDirectory)
	resolvedParent, err := filepath.EvalSymlinks(configuredParent)
	if err != nil {
		return nil, ErrUnsafeParent
	}
	parentInfo, err := validatedParent(configuredParent)
	if err != nil {
		return nil, err
	}
	config.ParentDirectory = resolvedParent

	return &Factory{
		config:      config,
		parentInfo:  parentInfo,
		entropy:     rand.Reader,
		nameEntropy: rand.Reader,
		openRoot:    openRootDirectory,
		mkdir: func(root rootDirectory, name string, mode os.FileMode) error {
			return root.Mkdir(name, mode)
		},
		chmod: func(root rootDirectory, name string, mode os.FileMode) error {
			return root.Chmod(name, mode)
		},
		removeAll: func(root rootDirectory, name string) error {
			return root.RemoveAll(name)
		},
		nextNonceDomain: allocateNonceDomain,
	}, nil
}

// Open creates one owner-only temporary work directory. The caller provides
// an AES-256 key for immediate cipher construction. The caller MUST call Close
// whenever Open returns a non-nil Store, including together with an error; an
// error result can carry ownership of construction residue whose first removal
// attempt failed.
func (factory *Factory) Open(
	ctx context.Context,
	key []byte,
) (*Store, error) {
	if factory == nil || !validConfig(factory.config) ||
		factory.parentInfo == nil || factory.entropy == nil ||
		factory.nameEntropy == nil ||
		factory.openRoot == nil || factory.mkdir == nil ||
		factory.chmod == nil || factory.removeAll == nil ||
		factory.nextNonceDomain == nil {
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
	block, _ := aes.NewCipher(key)
	authenticatedCipher, _ := cipher.NewGCM(block)
	identity := make([]byte, storeIdentityBytes)
	if _, err := io.ReadFull(factory.entropy, identity); err != nil {
		clear(identity)

		return nil, ErrEntropy
	}
	nonceDomain, ok := factory.nextNonceDomain()
	if !ok {
		clear(identity)

		return nil, ErrEntropy
	}
	root, err := factory.openRoot(factory.config.ParentDirectory)
	if err != nil {
		clear(identity)

		return nil, ErrUnsafeParent
	}
	parentInfo, err := root.Stat(".")
	if err != nil || !sameSafeDirectory(factory.parentInfo, parentInfo) {
		_ = root.Close()
		clear(identity)

		return nil, ErrUnsafeParent
	}
	directoryName, cleanupRequired, err := createStoreDirectory(
		ctx,
		root,
		factory.nameEntropy,
		factory.mkdir,
		factory.chmod,
		factory.removeAll,
	)
	if err != nil {
		if cleanupRequired {
			clear(identity)
			directory := filepath.Join(factory.config.ParentDirectory, directoryName)

			return &Store{storeState: &storeState{
				config: factory.config,
				removeAll: func(string) error {
					return factory.removeAll(root, directoryName)
				},
				root:          root,
				directory:     directory,
				directoryName: directoryName,
				closing:       true,
				cleanupOnly:   true,
			}}, err
		}
		_ = root.Close()
		clear(identity)

		return nil, err
	}
	directory := filepath.Join(factory.config.ParentDirectory, directoryName)
	state := &storeState{
		config:        factory.config,
		nameEntropy:   factory.nameEntropy,
		root:          root,
		directory:     directory,
		directoryName: directoryName,
		cipher:        authenticatedCipher,
		identity:      identity,
		nonceDomain:   nonceDomain,
		buffer: recordBuffer{
			data: make(
				[]byte,
				0,
				factory.config.RecordBytes*factory.config.ChunkRecords,
			),
			recordBytes: factory.config.RecordBytes,
		},
	}
	state.createTemp = func(string, string) (chunkFile, error) {
		return createRootTemporaryChunk(root, directoryName, state.nameEntropy)
	}
	state.openFile = func(path string) (chunkFile, error) {
		return root.Open(filepath.Join(directoryName, filepath.Base(path)))
	}
	state.remove = func(path string) error {
		return root.Remove(filepath.Join(directoryName, filepath.Base(path)))
	}
	state.removeAll = func(string) error {
		return root.RemoveAll(directoryName)
	}

	return &Store{storeState: state}, nil
}

// Store owns encrypted temporary chunks for one sort. Overlapping or reentrant
// lifecycle calls return ErrConcurrentUse. A record passed to the ForEachSorted
// callback is valid only until that callback returns.
type Store struct {
	*storeState
}

type storeState struct {
	mutex         sync.Mutex
	config        Config
	nameEntropy   io.Reader
	createTemp    func(string, string) (chunkFile, error)
	openFile      func(string) (chunkFile, error)
	remove        func(string) error
	removeAll     func(string) error
	root          rootDirectory
	directory     string
	directoryName string
	cipher        cipher.AEAD
	identity      []byte
	nonceDomain   uint64
	nonceCount    uint64
	buffer        recordBuffer
	chunks        []string
	total         int
	finalized     bool
	busy          bool
	closing       bool
	closed        bool
	cleanupOnly   bool
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
	if err := store.begin(true); err != nil {
		return err
	}
	defer store.finish()
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
	if err := store.begin(true); err != nil {
		return err
	}
	defer store.finish()
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
	if store == nil {
		return nil
	}
	if store.storeState == nil {
		return ErrInvalidConfiguration
	}
	store.mutex.Lock()
	if store.closed {
		store.mutex.Unlock()

		return nil
	}
	if store.busy {
		store.mutex.Unlock()

		return ErrConcurrentUse
	}
	if !store.validUnlocked() {
		store.mutex.Unlock()

		return ErrInvalidConfiguration
	}
	store.busy = true
	store.closing = true
	store.mutex.Unlock()
	store.buffer.clear()
	if err := store.removeAll(store.directory); err != nil {
		store.finish()

		return ErrStorage
	}
	rootCloseErr := store.root.Close()
	store.directory = ""
	store.directoryName = ""
	store.root = nil
	store.chunks = nil
	store.cipher = nil
	store.nameEntropy = nil
	clear(store.identity)
	store.identity = nil
	store.mutex.Lock()
	store.closing = false
	store.closed = true
	store.busy = false
	store.mutex.Unlock()
	if rootCloseErr != nil {
		return ErrStorage
	}

	return nil
}

func (store *Store) valid() bool {
	if store == nil || store.storeState == nil {
		return false
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()

	return store.validUnlocked()
}

func (store *Store) validUnlocked() bool {
	if store.cleanupOnly {
		return validConfig(store.config) && store.removeAll != nil &&
			store.root != nil && store.directory != "" && store.directoryName != ""
	}

	return validConfig(store.config) &&
		store.nameEntropy != nil &&
		store.createTemp != nil &&
		store.openFile != nil &&
		store.remove != nil &&
		store.removeAll != nil &&
		store.root != nil &&
		store.directory != "" &&
		store.directoryName != "" &&
		store.cipher != nil &&
		len(store.identity) == storeIdentityBytes &&
		store.buffer.recordBytes == store.config.RecordBytes
}

func (store *Store) begin(requireUnfinalized bool) error {
	if store.storeState == nil {
		return ErrInvalidConfiguration
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()

	if store.closed || store.closing {
		return ErrClosed
	}
	if store.busy {
		return ErrConcurrentUse
	}
	if requireUnfinalized && store.finalized {
		return ErrFinalized
	}
	if !store.validUnlocked() {
		return ErrInvalidConfiguration
	}
	store.busy = true

	return nil
}

func (store *Store) finish() {
	store.mutex.Lock()
	store.busy = false
	store.mutex.Unlock()
}

func (store *Store) seal() {
	store.mutex.Lock()
	store.closing = true
	store.mutex.Unlock()
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

func validatedParent(parent string) (os.FileInfo, error) {
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() ||
		info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm()&0o077 != 0 {
		return nil, ErrUnsafeParent
	}

	return info, nil
}

func sameSafeDirectory(expected os.FileInfo, actual os.FileInfo) bool {
	return expected != nil && actual != nil && actual.IsDir() &&
		actual.Mode().Perm()&0o077 == 0 && os.SameFile(expected, actual)
}

type nonceDomainAllocator struct {
	mutex       sync.Mutex
	entropy     io.Reader
	initialized bool
	seed        uint64
	next        uint64
}

func allocateNonceDomain() (uint64, bool) {
	return processNonceDomains.allocate()
}

func (allocator *nonceDomainAllocator) allocate() (uint64, bool) {
	allocator.mutex.Lock()
	defer allocator.mutex.Unlock()
	if !allocator.initialized {
		var seed [8]byte
		if _, err := io.ReadFull(allocator.entropy, seed[:]); err != nil {
			clear(seed[:])

			return 0, false
		}
		allocator.seed = binary.BigEndian.Uint64(seed[:])
		allocator.initialized = true
		clear(seed[:])
	}
	if allocator.next == ^uint64(0) {
		return 0, false
	}
	allocator.next++

	return allocator.seed ^ allocator.next, true
}

func (store *Store) spill(ctx context.Context) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	sort.Sort(&store.buffer)
	file, err := store.createTemp(store.directory, chunkPrefix)
	if err != nil {
		if errors.Is(err, ErrEntropy) {
			return ErrEntropy
		}

		return ErrStorage
	}
	path := file.Name()
	complete := false
	defer func() {
		if !complete {
			closeErr := file.Close()
			removeErr := store.remove(path)
			if closeErr != nil || removeErr != nil {
				store.seal()
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
		if store.nonceCount > uint64(^uint32(0)) {
			clear(nonce)

			return ErrEntropy
		}
		binary.BigEndian.PutUint64(nonce[:nonceCounterOffset], store.nonceDomain)
		binary.BigEndian.PutUint32(
			nonce[nonceCounterOffset:],
			uint32(store.nonceCount),
		)
		store.nonceCount++
		plaintext := store.buffer.record(recordIndex)
		aad := additionalData(
			store.identity,
			store.nonceDomain,
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
			identity:        store.identity,
			nonceDomain:     store.nonceDomain,
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
	identity        []byte
	nonceDomain     uint64
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

type rootDirectory interface {
	Stat(string) (os.FileInfo, error)
	Mkdir(string, os.FileMode) error
	Chmod(string, os.FileMode) error
	Open(string) (*os.File, error)
	OpenFile(string, int, os.FileMode) (*os.File, error)
	Remove(string) error
	RemoveAll(string) error
	Close() error
}

func createStoreDirectory(
	ctx context.Context,
	root rootDirectory,
	entropy io.Reader,
	mkdir func(rootDirectory, string, os.FileMode) error,
	chmod func(rootDirectory, string, os.FileMode) error,
	removeAll func(rootDirectory, string) error,
) (string, bool, error) {
	for range temporaryAttempts {
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		name, err := temporaryName(entropy, storePrefix)
		if err != nil {
			return "", false, err
		}
		if err := mkdir(root, name, 0o700); err == nil {
			if err := chmod(root, name, 0o700); err != nil {
				if removeErr := removeAll(root, name); removeErr != nil {
					return name, true, ErrStorage
				}

				return "", false, ErrStorage
			}
			info, err := root.Stat(name)
			if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
				if removeErr := removeAll(root, name); removeErr != nil {
					return name, true, ErrStorage
				}

				return "", false, ErrStorage
			}

			return name, false, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", false, ErrStorage
		}
	}

	return "", false, ErrStorage
}

func createRootTemporaryChunk(
	root rootDirectory,
	directory string,
	entropy io.Reader,
) (chunkFile, error) {
	for range temporaryAttempts {
		name, err := temporaryName(entropy, chunkPrefix)
		if err != nil {
			return nil, err
		}
		file, err := root.OpenFile(
			filepath.Join(directory, name),
			os.O_RDWR|os.O_CREATE|os.O_EXCL,
			0o600,
		)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, ErrStorage
		}
	}

	return nil, ErrStorage
}

func temporaryName(entropy io.Reader, prefix string) (string, error) {
	random := make([]byte, temporaryNameBytes)
	if _, err := io.ReadFull(entropy, random); err != nil {
		clear(random)

		return "", ErrEntropy
	}
	name := prefix + hex.EncodeToString(random)
	clear(random)

	return name, nil
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
		reader.identity,
		reader.nonceDomain,
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
	identity []byte,
	nonceDomain uint64,
	chunkIndex uint64,
	recordIndex uint64,
	recordBytes uint64,
) []byte {
	result := make([]byte, len(aadVersion)+storeIdentityBytes+32)
	copy(result, aadVersion)
	copy(result[len(aadVersion):], identity)
	offset := len(aadVersion) + storeIdentityBytes
	binary.BigEndian.PutUint64(result[offset:offset+8], nonceDomain)
	binary.BigEndian.PutUint64(result[offset+8:offset+16], chunkIndex)
	binary.BigEndian.PutUint64(result[offset+16:offset+24], recordIndex)
	binary.BigEndian.PutUint64(result[offset+24:], recordBytes)

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

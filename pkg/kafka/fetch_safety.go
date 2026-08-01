package kafka

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/klauspost/compress/s2"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"github.com/twmb/franz-go/pkg/kgo"
)

// ErrFetchBatchTooLarge identifies a Kafka record batch whose decoded bytes
// exceed the configured consumer-side safety limit.
var ErrFetchBatchTooLarge = errors.New(
	"kafka: fetched record batch exceeds configured decoded byte limit",
)

// ErrFetchBatchMalformed identifies compressed broker data that cannot be
// decoded as the Kafka compression codec declared by the record batch.
var ErrFetchBatchMalformed = errors.New(
	"kafka: fetched record batch compression is malformed",
)

// ErrFetchDecompressedBufferFull identifies a fetch whose decoded bytes would
// exceed the configured client-wide active decompression budget.
var ErrFetchDecompressedBufferFull = errors.New(
	"kafka: fetched record batches exceed configured decoded buffer limit",
)

var xerialSnappyPrefix = []byte{130, 83, 78, 65, 80, 80, 89, 0}

const (
	defaultBrokerMaxReadBytes        = 64 << 20
	maximumBrokerMaxReadBytes        = 512 << 20
	defaultDecompressedBatchMaxBytes = 8 << 20
	maximumDecompressedBatchMaxBytes = 512 << 20
	defaultBufferedDecompressedBytes = 64 << 20
	maximumBufferedDecompressedBytes = 1 << 30
)

func normalizeFetchSafety(
	fetchMaximumBytes int32,
	limits MessageLimits,
	brokerMaximumBytes int32,
	decompressedMaximumBytes int64,
	bufferedDecompressedMaximumBytes int64,
) (int32, int64, int64, bool) {
	if brokerMaximumBytes == 0 {
		brokerMaximumBytes = max(defaultBrokerMaxReadBytes, fetchMaximumBytes)
	}
	minimumDecompressedBytes := maximumRecordPolicyBytes(limits)
	if decompressedMaximumBytes == 0 {
		decompressedMaximumBytes = max(
			defaultDecompressedBatchMaxBytes,
			minimumDecompressedBytes,
		)
	}
	if bufferedDecompressedMaximumBytes == 0 {
		bufferedDecompressedMaximumBytes = max(
			defaultBufferedDecompressedBytes,
			decompressedMaximumBytes,
		)
	}
	if brokerMaximumBytes < fetchMaximumBytes ||
		brokerMaximumBytes > maximumBrokerMaxReadBytes ||
		decompressedMaximumBytes < minimumDecompressedBytes ||
		decompressedMaximumBytes > maximumDecompressedBatchMaxBytes ||
		bufferedDecompressedMaximumBytes < decompressedMaximumBytes ||
		bufferedDecompressedMaximumBytes > maximumBufferedDecompressedBytes {
		return 0, 0, 0, false
	}

	return brokerMaximumBytes, decompressedMaximumBytes,
		bufferedDecompressedMaximumBytes, true
}

type boundedDecompressor struct {
	maximumBytes int
	budget       *fetchDecompressionBudget
	gzipReaders  sync.Pool
	lz4Readers   sync.Pool
	zstdReaders  sync.Pool
}

func newBoundedDecompressor(maximumBytes int) kgo.Decompressor {
	decompressor := &boundedDecompressor{maximumBytes: maximumBytes}
	decompressor.initializePools()

	return decompressor
}

func newFetchDecompressionPolicy(
	maximumBatchBytes int64,
	maximumBufferedBytes int64,
) (*boundedDecompressor, *fetchDecompressionBudget) {
	budget := &fetchDecompressionBudget{maximumBytes: maximumBufferedBytes}
	decompressor := &boundedDecompressor{
		maximumBytes: int(maximumBatchBytes),
		budget:       budget,
	}
	decompressor.initializePools()

	return decompressor, budget
}

func (decompressor *boundedDecompressor) initializePools() {
	decompressor.gzipReaders.New = func() any { return new(gzip.Reader) }
	decompressor.lz4Readers.New = func() any { return lz4.NewReader(nil) }
	decompressor.zstdReaders.New = func() any {
		decoder, err := zstd.NewReader(
			nil,
			zstd.WithDecoderLowmem(true),
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderMaxMemory(uint64(decompressor.maximumBytes)),
		)

		return &zstdReader{decoder: decoder, err: err}
	}

}

func (decompressor *boundedDecompressor) Decompress(
	source []byte,
	codec kgo.CompressionCodecType,
) ([]byte, error) {
	switch codec {
	case kgo.CodecNone:
		if len(source) > decompressor.maximumBytes {
			return nil, ErrFetchBatchTooLarge
		}

		return source, nil
	case kgo.CodecGzip:
		reader := decompressor.gzipReaders.Get().(*gzip.Reader)
		defer decompressor.gzipReaders.Put(reader)
		if err := reader.Reset(bytes.NewReader(source)); err != nil {
			return nil, newFetchBatchMalformedError(err)
		}

		decoded, err := readBoundedFetchBatch(reader, decompressor.maximumBytes)

		return decompressor.reserveDecoded(decoded, err)
	case kgo.CodecSnappy:
		decoded, err := decompressor.decompressSnappy(source)

		return decompressor.reserveDecoded(decoded, err)
	case kgo.CodecLz4:
		reader := decompressor.lz4Readers.Get().(*lz4.Reader)
		defer decompressor.lz4Readers.Put(reader)
		reader.Reset(bytes.NewReader(source))

		decoded, err := readBoundedFetchBatch(reader, decompressor.maximumBytes)

		return decompressor.reserveDecoded(decoded, err)
	case kgo.CodecZstd:
		pooled := decompressor.zstdReaders.Get().(*zstdReader)
		if pooled.err != nil {
			return nil, newFetchBatchMalformedError(pooled.err)
		}
		defer decompressor.zstdReaders.Put(pooled)
		if err := pooled.decoder.Reset(bytes.NewReader(source)); err != nil {
			return nil, classifyZstdFetchError(err)
		}
		decoded, err := readBoundedFetchBatch(
			pooled.decoder,
			decompressor.maximumBytes,
		)
		if err != nil {
			return nil, classifyZstdFetchError(err)
		}

		return decompressor.reserveDecoded(decoded, nil)
	default:
		return nil, ErrFetchBatchMalformed
	}
}

func (decompressor *boundedDecompressor) reserveDecoded(
	decoded []byte,
	err error,
) ([]byte, error) {
	if err != nil || decompressor.budget == nil {
		return decoded, err
	}
	if !decompressor.budget.reserve(cap(decoded)) {
		return nil, ErrFetchDecompressedBufferFull
	}

	return decoded, nil
}

type fetchDecompressionBudget struct {
	maximumBytes int64
	activeBytes  atomic.Int64
}

func (budget *fetchDecompressionBudget) GetDecompressBytes(
	[]byte,
	kgo.CompressionCodecType,
) []byte {
	return nil
}

func (budget *fetchDecompressionBudget) PutDecompressBytes(decoded []byte) {
	released := int64(cap(decoded))
	for {
		active := budget.activeBytes.Load()
		remaining := active - min(active, released)
		if budget.activeBytes.CompareAndSwap(active, remaining) {
			return
		}
	}
}

func (budget *fetchDecompressionBudget) reserve(bytes int) bool {
	requested := int64(bytes)
	for {
		active := budget.activeBytes.Load()
		if requested > budget.maximumBytes-active {
			return false
		}
		if budget.activeBytes.CompareAndSwap(active, active+requested) {
			return true
		}
	}
}

func recycleFetchedRecords(records []*kgo.Record) {
	for _, record := range records {
		record.Recycle()
	}
}

type zstdReader struct {
	decoder *zstd.Decoder
	err     error
}

func readBoundedFetchBatch(reader io.Reader, maximumBytes int) ([]byte, error) {
	var destination bytes.Buffer
	written, err := io.Copy(
		&destination,
		io.LimitReader(reader, int64(maximumBytes)+1),
	)
	if err != nil {
		return nil, newFetchBatchMalformedError(err)
	}
	if written > int64(maximumBytes) {
		return nil, ErrFetchBatchTooLarge
	}

	return destination.Bytes(), nil
}

func (decompressor *boundedDecompressor) decompressSnappy(
	source []byte,
) ([]byte, error) {
	if len(source) > 16 && bytes.HasPrefix(source, xerialSnappyPrefix) {
		return decompressor.decompressXerialSnappy(source[16:])
	}
	decodedBytes, err := s2.DecodedLen(source)
	if err != nil {
		return nil, newFetchBatchMalformedError(err)
	}
	if decodedBytes > decompressor.maximumBytes {
		return nil, ErrFetchBatchTooLarge
	}
	decoded, err := s2.Decode(nil, source)
	if err != nil {
		return nil, newFetchBatchMalformedError(err)
	}

	return decoded, nil
}

func (decompressor *boundedDecompressor) decompressXerialSnappy(
	source []byte,
) ([]byte, error) {
	destination := make([]byte, 0, min(len(source), decompressor.maximumBytes))
	for len(source) > 0 {
		if len(source) < 4 {
			return nil, ErrFetchBatchMalformed
		}
		encodedBytes := binary.BigEndian.Uint32(source)
		source = source[4:]
		if uint64(encodedBytes) > uint64(len(source)) {
			return nil, ErrFetchBatchMalformed
		}
		chunk := source[:int(encodedBytes)]
		decodedBytes, err := s2.DecodedLen(chunk)
		if err != nil {
			return nil, newFetchBatchMalformedError(err)
		}
		if decodedBytes > decompressor.maximumBytes-len(destination) {
			return nil, ErrFetchBatchTooLarge
		}
		decoded, err := s2.Decode(nil, chunk)
		if err != nil {
			return nil, newFetchBatchMalformedError(err)
		}
		destination = append(destination, decoded...)
		source = source[encodedBytes:]
	}

	return destination, nil
}

type fetchBatchMalformedError struct {
	cause error
}

func newFetchBatchMalformedError(cause error) error {
	if errors.Is(cause, ErrFetchBatchTooLarge) {
		return ErrFetchBatchTooLarge
	}

	return &fetchBatchMalformedError{cause: cause}
}

func (err *fetchBatchMalformedError) Error() string {
	return ErrFetchBatchMalformed.Error()
}

func (err *fetchBatchMalformedError) Unwrap() []error {
	return []error{ErrFetchBatchMalformed, err.cause}
}

func classifyZstdFetchError(err error) error {
	if errors.Is(err, zstd.ErrDecoderSizeExceeded) ||
		errors.Is(err, zstd.ErrWindowSizeExceeded) {
		return ErrFetchBatchTooLarge
	}

	return newFetchBatchMalformedError(err)
}

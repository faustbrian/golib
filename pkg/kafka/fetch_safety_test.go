package kafka

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func TestBoundedDecompressorRejectsAnOversizedUncompressedBatch(t *testing.T) {
	decompressor := newBoundedDecompressor(4)

	decoded, err := decompressor.Decompress([]byte("12345"), kgo.CodecNone)

	if decoded != nil {
		t.Fatalf("Decompress() decoded = %q, want nil", decoded)
	}
	if !errors.Is(err, ErrFetchBatchTooLarge) {
		t.Fatalf("Decompress() error = %v, want %v", err, ErrFetchBatchTooLarge)
	}
}

func TestBoundedDecompressorReturnsAnInLimitUncompressedBatch(t *testing.T) {
	source := []byte("1234")

	decoded, err := newBoundedDecompressor(len(source)).Decompress(
		source,
		kgo.CodecNone,
	)

	if err != nil || !bytes.Equal(decoded, source) {
		t.Fatalf("Decompress() = (%q, %v)", decoded, err)
	}
	if &decoded[0] != &source[0] {
		t.Fatal("Decompress() copied an uncompressed franz-go-owned batch")
	}
}

func TestBoundedDecompressorEnforcesEveryKafkaCompressionCodec(t *testing.T) {
	const maximumBytes = 1 << 20
	codecs := []struct {
		name  string
		codec kgo.CompressionCodec
	}{
		{name: "gzip", codec: kgo.GzipCompression()},
		{name: "snappy", codec: kgo.SnappyCompression()},
		{name: "lz4", codec: kgo.Lz4Compression()},
		{name: "zstd", codec: kgo.ZstdCompression()},
	}
	for _, testCase := range codecs {
		t.Run(testCase.name, func(t *testing.T) {
			compressor, err := kgo.DefaultCompressor(testCase.codec)
			if err != nil {
				t.Fatalf("DefaultCompressor() error = %v", err)
			}
			decompressor := newBoundedDecompressor(maximumBytes)
			for _, size := range []int{maximumBytes, maximumBytes + 1} {
				source := bytes.Repeat([]byte("a"), size)
				var destination bytes.Buffer
				compressed, codec := compressor.Compress(&destination, source)
				if codec == kgo.CodecError {
					t.Fatal("Compress() failed")
				}

				decoded, decodeErr := decompressor.Decompress(compressed, codec)
				if size == maximumBytes {
					if decodeErr != nil || !bytes.Equal(decoded, source) {
						t.Fatalf(
							"Decompress(%d) = (%d bytes, %v)",
							size,
							len(decoded),
							decodeErr,
						)
					}

					continue
				}
				if decoded != nil || !errors.Is(decodeErr, ErrFetchBatchTooLarge) {
					t.Fatalf(
						"Decompress(%d) = (%d bytes, %v), want oversized",
						size,
						len(decoded),
						decodeErr,
					)
				}
			}
		})
	}
}

func TestBoundedDecompressorBoundsCumulativeXerialSnappyChunks(t *testing.T) {
	const maximumBytes = 1 << 10
	compressor, err := kgo.DefaultCompressor(kgo.SnappyCompression())
	if err != nil {
		t.Fatalf("DefaultCompressor() error = %v", err)
	}
	chunk := bytes.Repeat([]byte("a"), maximumBytes/2+1)
	var destination bytes.Buffer
	compressed, codec := compressor.Compress(&destination, chunk)
	if codec != kgo.CodecSnappy {
		t.Fatalf("Compress() codec = %v", codec)
	}
	framed := append([]byte(nil), []byte{
		130, 83, 78, 65, 80, 80, 89, 0,
		0, 0, 0, 1, 0, 0, 0, 1,
	}...)
	for range 2 {
		framed = binary.BigEndian.AppendUint32(framed, uint32(len(compressed)))
		framed = append(framed, compressed...)
	}

	decoded, decodeErr := newBoundedDecompressor(maximumBytes).Decompress(
		framed,
		kgo.CodecSnappy,
	)

	if decoded != nil || !errors.Is(decodeErr, ErrFetchBatchTooLarge) {
		t.Fatalf("Decompress() = (%d bytes, %v), want oversized", len(decoded), decodeErr)
	}
}

func TestBoundedDecompressorDecodesXerialSnappyChunks(t *testing.T) {
	const maximumBytes = len("firstsecond")
	compressor, err := kgo.DefaultCompressor(kgo.SnappyCompression())
	if err != nil {
		t.Fatalf("DefaultCompressor() error = %v", err)
	}
	chunks := [][]byte{[]byte("first"), []byte("second")}
	framed := append([]byte(nil), []byte{
		130, 83, 78, 65, 80, 80, 89, 0,
		0, 0, 0, 1, 0, 0, 0, 1,
	}...)
	for _, chunk := range chunks {
		var destination bytes.Buffer
		compressed, codec := compressor.Compress(&destination, chunk)
		if codec != kgo.CodecSnappy {
			t.Fatalf("Compress() codec = %v", codec)
		}
		framed = binary.BigEndian.AppendUint32(framed, uint32(len(compressed)))
		framed = append(framed, compressed...)
	}

	decoded, decodeErr := newBoundedDecompressor(maximumBytes).Decompress(
		framed,
		kgo.CodecSnappy,
	)

	if decodeErr != nil || !bytes.Equal(decoded, []byte("firstsecond")) {
		t.Fatalf("Decompress() = (%q, %v)", decoded, decodeErr)
	}
}

func TestBoundedDecompressorRedactsMalformedCompressionErrors(t *testing.T) {
	tests := []struct {
		name   string
		codec  kgo.CompressionCodecType
		source []byte
		exact  bool
	}{
		{name: "gzip", codec: kgo.CodecGzip, source: []byte("secret-gzip")},
		{name: "snappy", codec: kgo.CodecSnappy, source: []byte("secret-snappy")},
		{name: "snappy length", codec: kgo.CodecSnappy, source: []byte{0x80}},
		{name: "lz4", codec: kgo.CodecLz4, source: []byte("secret-lz4")},
		{name: "zstd", codec: kgo.CodecZstd, source: []byte("secret-zstd")},
		{name: "unknown", codec: kgo.CompressionCodecType(127), source: []byte("secret-unknown")},
		{name: "header-only xerial", codec: kgo.CodecSnappy, source: []byte{
			130, 83, 78, 65, 80, 80, 89, 0,
			0, 0, 0, 1, 0, 0, 0, 1,
		}},
		{name: "empty xerial chunk", codec: kgo.CodecSnappy, exact: true, source: []byte{
			130, 83, 78, 65, 80, 80, 89, 0,
			0, 0, 0, 1, 0, 0, 0, 1,
			0, 0, 0, 0,
		}},
		{name: "short xerial chunk", codec: kgo.CodecSnappy, source: append(
			[]byte{130, 83, 78, 65, 80, 80, 89, 0, 0, 0, 0, 1, 0, 0, 0, 1},
			1, 2, 3,
		)},
		{name: "oversized xerial chunk", codec: kgo.CodecSnappy, source: append(
			[]byte{130, 83, 78, 65, 80, 80, 89, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 5},
			1,
		)},
		{name: "malformed xerial chunk", codec: kgo.CodecSnappy, source: append(
			[]byte{130, 83, 78, 65, 80, 80, 89, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1},
			1,
		)},
		{name: "malformed xerial length", codec: kgo.CodecSnappy, source: append(
			[]byte{130, 83, 78, 65, 80, 80, 89, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1},
			0x80,
		)},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			decoded, err := newBoundedDecompressor(1<<20).Decompress(
				testCase.source,
				testCase.codec,
			)
			if decoded != nil || !errors.Is(err, ErrFetchBatchMalformed) {
				t.Fatalf("Decompress() = (%q, %v), want malformed", decoded, err)
			}
			if testCase.exact && err != ErrFetchBatchMalformed {
				t.Fatalf("Decompress() error = %#v, want stable sentinel", err)
			}
			if err.Error() != ErrFetchBatchMalformed.Error() {
				t.Fatalf("Decompress() diagnostic = %q", err)
			}
			if bytes.Contains([]byte(err.Error()), testCase.source) {
				t.Fatal("Decompress() diagnostic disclosed compressed bytes")
			}
		})
	}
}

func TestBoundedDecompressorContainsZstdInitializationFailures(t *testing.T) {
	constructorFailure := errors.New("zstd constructor secret")
	decompressor := newBoundedDecompressor(1 << 20).(*boundedDecompressor)
	decompressor.zstdReaders = sync.Pool{New: func() any {
		return &zstdReader{err: constructorFailure}
	}}
	decoded, err := decompressor.Decompress(nil, kgo.CodecZstd)
	if decoded != nil || !errors.Is(err, constructorFailure) ||
		err.Error() != ErrFetchBatchMalformed.Error() {
		t.Fatalf("constructor failure = (%q, %v)", decoded, err)
	}

	closed, closeErr := zstd.NewReader(nil)
	if closeErr != nil {
		t.Fatalf("zstd.NewReader() error = %v", closeErr)
	}
	closed.Close()
	decompressor.zstdReaders = sync.Pool{New: func() any {
		return &zstdReader{decoder: closed}
	}}
	decoded, err = decompressor.Decompress(nil, kgo.CodecZstd)
	if decoded != nil || !errors.Is(err, zstd.ErrDecoderClosed) ||
		err.Error() != ErrFetchBatchMalformed.Error() {
		t.Fatalf("closed decoder failure = (%q, %v)", decoded, err)
	}
}

func TestFetchDecompressionBudgetPoolAndZstdWindowClassification(t *testing.T) {
	budget := &fetchDecompressionBudget{maximumBytes: 1}
	if decoded := budget.GetDecompressBytes(nil, kgo.CodecGzip); decoded != nil {
		t.Fatalf("GetDecompressBytes() = %q", decoded)
	}
	if err := classifyZstdFetchError(zstd.ErrWindowSizeExceeded); !errors.Is(
		err,
		ErrFetchBatchTooLarge,
	) {
		t.Fatalf("classifyZstdFetchError() = %v", err)
	}
}

func TestBoundedDecompressorIsConcurrent(t *testing.T) {
	const maximumBytes = 1 << 20
	compressor, err := kgo.DefaultCompressor(kgo.GzipCompression())
	if err != nil {
		t.Fatalf("DefaultCompressor() error = %v", err)
	}
	source := bytes.Repeat([]byte("a"), maximumBytes)
	var destination bytes.Buffer
	compressed, codec := compressor.Compress(&destination, source)
	decompressor := newBoundedDecompressor(maximumBytes)
	var waitGroup sync.WaitGroup
	errorsFound := make(chan error, 16)
	for range 16 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			decoded, decodeErr := decompressor.Decompress(compressed, codec)
			if decodeErr != nil {
				errorsFound <- decodeErr
			} else if !bytes.Equal(decoded, source) {
				errorsFound <- errors.New("decoded batch mismatch")
			}
		}()
	}
	waitGroup.Wait()
	close(errorsFound)
	for decodeErr := range errorsFound {
		t.Fatalf("concurrent Decompress() error = %v", decodeErr)
	}
}

func TestNormalizeFetchSafetyDerivesCompatibleDefaults(t *testing.T) {
	limits := DefaultMessageLimits()
	limits.MaxValueBytes = 10 << 20

	brokerBytes, decompressedBytes, bufferedBytes, ok := normalizeFetchSafety(
		100<<20,
		limits,
		0,
		0,
		0,
	)

	if !ok || brokerBytes != 100<<20 ||
		decompressedBytes != maximumRecordPolicyBytes(limits) ||
		bufferedBytes != 64<<20 {
		t.Fatalf(
			"normalizeFetchSafety() = (%d, %d, %d, %t)",
			brokerBytes,
			decompressedBytes,
			bufferedBytes,
			ok,
		)
	}
}

func TestFetchDecompressionPolicyBoundsAndReleasesBufferedBytes(t *testing.T) {
	compressor, err := kgo.DefaultCompressor(kgo.SnappyCompression())
	if err != nil {
		t.Fatalf("DefaultCompressor() error = %v", err)
	}
	source := []byte("1234")
	var destination bytes.Buffer
	compressed, codec := compressor.Compress(&destination, source)
	decompressor, pool := newFetchDecompressionPolicy(4, 6)

	first, firstErr := decompressor.Decompress(compressed, codec)
	second, secondErr := decompressor.Decompress(compressed, codec)
	if firstErr != nil || !bytes.Equal(first, source) {
		t.Fatalf("first Decompress() = (%q, %v)", first, firstErr)
	}
	if second != nil || !errors.Is(secondErr, ErrFetchDecompressedBufferFull) {
		t.Fatalf("second Decompress() = (%q, %v), want full buffer", second, secondErr)
	}

	pool.PutDecompressBytes(first)
	third, thirdErr := decompressor.Decompress(compressed, codec)
	if thirdErr != nil || !bytes.Equal(third, source) {
		t.Fatalf("third Decompress() = (%q, %v)", third, thirdErr)
	}
	pool.PutDecompressBytes(third)
}

func TestFetchDecompressionBudgetAdmitsItsExactLimit(t *testing.T) {
	budget := &fetchDecompressionBudget{maximumBytes: 4}
	if !budget.reserve(4) || budget.activeBytes.Load() != 4 {
		t.Fatalf("reserve(exact limit) active = %d", budget.activeBytes.Load())
	}
	if budget.reserve(1) {
		t.Fatal("reserve() exceeded the active decoded-byte limit")
	}
	budget.PutDecompressBytes(make([]byte, 0, 4))
	if budget.activeBytes.Load() != 0 {
		t.Fatalf("released active bytes = %d", budget.activeBytes.Load())
	}
}

func TestRecycleFetchedRecordsReleasesFranzGoDecompressionMemory(t *testing.T) {
	record, budget := pooledCompressedFetchRecord(t, "events", 1, 1)
	if budget.activeBytes.Load() == 0 {
		t.Fatal("franz-go fetch did not retain decoded batch memory")
	}

	recycleFetchedRecords([]*kgo.Record{record})

	if active := budget.activeBytes.Load(); active != 0 {
		t.Fatalf("active decoded bytes = %d, want 0", active)
	}
	if !reflect.DeepEqual(*record, kgo.Record{}) {
		t.Fatalf("recycled record = %#v, want zero value", record)
	}
}

func TestTrailingOversizedBatchPreservesEarlierContiguousRecords(t *testing.T) {
	const maximumBytes = 1 << 20
	first := compressedRecordBatch(t, 0, []byte("accepted"))
	oversized := compressedRecordBatch(
		t,
		1,
		bytes.Repeat([]byte("a"), maximumBytes),
	)
	decompressor, budget := newFetchDecompressionPolicy(
		maximumBytes,
		2<<20,
	)
	partition, nextOffset := kgo.ProcessFetchPartition(
		kgo.ProcessFetchPartitionOpts{
			Offset: 0, Topic: "events", Partition: 0,
			Pools: []kgo.Pool{budget},
		},
		&kmsg.FetchResponseTopicPartition{
			Partition:     0,
			RecordBatches: append(first, oversized...),
		},
		decompressor,
		nil,
	)
	if partition.Err != nil || len(partition.Records) != 1 || nextOffset != 1 {
		t.Fatalf(
			"ProcessFetchPartition() = (%#v, %d), want one complete prefix",
			partition,
			nextOffset,
		)
	}
	if string(partition.Records[0].Value) != "accepted" {
		t.Fatalf("record value = %q", partition.Records[0].Value)
	}

	consumerBackend := &recordingConsumerBackend{
		fetches: fetchesForRecord(partition.Records[0]),
	}
	consumer := consumerWithBackend(
		consumerBackend,
		1,
		time.Second,
		time.Second,
	)
	result, err := consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error { return nil }),
	)
	if err != nil || result.Committed != 1 ||
		len(consumerBackend.committed) != 1 ||
		consumerBackend.committed[0].Offset != 0 ||
		budget.activeBytes.Load() != 0 {
		t.Fatalf(
			"RunOnce() = (%#v, %v), backend = %#v, active = %d",
			result,
			err,
			consumerBackend,
			budget.activeBytes.Load(),
		)
	}
}

func pooledCompressedFetchRecord(
	t *testing.T,
	topic string,
	partitionID int32,
	offset int64,
) (*kgo.Record, *fetchDecompressionBudget) {
	t.Helper()
	rawBatch := compressedRecordBatch(t, offset, []byte("value"))
	decompressor, budget := newFetchDecompressionPolicy(1<<20, 1<<20)
	partition, _ := kgo.ProcessFetchPartition(
		kgo.ProcessFetchPartitionOpts{
			Offset:    offset,
			Topic:     topic,
			Partition: partitionID,
			Pools:     []kgo.Pool{budget},
		},
		&kmsg.FetchResponseTopicPartition{
			Partition:     partitionID,
			RecordBatches: rawBatch,
		},
		decompressor,
		nil,
	)
	if partition.Err != nil || len(partition.Records) != 1 {
		t.Fatalf("ProcessFetchPartition() = %#v", partition)
	}

	return partition.Records[0], budget
}

func compressedRecordBatch(t *testing.T, offset int64, value []byte) []byte {
	t.Helper()
	kafkaRecord := &kmsg.Record{Key: []byte("key"), Value: value}
	recordBytes := kafkaRecord.AppendTo(nil)
	kafkaRecord.Length = int32(len(recordBytes) - 1)
	recordBytes = kafkaRecord.AppendTo(nil)
	compressor, err := kgo.DefaultCompressor(kgo.GzipCompression())
	if err != nil {
		t.Fatalf("DefaultCompressor() error = %v", err)
	}
	var compressedBuffer bytes.Buffer
	compressed, codec := compressor.Compress(&compressedBuffer, recordBytes)
	batch := kmsg.RecordBatch{
		FirstOffset:          offset,
		PartitionLeaderEpoch: 1,
		Magic:                2,
		Attributes:           int16(codec),
		LastOffsetDelta:      0,
		ProducerID:           -1,
		ProducerEpoch:        -1,
		FirstSequence:        -1,
		NumRecords:           1,
		Records:              compressed,
	}
	rawBatch := batch.AppendTo(nil)
	batch.Length = int32(len(rawBatch[12:]))
	rawBatch = batch.AppendTo(nil)
	batch.CRC = int32(crc32.Checksum(rawBatch[21:], crc32.MakeTable(crc32.Castagnoli)))

	return batch.AppendTo(nil)
}

func TestReadSurfacesReleaseFranzGoDecompressionMemory(t *testing.T) {
	t.Run("consumer record", func(t *testing.T) {
		record, budget := pooledCompressedFetchRecord(t, "events", 1, 1)
		backend := &recordingConsumerBackend{fetches: fetchesForRecord(record)}
		consumer := consumerWithBackend(backend, 1, time.Second, time.Second)
		result, err := consumer.RunOnce(
			context.Background(),
			HandlerFunc(func(context.Context, ConsumedMessage) error { return nil }),
		)
		if err != nil || result.Committed != 1 || budget.activeBytes.Load() != 0 {
			t.Fatalf("RunOnce() = (%#v, %v), active = %d", result, err, budget.activeBytes.Load())
		}
	})

	t.Run("consumer batch", func(t *testing.T) {
		record, budget := pooledCompressedFetchRecord(t, "events", 1, 1)
		backend := &recordingConsumerBackend{fetches: fetchesForRecord(record)}
		consumer := consumerWithBackend(backend, 1, time.Second, time.Second)
		result, err := consumer.RunBatchOnce(
			context.Background(),
			BatchHandlerFunc(func(context.Context, ConsumedBatch) error { return nil }),
		)
		if err != nil || result.Committed != 1 || budget.activeBytes.Load() != 0 {
			t.Fatalf("RunBatchOnce() = (%#v, %v), active = %d", result, err, budget.activeBytes.Load())
		}
	})

	t.Run("transaction processor", func(t *testing.T) {
		record, budget := pooledCompressedFetchRecord(t, "source-events", 0, 0)
		backend := &recordingTransactionProcessorBackend{
			fetches:    []kgo.Fetches{fetchesForRecord(record)},
			endResults: []transactionEndResult{{committed: true}},
		}
		processor := transactionProcessorForTest(t, backend)
		result, err := processor.RunOnce(
			context.Background(),
			TransactionHandlerFunc(func(context.Context, ConsumedRecord, Transaction) error {
				return nil
			}),
		)
		if err != nil || !result.Committed || budget.activeBytes.Load() != 0 {
			t.Fatalf("RunOnce() = (%#v, %v), active = %d", result, err, budget.activeBytes.Load())
		}
	})

	t.Run("replay", func(t *testing.T) {
		record, budget := pooledCompressedFetchRecord(t, "events", 1, 1)
		backend := &recordingReplayBackend{fetches: []kgo.Fetches{fetchesForRecord(record)}}
		reader := replayReaderWithBackend(backend, []ReplayRange{{
			Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
		}})
		result, err := reader.Replay(
			context.Background(),
			ReplayHandlerFunc(func(context.Context, ReplayRecord) error { return nil }),
		)
		if err != nil || result.Processed != 1 || budget.activeBytes.Load() != 0 {
			t.Fatalf("Replay() = (%#v, %v), active = %d", result, err, budget.activeBytes.Load())
		}
	})
}

func fetchesForRecord(record *kgo.Record) kgo.Fetches {
	return kgo.Fetches{{Topics: []kgo.FetchTopic{{
		Topic: record.Topic,
		Partitions: []kgo.FetchPartition{{
			Partition: record.Partition,
			Records:   []*kgo.Record{record},
		}},
	}}}}
}

func fetchBudgetFromClient(
	t *testing.T,
	client *kgo.Client,
) *fetchDecompressionBudget {
	t.Helper()
	pools := reflect.ValueOf(client.OptValue(kgo.WithPools))
	if pools.Kind() != reflect.Slice || pools.Len() != 1 {
		t.Fatalf("WithPools option = %#v", client.OptValue(kgo.WithPools))
	}
	budget, ok := pools.Index(0).Interface().(*fetchDecompressionBudget)
	if !ok {
		t.Fatalf("WithPools option = %#v", client.OptValue(kgo.WithPools))
	}

	return budget
}

func TestOversizedFetchBatchIsRejectedBeforeEveryReadHandler(t *testing.T) {
	fetches := kgo.NewErrFetch(ErrFetchBatchTooLarge)

	consumerBackend := &recordingConsumerBackend{fetches: fetches}
	consumer := consumerWithBackend(
		consumerBackend,
		1,
		time.Second,
		time.Second,
	)
	consumerResult, consumerErr := consumer.RunOnce(
		context.Background(),
		HandlerFunc(func(context.Context, ConsumedMessage) error {
			t.Fatal("consumer handler called for oversized fetch batch")

			return nil
		}),
	)
	var categorized *ConsumerError
	if consumerResult != (PollResult{}) ||
		!errors.As(consumerErr, &categorized) ||
		categorized.Category() != ErrorOversized ||
		!errors.Is(consumerErr, ErrFetchBatchTooLarge) ||
		len(consumerBackend.committed) != 0 {
		t.Fatalf("consumer result = %#v, error = %v", consumerResult, consumerErr)
	}

	transactionBackend := &recordingTransactionProcessorBackend{
		fetches: []kgo.Fetches{fetches},
	}
	processor := transactionProcessorForTest(t, transactionBackend)
	transactionResult, transactionErr := processor.RunOnce(
		context.Background(),
		TransactionHandlerFunc(func(context.Context, ConsumedRecord, Transaction) error {
			t.Fatal("transaction handler called for oversized fetch batch")

			return nil
		}),
	)
	if transactionResult != (TransactionPollResult{}) ||
		!errors.Is(transactionErr, ErrFetchBatchTooLarge) ||
		transactionBackend.beginCalls != 0 {
		t.Fatalf(
			"transaction result = %#v, error = %v, begins = %d",
			transactionResult,
			transactionErr,
			transactionBackend.beginCalls,
		)
	}

	replayBackend := &recordingReplayBackend{fetches: []kgo.Fetches{fetches}}
	reader := replayReaderWithBackend(replayBackend, []ReplayRange{{
		Topic: "events", Partition: 1, StartOffset: 1, EndOffset: 2,
	}})
	replayResult, replayErr := reader.Replay(
		context.Background(),
		ReplayHandlerFunc(func(context.Context, ReplayRecord) error {
			t.Fatal("replay handler called for oversized fetch batch")

			return nil
		}),
	)
	if replayResult.Processed != 0 ||
		!errors.Is(replayErr, ErrFetchBatchTooLarge) {
		t.Fatalf("replay result = %#v, error = %v", replayResult, replayErr)
	}
}

package kafka

import (
	"context"
	"crypto/tls"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestProducerConfigUsesExplicitCompressionPreference(t *testing.T) {
	t.Parallel()

	config, err := normalizeProducerConfig(ProducerConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "track",
	})
	if err != nil {
		t.Fatalf("normalize default config: %v", err)
	}
	if !reflect.DeepEqual(
		config.CompressionPreferences,
		[]CompressionCodec{CompressionSnappy, CompressionNone},
	) {
		t.Fatalf(
			"default compression = %#v",
			config.CompressionPreferences,
		)
	}

	preferences := []CompressionCodec{
		CompressionZstd,
		CompressionLz4,
		CompressionSnappy,
		CompressionNone,
	}
	config, err = normalizeProducerConfig(ProducerConfig{
		Brokers:                []string{"broker.internal:9092"},
		ClientID:               "track",
		CompressionPreferences: preferences,
	})
	if err != nil {
		t.Fatalf("normalize explicit config: %v", err)
	}
	preferences[0] = CompressionNone
	if !reflect.DeepEqual(
		config.CompressionPreferences,
		[]CompressionCodec{
			CompressionZstd,
			CompressionLz4,
			CompressionSnappy,
			CompressionNone,
		},
	) {
		t.Fatalf(
			"owned compression = %#v",
			config.CompressionPreferences,
		)
	}

	got := franzCompressionCodecs(config.CompressionPreferences)
	want := []kgo.CompressionCodec{
		kgo.ZstdCompression(),
		kgo.Lz4Compression(),
		kgo.SnappyCompression(),
		kgo.NoCompression(),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("franz compression = %#v, want %#v", got, want)
	}
}

func TestProducerConfigRejectsInvalidCompressionPreference(t *testing.T) {
	t.Parallel()

	tests := map[string][]CompressionCodec{
		"empty codec":       {0},
		"unknown codec":     {CompressionCodec(99)},
		"duplicate codec":   {CompressionSnappy, CompressionSnappy},
		"none before codec": {CompressionNone, CompressionSnappy},
		"too many codecs": {
			CompressionZstd,
			CompressionLz4,
			CompressionSnappy,
			CompressionGzip,
			CompressionNone,
			CompressionNone,
		},
	}
	for name, preferences := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			producer, err := NewProducer(ProducerConfig{
				Brokers:                []string{"broker.internal:9092"},
				ClientID:               "track",
				CompressionPreferences: preferences,
			})
			if producer != nil {
				producer.Close()
				t.Fatal("constructed producer with invalid compression")
			}
			if !errors.Is(err, ErrInvalidCompressionPreference) {
				t.Fatalf(
					"error = %v, want ErrInvalidCompressionPreference",
					err,
				)
			}
		})
	}
}

func TestCompressionCodecNamesAndFranzMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		codec CompressionCodec
		name  string
		franz kgo.CompressionCodec
	}{
		{CompressionNone, "none", kgo.NoCompression()},
		{CompressionGzip, "gzip", kgo.GzipCompression()},
		{CompressionSnappy, "snappy", kgo.SnappyCompression()},
		{CompressionLz4, "lz4", kgo.Lz4Compression()},
		{CompressionZstd, "zstd", kgo.ZstdCompression()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if test.codec.String() != test.name {
				t.Fatalf("name = %q, want %q", test.codec, test.name)
			}
			got := franzCompressionCodecs([]CompressionCodec{test.codec})
			if len(got) != 1 || !reflect.DeepEqual(got[0], test.franz) {
				t.Fatalf("franz codec = %#v, want %#v", got, test.franz)
			}
		})
	}
	if got := CompressionCodec(99).String(); got != "unknown" {
		t.Fatalf("unknown name = %q", got)
	}
}

func TestNewProducerRequiresAtLeastOneBroker(t *testing.T) {
	t.Parallel()

	producer, err := NewProducer(ProducerConfig{
		ClientID: "track",
	})

	if producer != nil {
		t.Fatal("NewProducer() returned a producer without any brokers")
	}
	if !errors.Is(err, ErrBrokersRequired) {
		t.Fatalf("NewProducer() error = %v, want %v", err, ErrBrokersRequired)
	}
}

func TestNewProducerRequiresClientIdentity(t *testing.T) {
	t.Parallel()

	producer, err := NewProducer(ProducerConfig{
		Brokers: []string{"broker.internal:9092"},
	})

	if producer != nil {
		t.Fatal("NewProducer() returned a producer without a client identity")
	}
	if !errors.Is(err, ErrClientIDRequired) {
		t.Fatalf("NewProducer() error = %v, want %v", err, ErrClientIDRequired)
	}
}

func TestNewProducerValidatesBrokerAndClientIdentity(t *testing.T) {
	t.Parallel()

	manyBrokers := make([]string, 33)
	for index := range manyBrokers {
		manyBrokers[index] = "broker-" + strings.Repeat("x", index+1)
	}

	tests := []struct {
		name    string
		brokers []string
		client  string
		want    error
	}{
		{
			name:    "empty broker",
			brokers: []string{""},
			client:  "track",
			want:    ErrInvalidBroker,
		},
		{
			name:    "whitespace broker",
			brokers: []string{" broker.internal:9092 "},
			client:  "track",
			want:    ErrInvalidBroker,
		},
		{
			name:    "oversized broker",
			brokers: []string{strings.Repeat("b", 256)},
			client:  "track",
			want:    ErrInvalidBroker,
		},
		{
			name:    "too many brokers",
			brokers: manyBrokers,
			client:  "track",
			want:    ErrTooManyBrokers,
		},
		{
			name:    "duplicate broker",
			brokers: []string{"broker:9092", "broker:9092"},
			client:  "track",
			want:    ErrDuplicateBroker,
		},
		{
			name:    "whitespace client ID",
			brokers: []string{"broker:9092"},
			client:  " ",
			want:    ErrClientIDRequired,
		},
		{
			name:    "oversized client ID",
			brokers: []string{"broker:9092"},
			client:  strings.Repeat("c", 256),
			want:    ErrClientIDTooLarge,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			producer, err := NewProducer(ProducerConfig{
				Brokers:  test.brokers,
				ClientID: test.client,
			})
			if producer != nil {
				producer.Close()
				t.Fatal("NewProducer() returned a producer with invalid identity")
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("NewProducer() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestNewProducerUsesBoundedMessageDefaults(t *testing.T) {
	t.Parallel()

	producer, err := NewProducer(ProducerConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "track",
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	t.Cleanup(producer.Close)

	if producer.limits != DefaultMessageLimits() {
		t.Fatalf("NewProducer() limits = %#v, want %#v", producer.limits, DefaultMessageLimits())
	}
}

func TestProducerConfigAppliesBoundedReliabilityDefaults(t *testing.T) {
	t.Parallel()

	config, err := normalizeProducerConfig(ProducerConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "track",
	})
	if err != nil {
		t.Fatalf("normalizeProducerConfig() error = %v", err)
	}

	if config.MaxBufferedRecords != 1_000 {
		t.Fatalf("MaxBufferedRecords = %d, want 1000", config.MaxBufferedRecords)
	}
	if config.MaxBatchBytes != 1<<20 {
		t.Fatalf("MaxBatchBytes = %d, want %d", config.MaxBatchBytes, 1<<20)
	}
	if config.RecordRetries != 10 {
		t.Fatalf("RecordRetries = %d, want 10", config.RecordRetries)
	}
	if config.DeliveryTimeout != 30*time.Second {
		t.Fatalf("DeliveryTimeout = %s, want 30s", config.DeliveryTimeout)
	}
	if config.RequestTimeout != 10*time.Second {
		t.Fatalf("RequestTimeout = %s, want 10s", config.RequestTimeout)
	}
	if config.DialTimeout != 10*time.Second {
		t.Fatalf("DialTimeout = %s, want 10s", config.DialTimeout)
	}
	if config.Linger != 5*time.Millisecond {
		t.Fatalf("Linger = %s, want 5ms", config.Linger)
	}

	transactionalConfig, err := normalizeProducerConfig(ProducerConfig{
		Brokers:         []string{"broker.internal:9092"},
		ClientID:        "track",
		TransactionalID: "track-outbox-0",
	})
	if err != nil {
		t.Fatalf("normalizeProducerConfig() transactional error = %v", err)
	}
	if transactionalConfig.TransactionTimeout != 30*time.Second ||
		transactionalConfig.TransactionEndTimeout != 30*time.Second {
		t.Fatalf("transactional defaults = %#v", transactionalConfig)
	}
}

func TestProducerConfigNormalizesSecureTransport(t *testing.T) {
	t.Parallel()

	sourceTLS := &tls.Config{}
	config, err := normalizeProducerConfig(ProducerConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "track",
		Security: ClientSecurity{TLS: sourceTLS},
	})
	if err != nil {
		t.Fatalf("normalizeProducerConfig() error = %v", err)
	}
	if config.Security.TLS == sourceTLS ||
		config.Security.TLS.MinVersion != tls.VersionTLS12 ||
		sourceTLS.MinVersion != 0 {
		t.Fatalf("normalized/source TLS = %#v/%#v", config.Security.TLS, sourceTLS)
	}

	tests := []*tls.Config{
		{InsecureSkipVerify: true},
		{MinVersion: tls.VersionTLS11},
		{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS12},
	}
	for _, tlsConfig := range tests {
		_, err := normalizeProducerConfig(ProducerConfig{
			Brokers:  []string{"broker.internal:9092"},
			ClientID: "track",
			Security: ClientSecurity{TLS: tlsConfig},
		})
		if !errors.Is(err, ErrInvalidSecurityConfig) {
			t.Fatalf("normalizeProducerConfig() security error = %v, want %v", err, ErrInvalidSecurityConfig)
		}
	}
}

func TestNewProducerRejectsUnboundedProducerConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*ProducerConfig)
	}{
		{
			name: "negative buffer",
			change: func(config *ProducerConfig) {
				config.MaxBufferedRecords = -1
			},
		},
		{
			name: "oversized buffer",
			change: func(config *ProducerConfig) {
				config.MaxBufferedRecords = 100_001
			},
		},
		{
			name: "negative batch",
			change: func(config *ProducerConfig) {
				config.MaxBatchBytes = -1
			},
		},
		{
			name: "batch below message budget",
			change: func(config *ProducerConfig) {
				config.MaxBatchBytes = 512
			},
		},
		{
			name: "oversized batch",
			change: func(config *ProducerConfig) {
				config.MaxBatchBytes = 101 << 20
			},
		},
		{
			name: "negative retries",
			change: func(config *ProducerConfig) {
				config.RecordRetries = -1
			},
		},
		{
			name: "excessive retries",
			change: func(config *ProducerConfig) {
				config.RecordRetries = 1_001
			},
		},
		{
			name: "negative delivery timeout",
			change: func(config *ProducerConfig) {
				config.DeliveryTimeout = -time.Second
			},
		},
		{
			name: "excessive delivery timeout",
			change: func(config *ProducerConfig) {
				config.DeliveryTimeout = 10*time.Minute + time.Nanosecond
			},
		},
		{
			name: "negative request timeout",
			change: func(config *ProducerConfig) {
				config.RequestTimeout = -time.Second
			},
		},
		{
			name: "excessive request timeout",
			change: func(config *ProducerConfig) {
				config.RequestTimeout = 2*time.Minute + time.Nanosecond
			},
		},
		{
			name: "request exceeds delivery timeout",
			change: func(config *ProducerConfig) {
				config.DeliveryTimeout = time.Second
				config.RequestTimeout = 2 * time.Second
			},
		},
		{
			name: "negative dial timeout",
			change: func(config *ProducerConfig) {
				config.DialTimeout = -time.Second
			},
		},
		{
			name: "excessive dial timeout",
			change: func(config *ProducerConfig) {
				config.DialTimeout = 2*time.Minute + time.Nanosecond
			},
		},
		{
			name: "negative linger",
			change: func(config *ProducerConfig) {
				config.Linger = -time.Second
			},
		},
		{
			name: "excessive linger",
			change: func(config *ProducerConfig) {
				config.Linger = time.Second + time.Nanosecond
			},
		},
		{
			name: "whitespace transactional ID",
			change: func(config *ProducerConfig) {
				config.TransactionalID = " transaction "
			},
		},
		{
			name: "oversized transactional ID",
			change: func(config *ProducerConfig) {
				config.TransactionalID = strings.Repeat("t", 256)
			},
		},
		{
			name: "transaction timeout without transactional ID",
			change: func(config *ProducerConfig) {
				config.TransactionTimeout = time.Second
			},
		},
		{
			name: "short transaction timeout",
			change: func(config *ProducerConfig) {
				config.TransactionalID = "track-outbox"
				config.TransactionTimeout = 999 * time.Millisecond
			},
		},
		{
			name: "excessive transaction timeout",
			change: func(config *ProducerConfig) {
				config.TransactionalID = "track-outbox"
				config.TransactionTimeout = 16 * time.Minute
			},
		},
		{
			name: "short transaction end timeout",
			change: func(config *ProducerConfig) {
				config.TransactionalID = "track-outbox"
				config.TransactionEndTimeout = 999 * time.Millisecond
			},
		},
		{
			name: "excessive transaction end timeout",
			change: func(config *ProducerConfig) {
				config.TransactionalID = "track-outbox"
				config.TransactionEndTimeout = 3 * time.Minute
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := ProducerConfig{
				Brokers:  []string{"broker.internal:9092"},
				ClientID: "track",
			}
			test.change(&config)

			producer, err := NewProducer(config)

			if producer != nil {
				producer.Close()
				t.Fatal("NewProducer() returned a producer with invalid configuration")
			}
			if !errors.Is(err, ErrInvalidProducerConfig) {
				t.Fatalf("NewProducer() error = %v, want %v", err, ErrInvalidProducerConfig)
			}
		})
	}
}

func TestNewProducerRejectsPartiallyConfiguredMessageLimits(t *testing.T) {
	t.Parallel()

	limits := DefaultMessageLimits()
	limits.MaxHeaders = 0

	producer, err := NewProducer(ProducerConfig{
		Brokers:  []string{"broker.internal:9092"},
		ClientID: "track",
		Limits:   limits,
	})

	if producer != nil {
		t.Fatal("NewProducer() returned a producer with invalid message limits")
	}
	if !errors.Is(err, ErrInvalidMessageLimits) {
		t.Fatalf("NewProducer() error = %v, want %v", err, ErrInvalidMessageLimits)
	}
}

func TestNewProducerPreservesClientConstructionFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("client construction failed")
	producer, err := newProducer(
		ProducerConfig{
			Brokers:         []string{"broker.internal:9092"},
			ClientID:        "track",
			TransactionalID: "track-outbox-0",
		},
		func(...kgo.Opt) (*kgo.Client, error) {
			return nil, want
		},
	)

	if producer != nil {
		t.Fatal("newProducer() returned a producer after client construction failed")
	}
	if !errors.Is(err, want) {
		t.Fatalf("newProducer() error = %v, want %v", err, want)
	}
}

func TestProducerPublishesMessageSynchronously(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := &Producer{client: backend, limits: DefaultMessageLimits()}
	message := Message{
		Topic: "track.tracking-event.v1",
		Key:   []byte("consignment-42"),
		Value: []byte(`{"event_id":"event-1"}`),
		Headers: []Header{
			{Key: "content-type", Value: []byte("application/json")},
			{Key: "schema-version", Value: []byte("1")},
		},
	}

	if err := producer.Publish(context.Background(), message); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if len(backend.records) != 1 {
		t.Fatalf("Publish() records = %d, want 1", len(backend.records))
	}
	record := backend.records[0]
	if record.Topic != message.Topic {
		t.Fatalf("Publish() topic = %q, want %q", record.Topic, message.Topic)
	}
	if string(record.Key) != string(message.Key) {
		t.Fatalf("Publish() key = %q, want %q", record.Key, message.Key)
	}
	if string(record.Value) != string(message.Value) {
		t.Fatalf("Publish() value = %q, want %q", record.Value, message.Value)
	}
	if len(record.Headers) != len(message.Headers) {
		t.Fatalf("Publish() headers = %d, want %d", len(record.Headers), len(message.Headers))
	}
	for index, header := range record.Headers {
		if header.Key != message.Headers[index].Key {
			t.Fatalf("Publish() header %d key = %q, want %q", index, header.Key, message.Headers[index].Key)
		}
		if string(header.Value) != string(message.Headers[index].Value) {
			t.Fatalf(
				"Publish() header %d value = %q, want %q",
				index,
				header.Value,
				message.Headers[index].Value,
			)
		}
	}
}

func TestProducerPreservesDeliveryAndHealthFailures(t *testing.T) {
	t.Parallel()

	deliveryErr := errors.New("delivery failed")
	healthErr := errors.New("broker unavailable")
	backend := &recordingProducerBackend{
		deliveryErr: deliveryErr,
		healthErr:   healthErr,
	}
	producer := &Producer{client: backend, limits: DefaultMessageLimits()}

	if err := producer.Publish(context.Background(), Message{
		Topic: "events",
		Value: []byte("payload"),
	}); !errors.Is(err, deliveryErr) {
		t.Fatalf("Publish() error = %v, want %v", err, deliveryErr)
	}
	if err := producer.Health(context.Background()); !errors.Is(err, healthErr) {
		t.Fatalf("Health() error = %v, want %v", err, healthErr)
	}
}

func TestProducerRunsSerializedTransaction(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := &Producer{
		client:                backend,
		limits:                DefaultMessageLimits(),
		transactionsEnabled:   true,
		transactionEndTimeout: time.Second,
	}

	err := producer.RunTransaction(context.Background(), func(transaction Transaction) error {
		return transaction.Publish(context.Background(), Message{
			Topic: "events",
			Key:   []byte("aggregate-1"),
			Value: []byte("payload"),
		})
	})

	if err != nil {
		t.Fatalf("RunTransaction() error = %v", err)
	}
	if backend.begins != 1 ||
		len(backend.records) != 1 ||
		len(backend.endTries) != 1 ||
		backend.endTries[0] != kgo.TryCommit ||
		backend.aborts != 0 {
		t.Fatalf("transaction backend = %#v", backend)
	}
}

func TestProducerAbortsFailedOrPanickingTransaction(t *testing.T) {
	t.Parallel()

	callbackErr := errors.New("application failed")
	tests := []struct {
		name     string
		callback func(Transaction) error
		want     error
	}{
		{
			name:     "callback error",
			callback: func(Transaction) error { return callbackErr },
			want:     callbackErr,
		},
		{
			name: "callback panic",
			callback: func(Transaction) error {
				panic("secret transaction state")
			},
			want: ErrTransactionPanic,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			backend := &recordingProducerBackend{}
			producer := &Producer{
				client:                backend,
				limits:                DefaultMessageLimits(),
				transactionsEnabled:   true,
				transactionEndTimeout: time.Second,
			}
			err := producer.RunTransaction(context.Background(), test.callback)

			if !errors.Is(err, test.want) ||
				strings.Contains(err.Error(), "secret transaction state") ||
				backend.begins != 1 ||
				backend.aborts != 1 ||
				len(backend.endTries) != 1 ||
				backend.endTries[0] != kgo.TryAbort {
				t.Fatalf("error/backend = %v/%#v", err, backend)
			}
		})
	}
}

func TestProducerRejectsUnavailableTransaction(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := &Producer{
		client:                backend,
		limits:                DefaultMessageLimits(),
		transactionEndTimeout: time.Second,
	}
	if err := producer.RunTransaction(context.Background(), func(Transaction) error {
		t.Fatal("callback called while transactions disabled")

		return nil
	}); !errors.Is(err, ErrTransactionsDisabled) {
		t.Fatalf("RunTransaction() error = %v, want %v", err, ErrTransactionsDisabled)
	}
	producer.transactionsEnabled = true
	if err := producer.RunTransaction(context.Background(), nil); !errors.Is(err, ErrTransactionRequired) {
		t.Fatalf("RunTransaction(nil) error = %v, want %v", err, ErrTransactionRequired)
	}

	backend.beginErr = errors.New("begin failed")
	if err := producer.RunTransaction(context.Background(), func(Transaction) error {
		return nil
	}); !errors.Is(err, backend.beginErr) {
		t.Fatalf("RunTransaction() begin error = %v, want %v", err, backend.beginErr)
	}
}

func TestProducerPreservesTransactionCleanupFailures(t *testing.T) {
	t.Parallel()

	callbackErr := errors.New("application failed")
	abortErr := errors.New("abort buffered failed")
	endErr := errors.New("end transaction failed")
	backend := &recordingProducerBackend{abortErr: abortErr, endErr: endErr}
	producer := &Producer{
		client:                backend,
		limits:                DefaultMessageLimits(),
		transactionsEnabled:   true,
		transactionEndTimeout: time.Second,
	}

	err := producer.RunTransaction(context.Background(), func(Transaction) error {
		return callbackErr
	})

	if !errors.Is(err, callbackErr) ||
		!errors.Is(err, abortErr) ||
		!errors.Is(err, endErr) {
		t.Fatalf("RunTransaction() error = %v", err)
	}
}

func TestProducerClassifiesTransactionCommitFailure(t *testing.T) {
	t.Parallel()

	unknownErr := errors.New("connection lost during commit")
	unknownBackend := &recordingProducerBackend{endErr: unknownErr}
	unknownProducer := transactionalProducer(unknownBackend)
	err := unknownProducer.RunTransaction(context.Background(), func(Transaction) error {
		return nil
	})
	if !errors.Is(err, unknownErr) ||
		!errors.Is(err, ErrTransactionOutcomeUnknown) ||
		unknownBackend.aborts != 0 ||
		len(unknownBackend.endTries) != 1 {
		t.Fatalf("unknown outcome error/backend = %v/%#v", err, unknownBackend)
	}

	abortableBackend := &recordingProducerBackend{
		endErrors: []error{kerr.TransactionAbortable, nil},
	}
	abortableProducer := transactionalProducer(abortableBackend)
	err = abortableProducer.RunTransaction(context.Background(), func(Transaction) error {
		return nil
	})
	if !errors.Is(err, kerr.TransactionAbortable) ||
		errors.Is(err, ErrTransactionOutcomeUnknown) ||
		abortableBackend.aborts != 1 ||
		len(abortableBackend.endTries) != 2 ||
		abortableBackend.endTries[0] != kgo.TryCommit ||
		abortableBackend.endTries[1] != kgo.TryAbort {
		t.Fatalf("abortable error/backend = %v/%#v", err, abortableBackend)
	}
}

func TestTransactionCannotPublishAfterCallbackReturns(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := transactionalProducer(backend)
	var retained Transaction
	if err := producer.RunTransaction(context.Background(), func(transaction Transaction) error {
		retained = transaction

		return nil
	}); err != nil {
		t.Fatalf("RunTransaction() error = %v", err)
	}

	err := retained.Publish(context.Background(), Message{Topic: "events"})
	if !errors.Is(err, ErrTransactionClosed) || len(backend.records) != 0 {
		t.Fatalf("retained Publish() error/backend = %v/%#v", err, backend)
	}
	if err := (Transaction{}).Publish(context.Background(), Message{
		Topic: "events",
	}); !errors.Is(err, ErrTransactionClosed) {
		t.Fatalf("zero Transaction.Publish() error = %v, want %v", err, ErrTransactionClosed)
	}
}

func TestTransactionWaitsForStartedPublishBeforeCommit(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{
		produceStarted: make(chan struct{}),
		produceRelease: make(chan struct{}),
	}
	producer := transactionalProducer(backend)
	publishDone := make(chan error, 1)
	transactionDone := make(chan error, 1)
	go func() {
		transactionDone <- producer.RunTransaction(
			context.Background(),
			func(transaction Transaction) error {
				go func() {
					publishDone <- transaction.Publish(context.Background(), Message{
						Topic: "events",
					})
				}()
				<-backend.produceStarted

				return nil
			},
		)
	}()
	<-backend.produceStarted
	select {
	case err := <-transactionDone:
		t.Fatalf("transaction completed before in-flight publish: %v", err)
	default:
	}
	close(backend.produceRelease)
	if err := <-publishDone; err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err := <-transactionDone; err != nil {
		t.Fatalf("RunTransaction() error = %v", err)
	}
	if len(backend.endTries) != 1 || backend.endTries[0] != kgo.TryCommit {
		t.Fatalf("transaction backend = %#v", backend)
	}
}

func transactionalProducer(backend producerBackend) *Producer {
	return &Producer{
		client:                backend,
		limits:                DefaultMessageLimits(),
		transactionsEnabled:   true,
		transactionEndTimeout: time.Second,
	}
}

func TestProducerRejectsMessageWithoutTopic(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	producer := &Producer{client: backend, limits: DefaultMessageLimits()}

	err := producer.Publish(context.Background(), Message{Value: []byte("payload")})

	if !errors.Is(err, ErrTopicRequired) {
		t.Fatalf("Publish() error = %v, want %v", err, ErrTopicRequired)
	}
	if len(backend.records) != 0 {
		t.Fatalf("Publish() records = %d, want 0", len(backend.records))
	}
}

func TestProducerRejectsTopicAboveConfiguredLimit(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	limits := DefaultMessageLimits()
	producer := &Producer{client: backend, limits: limits}

	err := producer.Publish(context.Background(), Message{
		Topic: strings.Repeat("t", limits.MaxTopicBytes+1),
		Value: []byte("payload"),
	})

	if !errors.Is(err, ErrTopicTooLarge) {
		t.Fatalf("Publish() error = %v, want %v", err, ErrTopicTooLarge)
	}
	if len(backend.records) != 0 {
		t.Fatalf("Publish() records = %d, want 0", len(backend.records))
	}
}

func TestProducerRejectsMessageFieldsAboveConfiguredLimits(t *testing.T) {
	t.Parallel()

	limits := DefaultMessageLimits()
	manyHeaders := make([]Header, limits.MaxHeaders+1)
	for index := range manyHeaders {
		manyHeaders[index] = Header{Key: "key"}
	}
	largeHeaders := make([]Header, 5)
	for index := range largeHeaders {
		largeHeaders[index] = Header{
			Key:   "key",
			Value: []byte(strings.Repeat("v", limits.MaxHeaderValueBytes)),
		}
	}

	tests := []struct {
		name    string
		message Message
		want    error
	}{
		{
			name: "key",
			message: Message{
				Topic: "events",
				Key:   []byte(strings.Repeat("k", limits.MaxKeyBytes+1)),
			},
			want: ErrKeyTooLarge,
		},
		{
			name: "value",
			message: Message{
				Topic: "events",
				Value: []byte(strings.Repeat("v", limits.MaxValueBytes+1)),
			},
			want: ErrValueTooLarge,
		},
		{
			name: "header count",
			message: Message{
				Topic:   "events",
				Headers: manyHeaders,
			},
			want: ErrTooManyHeaders,
		},
		{
			name: "empty header key",
			message: Message{
				Topic:   "events",
				Headers: []Header{{Value: []byte("value")}},
			},
			want: ErrHeaderKeyRequired,
		},
		{
			name: "header key",
			message: Message{
				Topic: "events",
				Headers: []Header{{
					Key: strings.Repeat("k", limits.MaxHeaderKeyBytes+1),
				}},
			},
			want: ErrHeaderKeyTooLarge,
		},
		{
			name: "header value",
			message: Message{
				Topic: "events",
				Headers: []Header{{
					Key:   "key",
					Value: []byte(strings.Repeat("v", limits.MaxHeaderValueBytes+1)),
				}},
			},
			want: ErrHeaderValueTooLarge,
		},
		{
			name: "aggregate headers",
			message: Message{
				Topic:   "events",
				Headers: largeHeaders,
			},
			want: ErrHeadersTooLarge,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			backend := &recordingProducerBackend{}
			producer := &Producer{client: backend, limits: limits}

			err := producer.Publish(context.Background(), test.message)

			if !errors.Is(err, test.want) {
				t.Fatalf("Publish() error = %v, want %v", err, test.want)
			}
			if len(backend.records) != 0 {
				t.Fatalf("Publish() records = %d, want 0", len(backend.records))
			}
		})
	}
}

func TestProducerRejectsAggregateHeaderKeyOverflow(t *testing.T) {
	t.Parallel()

	backend := &recordingProducerBackend{}
	limits := DefaultMessageLimits()
	limits.MaxHeaderBytes = 5
	producer := &Producer{client: backend, limits: limits}

	err := producer.Publish(context.Background(), Message{
		Topic: "events",
		Headers: []Header{
			{Key: "key", Value: []byte("12")},
			{Key: "x"},
		},
	})

	if !errors.Is(err, ErrHeadersTooLarge) {
		t.Fatalf("Publish() error = %v, want %v", err, ErrHeadersTooLarge)
	}
	if len(backend.records) != 0 {
		t.Fatalf("Publish() records = %d, want 0", len(backend.records))
	}
}

type recordingProducerBackend struct {
	records         []*kgo.Record
	deliveryErr     error
	deliveryErrors  []error
	healthErr       error
	beginErr        error
	abortErr        error
	endErr          error
	endErrors       []error
	begins          int
	aborts          int
	endTries        []kgo.TransactionEndTry
	produceStarted  chan struct{}
	produceRelease  chan struct{}
	prepareDelivery func(*kgo.Record)
	asyncPromises   []func(*kgo.Record, error)
	closes          int
	flushes         int
	flushErr        error
	flushStarted    chan struct{}
	flushRelease    chan struct{}
	omitDeliveries  bool
}

func (backend *recordingProducerBackend) ProduceSync(
	_ context.Context,
	records ...*kgo.Record,
) kgo.ProduceResults {
	backend.records = append(backend.records, records...)
	if backend.produceStarted != nil {
		close(backend.produceStarted)
		<-backend.produceRelease
	}
	if backend.omitDeliveries {
		return nil
	}

	results := make(kgo.ProduceResults, len(records))
	for index, record := range records {
		if backend.prepareDelivery != nil {
			backend.prepareDelivery(record)
		}
		deliveryErr := backend.deliveryErr
		if index < len(backend.deliveryErrors) {
			deliveryErr = backend.deliveryErrors[index]
		}
		results[index] = kgo.ProduceResult{Record: record, Err: deliveryErr}
	}

	return results
}

func (backend *recordingProducerBackend) Produce(
	_ context.Context,
	record *kgo.Record,
	promise func(*kgo.Record, error),
) {
	backend.records = append(backend.records, record)
	backend.asyncPromises = append(backend.asyncPromises, promise)
}

func (backend *recordingProducerBackend) completeAsync(
	index int,
	partition int32,
	offset int64,
	err error,
) {
	record := backend.records[index]
	record.Partition = partition
	record.Offset = offset
	backend.asyncPromises[index](record, err)
}

func (backend *recordingProducerBackend) completeAsyncMissing(index int, err error) {
	backend.asyncPromises[index](nil, err)
}

func (backend *recordingProducerBackend) Ping(context.Context) error {
	return backend.healthErr
}

func (backend *recordingProducerBackend) Flush(context.Context) error {
	backend.flushes++
	if backend.flushStarted != nil {
		close(backend.flushStarted)
		<-backend.flushRelease
	}

	return backend.flushErr
}

func (backend *recordingProducerBackend) BeginTransaction() error {
	backend.begins++

	return backend.beginErr
}

func (backend *recordingProducerBackend) AbortBufferedRecords(context.Context) error {
	backend.aborts++

	return backend.abortErr
}

func (backend *recordingProducerBackend) EndTransaction(
	_ context.Context,
	try kgo.TransactionEndTry,
) error {
	backend.endTries = append(backend.endTries, try)
	if len(backend.endErrors) != 0 {
		err := backend.endErrors[0]
		backend.endErrors = backend.endErrors[1:]

		return err
	}

	return backend.endErr
}

func (backend *recordingProducerBackend) Close() {
	backend.closes++
}

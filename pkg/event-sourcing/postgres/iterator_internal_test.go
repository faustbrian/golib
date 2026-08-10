package postgres

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestIteratorReadsMessagesAndOwnsClosure(t *testing.T) {
	t.Parallel()

	rows := &fakeRows{
		scans: []scanFunc{
			storedMessageScan(storedMessageValues{}),
			storedMessageScan(storedMessageValues{
				position:      2,
				streamVersion: 2,
				messageID:     "message-2",
				correlationID: stringPointer("correlation-1"),
				causationID:   stringPointer("causation-1"),
				tenant:        stringPointer("tenant-1"),
				partition:     stringPointer("partition-1"),
			}),
		},
	}
	iterator := &iterator{
		rows:                   rows,
		expectedStreamVersion:  1,
		expectedGlobalPosition: 1,
		checkStreamVersion:     true,
		checkGlobalPosition:    true,
	}
	var messages []eventsourcing.Message
	for iterator.Next(context.Background()) {
		messages = append(messages, iterator.Message())
	}
	if err := iterator.Err(); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 ||
		messages[0].ID().String() != "message-1" ||
		messages[1].ID().String() != "message-2" {
		t.Fatalf("messages = %#v", messages)
	}
	if err := iterator.Close(); err != nil {
		t.Fatal(err)
	}
	if err := iterator.Close(); err != nil {
		t.Fatal(err)
	}
	if iterator.Next(context.Background()) ||
		!errors.Is(iterator.Err(), eventsourcing.ErrIteratorClosed) ||
		!iterator.Message().ID().IsZero() {
		t.Fatalf(
			"closed iterator = next:%t err:%v message:%#v",
			iterator.Next(context.Background()),
			iterator.Err(),
			iterator.Message(),
		)
	}
}

func TestIteratorRejectsNonSequentialStoredHistory(t *testing.T) {
	t.Parallel()

	tests := map[string]*iterator{
		"stream": {
			rows: &fakeRows{
				scans: []scanFunc{
					storedMessageScan(storedMessageValues{
						streamVersion: 2,
					}),
				},
			},
			expectedStreamVersion: 1,
			checkStreamVersion:    true,
		},
		"global": {
			rows: &fakeRows{
				scans: []scanFunc{
					storedMessageScan(storedMessageValues{position: 2}),
				},
			},
			expectedGlobalPosition: 1,
			checkGlobalPosition:    true,
		},
	}
	for name, iterator := range tests {
		iterator := iterator
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if iterator.Next(context.Background()) ||
				!errors.Is(
					iterator.Err(),
					eventsourcing.ErrCorruptHistory,
				) {
				t.Fatalf("Next() = true or err=%v", iterator.Err())
			}
		})
	}
}

func TestIteratorStopsOnCancellationAndRowFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("row failure")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	var nilContext context.Context
	tests := map[string]struct {
		ctx  context.Context
		rows *fakeRows
		want error
	}{
		"nil context": {
			ctx:  nilContext,
			rows: &fakeRows{},
			want: eventsourcing.ErrInvalidArgument,
		},
		"cancelled context": {
			ctx:  cancelled,
			rows: &fakeRows{},
			want: context.Canceled,
		},
		"rows failure": {
			ctx:  context.Background(),
			rows: &fakeRows{err: failure},
			want: failure,
		},
		"scan failure": {
			ctx: context.Background(),
			rows: &fakeRows{
				scans: []scanFunc{
					func([]any) error { return failure },
				},
			},
			want: failure,
		},
	}
	for name, testCase := range tests {
		testCase := testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			iterator := &iterator{rows: testCase.rows}
			if iterator.Next(testCase.ctx) ||
				!errors.Is(iterator.Err(), testCase.want) ||
				!testCase.rows.closed {
				t.Fatalf(
					"Next() = true or err=%v closed=%t",
					iterator.Err(),
					testCase.rows.closed,
				)
			}
			if errors.Is(testCase.want, failure) {
				assertDriverErrorRedacted(t, iterator.Err(), failure)
			}
			if iterator.Next(context.Background()) {
				t.Fatal("failed iterator resumed")
			}
		})
	}
}

func TestIteratorClosePreservesRowsFailure(t *testing.T) {
	t.Parallel()

	failure := errors.New("close rows failure")
	iterator := &iterator{rows: &fakeRows{err: failure}}
	if err := iterator.Close(); !errors.Is(err, failure) ||
		!errors.Is(iterator.Err(), failure) {
		t.Fatalf("Close() = %v, iterator error = %v", err, iterator.Err())
	}
}

func TestScanMessageRejectsCorruptStoredRows(t *testing.T) {
	t.Parallel()

	scanFailure := errors.New("scan failure")
	tests := map[string]scanFunc{
		"scan": func([]any) error {
			return scanFailure
		},
		"position":       corruptStoredScan(0, int64(0)),
		"stream version": corruptStoredScan(4, int64(0)),
		"schema version": corruptStoredScan(6, int64(0)),
		"schema version overflow": corruptStoredScan(
			6,
			int64(^uint32(0))+1,
		),
		"metadata":              corruptStoredScan(9, []byte("{")),
		"metadata encoded size": corruptStoredScan(9, oversizedStoredMetadataJSON(t)),
		"stream":                corruptStoredScan(2, "not valid"),
		"event":                 corruptStoredScan(5, "not valid"),
		"pending":               corruptStoredScan(1, "not valid"),
	}
	for name, scan := range tests {
		scan := scan
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := scanMessage(fakeRow{scan: scan})
			if err == nil {
				t.Fatal("scanMessage() unexpectedly succeeded")
			}
			if name != "scan" &&
				!errors.Is(err, eventsourcing.ErrCorruptHistory) {
				t.Fatalf("scanMessage() = %v", err)
			}
			switch name {
			case "position", "stream version", "schema version", "schema version overflow":
				if err != eventsourcing.ErrCorruptHistory {
					t.Fatalf("scanMessage() classified stored envelope corruption as %v", err)
				}
			}
			if strings.Contains(err.Error(), "account-1") {
				t.Fatalf("scan error disclosed stored identity: %v", err)
			}
		})
	}
}

func TestScanMessageAcceptsMaximumStoredMetadataJSONSize(t *testing.T) {
	t.Parallel()

	message, err := scanMessage(fakeRow{scan: storedMessageScan(
		storedMessageValues{metadata: maximumStoredMetadataJSON(t)},
	)})
	if err != nil {
		t.Fatalf("scanMessage(maximum metadata) error = %v", err)
	}
	if len(message.Metadata()) != 15 {
		t.Fatalf("scanMessage(maximum metadata) entries = %d", len(message.Metadata()))
	}
}

func TestScanMessageAcceptsMaximumSchemaVersion(t *testing.T) {
	t.Parallel()

	message, err := scanMessage(fakeRow{scan: storedMessageScan(
		storedMessageValues{schemaVersion: int64(math.MaxUint32)},
	)})
	if err != nil {
		t.Fatalf("scanMessage(maximum schema version) error = %v", err)
	}
	if message.Event().Version() != eventsourcing.SchemaVersion(math.MaxUint32) {
		t.Fatalf(
			"scanMessage(maximum schema version) = %d",
			message.Event().Version(),
		)
	}
}

type storedMessageValues struct {
	position      int64
	messageID     string
	aggregateType string
	aggregateID   string
	streamVersion int64
	eventName     string
	schemaVersion int64
	contentType   string
	payload       []byte
	metadata      []byte
	recordedAt    time.Time
	correlationID *string
	causationID   *string
	tenant        *string
	partition     *string
}

func storedMessageScan(values storedMessageValues) scanFunc {
	if values.position == 0 {
		values.position = 1
	}
	if values.messageID == "" {
		values.messageID = "message-1"
	}
	if values.aggregateType == "" {
		values.aggregateType = "account"
	}
	if values.aggregateID == "" {
		values.aggregateID = "account-1"
	}
	if values.streamVersion == 0 {
		values.streamVersion = 1
	}
	if values.eventName == "" {
		values.eventName = "account.changed"
	}
	if values.schemaVersion == 0 {
		values.schemaVersion = 1
	}
	if values.contentType == "" {
		values.contentType = "application/json"
	}
	if values.payload == nil {
		values.payload = []byte(`{"changed":true}`)
	}
	if values.metadata == nil {
		values.metadata = []byte(`{"source":"test"}`)
	}
	if values.recordedAt.IsZero() {
		values.recordedAt = time.Unix(1, 123456000).UTC()
	}

	return func(destinations []any) error {
		*destinations[0].(*int64) = values.position
		*destinations[1].(*string) = values.messageID
		*destinations[2].(*string) = values.aggregateType
		*destinations[3].(*string) = values.aggregateID
		*destinations[4].(*int64) = values.streamVersion
		*destinations[5].(*string) = values.eventName
		*destinations[6].(*int64) = values.schemaVersion
		*destinations[7].(*string) = values.contentType
		*destinations[8].(*[]byte) = values.payload
		*destinations[9].(*[]byte) = values.metadata
		*destinations[10].(*time.Time) = values.recordedAt
		*destinations[11].(**string) = values.correlationID
		*destinations[12].(**string) = values.causationID
		*destinations[13].(**string) = values.tenant
		*destinations[14].(**string) = values.partition

		return nil
	}
}

func corruptStoredScan(index int, value any) scanFunc {
	valid := storedMessageScan(storedMessageValues{})

	return func(destinations []any) error {
		if err := valid(destinations); err != nil {
			return err
		}
		destination := reflectValue(destinations[index])
		destination.Set(reflectValue(value))

		return nil
	}
}

func reflectValue(value any) reflect.Value {
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Pointer {
		return reflected.Elem()
	}

	return reflected
}

func stringPointer(value string) *string {
	return &value
}

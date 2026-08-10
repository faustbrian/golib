package postgres

import (
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func FuzzScanMessage(f *testing.F) {
	f.Add(
		int64(1),
		"message-1",
		"account",
		"account-1",
		int64(1),
		"account.changed",
		int32(1),
		"application/json",
		[]byte(`{}`),
		[]byte(`{}`),
		int64(1),
	)
	f.Add(
		int64(0),
		"",
		"",
		"",
		int64(0),
		"",
		int32(0),
		"",
		[]byte(nil),
		[]byte(nil),
		int64(0),
	)
	f.Add(
		int64(1),
		"message-oversized-metadata",
		"account",
		"account-oversized-metadata",
		int64(1),
		"account.changed",
		int32(1),
		"application/json",
		[]byte(`{}`),
		oversizedStoredMetadataJSON(f),
		int64(1),
	)

	f.Fuzz(func(
		t *testing.T,
		position int64,
		messageID string,
		aggregateType string,
		aggregateID string,
		streamVersion int64,
		eventName string,
		schemaVersion int32,
		contentType string,
		payload []byte,
		metadata []byte,
		recordedSeconds int64,
	) {
		if len(messageID)+len(aggregateType)+len(aggregateID)+
			len(eventName)+len(contentType)+len(payload)+len(metadata) >
			2<<20 {
			return
		}
		scan := func(destinations []any) error {
			*destinations[0].(*int64) = position
			*destinations[1].(*string) = messageID
			*destinations[2].(*string) = aggregateType
			*destinations[3].(*string) = aggregateID
			*destinations[4].(*int64) = streamVersion
			*destinations[5].(*string) = eventName
			*destinations[6].(*int64) = int64(schemaVersion)
			*destinations[7].(*string) = contentType
			*destinations[8].(*[]byte) = payload
			*destinations[9].(*[]byte) = metadata
			*destinations[10].(*time.Time) = time.Unix(
				recordedSeconds,
				0,
			).UTC()

			return nil
		}

		_, err := scanMessage(fakeRow{scan: scan})
		if len(metadata) > maximumStoredMetadataJSONBytes &&
			err != eventsourcing.ErrCorruptHistory {
			t.Fatalf("oversized metadata error = %v", err)
		}
	})
}

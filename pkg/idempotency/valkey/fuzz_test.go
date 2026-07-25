package valkey

import (
	"encoding/json"
	"testing"
)

func FuzzPersistedHashDecoderFailsClosed(f *testing.F) {
	fields, err := encodeRecord(testRecord(f))
	if err != nil {
		f.Fatalf("encodeRecord() error = %v", err)
	}
	valid, err := json.Marshal(fields)
	if err != nil {
		f.Fatalf("json.Marshal() error = %v", err)
	}
	f.Add(valid)
	f.Add([]byte("not-json"))
	f.Add([]byte(`{"schema":"1","metadata":"{\"key\":42}"}`))
	f.Add([]byte(`{"schema":"2","result":"value"}`))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 2<<20 {
			return
		}
		var candidate map[string]string
		if err := json.Unmarshal(encoded, &candidate); err != nil {
			return
		}
		record, err := decodeRecord(candidate)
		if err != nil {
			return
		}
		roundTrip, err := encodeRecord(record)
		if err != nil {
			t.Fatalf("successfully decoded record could not encode: %v", err)
		}
		decoded, err := decodeRecord(roundTrip)
		if err != nil {
			t.Fatalf("encoded record could not decode: %v", err)
		}
		if decoded.Key != record.Key || !decoded.Fingerprint.Equal(record.Fingerprint) ||
			decoded.State != record.State || decoded.FencingToken != record.FencingToken ||
			decoded.Attempt != record.Attempt {
			t.Fatalf("record identity changed after round trip: %#v != %#v", decoded, record)
		}
	})
}

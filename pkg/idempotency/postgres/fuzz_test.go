package postgres

import "testing"

func FuzzPersistedRecordDecoderFailsClosed(f *testing.F) {
	valid, err := encodeRecord(codecRecord(f))
	if err != nil {
		f.Fatalf("encodeRecord() error = %v", err)
	}
	f.Add(valid)
	f.Add([]byte("not-json"))
	f.Add([]byte(`{"schema":1,"metadata":{"key":42}}`))
	f.Add([]byte(`{"schema":2,"result":"AA=="}`))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 2<<20 {
			return
		}
		record, err := decodeRecord(encoded)
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

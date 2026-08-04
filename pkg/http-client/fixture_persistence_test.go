package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRecorderWritesDeterministicVersionedFixtureAndLoaderChecksExpiry(t *testing.T) {
	clock := &fixtureTestClock{now: time.Unix(1_700_000_000, 0).UTC()}
	recorder, err := NewRecorderTransport(telemetryNoContentTransport(), RecorderOptions{
		Clock: clock, TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("construct persistence recorder: %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.test/?token=secret", nil)
	response, err := recorder.RoundTrip(request)
	if err != nil {
		t.Fatalf("record persistence interaction: %v", err)
	}
	_ = response.Body.Close()
	var first bytes.Buffer
	if err := recorder.WriteFixture(&first); err != nil {
		t.Fatalf("write first fixture: %v", err)
	}
	var second bytes.Buffer
	if err := recorder.WriteFixture(&second); err != nil {
		t.Fatalf("write second fixture: %v", err)
	}
	if first.String() != second.String() || strings.Contains(first.String(), "token=secret") {
		t.Fatalf("fixture output is unsafe or nondeterministic: %s", first.String())
	}

	loaded, err := ReadFixture(bytes.NewReader(first.Bytes()), FixtureLoadOptions{Clock: clock})
	if err != nil || loaded.SchemaVersion != FixtureSchemaVersion || len(loaded.Interactions) != 1 {
		t.Fatalf("load current fixture = %#v, %v", loaded, err)
	}
	clock.now = loaded.ExpiresAt
	if _, err := ReadFixture(bytes.NewReader(first.Bytes()), FixtureLoadOptions{Clock: clock}); !errors.Is(err, ErrFixtureExpired) {
		t.Fatalf("expired fixture error = %v", err)
	}
	if _, err := ReadFixture(bytes.NewReader(first.Bytes()), FixtureLoadOptions{
		Clock: clock, AllowExpired: true,
	}); err != nil {
		t.Fatalf("load explicitly allowed expired fixture: %v", err)
	}
}

func TestFixtureLoaderMigratesExplicitSchemasAndRejectsHostileInput(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	legacy := []byte(`{"schema_version":0,"legacy":true}`)
	migrator := FixtureMigratorFunc(func(payload json.RawMessage) (Fixture, error) {
		if !bytes.Contains(payload, []byte(`"legacy":true`)) {
			t.Fatalf("migration payload = %s", payload)
		}
		return Fixture{
			SchemaVersion: FixtureSchemaVersion,
			RecordedAt:    now,
			Interactions: []FixtureInteraction{{
				Request: FixtureRequest{
					Method: http.MethodGet, URL: "https://example.test/",
					BodySHA256: emptyFixtureBodyDigest,
				},
				Response: FixtureResponse{StatusCode: http.StatusNoContent},
			}},
		}, nil
	})
	loaded, err := ReadFixture(bytes.NewReader(legacy), FixtureLoadOptions{
		Clock:      &fixtureTestClock{now: now},
		Migrations: map[int]FixtureMigrator{0: migrator},
	})
	if err != nil || len(loaded.Interactions) != 1 {
		t.Fatalf("migrated fixture = %#v, %v", loaded, err)
	}

	valid, _ := json.Marshal(loaded)
	for _, test := range []struct {
		name    string
		payload []byte
		options FixtureLoadOptions
		want    error
		at      int
	}{
		{"empty", nil, FixtureLoadOptions{}, ErrInvalidFixture, -1},
		{"oversized", bytes.Repeat([]byte("x"), defaultMaximumFixtureFileBytes+1), FixtureLoadOptions{}, ErrInvalidFixture, -1},
		{"unknown schema", []byte(`{"schema_version":99}`), FixtureLoadOptions{}, ErrFixtureSchema, -1},
		{"typed nil migrator", legacy, FixtureLoadOptions{Migrations: map[int]FixtureMigrator{0: (*fixtureNilMigrator)(nil)}}, ErrInvalidFixture, -1},
		{"unknown field", append(append([]byte(nil), valid[:len(valid)-1]...), []byte(`,"unknown":true}`)...), FixtureLoadOptions{}, ErrInvalidFixture, -1},
		{"trailing document", append(append([]byte(nil), valid...), []byte(` {}`)...), FixtureLoadOptions{}, ErrInvalidFixture, -1},
		{"invalid current fixture", []byte(`{"schema_version":1,"interactions":[{}]}`), FixtureLoadOptions{}, ErrInvalidFixture, -1},
		{"invalid current interaction", []byte(`{"schema_version":1,"recorded_at":"2023-11-14T22:13:20Z","interactions":[{"request":{"method":"bad method","url":"https://example.test/"},"response":{"status_code":204}}]}`), FixtureLoadOptions{}, ErrInvalidFixture, 0},
		{"raw persisted request body", []byte(`{"schema_version":1,"recorded_at":"2023-11-14T22:13:20Z","interactions":[{"request":{"method":"POST","url":"https://example.test/","body":"c2VjcmV0"},"response":{"status_code":204}}]}`), FixtureLoadOptions{}, ErrInvalidFixture, 0},
		{"invalid current JSON type", []byte(`{"schema_version":1,"recorded_at":5}`), FixtureLoadOptions{}, ErrInvalidFixture, -1},
		{"invalid maximum", valid, FixtureLoadOptions{MaximumFileBytes: -1}, ErrInvalidFixture, -1},
		{"excessive maximum", valid, FixtureLoadOptions{MaximumFileBytes: maximumFixtureFileBytes + 1}, ErrInvalidFixture, -1},
		{"typed nil clock", valid, FixtureLoadOptions{Clock: (*fixtureNilClock)(nil)}, ErrInvalidFixture, -1},
		{"migration error", legacy, FixtureLoadOptions{Migrations: map[int]FixtureMigrator{0: FixtureMigratorFunc(func(json.RawMessage) (Fixture, error) { return Fixture{}, errors.New("migration failure") })}}, ErrInvalidFixture, -1},
		{"migration panic", legacy, FixtureLoadOptions{Migrations: map[int]FixtureMigrator{0: FixtureMigratorFunc(func(json.RawMessage) (Fixture, error) { panic("migration panic") })}}, ErrInvalidFixture, -1},
		{"expiry before recording", []byte(`{"schema_version":1,"recorded_at":"2023-11-14T22:13:20Z","expires_at":"2023-11-14T22:13:19Z"}`), FixtureLoadOptions{}, ErrInvalidFixture, -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ReadFixture(bytes.NewReader(test.payload), test.options)
			requireFixtureError(t, err, test.want, test.at, "load hostile input")
		})
	}
	_, err = ReadFixture(nil, FixtureLoadOptions{})
	requireFixtureError(t, err, ErrInvalidFixture, -1, "nil reader")
	readFailure := errors.New("reader failure")
	_, err = ReadFixture(fixtureFailureReader{err: readFailure}, FixtureLoadOptions{})
	requireFixtureError(t, err, readFailure, -1, "reader failure")
	if _, err := ReadFixture(bytes.NewReader(valid), FixtureLoadOptions{}); err != nil {
		t.Fatalf("default clock load = %v", err)
	}
	err = writeFixtureJSON(io.Discard, Fixture{})
	requireFixtureError(t, err, ErrInvalidFixture, -1, "invalid write metadata")
	invalidFixture := cloneFixture(loaded)
	invalidFixture.Interactions[0].Request.Method = "bad method"
	err = writeFixtureJSON(io.Discard, invalidFixture)
	requireFixtureError(t, err, ErrInvalidFixture, 0, "invalid fixture write")
	invalidFixture = cloneFixture(loaded)
	invalidFixture.ExpiresAt = invalidFixture.RecordedAt.Add(-time.Nanosecond)
	err = writeFixtureJSON(io.Discard, invalidFixture)
	requireFixtureError(t, err, ErrInvalidFixture, -1, "invalid fixture expiry write")
	err = writeFixtureJSON(nil, loaded)
	requireFixtureError(t, err, ErrInvalidFixture, -1, "nil helper writer")
	err = (*RecorderTransport)(nil).WriteFixture(io.Discard)
	requireFixtureError(t, err, ErrInvalidFixture, -1, "nil recorder")
	err = (&RecorderTransport{}).WriteFixture(nil)
	requireFixtureError(t, err, ErrInvalidFixture, -1, "nil writer")
	writerFailure := errors.New("writer failure")
	err = writeFixtureJSON(fixtureFailureWriter{err: writerFailure}, loaded)
	requireFixtureError(t, err, writerFailure, -1, "writer failure")
}

func TestFixtureFileMaximumBoundariesAndReadLimit(t *testing.T) {
	const expectedMaximumFixtureFileBytes int64 = 256 << 20

	for _, test := range []struct {
		configured int64
		want       int64
		valid      bool
	}{
		{0, defaultMaximumFixtureFileBytes, true},
		{1, 1, true},
		{expectedMaximumFixtureFileBytes, expectedMaximumFixtureFileBytes, true},
		{-1, 0, false},
		{expectedMaximumFixtureFileBytes + 1, 0, false},
	} {
		got, valid := fixtureFileMaximum(test.configured)
		if got != test.want || valid != test.valid {
			t.Fatalf("fixture maximum for %d = %d, %t; want %d, %t", test.configured, got, valid, test.want, test.valid)
		}
	}

	reader := &fixtureCountingReader{reader: bytes.NewReader(bytes.Repeat([]byte("x"), 33))}
	_, err := ReadFixture(reader, FixtureLoadOptions{MaximumFileBytes: 32})
	requireFixtureError(t, err, ErrInvalidFixture, -1, "bounded fixture read")
	if reader.bytesRead != 33 {
		t.Fatalf("bounded fixture read consumed %d bytes, want 33", reader.bytesRead)
	}

	fixture := Fixture{SchemaVersion: FixtureSchemaVersion, RecordedAt: time.Unix(1_700_000_000, 0).UTC()}
	payload, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal exact-bound fixture: %v", err)
	}
	if _, err := ReadFixture(bytes.NewReader(payload), FixtureLoadOptions{MaximumFileBytes: int64(len(payload))}); err != nil {
		t.Fatalf("read exact-bound fixture: %v", err)
	}
}

type fixtureNilMigrator struct{}

func (*fixtureNilMigrator) MigrateFixture(json.RawMessage) (Fixture, error) { return Fixture{}, nil }

type fixtureFailureWriter struct{ err error }

func (writer fixtureFailureWriter) Write([]byte) (int, error) { return 0, writer.err }

var _ io.Writer = fixtureFailureWriter{}

type fixtureFailureReader struct{ err error }

func (reader fixtureFailureReader) Read([]byte) (int, error) { return 0, reader.err }

type fixtureCountingReader struct {
	reader    io.Reader
	bytesRead int
}

func (reader *fixtureCountingReader) Read(payload []byte) (int, error) {
	count, err := reader.reader.Read(payload)
	reader.bytesRead += count
	return count, err
}

type fixtureNilClock struct{}

func (*fixtureNilClock) Now() time.Time                            { return time.Time{} }
func (*fixtureNilClock) Wait(context.Context, time.Duration) error { return nil }

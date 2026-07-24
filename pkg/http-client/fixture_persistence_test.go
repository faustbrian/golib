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
	clock.now = clock.now.Add(2 * time.Hour)
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
	}{
		{"empty", nil, FixtureLoadOptions{}, ErrInvalidFixture},
		{"oversized", bytes.Repeat([]byte("x"), defaultMaximumFixtureFileBytes+1), FixtureLoadOptions{}, ErrInvalidFixture},
		{"unknown schema", []byte(`{"schema_version":99}`), FixtureLoadOptions{}, ErrFixtureSchema},
		{"typed nil migrator", legacy, FixtureLoadOptions{Migrations: map[int]FixtureMigrator{0: (*fixtureNilMigrator)(nil)}}, ErrInvalidFixture},
		{"unknown field", append(append([]byte(nil), valid[:len(valid)-1]...), []byte(`,"unknown":true}`)...), FixtureLoadOptions{}, ErrInvalidFixture},
		{"trailing document", append(append([]byte(nil), valid...), []byte(` {}`)...), FixtureLoadOptions{}, ErrInvalidFixture},
		{"invalid current fixture", []byte(`{"schema_version":1,"interactions":[{}]}`), FixtureLoadOptions{}, ErrInvalidFixture},
		{"invalid current interaction", []byte(`{"schema_version":1,"recorded_at":"2023-11-14T22:13:20Z","interactions":[{"request":{"method":"bad method","url":"https://example.test/"},"response":{"status_code":204}}]}`), FixtureLoadOptions{}, ErrInvalidFixture},
		{"raw persisted request body", []byte(`{"schema_version":1,"recorded_at":"2023-11-14T22:13:20Z","interactions":[{"request":{"method":"POST","url":"https://example.test/","body":"c2VjcmV0"},"response":{"status_code":204}}]}`), FixtureLoadOptions{}, ErrInvalidFixture},
		{"invalid current JSON type", []byte(`{"schema_version":1,"recorded_at":5}`), FixtureLoadOptions{}, ErrInvalidFixture},
		{"invalid maximum", valid, FixtureLoadOptions{MaximumFileBytes: -1}, ErrInvalidFixture},
		{"excessive maximum", valid, FixtureLoadOptions{MaximumFileBytes: maximumFixtureBody*4 + 1}, ErrInvalidFixture},
		{"typed nil clock", valid, FixtureLoadOptions{Clock: (*fixtureNilClock)(nil)}, ErrInvalidFixture},
		{"migration error", legacy, FixtureLoadOptions{Migrations: map[int]FixtureMigrator{0: FixtureMigratorFunc(func(json.RawMessage) (Fixture, error) { return Fixture{}, errors.New("migration failure") })}}, ErrInvalidFixture},
		{"migration panic", legacy, FixtureLoadOptions{Migrations: map[int]FixtureMigrator{0: FixtureMigratorFunc(func(json.RawMessage) (Fixture, error) { panic("migration panic") })}}, ErrInvalidFixture},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ReadFixture(bytes.NewReader(test.payload), test.options); !errors.Is(err, test.want) {
				t.Fatalf("load error = %v, want %v", err, test.want)
			}
		})
	}
	if _, err := ReadFixture(nil, FixtureLoadOptions{}); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("nil reader error = %v", err)
	}
	readFailure := errors.New("reader failure")
	if _, err := ReadFixture(fixtureFailureReader{err: readFailure}, FixtureLoadOptions{}); !errors.Is(err, readFailure) {
		t.Fatalf("reader failure = %v", err)
	}
	if _, err := ReadFixture(bytes.NewReader(valid), FixtureLoadOptions{}); err != nil {
		t.Fatalf("default clock load = %v", err)
	}
	if err := writeFixtureJSON(io.Discard, Fixture{}); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("invalid write metadata = %v", err)
	}
	invalidFixture := cloneFixture(loaded)
	invalidFixture.Interactions[0].Request.Method = "bad method"
	if err := writeFixtureJSON(io.Discard, invalidFixture); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("invalid fixture write = %v", err)
	}
	if err := writeFixtureJSON(nil, loaded); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("nil helper writer = %v", err)
	}
	if err := (&RecorderTransport{}).WriteFixture(nil); !errors.Is(err, ErrInvalidFixture) {
		t.Fatalf("nil writer error = %v", err)
	}
	writerFailure := errors.New("writer failure")
	if err := writeFixtureJSON(fixtureFailureWriter{err: writerFailure}, loaded); !errors.Is(err, writerFailure) {
		t.Fatalf("writer failure = %v", err)
	}
}

type fixtureNilMigrator struct{}

func (*fixtureNilMigrator) MigrateFixture(json.RawMessage) (Fixture, error) { return Fixture{}, nil }

type fixtureFailureWriter struct{ err error }

func (writer fixtureFailureWriter) Write([]byte) (int, error) { return 0, writer.err }

var _ io.Writer = fixtureFailureWriter{}

type fixtureFailureReader struct{ err error }

func (reader fixtureFailureReader) Read([]byte) (int, error) { return 0, reader.err }

type fixtureNilClock struct{}

func (*fixtureNilClock) Now() time.Time                            { return time.Time{} }
func (*fixtureNilClock) Wait(context.Context, time.Duration) error { return nil }

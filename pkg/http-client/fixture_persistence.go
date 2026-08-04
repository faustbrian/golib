package httpclient

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	defaultMaximumFixtureFileBytes = 16 << 20
	maximumFixtureFileBytes        = maximumFixtureBody * 4
)

var (
	// ErrFixtureSchema indicates an unsupported schema without a migrator.
	ErrFixtureSchema = errors.New("unsupported HTTP fixture schema")
	// ErrFixtureExpired indicates that fixture expiry is at or before now.
	ErrFixtureExpired      = errors.New("HTTP fixture expired")
	emptyFixtureBodyDigest = func() string {
		digest := sha256.Sum256(nil)
		return hex.EncodeToString(digest[:])
	}()
)

// FixtureMigrator upgrades one explicitly supported raw schema to current.
type FixtureMigrator interface {
	MigrateFixture(json.RawMessage) (Fixture, error)
}

// FixtureMigratorFunc adapts a schema migration function.
type FixtureMigratorFunc func(json.RawMessage) (Fixture, error)

// MigrateFixture implements FixtureMigrator.
func (function FixtureMigratorFunc) MigrateFixture(payload json.RawMessage) (Fixture, error) {
	return function(payload)
}

// FixtureLoadOptions controls bounded compatibility and expiry checks.
type FixtureLoadOptions struct {
	MaximumFileBytes int64
	Clock            RetryClock
	AllowExpired     bool
	Migrations       map[int]FixtureMigrator
}

// WriteFixture writes a deterministic sanitized fixture JSON document.
func (recorder *RecorderTransport) WriteFixture(writer io.Writer) error {
	if recorder == nil {
		return fixturePersistenceError(ErrInvalidFixture)
	}
	if nilLike(writer) {
		return fixturePersistenceError(ErrInvalidFixture)
	}
	return writeFixtureJSON(writer, recorder.Fixture())
}

// ReadFixture loads one bounded strict JSON document and applies only an
// explicitly registered schema migration.
func ReadFixture(reader io.Reader, options FixtureLoadOptions) (Fixture, error) {
	if nilLike(reader) {
		return Fixture{}, fixturePersistenceError(ErrInvalidFixture)
	}
	maximum, ok := fixtureFileMaximum(options.MaximumFileBytes)
	if !ok {
		return Fixture{}, fixturePersistenceError(ErrInvalidFixture)
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return Fixture{}, fixturePersistenceError(errors.Join(ErrInvalidFixture, err))
	}
	if len(payload) == 0 {
		return Fixture{}, fixturePersistenceError(ErrInvalidFixture)
	}
	if int64(len(payload)) > maximum {
		return Fixture{}, fixturePersistenceError(ErrInvalidFixture)
	}
	var envelope struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return Fixture{}, fixturePersistenceError(ErrInvalidFixture)
	}
	var fixture Fixture
	if envelope.SchemaVersion == FixtureSchemaVersion {
		fixture, err = decodeCurrentFixture(payload)
	} else {
		migrator, ok := options.Migrations[envelope.SchemaVersion]
		if !ok {
			return Fixture{}, fixturePersistenceError(ErrFixtureSchema)
		}
		if nilLike(migrator) {
			return Fixture{}, fixturePersistenceError(ErrInvalidFixture)
		}
		fixture, err = invokeFixtureMigrator(migrator, payload)
	}
	if err != nil {
		return Fixture{}, fixturePersistenceError(errors.Join(ErrInvalidFixture, err))
	}
	if !validFixtureMetadata(fixture) {
		return Fixture{}, fixturePersistenceError(ErrInvalidFixture)
	}
	clock := options.Clock
	if clock == nil {
		clock = systemRetryClock{}
	} else if nilLike(clock) {
		return Fixture{}, fixturePersistenceError(ErrInvalidFixture)
	}
	if !options.AllowExpired && !fixture.ExpiresAt.IsZero() &&
		!clock.Now().Before(fixture.ExpiresAt) {
		return Fixture{}, fixturePersistenceError(ErrFixtureExpired)
	}
	for index, interaction := range fixture.Interactions {
		if len(interaction.Request.Body) > 0 {
			return Fixture{}, &FixtureError{Interaction: index, Cause: ErrInvalidFixture}
		}
	}
	replay, err := NewReplayTransport(fixture, ReplayOptions{})
	if err != nil {
		return Fixture{}, err
	}
	validated := cloneFixture(fixture)
	validated.Interactions = make([]FixtureInteraction, len(replay.interactions))
	copy(validated.Interactions, replay.interactions)
	validated.Match.Headers = append([]string(nil), replay.matchHeaders...)
	validated.Match.RedactedQueryParameters = sortedFixtureQueryNames(replay.redactedQuery)
	return cloneFixture(validated), nil
}

func decodeCurrentFixture(payload []byte) (Fixture, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var fixture Fixture
	if err := decoder.Decode(&fixture); err != nil {
		return Fixture{}, err
	}
	return fixture, nil
}

func invokeFixtureMigrator(
	migrator FixtureMigrator,
	payload json.RawMessage,
) (fixture Fixture, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			fixture = Fixture{}
			err = ErrInvalidFixture
		}
	}()
	return migrator.MigrateFixture(append(json.RawMessage(nil), payload...))
}

func writeFixtureJSON(writer io.Writer, fixture Fixture) error {
	if nilLike(writer) {
		return fixturePersistenceError(ErrInvalidFixture)
	}
	if !validFixtureMetadata(fixture) {
		return fixturePersistenceError(ErrInvalidFixture)
	}
	if _, err := NewReplayTransport(fixture, ReplayOptions{}); err != nil {
		return err
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(true)
	if err := encoder.Encode(cloneFixture(fixture)); err != nil {
		return fixturePersistenceError(fmt.Errorf("%w: %w", ErrInvalidFixture, err))
	}
	return nil
}

func fixtureFileMaximum(configured int64) (int64, bool) {
	if configured == 0 {
		return defaultMaximumFixtureFileBytes, true
	}
	if configured < 1 {
		return 0, false
	}
	if configured > maximumFixtureFileBytes {
		return 0, false
	}
	return configured, true
}

func validFixtureMetadata(fixture Fixture) bool {
	if fixture.SchemaVersion != FixtureSchemaVersion {
		return false
	}
	if fixture.RecordedAt.IsZero() {
		return false
	}
	if !fixture.ExpiresAt.IsZero() && fixture.ExpiresAt.Before(fixture.RecordedAt) {
		return false
	}
	return true
}

func fixturePersistenceError(cause error) error {
	return &FixtureError{Interaction: -1, Cause: cause}
}

package schemaregistry_test

import (
	"context"
	"errors"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

type valueCodecStub struct {
	encode func(context.Context, schemaregistry.Schema, any) ([]byte, error)
	decode func(context.Context, schemaregistry.Schema, []byte, any) error
}

func (codec valueCodecStub) Encode(ctx context.Context, schema schemaregistry.Schema, value any) ([]byte, error) {
	return codec.encode(ctx, schema, value)
}

func (codec valueCodecStub) Decode(ctx context.Context, schema schemaregistry.Schema, payload []byte, target any) error {
	return codec.decode(ctx, schema, payload, target)
}

type framerStub struct{}

func (framerStub) Frame(_ context.Context, id schemaregistry.ProviderID, payload []byte) ([]byte, error) {
	return append([]byte(id.Value+":"), payload...), nil
}

func (framerStub) Unframe(_ context.Context, framed []byte) (schemaregistry.ProviderID, []byte, error) {
	if len(framed) < 2 || framed[1] != ':' {
		return schemaregistry.ProviderID{}, nil, errors.New("bad frame")
	}
	return schemaregistry.ProviderID{Provider: "test", Value: string(framed[:1])}, append([]byte(nil), framed[2:]...), nil
}

func TestCodecIntegrationKeepsFramingResolutionAndBusinessCodecExplicit(t *testing.T) {
	t.Parallel()

	schema := compileAvroString(t)
	codec := valueCodecStub{
		encode: func(_ context.Context, got schemaregistry.Schema, value any) ([]byte, error) {
			if got.Fingerprint() != schema.Fingerprint() || value != "value" {
				t.Fatalf("Encode() inputs = (%s, %v)", got.Fingerprint(), value)
			}
			return []byte("payload"), nil
		},
		decode: func(_ context.Context, got schemaregistry.Schema, payload []byte, target any) error {
			if got.Fingerprint() != schema.Fingerprint() || string(payload) != "payload" {
				t.Fatalf("Decode() inputs = (%s, %q)", got.Fingerprint(), payload)
			}
			pointer := target.(*string)
			*pointer = "decoded"
			return nil
		},
	}
	integration, err := schemaregistry.NewCodecIntegration(codec, framerStub{}, schemaregistry.CodecLimits{
		MaxPayloadBytes: 16,
		MaxFrameBytes:   32,
	})
	if err != nil {
		t.Fatalf("NewCodecIntegration() error = %v", err)
	}

	framed, err := integration.Encode(
		context.Background(),
		schema,
		schemaregistry.ProviderID{Provider: "test", Value: "7"},
		"value",
	)
	if err != nil || string(framed) != "7:payload" {
		t.Fatalf("Encode() = (%q, %v)", framed, err)
	}
	message, err := integration.Parse(context.Background(), framed)
	if err != nil || message.ID.Value != "7" || string(message.Payload) != "payload" {
		t.Fatalf("Parse() = (%+v, %v)", message, err)
	}
	framed[2] = 'x'
	if string(message.Payload) != "payload" {
		t.Fatal("Parse() payload aliases framed input")
	}

	var target string
	if err := integration.Decode(context.Background(), schema, message, &target); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if target != "decoded" {
		t.Fatalf("decoded target = %q", target)
	}
}

func TestCodecIntegrationRejectsOversizedPayloads(t *testing.T) {
	t.Parallel()

	schema := compileAvroString(t)
	integration, err := schemaregistry.NewCodecIntegration(
		valueCodecStub{
			encode: func(context.Context, schemaregistry.Schema, any) ([]byte, error) {
				return []byte("toolarge"), nil
			},
			decode: func(context.Context, schemaregistry.Schema, []byte, any) error { return nil },
		},
		framerStub{},
		schemaregistry.CodecLimits{MaxPayloadBytes: 4, MaxFrameBytes: 16},
	)
	if err != nil {
		t.Fatalf("NewCodecIntegration() error = %v", err)
	}

	_, err = integration.Encode(
		context.Background(),
		schema,
		schemaregistry.ProviderID{Provider: "test", Value: "7"},
		"value",
	)
	if !errors.Is(err, schemaregistry.ErrLimitExceeded) {
		t.Fatalf("Encode() error = %v, want ErrLimitExceeded", err)
	}
	_, err = integration.Parse(context.Background(), []byte("7:large"))
	if !errors.Is(err, schemaregistry.ErrLimitExceeded) {
		t.Fatalf("Parse() error = %v, want ErrLimitExceeded", err)
	}
}
